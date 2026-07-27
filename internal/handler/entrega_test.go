package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/pdf"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerEntrega records what the platform asked the ledger to do, reproducing
// the behaviour observed against the live API: creation ignores any
// destination, transfers land as action=TRANSFER, and our own metadata fields
// survive while `action` does not.
type ledgerEntrega struct {
	creado       bool
	transferidoA string
	metadata     map[string]interface{}
	fallaUpdate  bool
}

func (l *ledgerEntrega) mux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			l.creado = true
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true, "msg": "Asset created", "id": "asset-emitido", "cost": 0.02,
			})
		case http.MethodPut:
			if l.fallaUpdate {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": "rechazado"})
				return
			}
			var req struct {
				To       string                 `json:"to"`
				Metadata map[string]interface{} `json:"metadata"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			l.transferidoA = req.To
			l.metadata = req.Metadata
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been transferred"})
		}
	})
	return mux
}

// usuariosFalsos resolves beneficiaries by NIF, as the user store does.
type usuariosFalsos map[string]*auth.User

func (u usuariosFalsos) GetUserByNIF(nif string) (*auth.User, error) {
	if user, ok := u[nif]; ok {
		return user, nil
	}
	return nil, auth.ErrUserNotFound
}

func emitirCon(t *testing.T, ph *PagareHandler, to string) map[string]interface{} {
	t.Helper()
	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true, "importe": 1500.0, "moneda": "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-06-30"},
				"localidad_pago":    "Alicante",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "nif": "12345678Z"},
				"localidad_emision": "Madrid", "fecha_emision": "2026-04-10",
				"firmante": map[string]interface{}{
					"nombre": "Maria", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "C/2", "localidad": "Madrid", "codigo_postal": "28001", "pais": "ES",
					},
				},
			},
		},
		"from": map[string]string{"pub": "pub-emisor", "pvt": "pvt-emisor"},
	}
	if to != "" {
		payload["to"] = to
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestEntrega_TransfiereElControlAlBeneficiario(t *testing.T) {
	l := &ledgerEntrega{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	resp := emitirCon(t, ph, "pub-beneficiario")

	assert.True(t, l.creado)
	assert.Equal(t, "pub-beneficiario", l.transferidoA,
		"el pagaré debe quedar en poder del beneficiario, no del emisor")

	entrega := resp["entrega"].(map[string]interface{})
	assert.True(t, entrega["entregado"].(bool))
	assert.Equal(t, "pub-beneficiario", entrega["a"])
}

// The ledger overwrites `action`, so the handover has to be recognisable by a
// field of our own or it will pass for an endoso.
func TestEntrega_SeMarcaParaNoConfundirseConEndoso(t *testing.T) {
	l := &ledgerEntrega{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	emitirCon(t, ph, "pub-beneficiario")

	require.NotNil(t, l.metadata)
	assert.Equal(t, TipoOperacionEntrega, l.metadata["tipo_operacion"])
}

func TestEntrega_ResuelveElBeneficiarioPorNIF(t *testing.T) {
	l := &ledgerEntrega{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	ph.SetBeneficiarios(usuariosFalsos{
		"12345678Z": {ID: "u1", Nombre: "Ana", NIF: "12345678Z", PubKeys: []string{"pub-de-ana"}},
	})

	resp := emitirCon(t, ph, "") // sin clave explícita: debe deducirla del NIF
	assert.Equal(t, "pub-de-ana", l.transferidoA)
	assert.True(t, resp["entrega"].(map[string]interface{})["entregado"].(bool))
}

// A beneficiario with no identity on the platform cannot receive control. The
// pagaré is still validly issued — a signed title not yet handed over — but it
// must not be reported as delivered.
func TestEntrega_BeneficiarioSinIdentidadQuedaPendiente(t *testing.T) {
	l := &ledgerEntrega{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	resp := emitirCon(t, ph, "")

	assert.True(t, l.creado, "el pagaré se emite igualmente")
	assert.Empty(t, l.transferidoA, "pero no se transfiere a nadie")

	entrega := resp["entrega"].(map[string]interface{})
	assert.False(t, entrega["entregado"].(bool))
	assert.Contains(t, resp["msg"], "pendiente de entrega")
}

func TestEntrega_FalloDeRedNoSeReportaComoEntregado(t *testing.T) {
	l := &ledgerEntrega{fallaUpdate: true}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	resp := emitirCon(t, ph, "pub-beneficiario")

	entrega := resp["entrega"].(map[string]interface{})
	assert.False(t, entrega["entregado"].(bool))
	assert.Contains(t, entrega["msg"], "rechazó la entrega")
	assert.Contains(t, resp["msg"], "pendiente de entrega")
	assert.NotEmpty(t, resp["id"], "el ID debe volver para poder reintentar")
}

func TestEntrega_BeneficiarioQueEsElPropioEmisorNoSeTransfiere(t *testing.T) {
	l := &ledgerEntrega{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)
	resp := emitirCon(t, ph, "pub-emisor")

	assert.Empty(t, l.transferidoA)
	assert.False(t, resp["entrega"].(map[string]interface{})["entregado"].(bool))
}

// The state and the endorsement chain both read TRANSFER entries; neither may
// mistake the handover for an endoso.
func TestEntrega_NoCuentaComoEndoso(t *testing.T) {
	historia := map[string]interface{}{
		"ok": true,
		"history": []interface{}{
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "CREATE", "from": "pub-emisor",
			}},
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "TRANSFER", "from": "pub-emisor", "to": "pub-beneficiario",
				"tipo_operacion": TipoOperacionEntrega,
			}},
		},
	}
	body, _ := json.Marshal(historia)

	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewConsultaHandler(client)

	estado := h.resolveEstado("asset-emitido", map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO", "ENDOSO": "ENDOSADO",
	})
	assert.NotEqual(t, "ENDOSADO", estado, "una entrega no endosa el pagaré")

	endosos, firmantePub := h.parseHistory(body)
	assert.Empty(t, endosos, "la entrega no abre la cadena de endosos")
	assert.Equal(t, "pub-emisor", firmantePub)
}

// An actual endoso after the handover must still be picked up.
func TestEntrega_ElEndosoPosteriorSiCuenta(t *testing.T) {
	historia := map[string]interface{}{
		"ok": true,
		"history": []interface{}{
			map[string]interface{}{"metadata": map[string]interface{}{"action": "CREATE", "from": "pub-emisor"}},
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "TRANSFER", "from": "pub-emisor", "to": "pub-beneficiario",
				"tipo_operacion": TipoOperacionEntrega,
			}},
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "TRANSFER", "from": "pub-beneficiario", "to": "pub-tercero",
				"tipo_endoso": "en_propiedad", "fecha": "2026-08-01",
			}},
		},
	}
	body, _ := json.Marshal(historia)

	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewConsultaHandler(client)

	estado := h.resolveEstado("asset-emitido", map[string]string{"ENDOSO": "ENDOSADO"})
	assert.Equal(t, "ENDOSADO", estado)

	endosos, _ := h.parseHistory(body)
	require.Len(t, endosos, 1)
	assert.Equal(t, pdf.Endoso{
		Tipo: "en_propiedad", EndosantePub: "pub-beneficiario", Fecha: "2026-08-01",
		Endosatario: "clave pub-terc…ercero",
	}.Tipo, endosos[0].Tipo)
	assert.Equal(t, "pub-beneficiario", endosos[0].EndosantePub)
}

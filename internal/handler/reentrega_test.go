package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerReentrega serves a pagaré and a history the platform reads to decide
// whether the handover is still pending.
type ledgerReentrega struct {
	historia     []interface{}
	transferidoA string
	metadata     map[string]interface{}
}

func (l *ledgerReentrega) mux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "history": l.historia})
	})
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"asset": map[string]interface{}{
					"id": "asset-pendiente",
					"data": map[string]interface{}{
						"type":         "pagare_electronico",
						"beneficiario": map[string]interface{}{"nombre": "Ana", "nif": "12345678Z"},
					},
				},
			})
		case http.MethodPut:
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

func soloEmitido() []interface{} {
	return []interface{}{
		map[string]interface{}{"metadata": map[string]interface{}{"action": "CREATE", "from": "pub-emisor"}},
	}
}

func emitidoYEntregado() []interface{} {
	return append(soloEmitido(), map[string]interface{}{"metadata": map[string]interface{}{
		"action": "TRANSFER", "from": "pub-emisor", "to": "pub-beneficiario",
		"tipo_operacion": TipoOperacionEntrega,
	}})
}

func pedirEntrega(t *testing.T, ph *PagareHandler, cuerpo map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(cuerpo)
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/entrega", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Entregar(w, withPrincipal(req))
	return w
}

func TestReentrega_CompletaLaEntregaPendiente(t *testing.T) {
	l := &ledgerReentrega{historia: soloEmitido()}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, crypto.NewService(client), clavesDePrueba())
	w := pedirEntrega(t, ph, map[string]interface{}{"id": "asset-pendiente", "to": "pub-beneficiario"})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "pub-beneficiario", l.transferidoA)
	assert.Equal(t, TipoOperacionEntrega, l.metadata["tipo_operacion"],
		"la entrega tardía sigue siendo una entrega, no un endoso")
}

// The destination comes from the NIF on the title when none is supplied, which
// is what makes the retry work once the beneficiario finally registers.
func TestReentrega_ResuelveElDestinoPorElNIFDelTitulo(t *testing.T) {
	l := &ledgerReentrega{historia: soloEmitido()}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, crypto.NewService(client), clavesDePrueba())
	ph.SetBeneficiarios(usuariosFalsos{
		"12345678Z": {ID: "u1", Nombre: "Ana", NIF: "12345678Z", PubKeys: []string{"pub-de-ana"}},
	})

	w := pedirEntrega(t, ph, map[string]interface{}{"id": "asset-pendiente"})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "pub-de-ana", l.transferidoA)
}

// Once the title has moved, any further transmission is an endoso or a cesión,
// each with its own régimen. Calling it an entrega would let a holder transfer
// without assuming the liability the chosen route carries.
func TestReentrega_NoSeEntregaDosVeces(t *testing.T) {
	l := &ledgerReentrega{historia: emitidoYEntregado()}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, crypto.NewService(client), clavesDePrueba())
	w := pedirEntrega(t, ph, map[string]interface{}{"id": "asset-pendiente", "to": "otra-clave"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, l.transferidoA, "no debe transferirse")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["msg"], "ya fue entregado")
}

func TestReentrega_SinDestinoConocidoSiguePendiente(t *testing.T) {
	l := &ledgerReentrega{historia: soloEmitido()}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, crypto.NewService(client), clavesDePrueba())
	w := pedirEntrega(t, ph, map[string]interface{}{"id": "asset-pendiente"})

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, l.transferidoA)
}

// A pagaré that never moved shows as pending so the issuer can see there is
// something to finish; one already handed over does not.
func TestReentrega_ElEstadoDistingueLoPendiente(t *testing.T) {
	casos := []struct {
		nombre   string
		historia []interface{}
		esperado string
	}{
		{"emitido sin entregar", soloEmitido(), "PENDIENTE_ENTREGA"},
		{"emitido y entregado", emitidoYEntregado(), ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			l := &ledgerReentrega{historia: c.historia}
			client, server := newTestBCFClient(l.mux(t))
			defer server.Close()

			h := NewConsultaHandler(client)
			estado := h.resolveEstado("asset-pendiente", map[string]string{"ENDOSO": "ENDOSADO"})
			assert.Equal(t, c.esperado, estado)
		})
	}
}

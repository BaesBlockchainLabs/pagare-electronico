package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ledgerCertificado sirve un pagaré con una vida completa: emisión, entrega,
// endoso y cesión, que es lo que el certificado tiene que saber distinguir.
type ledgerCertificado struct{ dueño string }

func (l *ledgerCertificado) historia() []interface{} {
	return []interface{}{
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "CREATE", "from": "clave-emisor", "updated_at": 1785156671724.0,
		}},
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "TRANSFER", "from": "clave-emisor", "to": "clave-tenedor",
			"tipo_operacion": TipoOperacionEntrega, "fecha": "2026-07-27T10:00:00Z",
		}},
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "TRANSFER", "from": "clave-tenedor", "to": "clave-endosatario",
			"tipo_endoso": "en_garantia", "clausula": "sin_gastos", "fecha": "2026-08-01T10:00:00Z",
			"endosatario": map[string]interface{}{"nombre": "Bea", "apellido": "Soler", "nif": "11111111H"},
		}},
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "TRANSFER", "from": "clave-endosatario", "to": "clave-cesionario",
			"tipo_operacion": TipoOperacionCesion, "fecha": "2026-08-15T10:00:00Z",
			"cesionario": map[string]interface{}{"nombre": "Carlos", "nif": "22222222J"},
		}},
	}
}

func (l *ledgerCertificado) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "asset": map[string]interface{}{"id": "asset-1", "data": datosCompletos()},
		})
	})
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "history": l.historia()})
	})
	mux.HandleFunc("/asset/owners", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "owners": []map[string]interface{}{{"pub": l.dueño}},
		})
	})
	return mux
}

func pedirCertificado(t *testing.T, principal *auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	l := &ledgerCertificado{dueño: "clave-cesionario"}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	h := NewConsultaHandler(client)
	h.SetCrypto(crypto.NewService(client))
	h.SetCertificador(Certificador{
		Nombre: "Ana Ruiz", Cargo: "Directora", Entidad: "BlockchainFUE",
	})

	req := httptest.NewRequest(http.MethodGet, "/certificado?id=asset-1&network=test", nil)
	if principal != nil {
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), principal))
	}
	w := httptest.NewRecorder()
	h.DescargarCertificado(w, req)
	return w
}

func TestCertificado_SoloParaQuienEsParte(t *testing.T) {
	assert.Equal(t, http.StatusUnauthorized, pedirCertificado(t, nil).Code,
		"sin sesión no se expide")

	ajeno := pedirCertificado(t, &auth.Principal{UserID: "u9", PubKeys: []string{"otra-clave"}})
	assert.Equal(t, http.StatusForbidden, ajeno.Code, "un tercero no es parte")
	assert.Contains(t, ajeno.Body.String(), "parte de este pagaré")
}

func TestCertificado_SeExpideAlTitular(t *testing.T) {
	w := pedirCertificado(t, &auth.Principal{UserID: "u1", PubKeys: []string{"clave-cesionario"}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "certificado-")
	assert.Greater(t, w.Body.Len(), 5000, "un PDF con contenido")
	assert.Equal(t, "%PDF", w.Body.String()[:4])
}

// Quien endosó sigue respondiendo del pago (art. 18 LCCH), así que conserva
// interés en el título aunque ya no lo tenga.
func TestCertificado_ElQueLoTuvoAntesSigueSiendoParte(t *testing.T) {
	w := pedirCertificado(t, &auth.Principal{UserID: "u2", PubKeys: []string{"clave-tenedor"}})
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// El registro apunta toda transmisión como TRANSFER; el certificado tiene que
// contar qué fue cada una, porque cada una obliga distinto a quien transmitió.
func TestHistoricoCertificado_DistingueLasTransmisiones(t *testing.T) {
	l := &ledgerCertificado{}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	body, _ := json.Marshal(map[string]interface{}{"history": l.historia()})
	ops := NewConsultaHandler(client).historicoCertificado(body)
	require.Len(t, ops, 4)

	assert.Equal(t, "EMISION", ops[0].Tipo)
	assert.Equal(t, "2026-07-27", ops[0].Fecha, "la fecha sale del sello de la red")

	assert.Equal(t, "ENTREGA", ops[1].Tipo)

	assert.Equal(t, "ENDOSO", ops[2].Tipo)
	assert.Contains(t, ops[2].Articulos, "14-24")
	detalle := ops[2].Detalle
	assert.Contains(t, detalle[1], "garantía", "la clase de endoso debe explicarse")
	assert.Contains(t, detalle[2], "art. 56", "la cláusula, también")

	assert.Equal(t, "CESION", ops[3].Tipo)
	assert.Contains(t, ops[3].Articulos, "347-348")
	// Sin notificación al deudor la cesión no le es oponible: hay que decirlo.
	unido := ""
	for _, d := range ops[3].Detalle {
		unido += d + " "
	}
	assert.Contains(t, unido, "1527")
	assert.Contains(t, unido, "1529", "el cedente no responde de la solvencia")
}

func TestHistoricoCertificado_ElCierreSeRelataUnaVez(t *testing.T) {
	historia := []interface{}{
		map[string]interface{}{"metadata": map[string]interface{}{"action": "CREATE", "from": "e"}},
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "UPDATE", "tipo_cierre": "PAGO", "fecha": "2026-09-01T10:00:00Z",
			"referencia": "transferencia 123",
		}},
		map[string]interface{}{"metadata": map[string]interface{}{
			"action": "BURN", "tipo_cierre": "PAGO", "fecha": "2026-09-01T10:00:00Z",
		}},
	}
	body, _ := json.Marshal(map[string]interface{}{"history": historia})

	client, server := newTestBCFClient(http.NewServeMux())
	defer server.Close()
	ops := NewConsultaHandler(client).historicoCertificado(body)

	require.Len(t, ops, 2, "la quema acompaña al cierre, no es una operación aparte")
	assert.Equal(t, "PAGO", ops[1].Tipo)
	assert.Contains(t, ops[1].Detalle[0], "satisfecho")
}

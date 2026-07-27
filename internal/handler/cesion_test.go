package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ledgerCesion struct {
	transferidoA string
	metadata     map[string]interface{}
}

func (l *ledgerCesion) mux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
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
	})
	return mux
}

func ceder(t *testing.T, ph *PagareHandler, cuerpo map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(cuerpo)
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/cesion", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Ceder(w, withPrincipal(req))
	return w
}

func TestCesion_TransfiereElCredito(t *testing.T) {
	l := &ledgerCesion{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	w := ceder(t, NewPagareHandler(client, nil, clavesDePrueba()), map[string]interface{}{
		"id": "asset-1", "to": "pub-cesionario",
		"cesionario": map[string]interface{}{"nombre": "Bea", "nif": "11111111H"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "pub-cesionario", l.transferidoA)
}

// The move looks like an endoso on the ledger but carries different law, so it
// must be recognisable as its own operation.
func TestCesion_SeMarcaComoCesionYNoComoEndoso(t *testing.T) {
	l := &ledgerCesion{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()

	ceder(t, NewPagareHandler(client, nil, clavesDePrueba()), map[string]interface{}{
		"id": "asset-1", "to": "pub-cesionario",
	})

	require.NotNil(t, l.metadata)
	assert.Equal(t, TipoOperacionCesion, l.metadata["tipo_operacion"])
	assert.NotEqual(t, TipoOperacionEntrega, l.metadata["tipo_operacion"])
}

// Until the debtor is notified the assignment is not enforceable against them
// and payment to the cedente still discharges the debt (art. 1527 CC), so the
// answer has to say so when no notice was recorded.
func TestCesion_AvisaDeLaNotificacionAlDeudor(t *testing.T) {
	l := &ledgerCesion{}
	client, server := newTestBCFClient(l.mux(t))
	defer server.Close()
	ph := NewPagareHandler(client, nil, clavesDePrueba())

	sinNotificar := ceder(t, ph, map[string]interface{}{"id": "asset-1", "to": "pub-cesionario"})
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(sinNotificar.Body.Bytes(), &resp))
	assert.Contains(t, resp["msg"], "art. 1527 CC")

	conNotificacion := ceder(t, ph, map[string]interface{}{
		"id": "asset-1", "to": "pub-cesionario",
		"notificacion_fecha": "2026-08-01", "notificacion_medio": "burofax",
	})
	require.NoError(t, json.Unmarshal(conNotificacion.Body.Bytes(), &resp))
	assert.NotContains(t, resp["msg"], "art. 1527 CC")
	assert.Equal(t, "2026-08-01", l.metadata["notificacion_fecha"])
	assert.Equal(t, "burofax", l.metadata["notificacion_medio"])
}

func TestCesion_ExigeIdYCesionario(t *testing.T) {
	client, server := newTestBCFClient((&ledgerCesion{}).mux(t))
	defer server.Close()
	ph := NewPagareHandler(client, nil, clavesDePrueba())

	assert.Equal(t, http.StatusBadRequest,
		ceder(t, ph, map[string]interface{}{"to": "pub-cesionario"}).Code)
	assert.Equal(t, http.StatusBadRequest,
		ceder(t, ph, map[string]interface{}{"id": "asset-1"}).Code)
}

// A cesión is not an endoso: the cedente does not answer for the debtor's
// solvency (art. 1529 CC), so showing it in the chain of endosos would suggest
// a liability never assumed.
func TestCesion_NoEntraEnLaCadenaDeEndosos(t *testing.T) {
	historia := map[string]interface{}{
		"ok": true,
		"history": []interface{}{
			map[string]interface{}{"metadata": map[string]interface{}{"action": "CREATE", "from": "pub-emisor"}},
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "TRANSFER", "from": "pub-emisor", "to": "pub-beneficiario",
				"tipo_operacion": TipoOperacionEntrega,
			}},
			map[string]interface{}{"metadata": map[string]interface{}{
				"action": "TRANSFER", "from": "pub-beneficiario", "to": "pub-cesionario",
				"tipo_operacion": TipoOperacionCesion, "fecha": "2026-08-01",
			}},
		},
	}
	body, _ := json.Marshal(historia)

	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewConsultaHandler(client)

	endosos, _ := h.parseHistory(body)
	assert.Empty(t, endosos, "la cesión no pertenece a la cadena de endosos")

	estado := h.resolveEstado("asset-1", map[string]string{"ENDOSO": "ENDOSADO"})
	assert.Equal(t, "CEDIDO", estado, "un pagaré cedido no está endosado")
}

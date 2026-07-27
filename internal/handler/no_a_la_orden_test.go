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

// ledgerEndoso serves the lookup that precedes an endoso and records whether
// the transfer was ever attempted.
type ledgerEndoso struct {
	noALaOrden   bool
	fallaLectura bool
	transferido  bool
}

func (l *ledgerEndoso) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if l.fallaLectura {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"asset": map[string]interface{}{
					"id": "asset-1",
					"data": map[string]interface{}{
						"type": "pagare_electronico", "no_a_la_orden": l.noALaOrden,
					},
				},
			})
		case http.MethodPut:
			l.transferido = true
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been transferred"})
		}
	})
	return mux
}

func endosar(t *testing.T, ph *PagareHandler) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"id": "asset-1", "to": "pub-endosatario",
		"metadata": map[string]interface{}{"tipo_endoso": "en_propiedad"},
		"from":     map[string]string{"pub": "pub-tenedor", "pvt": "pvt-tenedor"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Endosar(w, withPrincipal(req))
	return w
}

func TestNoALaOrden_ImpideElEndoso(t *testing.T) {
	l := &ledgerEndoso{noALaOrden: true}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	w := endosar(t, NewPagareHandler(client, nil, nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, l.transferido, "no debe llegar a transferirse")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "art. 14 LCCH", resp["articulo_lcch"])
	assert.Contains(t, resp["msg"], "cesión ordinaria")
}

func TestNoALaOrden_ElPagareCorrienteSiSeEndosa(t *testing.T) {
	l := &ledgerEndoso{noALaOrden: false}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	w := endosar(t, NewPagareHandler(client, nil, nil))

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, l.transferido)
}

// Endorsing a non-endorsable title leaves a chain of holders on something that
// cannot circulate, which is far worse to unwind than a refusal the caller can
// retry. So an unreadable asset blocks the endoso rather than letting it pass.
func TestNoALaOrden_SiNoPuedeComprobarseNoSeEndosa(t *testing.T) {
	l := &ledgerEndoso{fallaLectura: true}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	w := endosar(t, NewPagareHandler(client, nil, nil))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.False(t, l.transferido)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["msg"], "no se pudo")
}

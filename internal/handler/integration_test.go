package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"

	"github.com/stretchr/testify/assert"
)

func newTestBCFClient(handler http.Handler) (*bcfclient.Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	client := bcfclient.NewTestClient(server.URL, "test", "test", server.Client())
	return client, server
}

// withPrincipal attaches an authenticated principal to the request context, as
// the auth middleware would do for a logged-in user.
func withPrincipal(req *http.Request) *http.Request {
	// Admin so ownership filtering in consulta handlers doesn't hide test assets.
	p := &auth.Principal{UserID: "u-test", Username: "tester", Role: auth.RoleAdmin}
	return req.WithContext(auth.ContextWithPrincipal(req.Context(), p))
}

func TestIdentidadHandler_GenerateKeypair(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/keypair", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "pub": "pubkey123", "pvt": "pvtkey456"})
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewIdentidadHandler(client)

	body, _ := json.Marshal(map[string]string{"seed": "my-seed", "pin": "1234"})
	req := httptest.NewRequest(http.MethodPost, "/keypair", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.GenerateKeypair(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestIdentidadHandler_GenerateKeypair_MissingFields(t *testing.T) {
	client, _ := newTestBCFClient(nil)

	h := NewIdentidadHandler(client)

	body, _ := json.Marshal(map[string]string{"seed": ""})
	req := httptest.NewRequest(http.MethodPost, "/keypair", bytes.NewReader(body))
	w := httptest.NewRecorder()

	h.GenerateKeypair(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestIdentidadHandler_GetApplicationKeypair(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/keypair/application", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "pub": "apppub", "pvt": "apppvt"})
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewIdentidadHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/keypair/application", nil)
	w := httptest.NewRecorder()

	h.GetApplicationKeypair(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConsultaHandler_GetHistorico(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"history": []map[string]interface{}{
				{"id": "abc", "metadata": map[string]interface{}{"action": "CREATE"}},
			},
		})
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	h := NewConsultaHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/historico?id=abc", nil)
	w := httptest.NewRecorder()

	h.GetHistorico(w, withPrincipal(req))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestConsultaHandler_GetHistorico_MissingID(t *testing.T) {
	client, _ := newTestBCFClient(nil)

	h := NewConsultaHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/historico", nil)
	w := httptest.NewRecorder()

	h.GetHistorico(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestConsultaHandler_GetPublicAsset_MissingParams(t *testing.T) {
	client, _ := newTestBCFClient(nil)

	h := NewConsultaHandler(client)
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	w := httptest.NewRecorder()

	h.GetPublicAsset(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPagareEmitir_WithBCFServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true, "msg": "Asset created", "id": "testasset123", "cost": 0.05,
			})
		}
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion":      "PAGARÉ",
				"promesa_pago":      true,
				"importe":           2000.0,
				"moneda":            "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-06-30"},
				"localidad_pago":    "Valencia",
				"beneficiario":      map[string]interface{}{"nombre": "Pedro", "nif": "12345678Z"},
				"localidad_emision": "Madrid",
				"fecha_emision":     "2026-04-10",
				"firmante": map[string]interface{}{
					"nombre": "Maria", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{"direccion": "C/2", "localidad": "Madrid", "codigo_postal": "28001", "pais": "ES"},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph.Emitir(w, withPrincipal(req))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.True(t, resp["ok"].(bool))
	assert.Equal(t, "testasset123", resp["id"])
}

func TestPagareEndosar_WithBCFServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been transferred"})
		}
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)

	payload := map[string]interface{}{
		"id": "testasset123",
		"to": "newownerpubkey",
		"metadata": map[string]interface{}{
			"tipo_endoso": "en_propiedad",
			"endosatario": map[string]interface{}{"nombre": "Bea", "nif": "11111111H"},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph.Endosar(w, withPrincipal(req))

	if !assert.Equal(t, http.StatusOK, w.Code) {
		t.Logf("Response: %s", w.Body.String())
	}
}

func TestPagarePagarAnular_WithBCFServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been burnt"})
		}
		if r.Method == http.MethodPut {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Updated"})
		}
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)

	payload := map[string]interface{}{
		"id": "testasset123",
		"metadata": map[string]interface{}{
			"action": "PAGO",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodDelete, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph.PagarAnular(w, withPrincipal(req))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPagareEndoso_EnBlanco_WithBCFServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been transferred"})
		}
	})

	client, server := newTestBCFClient(mux)
	defer server.Close()

	ph := NewPagareHandler(client, nil, nil)

	payload := map[string]interface{}{
		"id": "test123",
		"to": "somekey",
		"metadata": map[string]interface{}{
			"tipo_endoso": "en_blanco",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph.Endosar(w, withPrincipal(req))

	assert.Equal(t, http.StatusOK, w.Code)
}

package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/crypto"

	"github.com/stretchr/testify/assert"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h := New()
	h.Health(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.True(t, resp["ok"].(bool))
	assert.Equal(t, "Pagaré Electrónico API - Running", resp["message"])
}

func TestHomeEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	h := New()
	h.Home(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "Pagaré Electrónico", resp["Title"])
}

func TestPagareEmitir_BadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader([]byte("invalid")))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.Emitir(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.False(t, resp["ok"].(bool))
	assert.Equal(t, "Body JSON inválido", resp["msg"])
}

func TestPagareEmitir_MissingRequiredFields(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"type": "pagare_electronico",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.Emitir(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.False(t, resp["ok"].(bool))
	assert.Equal(t, "Validación LCCH fallida", resp["msg"])
}

func TestPagareEndosar_BadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader([]byte("invalid")))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.Endosar(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPagareEndosar_MissingID(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"to": "somekey",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.Endosar(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp["msg"], "id es obligatorio")
}

func TestPagareEndosar_MissingTo(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"id": "abc123",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.Endosar(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp["msg"], "to")
}

func TestPagarePagarAnular_BadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/api/pagares", bytes.NewReader([]byte("invalid")))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.PagarAnular(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPagarePagarAnular_MissingID(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{"action": "PAGO"},
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.PagarAnular(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp["msg"], "id es obligatorio")
}

func TestPagarePagarAnular_MissingAction(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"id": "abc123",
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.PagarAnular(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp["msg"], "action")
}

func TestPagarePagarAnular_InvalidAction(t *testing.T) {
	body, _ := json.Marshal(map[string]interface{}{
		"id":       "abc123",
		"metadata": map[string]interface{}{"action": "INVALIDO"},
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(nil, nil, nil)
	ph.PagarAnular(w, withPrincipal(req))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPagareEmitir_ValidPayload_Structure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "id": "test", "cost": 0.01})
	})
	client, server := newTestBCFClient(conFirma(mux))
	defer server.Close()

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion":      "PAGARÉ",
				"promesa_pago":      true,
				"importe":           1000.0,
				"moneda":            "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-01-01"},
				"localidad_pago":    "Madrid",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "nif": "12345678Z"},
				"localidad_emision": "Barcelona",
				"fecha_emision":     "2026-04-10",
				"firmante": map[string]interface{}{
					"nombre": "Carlos", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{"direccion": "C/1", "localidad": "BCN", "codigo_postal": "08001", "pais": "ES"},
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ph := NewPagareHandler(client, crypto.NewService(client), clavesDePrueba())
	ph.Emitir(w, withPrincipal(req))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	assert.True(t, resp["ok"].(bool))
	assert.Equal(t, "test", resp["id"])
}

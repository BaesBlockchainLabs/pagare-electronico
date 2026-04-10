package handler

import (
	"encoding/json"
	"net/http"

	"pagare/internal/bcfclient"
	"pagare/internal/models"
)

type IdentidadHandler struct {
	client *bcfclient.Client
}

func NewIdentidadHandler(client *bcfclient.Client) *IdentidadHandler {
	return &IdentidadHandler{client: client}
}

func (h *IdentidadHandler) GenerateKeypair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seed string `json:"seed"`
		Pin  string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Body inválido"})
		return
	}
	if req.Seed == "" || req.Pin == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "seed y pin son obligatorios"})
		return
	}

	body, status, err := h.client.GenerateKeypair(req.Seed, req.Pin)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *IdentidadHandler) GetApplicationKeypair(w http.ResponseWriter, r *http.Request) {
	body, status, err := h.client.GetApplicationKeypair()
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *IdentidadHandler) AddPubKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pub string `json:"pub"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Body inválido"})
		return
	}
	if req.Pub == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "pub es obligatorio"})
		return
	}

	body, status, err := h.client.AddPubKey(req.Pub)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *IdentidadHandler) GenerateDID(w http.ResponseWriter, r *http.Request) {
	var req models.DIDGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Body inválido"})
		return
	}

	body, status, err := h.client.GenerateDID(req)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func WriteRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

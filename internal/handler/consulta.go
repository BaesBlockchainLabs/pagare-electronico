package handler

import (
	"encoding/json"
	"net/http"

	"pagare/internal/bcfclient"
)

type ConsultaHandler struct {
	client *bcfclient.Client
}

func NewConsultaHandler(client *bcfclient.Client) *ConsultaHandler {
	return &ConsultaHandler{client: client}
}

func (h *ConsultaHandler) ListPagares(w http.ResponseWriter, r *http.Request) {
	query := map[string]interface{}{
		"data": map[string]string{"type": "pagare_electronico"},
	}
	if q := r.URL.Query().Get("query"); q != "" {
		var customQuery map[string]interface{}
		if err := json.Unmarshal([]byte(q), &customQuery); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Query JSON inválido"})
			return
		}
		query = customQuery
	}

	body, status, err := h.client.GetAsset(query)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetPagare(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	body, status, err := h.client.GetAsset(map[string]string{"id": id})
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetHistorico(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	body, status, err := h.client.GetAssetHistory(id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetPropietario(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}

	body, status, err := h.client.GetAssetOwners(id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *ConsultaHandler) GetPublicAsset(w http.ResponseWriter, r *http.Request) {
	network := r.URL.Query().Get("network")
	id := r.URL.Query().Get("id")
	if network == "" || id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "network e id son obligatorios"})
		return
	}

	body, status, err := h.client.GetPublicAsset(network, id)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

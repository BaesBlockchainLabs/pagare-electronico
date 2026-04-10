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
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	assets, _ := raw["assets"].([]interface{})
	actionToEstado := map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
	}

	for _, a := range assets {
		asset, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		assetID, _ := asset["id"].(string)
		estado := h.resolveEstado(assetID, actionToEstado)
		if estado == "" {
			continue
		}
		data, _ := asset["data"].(map[string]interface{})
		if data == nil {
			data = make(map[string]interface{})
		}
		data["estado"] = estado
		asset["data"] = data
	}

	WriteJSON(w, status, raw)
}

func (h *ConsultaHandler) resolveEstado(assetID string, actionMap map[string]string) string {
	histBody, histStatus, err := h.client.GetAssetHistory(assetID)
	if err != nil || histStatus != 200 {
		return ""
	}
	var histRaw map[string]interface{}
	if err := json.Unmarshal(histBody, &histRaw); err != nil {
		return ""
	}
	history, _ := histRaw["history"].([]interface{})
	for i := len(history) - 1; i >= 0; i-- {
		entry, _ := history[i].(map[string]interface{})
		metadata, _ := entry["metadata"].(map[string]interface{})
		if metadata == nil {
			continue
		}
		if tipo, ok := metadata["tipo_cierre"].(string); ok {
			if estado, found := actionMap[tipo]; found {
				return estado
			}
		}
		action, _ := metadata["action"].(string)
		if estado, found := actionMap[action]; found {
			return estado
		}
	}
	return ""
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

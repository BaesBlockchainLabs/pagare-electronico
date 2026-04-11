package handler

import (
	"encoding/json"
	"net/http"
	"time"

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
		"ENDOSO": "ENDOSADO",
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
	var cierre, transfer, update string
	var hasBurn bool
	for i := len(history) - 1; i >= 0; i-- {
		entry, _ := history[i].(map[string]interface{})
		metadata, _ := entry["metadata"].(map[string]interface{})
		if metadata == nil {
			continue
		}
		if tipo, ok := metadata["tipo_cierre"].(string); ok {
			if estado, found := actionMap[tipo]; found && cierre == "" {
				cierre = estado
			}
		}
		action, _ := metadata["action"].(string)
		if action == "TRANSFER" && transfer == "" {
			transfer = "ENDOSADO"
		}
		if action == "ENDOSO" && update == "" {
			update = "ENDOSADO"
		}
		if action == "BURN" {
			hasBurn = true
		}
		if hasBurn && cierre == "" {
			if estado, found := actionMap[action]; found {
				cierre = estado
			}
		}
	}
	if cierre != "" {
		return cierre
	}
	if hasBurn {
		return "PAGADO"
	}
	if transfer != "" {
		return transfer
	}
	if update != "" {
		return update
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
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	actionLabels := map[string]string{
		"CREATE": "Emisión", "UPDATE": "Actualización", "TRANSFER": "Endoso (transferencia)",
		"BURN": "Quema", "ENDOSO": "Endoso",
		"PAGO": "Pago", "ANULACION": "Anulación", "PRESCRIPCION": "Prescripción",
	}

	history, _ := raw["history"].([]interface{})
	for _, entry := range history {
		e, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		metadata, _ := e["metadata"].(map[string]interface{})
		if metadata == nil {
			continue
		}
		action, _ := metadata["action"].(string)
		if label, found := actionLabels[action]; found {
			metadata["action_label"] = label
		} else {
			metadata["action_label"] = action
		}
		if tipo, ok := metadata["tipo_cierre"].(string); ok {
			if label, found := actionLabels[tipo]; found {
				metadata["tipo_cierre_label"] = label
			}
		}
		if tipo, ok := metadata["tipo_endoso"].(string); ok {
			endosoLabels := map[string]string{
				"en_propiedad":   "En propiedad (art. 97)",
				"en_procuracion": "En procuración (art. 100)",
				"en_blanco":      "En blanco (art. 99)",
			}
			if label, found := endosoLabels[tipo]; found {
				metadata["tipo_endoso_label"] = label
			}
		}
		if ts, ok := metadata["updated_at"].(float64); ok {
			metadata["fecha"] = time.UnixMilli(int64(ts)).Format("2006-01-02 15:04:05")
		}
		if ts, ok := metadata["fecha_pago"].(string); ok {
			metadata["fecha"] = ts
		}
		if _, ok := metadata["fecha"]; !ok {
			if ts, ok := metadata["updated_at"].(float64); ok {
				metadata["fecha"] = time.UnixMilli(int64(ts)).Format("2006-01-02 15:04:05")
			}
		}
	}

	WriteJSON(w, status, raw)
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
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		WriteRaw(w, status, body)
		return
	}

	asset, _ := raw["asset"].(map[string]interface{})
	if asset == nil {
		WriteRaw(w, status, body)
		return
	}

	estado := h.resolveEstado(id, map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
		"ENDOSO": "ENDOSADO",
	})
	data, _ := asset["data"].(map[string]interface{})
	if data == nil {
		data = make(map[string]interface{})
	}
	if estado != "" {
		data["estado"] = estado
	}

	ownerBody, ownerStatus, _ := h.client.GetAssetOwners(id)
	if ownerStatus == 200 {
		var ownerRaw map[string]interface{}
		if json.Unmarshal(ownerBody, &ownerRaw) == nil {
			if owners, ok := ownerRaw["owners"].([]interface{}); ok && len(owners) > 0 {
				if first, ok := owners[0].(map[string]interface{}); ok {
					if pub, ok := first["pub"].(string); ok {
						data["propietario_actual"] = pub
					}
				}
			}
		}
	}

	asset["data"] = data

	WriteJSON(w, status, raw)
}

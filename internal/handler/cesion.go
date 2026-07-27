package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pagare/internal/auth"
	"pagare/internal/models"
)

// TipoOperacionCesion marks, in the ledger history, a transfer made by ordinary
// assignment of the credit (arts. 347-348 CCom, arts. 1526 ss. CC) rather than
// by endoso.
//
// The distinction is not cosmetic bookkeeping. The two moves look identical on
// the ledger — both are a TRANSFER — but they carry different law:
//
//   - The cedente answers for the existence and legitimacy of the credit but
//     NOT for the debtor's solvency, unless expressly agreed (art. 1529 CC).
//     The endosante does answer for payment (art. 18 LCCH).
//   - The debtor may raise against the cesionario the defences they held
//     against the cedente: no acquisition free of personal defences, none of
//     the autonomy that makes a título cambiario what it is.
//   - It must be notified to the debtor to be enforceable against them, and
//     until then payment to the cedente still discharges the debt (art. 1527 CC).
//
// Printing a cesión inside the chain of endosos would therefore suggest a
// liability the cedente never assumed.
const TipoOperacionCesion = "CESION"

// Cesion is the assignment request: who receives the credit and, where
// available, the record of the notice given to the debtor.
type Cesion struct {
	// To: clave pública del cesionario.
	To string `json:"to"`
	// Cesionario: datos del adquirente, para que consten en el título.
	Cesionario *models.Persona `json:"cesionario,omitempty"`
	// NotificacionFecha y NotificacionMedio: constancia de la notificación al
	// deudor (art. 1527 CC). La notificación ocurre fuera del sistema; aquí solo
	// se registra que se hizo, para que el historial no la dé por supuesta.
	NotificacionFecha string `json:"notificacion_fecha,omitempty"`
	NotificacionMedio string `json:"notificacion_medio,omitempty"`
	// Motivo: causa de la cesión, si se quiere hacer constar.
	Motivo string `json:"motivo,omitempty"`
}

// Ceder transfers a pagaré by ordinary assignment of the credit.
//
// This is the route left to a pagaré issued «no a la orden», which art. 14 LCCH
// bars from endoso. Without it those titles could not move at all inside the
// platform, and the only way to assign them would be a paper document off the
// ledger — leaving the record no longer reflecting who holds the credit, which
// is precisely what the infrastructure exists to prevent.
func (h *PagareHandler) Ceder(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		ID string `json:"id"`
		Cesion
		From *models.IdentidadBC `json:"from,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Body JSON inválido"})
		return
	}
	if req.ID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}
	if req.To == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "to (clave pública del cesionario) es obligatorio"})
		return
	}

	from, err := h.resolveFrom(principal, req.From)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if from == nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "no dispones de una identidad con la que ceder el pagaré",
		})
		return
	}

	metadata := map[string]interface{}{
		"action":         TipoOperacionCesion,
		"tipo_operacion": TipoOperacionCesion,
		"fecha":          time.Now().Format(time.RFC3339),
	}
	if req.Cesionario != nil {
		metadata["cesionario"] = req.Cesionario
	}
	if req.NotificacionFecha != "" {
		metadata["notificacion_fecha"] = req.NotificacionFecha
	}
	if req.NotificacionMedio != "" {
		metadata["notificacion_medio"] = req.NotificacionMedio
	}
	if req.Motivo != "" {
		metadata["motivo"] = req.Motivo
	}

	body, status, err := h.client.UpdateAsset(map[string]interface{}{
		"id":       req.ID,
		"to":       req.To,
		"metadata": metadata,
		"from":     map[string]string{"pub": from.Pub, "pvt": from.Pvt},
	})
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	aviso := "Pagaré cedido. Recuerda notificar la cesión al deudor: hasta entonces no le es oponible y el pago al cedente le libera (art. 1527 CC)."
	if req.NotificacionFecha != "" {
		aviso = fmt.Sprintf("Pagaré cedido, con notificación al deudor de %s.", req.NotificacionFecha)
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "msg": aviso, "id": req.ID, "a": req.To,
	})
}

// esCesion reports whether a history entry is an assignment of the credit
// rather than an endoso.
func esCesion(metadata map[string]interface{}) bool {
	return strVal(metadata["tipo_operacion"]) == TipoOperacionCesion
}

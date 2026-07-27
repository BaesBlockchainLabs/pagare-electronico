package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"pagare/internal/auth"
	"pagare/internal/models"
)

// TipoOperacionEntrega marks, in the ledger history, the transfer that hands a
// freshly issued pagaré to its beneficiario.
//
// The marker is needed because BlockchainFUE overwrites the `action` we send
// with its own verb — a handover is recorded as TRANSFER, exactly like an
// endoso — while preserving any other field we add. Without it, the entrega
// would be counted as the first endoso of the chain and the pagaré would show
// as ENDOSADO from the moment it was issued.
const TipoOperacionEntrega = "ENTREGA"

// BeneficiaryResolver finds the blockchain key of a registered user from their
// NIF, so a pagaré can be handed over when the caller identified the
// beneficiario only by the fiscal number that art. 94 requires on the title.
// Satisfied by *auth.Store.
type BeneficiaryResolver interface {
	GetUserByNIF(nif string) (*auth.User, error)
}

// Entrega reports whether the issued pagaré reached its beneficiario.
//
// A pagaré that was created but not handed over is not a failure: in paper
// terms it is a title signed and still in the drawer, which is a real
// intermediate state. What would be wrong is to report it as delivered, since
// the control — and with it the legitimation to claim payment — is still the
// issuer's.
type Entrega struct {
	Entregado bool   `json:"entregado"`
	A         string `json:"a,omitempty"`
	Msg       string `json:"msg"`
}

// entregar transfers control of a newly created pagaré to its beneficiario.
//
// The ledger creates every asset owned by whoever signed the creation, and
// ignores any destination given at that point, so the handover has to be a
// second call. Until it happens the beneficiario holds nothing: under the MLETR
// model, control — not the record of the title — is what stands for possession.
func (h *PagareHandler) entregar(assetID string, p *models.PagareElectronico, to string, from *models.IdentidadBC) Entrega {
	destino := to
	if destino == "" {
		destino = h.pubDelBeneficiario(p)
	}
	if destino == "" {
		return Entrega{Msg: "El beneficiario no tiene identidad en la plataforma: el pagaré queda emitido y pendiente de entrega"}
	}
	if from == nil || from.Pub == "" {
		return Entrega{Msg: "Sin identidad de firma no puede transferirse el control: el pagaré queda emitido y pendiente de entrega"}
	}
	if destino == from.Pub {
		return Entrega{Msg: "El beneficiario es el propio emisor: no hay entrega que realizar"}
	}

	body := map[string]interface{}{
		"id": assetID,
		"to": destino,
		"metadata": map[string]interface{}{
			"action":         TipoOperacionEntrega,
			"tipo_operacion": TipoOperacionEntrega,
		},
		"from": map[string]string{"pub": from.Pub, "pvt": from.Pvt},
	}

	_, status, err := h.client.UpdateAsset(body)
	if err != nil {
		return Entrega{A: destino, Msg: fmt.Sprintf("El pagaré se emitió pero no pudo entregarse: %v", err)}
	}
	if status != 200 {
		return Entrega{A: destino, Msg: "El pagaré se emitió pero la red rechazó la entrega al beneficiario"}
	}

	return Entrega{Entregado: true, A: destino, Msg: "Entregado al beneficiario"}
}

// pubDelBeneficiario resolves the beneficiario's key from the NIF on the title.
func (h *PagareHandler) pubDelBeneficiario(p *models.PagareElectronico) string {
	if h.beneficiarios == nil {
		return ""
	}
	u, err := h.beneficiarios.GetUserByNIF(p.Beneficiario.NIF)
	if err != nil || u == nil || len(u.PubKeys) == 0 {
		return ""
	}
	return u.PubKeys[0]
}

// esEntrega reports whether a history entry is the handover at emission rather
// than an endoso.
func esEntrega(metadata map[string]interface{}) bool {
	return strVal(metadata["tipo_operacion"]) == TipoOperacionEntrega
}

// Entregar completes the handover of a pagaré that was issued but never
// delivered — the beneficiario had no identity at the time, or the ledger
// refused the transfer.
//
// Without this the title would stay in the issuer's hands for good, since the
// only other way out is to void it and issue again under a new ID. A pagaré
// pending delivery is a valid title, and it should be able to reach its holder
// without losing its identity on the way.
func (h *PagareHandler) Entregar(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		ID   string              `json:"id"`
		To   string              `json:"to,omitempty"`
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

	// A second handover is not a handover: once the title has moved, any
	// further transmission is an endoso or a cesión, with their own régimen.
	entregado, err := h.yaFueEntregado(req.ID)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	if entregado {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false,
			"msg": "Este pagaré ya fue entregado. Una transmisión posterior es un " +
				"endoso o una cesión, no una entrega.",
		})
		return
	}

	body, status, err := h.client.GetAsset(map[string]string{"id": req.ID})
	if err != nil || status != 200 {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": "no se pudo recuperar el pagaré"})
		return
	}
	pagare, err := assetToPagare(body)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "datos del pagaré no válidos"})
		return
	}

	from, err := h.resolveFrom(principal, req.From)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	entrega := h.entregar(req.ID, pagare, req.To, from)
	if !entrega.Entregado {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": entrega.Msg, "entrega": entrega,
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "msg": entrega.Msg, "id": req.ID, "entrega": entrega,
	})
}

// yaFueEntregado reports whether the title has already left the issuer's hands,
// by handover or by any later transmission.
func (h *PagareHandler) yaFueEntregado(id string) (bool, error) {
	body, status, err := h.client.GetAssetHistory(id)
	if err != nil {
		return false, fmt.Errorf("no se pudo comprobar si el pagaré ya fue entregado: %w", err)
	}
	if status != 200 {
		return false, fmt.Errorf("no se pudo recuperar el historial del pagaré")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return false, fmt.Errorf("historial del pagaré ilegible")
	}
	history, _ := raw["history"].([]interface{})
	for _, item := range history {
		e, _ := item.(map[string]interface{})
		if e == nil {
			continue
		}
		meta, _ := e["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}
		if strVal(meta["action"]) == "TRANSFER" {
			return true, nil
		}
	}
	return false, nil
}

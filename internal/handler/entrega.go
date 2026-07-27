package handler

import (
	"fmt"

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

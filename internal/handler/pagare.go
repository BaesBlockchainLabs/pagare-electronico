package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"
	"pagare/internal/crypto"
	"pagare/internal/models"
	"pagare/internal/validator"
)

// SigningKeys resolves the sealed private key of a user's public key. Satisfied
// by *auth.Store; lets the server sign on behalf of the logged-in user without
// the private key ever leaving the backend.
type SigningKeys interface {
	GetPrivateKey(userID, pub string) (string, error)
}

type PagareHandler struct {
	client        *bcfclient.Client
	validator     *validator.LCCHValidator
	crypto        *crypto.Service
	keys          SigningKeys
	beneficiarios BeneficiaryResolver
}

// SetBeneficiarios wires the resolver that turns the beneficiario's NIF into
// the key the pagaré is handed to at emission.
func (h *PagareHandler) SetBeneficiarios(b BeneficiaryResolver) { h.beneficiarios = b }

func NewPagareHandler(client *bcfclient.Client, cryptoSvc *crypto.Service, keys SigningKeys) *PagareHandler {
	return &PagareHandler{
		client:    client,
		validator: validator.NewLCCHValidator(),
		crypto:    cryptoSvc,
		keys:      keys,
	}
}

// resolveFrom decides which blockchain identity signs an operation for the
// logged-in principal.
//
//   - If the client supplied a private key (from.Pvt), it is used verbatim — the
//     "manual / advanced" fallback for keys not stored on the platform.
//   - Otherwise the server looks up the sealed private key for the chosen public
//     key (from.Pub, or the principal's first key) and uses it, provided the key
//     belongs to the principal. This is the default path: the user never handles
//     their private key.
//
// Returns nil (no signing identity) only when the user has no usable key and
// supplied none, letting the caller decide whether that is an error.
func (h *PagareHandler) resolveFrom(principal *auth.Principal, from *models.IdentidadBC) (*models.IdentidadBC, error) {
	// Manual/advanced path: caller provided an explicit private key.
	if from != nil && from.Pvt != "" {
		return from, nil
	}

	pub := ""
	if from != nil {
		pub = from.Pub
	}
	if pub == "" {
		if len(principal.PubKeys) == 0 {
			return nil, nil // nothing to sign with; caller decides
		}
		pub = principal.PubKeys[0]
	}
	if !principal.HasPubKey(pub) {
		return nil, fmt.Errorf("la clave %s no pertenece a tu cuenta", pub)
	}
	if h.keys == nil {
		return nil, fmt.Errorf("firma en servidor no disponible")
	}
	pvt, err := h.keys.GetPrivateKey(principal.UserID, pub)
	if err != nil {
		return nil, fmt.Errorf("no se pudo recuperar tu clave de firma: %w", err)
	}
	return &models.IdentidadBC{Pub: pub, Pvt: pvt}, nil
}

func (h *PagareHandler) Emitir(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		Asset struct {
			Data     models.PagareElectronico `json:"data"`
			Metadata *models.MetadataEmision  `json:"metadata,omitempty"`
		} `json:"asset"`
		From *models.IdentidadBC `json:"from,omitempty"`
		// To: clave pública del beneficiario a quien se entrega el pagaré. Si no
		// se indica, se resuelve por el NIF del beneficiario.
		To string `json:"to,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "Body JSON inválido",
		})
		return
	}

	if req.Asset.Data.IDPagare == "" {
		req.Asset.Data.IDPagare = fmt.Sprintf("urn:pagare:%d", time.Now().UnixNano())
	}

	result := h.validator.ValidatePagare(&req.Asset.Data)
	if !result.Valid {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "Validación LCCH fallida", "errors": result.Errors,
		})
		return
	}

	from, err := h.resolveFrom(principal, req.From)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	// Sign the canonical content with the firmante's identity (art. 94.7 LCCH).
	// The signature travels inside the asset data, not in the metadata: the
	// public endpoint only exposes data, and a third party must be able to
	// verify integrity without an account.
	firma, err := h.firmarContenido(&req.Asset.Data, from)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok": false, "msg": fmt.Sprintf("No se pudo firmar el pagaré: %v", err),
		})
		return
	}

	asset := map[string]interface{}{
		"data":     buildAssetData(&req.Asset.Data, firma),
		"metadata": buildEmisionMetadata(req.Asset.Metadata),
	}
	// Per the BCF schema, the creating identity (from) goes INSIDE asset, so the
	// asset is owned by that identity rather than the application key.
	if from != nil {
		asset["from"] = map[string]string{"pub": from.Pub, "pvt": from.Pvt}
	}
	bcfReq := map[string]interface{}{"asset": asset}

	body, status, err := h.client.CreateAsset(bcfReq)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{
			"ok": false, "msg": fmt.Sprintf("Error conectando BlockchainFUE: %v", err),
		})
		return
	}

	if status != 200 {
		WriteRaw(w, status, body)
		return
	}

	var resp models.BCFAssetResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"ok": false, "msg": "Error procesando respuesta de blockchain",
		})
		return
	}

	// The ledger created the asset owned by the firmante. Hand it to the
	// beneficiario, which is what gives them control — the electronic
	// equivalent of handing over the paper title.
	entrega := h.entregar(resp.ID, &req.Asset.Data, req.To, from)

	msg := "Pagaré emitido y entregado al beneficiario"
	if !entrega.Entregado {
		msg = "Pagaré emitido, pendiente de entrega al beneficiario"
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"msg":     msg,
		"id":      resp.ID,
		"cost":    resp.Cost,
		"entrega": entrega,
	})
}

func (h *PagareHandler) Endosar(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		ID       string                `json:"id"`
		To       string                `json:"to"`
		Metadata models.MetadataEndoso `json:"metadata"`
		From     *models.IdentidadBC   `json:"from,omitempty"`
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
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "to (clave pública endosatario) es obligatorio"})
		return
	}

	if err := h.comprobarEndosable(req.ID); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": err.Error(), "articulo_lcch": "art. 14 LCCH",
		})
		return
	}

	if req.Metadata.TipoEndoso == "" {
		req.Metadata.TipoEndoso = "en_propiedad"
	}

	endoso := models.Endoso{
		Tipo:                 req.Metadata.TipoEndoso,
		Fecha:                time.Now().Format(time.RFC3339),
		Endosatario:          req.Metadata.Endosatario,
		IdentidadEndosatario: req.To,
		Clausula:             req.Metadata.Clausula,
	}
	if endoso.Endosatario == nil && req.To != "" {
		endoso.Endosatario = &models.Persona{Nombre: req.To}
	}
	endosoResult := h.validator.ValidateEndoso(&endoso)
	if !endosoResult.Valid {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "Validación de endoso LCCH fallida", "errors": endosoResult.Errors,
		})
		return
	}

	from, err := h.resolveFrom(principal, req.From)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	bcfReq := map[string]interface{}{
		"id":       req.ID,
		"to":       req.To,
		"metadata": buildEndosoMetadata(&req.Metadata),
	}
	if from != nil {
		bcfReq["from"] = map[string]string{"pub": from.Pub, "pvt": from.Pvt}
	}

	body, status, err := h.client.UpdateAsset(bcfReq)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

func (h *PagareHandler) PagarAnular(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		ID       string              `json:"id"`
		Metadata models.MetadataPago `json:"metadata"`
		From     *models.IdentidadBC `json:"from,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "Body JSON inválido"})
		return
	}

	if req.ID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}
	if req.Metadata.Action == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "metadata.action es obligatorio (PAGO, ANULACION, PRESCRIPCION)"})
		return
	}

	validActions := map[string]bool{"PAGO": true, "ANULACION": true, "PRESCRIPCION": true}
	if !validActions[req.Metadata.Action] {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "metadata.action debe ser PAGO, ANULACION o PRESCRIPCION"})
		return
	}

	from, err := h.resolveFrom(principal, req.From)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	bcfQuery := map[string]interface{}{
		"id": req.ID,
	}
	if from != nil {
		bcfQuery["from"] = map[string]string{"pub": from.Pub, "pvt": from.Pvt}
	}

	motivoCierre := map[string]string{
		"PAGO":         "Este pagaré ha sido pagado",
		"ANULACION":    "Este pagaré ha sido anulado",
		"PRESCRIPCION": "Este pagaré ha prescrito",
	}

	updateBody := map[string]interface{}{
		"id": req.ID,
		"metadata": map[string]interface{}{
			"action":        req.Metadata.Action,
			"tipo_cierre":   req.Metadata.Action,
			"fecha":         time.Now().Format(time.RFC3339),
			"motivo_cierre": motivoCierre[req.Metadata.Action],
			"referencia":    req.Metadata.Referencia,
			"motivo":        req.Metadata.Motivo,
		},
	}
	if from != nil {
		updateBody["from"] = map[string]string{"pub": from.Pub, "pvt": from.Pvt}
	}

	updateResp, updateStatus, err := h.client.UpdateAsset(updateBody)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": "Error actualizando asset: " + err.Error()})
		return
	}
	if updateStatus != 200 {
		WriteJSON(w, updateStatus, map[string]interface{}{"ok": false, "msg": "Error actualizando asset antes de quemar", "detail": string(updateResp)})
		return
	}

	body, status, err := h.client.BurnAsset(bcfQuery)
	if err != nil {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	WriteRaw(w, status, body)
}

// comprobarEndosable rejects the endoso of a pagaré the librador issued with
// the «no a la orden» clause (art. 14 LCCH), which strips the title of its
// endorsable condition: it can only pass by ordinary assignment.
//
// The check fails closed. If the asset cannot be read we do not know whether
// the clause is there, and endorsing a non-endorsable title would put a chain
// of holders on something that cannot circulate — a mess to unwind, whereas a
// refusal during a network hiccup is merely a retry.
func (h *PagareHandler) comprobarEndosable(id string) error {
	body, status, err := h.client.GetAsset(map[string]string{"id": id})
	if err != nil {
		return fmt.Errorf("no se pudo comprobar si el pagaré es endosable: %w", err)
	}
	if status != 200 {
		return fmt.Errorf("no se pudo recuperar el pagaré para comprobar si es endosable")
	}
	p, err := assetToPagare(body)
	if err != nil {
		return fmt.Errorf("no se pudo interpretar el pagaré para comprobar si es endosable")
	}
	if p.NoALaOrden {
		return fmt.Errorf("este pagaré se emitió «no a la orden» y no puede endosarse; solo cabe transmitirlo por cesión ordinaria")
	}
	return nil
}

// firmarContenido signs the canonical form of the pagaré with the firmante's
// identity. Returns an empty signature (and no error) when there is no signing
// identity or no crypto service, which is the keyless path used by tests and by
// accounts that have not been provisioned a key; verification then reports the
// pagaré as unsigned rather than as tampered with.
func (h *PagareHandler) firmarContenido(p *models.PagareElectronico, from *models.IdentidadBC) (string, error) {
	if h.crypto == nil || from == nil || from.Pvt == "" || from.Pub == "" {
		return "", nil
	}
	canonical, err := models.CanonicalJSON(p)
	if err != nil {
		return "", err
	}
	return h.crypto.SignPagareContent(string(canonical), from.Pvt, from.Pub)
}

// buildAssetData is the canonical content plus the platform's own type marker
// and the firmante's signature over that content. Keeping the stored data and
// the signed form in step is what lets a verifier recompute the latter from the
// former.
func buildAssetData(p *models.PagareElectronico, firma string) map[string]interface{} {
	data := models.CanonicalContent(p)
	data["type"] = "pagare_electronico"
	if firma != "" {
		if firmante, ok := data["firmante"].(map[string]interface{}); ok {
			firmante["firma_digital"] = firma
		}
	}
	return data
}

func buildEmisionMetadata(meta *models.MetadataEmision) map[string]interface{} {
	m := map[string]interface{}{
		"action": "CREATE",
	}
	if meta != nil {
		if meta.FirmaDigitalPagare != "" {
			m["firma_digital_pagare"] = meta.FirmaDigitalPagare
		}
		if meta.FirmaDigitalBeneficiario != "" {
			m["firma_digital_beneficiario"] = meta.FirmaDigitalBeneficiario
		}
	}
	return m
}

func buildEndosoMetadata(meta *models.MetadataEndoso) map[string]interface{} {
	m := map[string]interface{}{
		"action":      "ENDOSO",
		"tipo_endoso": meta.TipoEndoso,
	}
	if meta.Endosatario != nil {
		m["endosatario"] = meta.Endosatario
	}
	if meta.FirmaDigitalEndoso != "" {
		m["firma_digital_endoso"] = meta.FirmaDigitalEndoso
	}
	if meta.Clausula != "" {
		m["clausula"] = meta.Clausula
	}
	if meta.Motivo != "" {
		m["motivo"] = meta.Motivo
	}
	return m
}

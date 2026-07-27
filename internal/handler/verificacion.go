package handler

import (
	"encoding/json"

	"pagare/internal/models"
)

// Verificacion is the outcome of checking the firmante's signature over the
// content of a pagaré, as reported to whoever consults it.
//
// The two flags answer different questions and both matter. Firmado says a
// signature is present and was produced by the key that created the asset on
// the ledger. Integro says the content stored today is byte-for-byte the
// content that key signed. A pagaré can be signed but not intact (someone
// altered a field after emission) and that is precisely the case a tenedor
// needs to be warned about.
type Verificacion struct {
	Firmado bool   `json:"firmado"`
	Integro bool   `json:"integro"`
	Clave   string `json:"clave,omitempty"`
	Msg     string `json:"msg"`
}

// verificarContenido checks the signature carried in firmante.firma_digital
// against the key that created the asset (data.from, recorded by the ledger,
// not by us) and against the content recomputed from the stored data.
//
// Recomputing the canonical form rather than trusting the signed blob is the
// point of the exercise: the blob carries its own message, so verifying it in
// isolation would only prove that its bearer signed *something*. Integrity
// holds when what they signed is what the asset holds now.
func (h *ConsultaHandler) verificarContenido(data map[string]interface{}) *Verificacion {
	if h.crypto == nil {
		return &Verificacion{Msg: "Verificación de firma no disponible"}
	}

	firma := firmaDigitalDe(data)
	if firma == "" {
		return &Verificacion{Msg: "Este pagaré se emitió sin firma del contenido"}
	}

	pub := strVal(data["from"])
	if pub == "" {
		return &Verificacion{Msg: "No consta la clave que emitió el pagaré"}
	}

	firmado, err := h.crypto.VerifyPagareSignature(firma, pub)
	if err != nil {
		return &Verificacion{
			Clave: pub,
			Msg:   "La firma no es válida para la clave que emitió el pagaré",
		}
	}

	p, err := pagareFromData(data)
	if err != nil {
		return &Verificacion{
			Firmado: true, Clave: pub,
			Msg: "Firma válida, pero el contenido no ha podido interpretarse",
		}
	}
	canonical, err := models.CanonicalJSON(p)
	if err != nil {
		return &Verificacion{
			Firmado: true, Clave: pub,
			Msg: "Firma válida, pero el contenido no ha podido interpretarse",
		}
	}

	if firmado != string(canonical) {
		return &Verificacion{
			Firmado: true, Clave: pub,
			Msg: "ATENCIÓN: el contenido no coincide con lo que se firmó al emitir",
		}
	}

	return &Verificacion{
		Firmado: true, Integro: true, Clave: pub,
		Msg: "Contenido firmado por el emisor e inalterado desde la emisión",
	}
}

// firmaDigitalDe extracts firmante.firma_digital from stored asset data.
func firmaDigitalDe(data map[string]interface{}) string {
	firmante, ok := data["firmante"].(map[string]interface{})
	if !ok {
		return ""
	}
	return strVal(firmante["firma_digital"])
}

// pagareFromData reinterprets stored asset data as a pagaré. Fields the ledger
// adds on its own are ignored, as they are absent from the model.
func pagareFromData(data map[string]interface{}) (*models.PagareElectronico, error) {
	db, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var p models.PagareElectronico
	if err := json.Unmarshal(db, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

package handler

import (
	"strings"

	"pagare/internal/auth"
)

// vistaPublica reduces the asset content to what a stranger needs in order to
// check the pagaré, leaving out what identifies the people behind it.
//
// A título valor is meant to be shown — whoever holds it lets others read it,
// because that is how the next taker decides whether to accept it. But on paper
// reading it requires *having* the document, whereas here it requires knowing a
// string that travels in printed QR codes, in URLs and in forwarded PDFs.
// Having seen an identifier is not the same as holding the title.
//
// So the public view keeps what proves the pagaré is genuine, intact and in
// force, and drops the rest. NIFs survive masked: someone with the title in
// hand can check they match, which a stranger cannot turn into an identity.
//
// The representante is dropped whole. That person is not a party to the credit
// — they owe nothing and collect nothing, they merely signed for the company —
// so nothing about the title requires naming them to an outsider.
func vistaPublica(data map[string]interface{}) map[string]interface{} {
	pub := map[string]interface{}{}

	// Lo que acredita el título sin identificar a nadie.
	for _, campo := range []string{
		"type", "denominacion", "promesa_pago", "importe", "moneda",
		"vencimiento", "localidad_pago", "localidad_emision", "fecha_emision",
		"no_a_la_orden", "clausulas", "estado", "created_at",
	} {
		if v, ok := data[campo]; ok {
			pub[campo] = v
		}
	}

	// De las partes, sólo el NIF enmascarado: sirve para cotejar, no para
	// identificar.
	if b, ok := data["beneficiario"].(map[string]interface{}); ok {
		pub["beneficiario"] = map[string]interface{}{"nif": enmascararNIF(strVal(b["nif"]))}
	}
	if f, ok := data["firmante"].(map[string]interface{}); ok {
		firmante := map[string]interface{}{"nif": enmascararNIF(strVal(f["nif"]))}
		// El tipo no identifica y explica la lectura del título: si emite una
		// sociedad, quien firmó fue alguien por ella.
		if t := strVal(f["tipo"]); t != "" {
			firmante["tipo"] = t
		}
		pub["firmante"] = firmante
	}
	// Del aval, su alcance: afecta a la garantía del crédito, no a quién sea el
	// avalista.
	if a, ok := data["aval"].(map[string]interface{}); ok {
		aval := map[string]interface{}{}
		for _, campo := range []string{"alcance", "importe_parcial"} {
			if v, ok := a[campo]; ok {
				aval[campo] = v
			}
		}
		pub["aval"] = aval
	}

	return pub
}

// enmascararNIF leaves the last three characters visible, as official listings
// do: enough for whoever holds the title to confirm it matches, not enough for
// a stranger to derive an identity.
func enmascararNIF(nif string) string {
	nif = strings.TrimSpace(nif)
	if len(nif) <= 3 {
		return strings.Repeat("*", len(nif))
	}
	return strings.Repeat("*", len(nif)-3) + nif[len(nif)-3:]
}

// esInteresado reports whether the requester is a party to the pagaré: the
// issuer, the current holder, or anyone who held it along the way.
//
// Past holders count. An endosante remains liable for payment (art. 18 LCCH)
// and a cedente for the existence of the credit (art. 1529 CC), so they keep a
// legitimate interest in a title that has already left their hands.
func (h *ConsultaHandler) esInteresado(id string, principal *auth.Principal) bool {
	if principal == nil {
		return false
	}
	if principal.IsAdmin() {
		return true
	}
	return h.assetOwnedBy(id, principal)
}

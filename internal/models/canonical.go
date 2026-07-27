package models

import "encoding/json"

// CanonicalContent builds the deterministic representation of the pagaré that
// the firmante signs: the menciones of art. 94 LCCH plus the optional aval and
// cláusulas.
//
// It is an explicit whitelist built from nested maps rather than from the
// structs, for two reasons. First, the ledger adds fields of its own to the
// stored asset data (app, from, namespace, token, created_at) and those must
// never enter the signature. Second, what was signed must not shift under a
// later change to a struct tag: the shape below is a stable contract, and any
// change to it invalidates every signature already issued.
//
// firmante.firma_digital is excluded — a signature cannot cover itself.
func CanonicalContent(p *PagareElectronico) map[string]interface{} {
	c := map[string]interface{}{
		"denominacion": p.Denominacion,
		"promesa_pago": p.PromesaPago,
		"importe":      p.Importe,
		"moneda":       p.Moneda,
		"vencimiento": map[string]interface{}{
			"tipo":  p.Vencimiento.Tipo,
			"fecha": p.Vencimiento.Fecha,
		},
		"localidad_pago": p.LocalidadPago,
		"beneficiario": map[string]interface{}{
			"nombre":   p.Beneficiario.Nombre,
			"apellido": p.Beneficiario.Apellido,
			"nif":      p.Beneficiario.NIF,
		},
		"localidad_emision": p.LocalidadEmision,
		"fecha_emision":     p.FechaEmision,
		"firmante": map[string]interface{}{
			"nombre":   p.Firmante.Nombre,
			"apellido": p.Firmante.Apellido,
			"nif":      p.Firmante.NIF,
			"direccion_postal": map[string]interface{}{
				"direccion":     p.Firmante.DireccionPostal.Direccion,
				"localidad":     p.Firmante.DireccionPostal.Localidad,
				"codigo_postal": p.Firmante.DireccionPostal.CodigoPostal,
				"region":        p.Firmante.DireccionPostal.Region,
				"pais":          p.Firmante.DireccionPostal.Pais,
			},
		},
		"no_a_la_orden": p.NoALaOrden,
	}

	// El tipo y el representante se incluyen sólo cuando constan, como el aval y
	// las cláusulas. No es una comodidad: añadirlos siempre cambiaría la forma
	// canónica de los pagarés ya firmados y sus firmas dejarían de validar. Un
	// pagaré sin estos campos produce exactamente los mismos bytes que antes de
	// que existieran.
	if p.Firmante.Tipo != "" {
		firmante, _ := c["firmante"].(map[string]interface{})
		firmante["tipo"] = p.Firmante.Tipo
	}
	if r := p.Firmante.Representante; r != nil {
		firmante, _ := c["firmante"].(map[string]interface{})
		firmante["representante"] = map[string]interface{}{
			"nombre":       r.Nombre,
			"apellido":     r.Apellido,
			"nif":          r.NIF,
			"cargo":        r.Cargo,
			"acreditacion": r.Acreditacion,
			"referencia":   r.Referencia,
			"fecha":        r.Fecha,
		}
	}

	if p.Aval != nil {
		c["aval"] = map[string]interface{}{
			"avalista": map[string]interface{}{
				"nombre":   p.Aval.Avalista.Nombre,
				"apellido": p.Aval.Avalista.Apellido,
				"nif":      p.Aval.Avalista.NIF,
			},
			"alcance":         p.Aval.Alcance,
			"importe_parcial": p.Aval.ImporteParcial,
			"avalado":         p.Aval.Avalado,
		}
	}
	if len(p.Clausulas) > 0 {
		c["clausulas"] = p.Clausulas
	}

	return c
}

// CanonicalJSON serialises CanonicalContent. encoding/json emits map keys in
// sorted order at every level, so signer and verifier reach identical bytes
// from identical content.
func CanonicalJSON(p *PagareElectronico) ([]byte, error) {
	return json.Marshal(CanonicalContent(p))
}

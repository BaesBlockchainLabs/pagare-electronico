package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pagare/internal/auth"
	"pagare/internal/pdf"
)

// Certificador identifies who signs the certificates, taken from configuration
// so it is not baked into the code.
type Certificador = pdf.Certificador

// SetCertificador wires who issues the certificates.
func (h *ConsultaHandler) SetCertificador(c Certificador) { h.certificador = c }

// DescargarCertificado issues the attestation of a pagaré's content, signature
// and history, in a form a reader outside the platform can use.
//
// Its purpose is to accompany a protesto. The protesto is drawn up by acta
// notarial (art. 51.1 LCCH) and a notary cannot read a ledger: they need a
// document that says, in words, what the record holds and who signed it.
//
// Restricted to the parties, for the same reason the public view is trimmed
// (see vistapublica.go): the certificate gathers every identifying datum of the
// title into one document meant to circulate. Whoever needs it — the tenedor
// raising the protesto — is a party by definition.
func (h *ConsultaHandler) DescargarCertificado(w http.ResponseWriter, r *http.Request) {
	principal := auth.GetPrincipal(r)
	if principal == nil {
		WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "id es obligatorio"})
		return
	}
	if !h.esInteresado(id, principal) {
		WriteJSON(w, http.StatusForbidden, map[string]interface{}{
			"ok": false, "msg": "el certificado sólo se expide a quienes son parte de este pagaré",
		})
		return
	}

	body, status, err := h.client.GetAsset(map[string]string{"id": id})
	if err != nil || status != 200 {
		WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": "no se pudo recuperar el pagaré"})
		return
	}
	pagare, err := assetToPagare(body)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "datos del pagaré no válidos"})
		return
	}

	// La verificación se hace sobre el contenido tal como consta, que es lo que
	// se firmó.
	var raw map[string]interface{}
	_ = json.Unmarshal(body, &raw)
	data := map[string]interface{}{}
	if a, ok := raw["asset"].(map[string]interface{}); ok {
		data, _ = a["data"].(map[string]interface{})
	}
	verificacion := h.verificarContenido(data)

	var operaciones []pdf.Operacion
	if histBody, hs, herr := h.client.GetAssetHistory(id); herr == nil && hs == 200 {
		operaciones = h.historicoCertificado(histBody)
	}

	estado := h.resolveEstado(id, map[string]string{
		"PAGO": "PAGADO", "ANULACION": "ANULADO", "PRESCRIPCION": "PRESCRITO",
	})

	titular := ""
	if ownerBody, os, oerr := h.client.GetAssetOwners(id); oerr == nil && os == 200 {
		var o struct {
			Owners []struct {
				Pub string `json:"pub"`
			} `json:"owners"`
		}
		if json.Unmarshal(ownerBody, &o) == nil && len(o.Owners) > 0 {
			titular = o.Owners[0].Pub
		}
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	ahora := time.Now()

	out, err := pdf.Certificado(pdf.CertificadoInput{
		P:             pagare,
		AssetID:       id,
		Red:           r.URL.Query().Get("network"),
		VerifyURL:     fmt.Sprintf("%s://%s/pagares/verificar?id=%s", scheme, r.Host, id),
		Estado:        estado,
		FirmantePub:   verificacion.Clave,
		TitularActual: titular,
		Firmado:       verificacion.Firmado,
		Integro:       verificacion.Integro,
		VerificaMsg:   verificacion.Msg,
		Operaciones:   operaciones,
		Certificador:  h.certificador,
		Expedido:      ahora,
		// Referencia reproducible desde el propio certificado: identifica el
		// título y el momento de expedición, sin necesidad de llevar registro.
		Referencia: fmt.Sprintf("%s-%s", shortIDStr(id), ahora.Format("20060102-1504")),
	})
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo generar el certificado"})
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"certificado-%s.pdf\"", shortIDStr(id)))
	w.Write(out)
}

// historicoCertificado turns the ledger history into the narrative the
// certificate tells, naming each operation for what it legally is.
//
// The ledger records every transfer as TRANSFER, so entrega, endoso and cesión
// would look alike; telling them apart matters because each binds the
// transferor differently, and a certificate that blurred them would misstate
// who owes what.
func (h *ConsultaHandler) historicoCertificado(body []byte) []pdf.Operacion {
	var raw map[string]interface{}
	if json.Unmarshal(body, &raw) != nil {
		return nil
	}
	hist, _ := raw["history"].([]interface{})

	var ops []pdf.Operacion
	for _, item := range hist {
		e, _ := item.(map[string]interface{})
		if e == nil {
			continue
		}
		meta, _ := e["metadata"].(map[string]interface{})
		if meta == nil {
			continue
		}

		op := pdf.Operacion{
			Desde: strVal(meta["from"]),
			Hacia: strVal(meta["to"]),
			Fecha: fechaDeOperacion(meta),
		}

		switch {
		case strVal(meta["action"]) == "CREATE":
			op.Tipo = "EMISION"
			op.Titulo = "Emisión del pagaré"
			op.Articulos = "art. 94 LCCH"
			op.Detalle = []string{"Asentado por la clave " + op.Desde}

		case esEntrega(meta):
			op.Tipo = "ENTREGA"
			op.Titulo = "Entrega al beneficiario"
			op.Detalle = []string{
				"El control del registro pasa al beneficiario, que es el equivalente electrónico de la entrega del título.",
				"De " + op.Desde + " a " + op.Hacia,
			}

		case esCesion(meta):
			op.Tipo = "CESION"
			op.Titulo = "Cesión ordinaria del crédito"
			op.Articulos = "arts. 347-348 CCom; arts. 1526 y ss. CC"
			op.Detalle = []string{
				"De " + op.Desde + " a " + op.Hacia,
				"El cedente responde de la existencia y legitimidad del crédito, pero no de la solvencia del deudor (art. 1529 CC).",
			}
			if ce, ok := meta["cesionario"].(map[string]interface{}); ok {
				op.Detalle = append(op.Detalle, "Cesionario: "+personaLegible(ce))
			}
			if f := strVal(meta["notificacion_fecha"]); f != "" {
				aviso := "Notificada al deudor el " + f
				if m := strVal(meta["notificacion_medio"]); m != "" {
					aviso += " por " + m
				}
				op.Detalle = append(op.Detalle, aviso)
			} else {
				op.Detalle = append(op.Detalle,
					"No consta notificación al deudor: hasta que se produzca, la cesión no le es oponible y el pago al cedente le libera (art. 1527 CC).")
			}

		case strVal(meta["action"]) == "TRANSFER" || strVal(meta["tipo_endoso"]) != "":
			op.Tipo = "ENDOSO"
			op.Titulo = "Endoso"
			op.Articulos = "arts. 14-24 y 96 LCCH"
			op.Detalle = []string{"De " + op.Desde + " a " + op.Hacia}
			if t := strVal(meta["tipo_endoso"]); t != "" {
				op.Detalle = append(op.Detalle, "Clase de endoso: "+tipoEndosoCertificado(t))
			}
			if c := strVal(meta["clausula"]); c != "" {
				op.Detalle = append(op.Detalle, "Cláusula: "+clausulaCertificado(c))
			}
			if en, ok := meta["endosatario"].(map[string]interface{}); ok {
				op.Detalle = append(op.Detalle, "Endosatario: "+personaLegible(en))
			}

		case strVal(meta["action"]) == "BURN":
			// La quema es la contrapartida técnica del cierre, que la entrada
			// anterior ya relata; contarla aparte duplicaría el hecho. Va antes
			// que el cierre porque el propio asiento de quema lleva tipo_cierre.
			continue

		case strVal(meta["tipo_cierre"]) != "":
			cierre := strVal(meta["tipo_cierre"])
			op.Tipo = cierre
			switch cierre {
			case "PAGO":
				op.Titulo = "Pago del pagaré"
				op.Detalle = []string{"El crédito queda satisfecho y el título se extingue en el registro."}
			case "ANULACION":
				op.Titulo = "Anulación del pagaré"
			case "PRESCRIPCION":
				op.Titulo = "Declaración de prescripción"
				op.Articulos = "art. 88 LCCH"
			default:
				op.Titulo = cierre
			}
			if ref := strVal(meta["referencia"]); ref != "" {
				op.Detalle = append(op.Detalle, "Referencia: "+ref)
			}
			if m := strVal(meta["motivo"]); m != "" {
				op.Detalle = append(op.Detalle, "Motivo: "+m)
			}

		default:
			op.Tipo = "OTRO"
			op.Titulo = "Actualización del asiento"
		}

		ops = append(ops, op)
	}
	return ops
}

func fechaDeOperacion(meta map[string]interface{}) string {
	if f := strVal(meta["fecha"]); len(f) >= 10 {
		return f[:10]
	}
	if ts, ok := meta["updated_at"].(float64); ok {
		return time.UnixMilli(int64(ts)).Format("2006-01-02")
	}
	return ""
}

func personaLegible(m map[string]interface{}) string {
	nombre := strings.TrimSpace(strVal(m["nombre"]) + " " + strVal(m["apellido"]))
	if nif := strVal(m["nif"]); nif != "" {
		if nombre == "" {
			return "NIF " + nif
		}
		return nombre + ", con NIF " + nif
	}
	return nombre
}

func tipoEndosoCertificado(t string) string {
	switch t {
	case "en_propiedad":
		return "en propiedad, que transmite todos los derechos del título (art. 17 LCCH)"
	case "en_blanco":
		return "en blanco, perfeccionado con la sola firma del endosante (art. 15 LCCH)"
	case "en_procuracion":
		return "en procuración o comisión de cobranza, que no transmite la propiedad (art. 21 LCCH)"
	case "en_garantia":
		return "en garantía o prenda (art. 22 LCCH)"
	default:
		return t
	}
}

func clausulaCertificado(c string) string {
	switch c {
	case "sin_responsabilidad":
		return "«sin mi responsabilidad»: el endosante no responde del pago (art. 18 LCCH)"
	case "no_a_la_orden":
		return "prohibición de nuevo endoso: el endosante no responde frente a endosatarios posteriores (art. 18 LCCH)"
	case "sin_gastos":
		return "«sin gastos»: dispensa de levantar protesto para conservar las acciones de regreso (art. 56 LCCH)"
	default:
		return c
	}
}

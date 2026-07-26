package pdf

import (
	"math"
	"strings"
)

// importeEnLetra expresa un importe monetario en palabras (español), con la
// forma habitual en efectos cambiarios, p.ej.:
//
//	1500.00 -> "mil quinientos euros"
//	1.5     -> "un euro con cincuenta céntimos"
//	21      -> "veintiún euros"
//
// Cubre 0..999.999.999,99. El importe en letra prevalece sobre la cifra en caso
// de divergencia (art. 7 LCCH), de ahí que sea obligatorio en el documento.
func importeEnLetra(importe float64) string {
	if importe < 0 {
		importe = 0
	}
	entero := int64(math.Floor(importe + 1e-9))
	centimos := int64(math.Round((importe - float64(entero)) * 100))
	if centimos == 100 { // corrección de redondeo
		entero++
		centimos = 0
	}

	euros := enteroEnLetra(entero)
	nombreEuro := "euros"
	if entero == 1 {
		nombreEuro = "euro"
	}
	out := euros + " " + nombreEuro

	if centimos > 0 {
		nombreCent := "céntimos"
		if centimos == 1 {
			nombreCent = "céntimo"
		}
		out += " con " + enteroEnLetra(centimos) + " " + nombreCent
	}
	return out
}

var unidades = []string{
	"cero", "un", "dos", "tres", "cuatro", "cinco", "seis", "siete", "ocho", "nueve",
	"diez", "once", "doce", "trece", "catorce", "quince", "dieciséis", "diecisiete",
	"dieciocho", "diecinueve", "veinte", "veintiún", "veintidós", "veintitrés",
	"veinticuatro", "veinticinco", "veintiséis", "veintisiete", "veintiocho", "veintinueve",
}

var decenas = []string{"", "", "", "treinta", "cuarenta", "cincuenta", "sesenta", "setenta", "ochenta", "noventa"}

var centenas = []string{"", "ciento", "doscientos", "trescientos", "cuatrocientos",
	"quinientos", "seiscientos", "setecientos", "ochocientos", "novecientos"}

// tresCifras convierte 0..999 a palabras (con "uno" apocopado a "un", como
// corresponde antes de un sustantivo: "un euro", "veintiún mil").
func tresCifras(n int) string {
	if n == 0 {
		return ""
	}
	if n == 100 {
		return "cien"
	}
	var parts []string
	c := n / 100
	resto := n % 100
	if c > 0 {
		parts = append(parts, centenas[c])
	}
	if resto > 0 {
		if resto <= 29 {
			parts = append(parts, unidades[resto])
		} else {
			d := resto / 10
			u := resto % 10
			if u == 0 {
				parts = append(parts, decenas[d])
			} else {
				parts = append(parts, decenas[d]+" y "+unidades[u])
			}
		}
	}
	return strings.Join(parts, " ")
}

// enteroEnLetra convierte 0..999.999.999 a palabras.
func enteroEnLetra(n int64) string {
	if n == 0 {
		return "cero"
	}
	var parts []string

	millones := n / 1_000_000
	resto := n % 1_000_000
	miles := resto / 1000
	cientos := resto % 1000

	if millones > 0 {
		if millones == 1 {
			parts = append(parts, "un millón")
		} else {
			parts = append(parts, tresCifras(int(millones))+" millones")
		}
	}
	if miles > 0 {
		if miles == 1 {
			parts = append(parts, "mil")
		} else {
			parts = append(parts, tresCifras(int(miles))+" mil")
		}
	}
	if cientos > 0 {
		parts = append(parts, tresCifras(int(cientos)))
	}
	return strings.Join(parts, " ")
}

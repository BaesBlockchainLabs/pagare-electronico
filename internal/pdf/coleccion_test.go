package pdf

import (
	"bytes"
	"testing"

	"pagare/internal/models"
)

// Un PDF con /EmbeddedFiles es una colección de documentos para quien lo lea,
// y Logalty lo rechaza por eso. Ninguno de los que generamos puede llevarlo.
func TestGenerate_NoEsUnaColeccion(t *testing.T) {
	documento, err := Generate(Input{
		P:         pagareDeMuestra(),
		AssetID:   "prueba",
		VerifyURL: "https://ejemplo/verificar?id=prueba",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if bytes.Contains(documento, []byte("/EmbeddedFiles")) {
		t.Error("el pagaré lleva /EmbeddedFiles: el portal lo tomará por una colección")
	}
	if bytes.Contains(documento, []byte("/AcroForm")) {
		t.Error("el pagaré lleva /AcroForm, que el portal tampoco acepta")
	}
}

// Quitar la entrada no puede mover ningún byte, porque los offsets de la tabla
// xref no se recalculan.
func TestSinColeccion_ConservaLosOffsets(t *testing.T) {
	entrada := []byte("%PDF-1.3\n7 0 obj\n<<\n/Type /Catalog\n/Pages 1 0 R\n" +
		"/Names <<\n/EmbeddedFiles << /Names [\n  \n] >>\n>>\n>>\nendobj\nxref\n" +
		"0 8\n0000000000 65535 f \ntrailer\n<<\n/Size 8\n>>\nstartxref\n834\n%%EOF\n")

	salida := sinColeccion(entrada)
	if len(salida) != len(entrada) {
		t.Fatalf("la longitud cambió: %d -> %d", len(entrada), len(salida))
	}
	if bytes.Contains(salida, []byte("/EmbeddedFiles")) {
		t.Error("la entrada sigue ahí")
	}
	// El resto del catálogo tiene que seguir intacto, o el PDF deja de ser
	// legible.
	for _, resto := range []string{"/Type /Catalog", "/Pages 1 0 R", "startxref\n834", "%%EOF"} {
		if !bytes.Contains(salida, []byte(resto)) {
			t.Errorf("se perdió %q", resto)
		}
	}
	// Los delimitadores tienen que seguir cuadrando: la entrada borrada
	// aportaba dos de cada, y el resto del catálogo no se toca.
	cuenta := func(d []byte, s string) int { return bytes.Count(d, []byte(s)) }
	abren, cierran := cuenta(salida, "<<"), cuenta(salida, ">>")
	if abren != cierran {
		t.Errorf("diccionarios desbalanceados: <<=%d >>=%d", abren, cierran)
	}
	if abren != cuenta(entrada, "<<")-2 {
		t.Errorf("<< = %d, se esperaban %d", abren, cuenta(entrada, "<<")-2)
	}
}

// Un PDF que no la trae se queda exactamente igual.
func TestSinColeccion_SinEntradaNoToca(t *testing.T) {
	entrada := []byte("%PDF-1.7\n7 0 obj\n<</Type /Catalog/Pages 1 0 R>>\nendobj\n%%EOF\n")
	if salida := sinColeccion(entrada); !bytes.Equal(salida, entrada) {
		t.Errorf("modificó un PDF que no llevaba la entrada:\n%s", salida)
	}
}

// El árbol con adjuntos de verdad no se toca: borrarlo perdería los ficheros.
// Hoy no adjuntamos nada, pero el día que se adjunte hay que enterarse por un
// motivo real y no por un PDF mutilado en silencio.
func TestSinColeccion_ConAdjuntosNoToca(t *testing.T) {
	entrada := []byte("/Names <<\n/EmbeddedFiles << /Names [\n (Attachement1) 12 0 R \n] >>\n>>")
	if salida := sinColeccion(entrada); !bytes.Equal(salida, entrada) {
		t.Errorf("borró un árbol con adjuntos:\n%s", salida)
	}
}

func pagareDeMuestra() *models.PagareElectronico {
	return &models.PagareElectronico{
		IDPagare:         "urn:pagare:prueba",
		Denominacion:     "PAGARÉ",
		PromesaPago:      true,
		Importe:          1500,
		Moneda:           "EUR",
		Vencimiento:      models.Vencimiento{Tipo: "fecha_fija", Fecha: "2027-01-15"},
		LocalidadPago:    "Monóvar",
		Beneficiario:     models.Persona{Nombre: "Ana", Apellido: "López", NIF: "12345678Z"},
		LocalidadEmision: "Monóvar",
		FechaEmision:     "2026-09-12",
		Firmante: models.Firmante{
			Nombre: "Carlos", Apellido: "Ruiz", NIF: "87654321X",
			DireccionPostal: models.DireccionPostal{
				Direccion: "Calle Mayor 5", Localidad: "Monóvar",
				CodigoPostal: "03640", Pais: "ES",
			},
		},
	}
}

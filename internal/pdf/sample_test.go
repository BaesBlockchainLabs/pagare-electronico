package pdf

import (
	"os"
	"path/filepath"
	"testing"

	"pagare/internal/models"
)

func sampleInput() Input {
	return Input{
		AssetID:   "0528c3d0e85bca2f7d3eff56771f343540e3eec736cd4ee194fe20e54494b196",
		VerifyURL: "https://pagare.example/pagares/verificar?id=0528c3d0e85bca2f",
		P: &models.PagareElectronico{
			Denominacion:     "PAGARÉ",
			PromesaPago:      true,
			Importe:          1500.50,
			Moneda:           "EUR",
			Vencimiento:      models.Vencimiento{Tipo: "fecha_fija", Fecha: "2027-01-15"},
			LocalidadPago:    "Madrid",
			LocalidadEmision: "A Coruña",
			FechaEmision:     "2026-07-26",
			Beneficiario:     models.Persona{Nombre: "Ana", Apellido: "García López", NIF: "12345678Z"},
			Firmante: models.Firmante{
				Nombre: "José", Apellido: "Muñoz Peña", NIF: "87131862L",
				DireccionPostal: models.DireccionPostal{
					Direccion: "Calle Mayor 10", Localidad: "A Coruña", CodigoPostal: "15001", Pais: "ES",
				},
			},
			Aval: &models.Aval{
				Avalista: models.Persona{Nombre: "Luis", Apellido: "Soto", NIF: "11223344A"},
				Alcance:  "total",
			},
		},
	}
}

func TestGenerateSample(t *testing.T) {
	b, err := Generate(sampleInput())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(b) < 1000 {
		t.Fatalf("PDF demasiado pequeño: %d", len(b))
	}
	if out := os.Getenv("PDF_POC_OUT"); out != "" {
		path := filepath.Join(out, "anverso.pdf")
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("anverso escrito en %s (%d bytes)", path, len(b))
	}
}

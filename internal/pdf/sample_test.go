package pdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-pdf/fpdf"

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
		Endosos: []Endoso{
			{Fecha: "2026-08-10", Tipo: "en_propiedad", Endosatario: "Pedro García", NIF: "38240458Z"},
			{Fecha: "2026-09-02", Tipo: "en_garantia", Endosatario: "Banco Ejemplo S.A.", NIF: "A12345674", Clausula: "sin gastos"},
			{Fecha: "2026-10-01", Tipo: "en_blanco"},
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
	out := os.Getenv("PDF_POC_OUT")
	if out == "" {
		return
	}
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(out, name), data, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("pagare.pdf", b)

	// Vistas previas de una sola página para inspección visual.
	write("anverso.pdf", onePage(renderAnverso))
	write("reverso.pdf", onePage(renderReverso))
	t.Logf("PDFs escritos en %s", out)
}

// onePage renders a single side into its own one-page PDF (preview helper).
func onePage(render func(*fpdf.Fpdf, Input)) []byte {
	p := fpdf.New("L", "mm", "A4", "")
	p.SetMargins(0, 0, 0)
	p.SetAutoPageBreak(false, 0)
	registerFonts(p)
	p.AddPage()
	render(p, sampleInput())
	var buf bytes.Buffer
	_ = p.Output(&buf)
	return buf.Bytes()
}

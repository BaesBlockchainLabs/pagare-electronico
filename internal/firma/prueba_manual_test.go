//go:build manual

// Prueba de ida y vuelta contra el entorno DEMO de Logalty. No corre con el
// resto: necesita credenciales y crea un envío de verdad, con su SMS a una
// persona de verdad.
//
//	set -a; . ./.env; set +a
//	go test -tags manual ./internal/firma/ -run TestManual -v \
//	    -firmante-nif 00000000T -firmante-email tu@correo -firmante-movil +34600000000
//
// Con -guid en lugar de lo anterior, descarga el PDF firmado de un envío que
// ya se haya firmado.
package firma

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pagare/internal/config"
	"pagare/internal/models"
	"pagare/internal/pdf"
)

var (
	nif    = flag.String("firmante-nif", "", "NIF del firmante")
	correo = flag.String("firmante-email", "", "email del firmante")
	movil  = flag.String("firmante-movil", "", "móvil del firmante")
	nombre = flag.String("firmante-nombre", "Pruebas", "nombre del firmante")
	guid   = flag.String("guid", "", "descargar el PDF firmado de este envío en lugar de crear uno")
	hashOK = flag.String("hash-original", "", "hash del PDF que se mandó, para cotejarlo")
)

func servicio(t *testing.T) *Servicio {
	t.Helper()
	cfg := config.LogaltyConfig{
		Endpoint:     os.Getenv("LOGALTY_ENDPOINT"),
		Usuario:      os.Getenv("LOGALTY_USER"),
		Password:     os.Getenv("LOGALTY_PASSWORD"),
		Empresa:      os.Getenv("LOGALTY_COMPANY"),
		TipoContrato: os.Getenv("LOGALTY_TYPE_CONTRACT"),
		Remitente:    os.Getenv("LOGALTY_SENDER_NAME"),
	}
	if !cfg.FirmaActiva() {
		t.Skip("sin LOGALTY_* en el entorno; usa: set -a; . ./.env; set +a")
	}
	s, err := NuevoServicio(cfg)
	if err != nil {
		t.Fatalf("NuevoServicio: %v", err)
	}
	return s
}

// pagarePDF es un pagaré de muestra, el mismo que generaría una emisión.
func pagarePDF(t *testing.T) []byte {
	t.Helper()
	p := &models.PagareElectronico{
		IDPagare:     "urn:pagare:prueba-firma",
		Denominacion: "PAGARÉ",
		PromesaPago:  true,
		Importe:      1500,
		Moneda:       "EUR",
		Vencimiento: models.Vencimiento{
			Tipo:  "fecha_fija",
			Fecha: time.Now().AddDate(0, 3, 0).Format("2006-01-02"),
		},
		LocalidadPago:    "Monóvar",
		Beneficiario:     models.Persona{Nombre: "Ana", Apellido: "López", NIF: "12345678Z"},
		LocalidadEmision: "Monóvar",
		FechaEmision:     time.Now().Format("2006-01-02"),
		Firmante: models.Firmante{
			Nombre: *nombre, Apellido: "Logalty", NIF: *nif,
			DireccionPostal: models.DireccionPostal{
				Direccion: "Calle Mayor 1", Localidad: "Monóvar",
				CodigoPostal: "03640", Pais: "ES",
			},
		},
	}
	out, err := pdf.Generate(pdf.Input{
		P:         p,
		AssetID:   "prueba-firma-0001",
		VerifyURL: "https://pagare.blockchainfue.com/pagares/verificar?id=prueba-firma-0001",
	})
	if err != nil {
		t.Fatalf("generando el PDF: %v", err)
	}
	return out
}

func TestManualFirma(t *testing.T) {
	s := servicio(t)
	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelar()

	if *guid != "" {
		f, err := s.Recoger(ctx, *guid, *hashOK)
		if err != nil {
			t.Fatalf("Recoger: %v", err)
		}
		ruta := filepath.Join(t.TempDir(), "firmado.pdf")
		if err := os.WriteFile(ruta, f.PDF, 0600); err != nil {
			t.Fatal(err)
		}
		// Se copia fuera del TempDir para poder abrirlo después del test.
		destino := "/tmp/pagare-firmado.pdf"
		_ = os.WriteFile(destino, f.PDF, 0600)
		t.Logf("PDF firmado: %d bytes, sha256 %s", len(f.PDF), f.Hash)
		t.Logf("original devuelto: sha256 %s", f.HashOriginal)
		t.Logf("guardado en %s", destino)
		return
	}

	if *nif == "" || *correo == "" || *movil == "" {
		t.Skip("hacen falta -firmante-nif, -firmante-email y -firmante-movil")
	}

	documento := pagarePDF(t)
	t.Logf("PDF a firmar: %d bytes, AcroForm=%v", len(documento), EsAcroForm(documento))
	if vuelca := os.Getenv("VOLCAR_PDF"); vuelca != "" {
		if err := os.WriteFile(vuelca, documento, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("PDF volcado en %s", vuelca)
		return
	}

	ref := fmt.Sprintf("prueba-firma-%d", time.Now().Unix())
	envio, err := s.Iniciar(ctx, Peticion{
		Referencia: ref,
		Fichero:    "pagare.pdf",
		Asunto:     "Firma del pagaré de prueba",
		PDF:        documento,
		Firmante: Firmante{
			Nombre: *nombre, Apellidos: "Logalty",
			NIF: *nif, Email: *correo, Movil: *movil,
		},
	})
	if err != nil {
		t.Fatalf("Iniciar: %v", err)
	}
	t.Logf("referencia:     %s", envio.Referencia)
	t.Logf("guid:           %q", envio.GUID)
	t.Logf("hash original:  %s", envio.HashOriginal)
	t.Log("el portal avisa al firmante por correo y SMS; el enlace va ahí")

	sit, err := s.Consultar(ctx, ref)
	if err != nil {
		t.Logf("Consultar (todavía puede no estar): %v", err)
		return
	}
	t.Logf("estado: %s (terminado=%v firmado=%v guid=%q)",
		sit.Descripcion, sit.Terminado, sit.Firmado, sit.GUID)
}

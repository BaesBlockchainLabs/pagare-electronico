package validator

import (
	"testing"
	"time"

	"pagare/internal/models"

	"github.com/stretchr/testify/assert"
)

func validPagare() models.PagareElectronico {
	return models.PagareElectronico{
		IDPagare:         "urn:pagare:test-001",
		Denominacion:     "PAGARÉ",
		PromesaPago:      true,
		Importe:          5000.00,
		Moneda:           "EUR",
		Vencimiento:      models.Vencimiento{Tipo: "fecha_fija", Fecha: "2027-01-15"},
		LocalidadPago:    "Madrid",
		Beneficiario:     models.Persona{Nombre: "Ana", Apellido: "López", NIF: "12345678Z"},
		LocalidadEmision: "Barcelona",
		FechaEmision:     "2026-04-10",
		Firmante: models.Firmante{
			Nombre:          "Carlos",
			NIF:             "87654321X",
			DireccionPostal: models.DireccionPostal{Direccion: "Calle Mayor 5", Localidad: "Barcelona", CodigoPostal: "08001", Pais: "ES"},
		},
	}
}

func TestValidatePagare_Valid(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	result := lv.ValidatePagare(&p)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidatePagare_DenominacionIncorrecta(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Denominacion = "PAGARE"
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
	found := false
	for _, e := range result.Errors {
		if e.Campo == "Denominacion" && e.ArticuloLCCH == "art. 94 LCCH" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestValidatePagare_PromesaPagoFalse(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.PromesaPago = false
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
}

func TestValidatePagare_ImporteCero(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Importe = 0
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
}

func TestValidatePagare_VencimientoFijaSinFecha(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Vencimiento = models.Vencimiento{Tipo: "fecha_fija", Fecha: ""}
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
}

func TestValidatePagare_VencimientoFijaPasada(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Vencimiento = models.Vencimiento{Tipo: "fecha_fija", Fecha: "2020-01-01"}
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
}

func TestValidatePagare_VistaConPlazo(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Vencimiento = models.Vencimiento{Tipo: "a_la_vista"}
	result := lv.ValidatePagare(&p)
	assert.True(t, result.Valid)
	hasArt39 := false
	for _, e := range result.Errors {
		if e.ArticuloLCCH == "art. 39 LCCH" {
			hasArt39 = true
		}
	}
	assert.True(t, hasArt39)
}

func TestValidatePagare_AvalParcialSinImporte(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Aval = &models.Aval{
		Avalista:       models.Persona{Nombre: "Avalista", NIF: "11111111H"},
		Alcance:        "parcial",
		ImporteParcial: 0,
	}
	result := lv.ValidatePagare(&p)
	assert.False(t, result.Valid)
}

func TestValidatePagare_AvalValido(t *testing.T) {
	lv := NewLCCHValidator()
	p := validPagare()
	p.Aval = &models.Aval{
		Avalista: models.Persona{Nombre: "Avalista", NIF: "11111111H"},
		Alcance:  "total",
	}
	result := lv.ValidatePagare(&p)
	assert.True(t, result.Valid)
}

func TestValidateEndoso_Valid(t *testing.T) {
	lv := NewLCCHValidator()
	e := models.Endoso{
		Tipo:        "en_propiedad",
		Endosante:   models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Endosatario: &models.Persona{Nombre: "Bea", NIF: "87654321X"},
		Fecha:       "2026-05-01",
	}
	result := lv.ValidateEndoso(&e)
	assert.True(t, result.Valid)
}

func TestValidateEndoso_EnPropiedadSinEndosatario(t *testing.T) {
	lv := NewLCCHValidator()
	e := models.Endoso{
		Tipo:      "en_propiedad",
		Endosante: models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Fecha:     "2026-05-01",
	}
	result := lv.ValidateEndoso(&e)
	assert.False(t, result.Valid)
}

func TestValidateEndoso_EnBlanco(t *testing.T) {
	lv := NewLCCHValidator()
	e := models.Endoso{
		Tipo:      "en_blanco",
		Endosante: models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Fecha:     "2026-05-01",
	}
	result := lv.ValidateEndoso(&e)
	assert.True(t, result.Valid)
}

func TestValidateEndoso_EnGarantia(t *testing.T) {
	lv := NewLCCHValidator()
	// con endosatario (acreedor pignoraticio) es válido — art. 22
	e := models.Endoso{
		Tipo:        "en_garantia",
		Endosante:   models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Endosatario: &models.Persona{Nombre: "Banco", NIF: "B12345678"},
		Fecha:       "2026-05-01",
	}
	assert.True(t, lv.ValidateEndoso(&e).Valid)

	// sin endosatario debe fallar
	sinEndosatario := models.Endoso{
		Tipo:      "en_garantia",
		Endosante: models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Fecha:     "2026-05-01",
	}
	assert.False(t, lv.ValidateEndoso(&sinEndosatario).Valid)
}

func TestValidateEndoso_ClausulaSinGastos(t *testing.T) {
	lv := NewLCCHValidator()
	e := models.Endoso{
		Tipo:        "en_propiedad",
		Endosante:   models.Persona{Nombre: "Ana", NIF: "12345678Z"},
		Endosatario: &models.Persona{Nombre: "Bea", NIF: "87654321X"},
		Fecha:       "2026-05-01",
		Clausula:    "sin_gastos",
	}
	assert.True(t, lv.ValidateEndoso(&e).Valid, "la cláusula sin_gastos (art. 56) debe ser válida")
}

func TestIsPrescrito(t *testing.T) {
	lv := NewLCCHValidator()
	assert.False(t, lv.IsPrescrito(mustParseDate("2026-01-01")))
	assert.True(t, lv.IsPrescrito(mustParseDate("2020-01-01")))
}

func mustParseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

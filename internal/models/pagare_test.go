package models

import (
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
)

func setupValidator(t *testing.T) *validator.Validate {
	t.Helper()
	v := validator.New()
	RegisterCustomValidations(v)
	return v
}

func TestPagareElectronico_Valid(t *testing.T) {
	v := setupValidator(t)
	p := PagareElectronico{
		IDPagare:         "urn:pagare:test-001",
		Denominacion:     "PAGARÉ",
		PromesaPago:      true,
		Importe:          1000.50,
		Moneda:           "EUR",
		Vencimiento:      Vencimiento{Tipo: "fecha_fija", Fecha: "2026-12-31"},
		LocalidadPago:    "Madrid",
		Beneficiario:     Persona{Nombre: "Juan", Apellido: "García López", NIF: "12345678Z"},
		LocalidadEmision: "Barcelona",
		FechaEmision:     "2026-04-10",
		Firmante: Firmante{
			Nombre:          "María",
			NIF:             "87654321X",
			DireccionPostal: DireccionPostal{Direccion: "Calle Mayor 1", Localidad: "Barcelona", CodigoPostal: "08001", Pais: "ES"},
		},
	}
	err := v.Struct(p)
	assert.NoError(t, err)
}

func TestPagareElectronico_MissingFields(t *testing.T) {
	v := setupValidator(t)
	p := PagareElectronico{}
	err := v.Struct(p)
	assert.Error(t, err)
}

func TestPagareElectronico_PromesaPagoFalse(t *testing.T) {
	v := setupValidator(t)
	p := PagareElectronico{
		Denominacion: "PAGARÉ",
		PromesaPago:  false,
		Moneda:       "EUR",
		Vencimiento:  Vencimiento{Tipo: "fecha_fija"},
		Beneficiario: Persona{Nombre: "Test", NIF: "12345678Z"},
		Firmante: Firmante{
			Nombre: "Test", NIF: "12345678Z",
			DireccionPostal: DireccionPostal{Direccion: "Calle 1", Localidad: "Madrid", CodigoPostal: "28001", Pais: "ES"},
		},
	}
	err := v.Struct(p)
	assert.Error(t, err)
}

func TestVencimiento_Tipos(t *testing.T) {
	v := setupValidator(t)
	validTypes := []string{"fecha_fija", "a_la_vista"}
	for _, tipo := range validTypes {
		vc := Vencimiento{Tipo: tipo}
		err := v.Struct(vc)
		assert.NoError(t, err, "expected %s to be valid", tipo)
	}
}

func TestNIFValidation(t *testing.T) {
	v := setupValidator(t)
	tests := []struct {
		nif   string
		valid bool
	}{
		{"12345678Z", true},
		{"X1234567L", true},
		{"Y1234567X", true},
		{"Z1234567R", true},
		{"A12345678", true},
		{"1234567", false},
		{"ABCDEFG", false},
		{"", false},
	}
	for _, tt := range tests {
		p := Persona{Nombre: "Test", NIF: tt.nif}
		err := v.Struct(p)
		if tt.valid {
			assert.NoError(t, err, "expected %s to be valid", tt.nif)
		} else {
			assert.Error(t, err, "expected %s to be invalid", tt.nif)
		}
	}
}

func TestDireccionPostal_Pais(t *testing.T) {
	v := setupValidator(t)
	dp := DireccionPostal{Direccion: "Calle 1", Localidad: "Madrid", CodigoPostal: "28001", Pais: "ES"}
	err := v.Struct(dp)
	assert.NoError(t, err)

	dp.Pais = "ESP"
	err = v.Struct(dp)
	assert.Error(t, err)
}

func TestEndoso_Valid(t *testing.T) {
	v := setupValidator(t)
	e := Endoso{
		Tipo:      "en_propiedad",
		Endosante: Persona{Nombre: "Juan", NIF: "12345678Z"},
		Fecha:     "2026-04-10T12:00:00Z",
	}
	err := v.Struct(e)
	assert.NoError(t, err)
}

func TestEndoso_Tipos(t *testing.T) {
	v := setupValidator(t)
	tipos := []string{"en_propiedad", "en_procuracion", "en_blanco"}
	for _, tipo := range tipos {
		e := Endoso{Tipo: tipo, Endosante: Persona{Nombre: "T", NIF: "12345678Z"}, Fecha: "2026-01-01"}
		err := v.Struct(e)
		assert.NoError(t, err, "expected %s to be valid", tipo)
	}
}

func TestEndoso_InvalidTipo(t *testing.T) {
	v := setupValidator(t)
	e := Endoso{Tipo: "invalido", Endosante: Persona{Nombre: "T", NIF: "12345678Z"}, Fecha: "2026-01-01"}
	err := v.Struct(e)
	assert.Error(t, err)
}

func TestMetadataPago_Valid(t *testing.T) {
	v := setupValidator(t)
	actions := []string{"PAGO", "ANULACION", "PRESCRIPCION"}
	for _, action := range actions {
		m := MetadataPago{Action: action}
		err := v.Struct(m)
		assert.NoError(t, err, "expected %s to be valid", action)
	}
}

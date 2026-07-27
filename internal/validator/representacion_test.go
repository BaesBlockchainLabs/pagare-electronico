package validator

import (
	"testing"

	"pagare/internal/models"

	"github.com/stretchr/testify/assert"
)

func pagareDeSociedad() models.PagareElectronico {
	p := validPagare()
	p.Firmante.Tipo = "juridica"
	p.Firmante.Nombre = "Ferretería Levante, S.L."
	p.Firmante.NIF = "B12345678"
	p.Firmante.Representante = &models.Representante{
		Nombre: "Luis", Apellido: "Server", NIF: "12345678Z",
		Cargo: "administrador único", Acreditacion: "copia autorizada",
		Referencia: "protocolo 1234",
	}
	return p
}

func erroresPor(result *ValidationResult, campo string) []ValidationError {
	var out []ValidationError
	for _, e := range result.Errors {
		if e.Campo == campo {
			out = append(out, e)
		}
	}
	return out
}

func TestValidateFirmante_SociedadCompletaEsValida(t *testing.T) {
	p := pagareDeSociedad()
	assert.True(t, NewLCCHValidator().ValidatePagare(&p).Valid)
}

// A company cannot sign; someone signs for it. Without saying who, the title
// does not say who bound the company.
func TestValidateFirmante_SociedadSinRepresentanteNoVale(t *testing.T) {
	p := pagareDeSociedad()
	p.Firmante.Representante = nil

	result := NewLCCHValidator().ValidatePagare(&p)
	assert.False(t, result.Valid)
	errs := erroresPor(result, "Representante")
	assert.NotEmpty(t, errs)
	assert.Equal(t, "art. 9 LCCH", errs[0].ArticuloLCCH)
}

// Art. 9 requires the cargo to appear clearly in the antefirma, because that is
// what makes the company the obligado instead of the person signing.
func TestValidateFirmante_ElCargoEsObligatorio(t *testing.T) {
	p := pagareDeSociedad()
	p.Firmante.Representante.Cargo = ""

	result := NewLCCHValidator().ValidatePagare(&p)
	assert.False(t, result.Valid)
	assert.NotEmpty(t, erroresPor(result, "Representante.Cargo"))
}

func TestValidateFirmante_RepresentanteSinNIFNoVale(t *testing.T) {
	p := pagareDeSociedad()
	p.Firmante.Representante.NIF = ""

	assert.False(t, NewLCCHValidator().ValidatePagare(&p).Valid)
}

// Signing for another without a poder binds the signer personally, so the
// absence is worth flagging — but it does not make the pagaré invalid, and the
// platform is in no position to verify the poder anyway.
func TestValidateFirmante_SinAcreditacionAvisaPeroNoInvalida(t *testing.T) {
	p := pagareDeSociedad()
	p.Firmante.Representante.Acreditacion = ""
	p.Firmante.Representante.Referencia = ""

	result := NewLCCHValidator().ValidatePagare(&p)
	assert.True(t, result.Valid, "la falta de acreditación no priva de validez al título")
	avisos := erroresPor(result, "Representante.Acreditacion")
	assert.NotEmpty(t, avisos)
	assert.Contains(t, avisos[0].Mensaje, "obligado personalmente")
}

func TestValidateFirmante_PersonaFisicaConRepresentanteEsIncoherente(t *testing.T) {
	p := validPagare()
	p.Firmante.Representante = &models.Representante{
		Nombre: "Luis", NIF: "12345678Z", Cargo: "apoderado",
	}

	result := NewLCCHValidator().ValidatePagare(&p)
	assert.NotEmpty(t, erroresPor(result, "Representante"))
}

func TestValidateFirmante_PersonaFisicaSigueSiendoValida(t *testing.T) {
	p := validPagare()
	result := NewLCCHValidator().ValidatePagare(&p)
	assert.True(t, result.Valid)
	assert.Empty(t, erroresPor(result, "Representante"))
}

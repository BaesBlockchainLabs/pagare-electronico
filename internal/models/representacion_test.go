package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Adding the company fields must leave the canonical form of a pagaré that
// does not use them byte-for-byte identical, or every signature already issued
// would stop validating and the titles would read as tampered with.
func TestCanonical_LaPersonaFisicaNoCambiaDeForma(t *testing.T) {
	p := canonicalPagare()
	actual, err := CanonicalJSON(&p)
	require.NoError(t, err)

	// Forma canónica tal como era antes de existir tipo ni representante.
	esperada := map[string]interface{}{
		"denominacion": p.Denominacion,
		"promesa_pago": p.PromesaPago,
		"importe":      p.Importe,
		"moneda":       p.Moneda,
		"vencimiento": map[string]interface{}{
			"tipo": p.Vencimiento.Tipo, "fecha": p.Vencimiento.Fecha,
		},
		"localidad_pago": p.LocalidadPago,
		"beneficiario": map[string]interface{}{
			"nombre": p.Beneficiario.Nombre, "apellido": p.Beneficiario.Apellido, "nif": p.Beneficiario.NIF,
		},
		"localidad_emision": p.LocalidadEmision,
		"fecha_emision":     p.FechaEmision,
		"firmante": map[string]interface{}{
			"nombre": p.Firmante.Nombre, "apellido": p.Firmante.Apellido, "nif": p.Firmante.NIF,
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
	previa, err := json.Marshal(esperada)
	require.NoError(t, err)

	assert.Equal(t, string(previa), string(actual),
		"la forma canónica del pagaré de persona física no debe haber cambiado")
}

func TestCanonical_LaRepresentacionEntraEnLaFirma(t *testing.T) {
	base := canonicalPagare()
	sinRep, err := CanonicalJSON(&base)
	require.NoError(t, err)

	casos := map[string]func(*PagareElectronico){
		"tipo": func(p *PagareElectronico) { p.Firmante.Tipo = "juridica" },
		"representante": func(p *PagareElectronico) {
			p.Firmante.Representante = &Representante{Nombre: "Luis", NIF: "12345678Z", Cargo: "apoderado"}
		},
		"cargo": func(p *PagareElectronico) {
			p.Firmante.Representante = &Representante{Nombre: "Luis", NIF: "12345678Z", Cargo: "administrador único"}
		},
	}
	for nombre, alterar := range casos {
		t.Run(nombre, func(t *testing.T) {
			p := canonicalPagare()
			alterar(&p)
			con, err := CanonicalJSON(&p)
			require.NoError(t, err)
			assert.NotEqual(t, string(sinRep), string(con))
		})
	}
}

// The verifier rebuilds the canonical form from what the ledger stored, so a
// company pagaré must survive the round trip like any other.
func TestCanonical_LaSociedadSobreviveElViajePorElLedger(t *testing.T) {
	p := canonicalPagare()
	p.Firmante.Tipo = "juridica"
	p.Firmante.Nombre = "Ferretería Levante, S.L."
	p.Firmante.NIF = "B12345678"
	p.Firmante.Representante = &Representante{
		Nombre: "Luis", Apellido: "Server", NIF: "12345678Z",
		Cargo: "administrador único", Acreditacion: "copia autorizada",
		Referencia: "protocolo 1234", Fecha: "2025-03-10",
	}
	original, err := CanonicalJSON(&p)
	require.NoError(t, err)

	stored := CanonicalContent(&p)
	stored["type"] = "pagare_electronico"
	stored["from"] = "una-clave"
	raw, err := json.Marshal(stored)
	require.NoError(t, err)

	var back PagareElectronico
	require.NoError(t, json.Unmarshal(raw, &back))
	recomputed, err := CanonicalJSON(&back)
	require.NoError(t, err)

	assert.Equal(t, string(original), string(recomputed))
	assert.True(t, back.Firmante.EsPersonaJuridica())
	assert.Equal(t, "administrador único", back.Firmante.Representante.Cargo)
}

package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func canonicalPagare() PagareElectronico {
	return PagareElectronico{
		IDPagare:         "urn:pagare:test-canon",
		Denominacion:     "PAGARÉ",
		PromesaPago:      true,
		Importe:          1234.56,
		Moneda:           "EUR",
		Vencimiento:      Vencimiento{Tipo: "fecha_fija", Fecha: "2027-01-15"},
		LocalidadPago:    "Alicante",
		Beneficiario:     Persona{Nombre: "Ana", Apellido: "López", NIF: "12345678Z"},
		LocalidadEmision: "Barcelona",
		FechaEmision:     "2026-04-10",
		Firmante: Firmante{
			Nombre:          "Carlos",
			NIF:             "87654321X",
			DireccionPostal: DireccionPostal{Direccion: "Calle Mayor 5", Localidad: "Barcelona", CodigoPostal: "08001", Pais: "ES"},
		},
	}
}

func TestCanonicalJSON_EsDeterminista(t *testing.T) {
	p := canonicalPagare()
	first, err := CanonicalJSON(&p)
	require.NoError(t, err)

	for i := 0; i < 50; i++ {
		again, err := CanonicalJSON(&p)
		require.NoError(t, err)
		require.Equal(t, string(first), string(again), "iteración %d", i)
	}
}

// The signature is verified by recomputing the canonical form from what the
// ledger stored, so a round trip through JSON must be a fixed point.
func TestCanonicalJSON_SobreviveElViajePorElLedger(t *testing.T) {
	p := canonicalPagare()
	original, err := CanonicalJSON(&p)
	require.NoError(t, err)

	stored := CanonicalContent(&p)
	stored["type"] = "pagare_electronico"
	// Fields the ledger adds on its own, which must not disturb the result.
	stored["app"] = "pagare"
	stored["from"] = "EiCPZeX3AzzJ48hNsJVjZnUhwBoxktLuLPYxecVra12d"
	stored["namespace"] = "test"
	stored["token"] = false
	stored["created_at"] = 1785156671724
	if firmante, ok := stored["firmante"].(map[string]interface{}); ok {
		firmante["firma_digital"] = "una-firma-cualquiera"
	}

	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	var back PagareElectronico
	require.NoError(t, json.Unmarshal(raw, &back))

	recomputed, err := CanonicalJSON(&back)
	require.NoError(t, err)
	assert.Equal(t, string(original), string(recomputed))
}

func TestCanonicalJSON_ExcluyeLaPropiaFirma(t *testing.T) {
	p := canonicalPagare()
	sinFirma, err := CanonicalJSON(&p)
	require.NoError(t, err)

	p.Firmante.FirmaDigital = "firma-que-no-debe-contar"
	conFirma, err := CanonicalJSON(&p)
	require.NoError(t, err)

	assert.Equal(t, string(sinFirma), string(conFirma))
}

// A private key reaching the ledger would be unrecoverable, so it must not be
// part of what gets signed either.
func TestCanonicalJSON_ExcluyeLaIdentidadBlockchain(t *testing.T) {
	p := canonicalPagare()
	sin, err := CanonicalJSON(&p)
	require.NoError(t, err)

	p.Firmante.IdentidadBlockchain = &IdentidadBC{Pub: "una-pub", Pvt: "una-pvt"}
	con, err := CanonicalJSON(&p)
	require.NoError(t, err)

	assert.Equal(t, string(sin), string(con))
	assert.NotContains(t, string(con), "una-pvt")
}

func TestCanonicalJSON_CambiaConCadaMencionDelArt94(t *testing.T) {
	base := canonicalPagare()
	baseJSON, err := CanonicalJSON(&base)
	require.NoError(t, err)

	casos := map[string]func(*PagareElectronico){
		"denominación":       func(p *PagareElectronico) { p.Denominacion = "PAGARE" },
		"promesa de pago":    func(p *PagareElectronico) { p.PromesaPago = false },
		"importe":            func(p *PagareElectronico) { p.Importe = 1234.57 },
		"moneda":             func(p *PagareElectronico) { p.Moneda = "USD" },
		"vencimiento":        func(p *PagareElectronico) { p.Vencimiento.Fecha = "2027-01-16" },
		"localidad de pago":  func(p *PagareElectronico) { p.LocalidadPago = "Elche" },
		"beneficiario":       func(p *PagareElectronico) { p.Beneficiario.NIF = "87654321X" },
		"localidad emisión":  func(p *PagareElectronico) { p.LocalidadEmision = "Madrid" },
		"fecha de emisión":   func(p *PagareElectronico) { p.FechaEmision = "2026-04-11" },
		"firmante":           func(p *PagareElectronico) { p.Firmante.NIF = "12345678Z" },
		"dirección firmante": func(p *PagareElectronico) { p.Firmante.DireccionPostal.Localidad = "Valencia" },
		"no a la orden":      func(p *PagareElectronico) { p.NoALaOrden = true },
		"aval": func(p *PagareElectronico) {
			p.Aval = &Aval{Avalista: Persona{Nombre: "Avalista", NIF: "11111111H"}, Alcance: "total"}
		},
		"cláusulas": func(p *PagareElectronico) { p.Clausulas = []string{"sin gastos"} },
	}

	for nombre, alterar := range casos {
		t.Run(nombre, func(t *testing.T) {
			p := canonicalPagare()
			alterar(&p)
			alterado, err := CanonicalJSON(&p)
			require.NoError(t, err)
			assert.NotEqual(t, string(baseJSON), string(alterado),
				"alterar %s debe cambiar la forma canónica", nombre)
		})
	}
}

// The identifier is not a mención of art. 94 and the ledger does not store it,
// so it stays out of the signature.
func TestCanonicalJSON_IgnoraElIdentificador(t *testing.T) {
	p := canonicalPagare()
	uno, err := CanonicalJSON(&p)
	require.NoError(t, err)

	p.IDPagare = "urn:pagare:otro"
	dos, err := CanonicalJSON(&p)
	require.NoError(t, err)

	assert.Equal(t, string(uno), string(dos))
}

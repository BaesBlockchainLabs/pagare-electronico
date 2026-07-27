package apidocs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Los dos ficheros del repositorio deben cargarse: son los que la página sirve.
func TestCargar_LosSpecsDelRepositorio(t *testing.T) {
	casos := map[string]struct {
		ruta        string
		conteniendo string
	}{
		"nuestra API":       {"../../openapi.yaml", "/api/pagares"},
		"la API que usamos": {"../../openapi-bcf.yaml", "/asset"},
	}

	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			spec, err := Cargar(c.ruta)
			require.NoError(t, err)
			assert.NotEmpty(t, spec.Titulo)
			assert.NotEmpty(t, spec.Grupos)
			assert.Greater(t, spec.Rutas, 0)
			assert.Greater(t, spec.Esquemas, 0)

			var rutas []string
			for _, g := range spec.Grupos {
				for _, op := range g.Operaciones {
					rutas = append(rutas, op.Ruta)
				}
			}
			assert.Contains(t, rutas, c.conteniendo)
		})
	}
}

// La separación es el objeto del cambio: nuestra API no debe describir rutas de
// la red, ni la de la red rutas nuestras.
func TestCargar_LasDosApisNoSeMezclan(t *testing.T) {
	nuestra, err := Cargar("../../openapi.yaml")
	require.NoError(t, err)
	suya, err := Cargar("../../openapi-bcf.yaml")
	require.NoError(t, err)

	rutas := func(s *Spec) map[string]bool {
		out := map[string]bool{}
		for _, g := range s.Grupos {
			for _, op := range g.Operaciones {
				out[op.Ruta] = true
			}
		}
		return out
	}
	nuestras, suyas := rutas(nuestra), rutas(suya)

	for r := range nuestras {
		assert.True(t, len(r) > 4 && (r[:5] == "/api/" || r[:7] == "/admin/" || r == "/health"),
			"nuestra API no debería exponer %s", r)
	}
	for r := range suyas {
		assert.False(t, len(r) >= 5 && r[:5] == "/api/",
			"la API de la red no debería incluir la ruta nuestra %s", r)
	}
}

// Las operaciones públicas deben distinguirse: son las que un tercero puede
// llamar sin cuenta, y la verificación del pagaré es justamente una de ellas.
func TestCargar_DistingueLoPublico(t *testing.T) {
	spec, err := Cargar("../../openapi.yaml")
	require.NoError(t, err)

	publicas := map[string]bool{}
	for _, g := range spec.Grupos {
		for _, op := range g.Operaciones {
			if op.Publica {
				publicas[op.Ruta] = true
			}
		}
	}
	assert.True(t, publicas["/api/pagares/public"],
		"la verificación pública debe poder usarse sin autenticación")
	assert.True(t, publicas["/health"])
	assert.False(t, publicas["/api/pagares"],
		"emitir no puede ser público")
}

func TestCargar_FicheroInexistente(t *testing.T) {
	_, err := Cargar("no-existe.yaml")
	assert.Error(t, err)
}

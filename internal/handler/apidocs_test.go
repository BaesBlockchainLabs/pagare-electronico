package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pedirDocs(t *testing.T, specs ...string) *httptest.ResponseRecorder {
	t.Helper()
	ph := NewPageHandler(false)
	ph.SetSpecs(specs...)
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	w := httptest.NewRecorder()
	ph.ApiDocs(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	return w
}

// La página se sirve sin sesión: describe la interfaz, no datos de nadie.
func TestApiDocs_RenderizaLasDosEspecificaciones(t *testing.T) {
	body := pedirDocs(t, "../../openapi.yaml", "../../openapi-bcf.yaml").Body.String()

	assert.Contains(t, body, "Pagaré Electrónico")
	assert.Contains(t, body, "BlockchainFUE")
	assert.Contains(t, body, "/api/pagares")
	assert.Contains(t, body, "/asset")
	assert.Contains(t, body, "Pública", "las operaciones sin autenticación deben distinguirse")
}

// Sin las especificaciones la página debe decirlo, no romperse.
func TestApiDocs_SinFicherosAvisa(t *testing.T) {
	body := pedirDocs(t, "no-existe.yaml").Body.String()
	assert.Contains(t, body, "no se pudo leer")
}

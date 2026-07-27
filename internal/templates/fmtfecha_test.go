package templates

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Las funciones de fecha viven en el JavaScript de base.templ, así que el test
// las extrae del propio fichero y las ejecuta con node. Copiarlas aquí las
// dejaría a merced de divergir de las que realmente se sirven, que es
// precisamente lo que no queremos comprobar.
func TestFmtFecha(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("sin node: no se puede ejecutar el JavaScript de las plantillas")
	}

	fuente, err := os.ReadFile("base.templ")
	require.NoError(t, err)

	funciones := regexp.MustCompile(`(?s)window\.(fmtFecha|fmtDMY|fmtFechaHora) = function.*?\n\t\t\};`)
	extraidas := funciones.FindAllString(string(fuente), -1)
	require.Len(t, extraidas, 3, "esperaba fmtFecha, fmtDMY y fmtFechaHora en base.templ")

	casos := []struct {
		nombre   string
		entrada  string
		esperado string
	}{
		// El caso que motivó el arreglo: la red devuelve el sello como número.
		{"epoch en milisegundos (número)", "1785156671724", "27/07/2026"},
		{"epoch en milisegundos (cadena)", "'1785156671724'", "27/07/2026"},
		{"epoch en segundos", "1785156671", "27/07/2026"},
		// Fecha pura: sin conversión de zona, para no perder un día.
		{"fecha ISO", "'2026-04-10'", "10/04/2026"},
		{"fecha con hora", "'2026-04-10T15:30:00Z'", "10/04/2026"},
		{"vacío", "''", "—"},
		{"nulo", "null", "—"},
		{"texto no interpretable", "'sin fecha'", "sin fecha"},
	}

	var script strings.Builder
	script.WriteString("var window = {};\n")
	for _, f := range extraidas {
		script.WriteString(f + "\n")
	}
	for _, c := range casos {
		script.WriteString("console.log(window.fmtFecha(" + c.entrada + "));\n")
	}

	tmp := filepath.Join(t.TempDir(), "fmtfecha.js")
	require.NoError(t, os.WriteFile(tmp, []byte(script.String()), 0o644))

	// Zona fija: fmtDMY usa los componentes locales de la fecha, así que sin
	// esto el resultado dependería de dónde corra el test.
	cmd := exec.Command(node, tmp)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	salida, err := cmd.CombinedOutput()
	require.NoError(t, err, string(salida))

	lineas := strings.Split(strings.TrimSpace(string(salida)), "\n")
	require.Len(t, lineas, len(casos))
	for i, c := range casos {
		assert.Equal(t, c.esperado, lineas[i], "caso %q", c.nombre)
	}
}

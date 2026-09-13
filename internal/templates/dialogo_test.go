package templates

import (
	"context"
	"strings"
	"testing"
)

// El diálogo del resultado tiene que estar en la página y bien formado: es lo
// único que le cuenta al emisor que falta su firma.
func TestNuevoPagare_DialogoDeResultado(t *testing.T) {
	var b strings.Builder
	if err := NuevoPagare(false, &CurrentUser{Username: "ana", Role: "user"}).Render(context.Background(), &b); err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := b.String()

	for _, pieza := range []string{
		`id="emitido-modal"`, `class="modal-overlay"`, `id="emitido-cuerpo"`,
		`id="ver-pagare"`, `onclick="otroPagare()"`, `Ir al panel`,
		`function muestraResultado`, `function otroPagare`,
	} {
		if !strings.Contains(html, pieza) {
			t.Errorf("falta %q", pieza)
		}
	}
	// Y el aviso de pie que sustituye no puede seguir ahí.
	if strings.Contains(html, `id="form-success"`) {
		t.Error("quedó el aviso de pie que el diálogo sustituye")
	}
	// Los estilos del modal viven en el layout compartido.
	if !strings.Contains(html, ".modal-overlay.open") {
		t.Error("el layout no trae los estilos del diálogo")
	}
	if strings.Count(html, `class="modal-overlay"`) != 1 {
		t.Errorf("diálogos = %d, se esperaba 1", strings.Count(html, `class="modal-overlay"`))
	}
}

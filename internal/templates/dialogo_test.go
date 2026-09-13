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
		`function muestraResultado`, `function otroPagare`,
		`window.dialogoResultado`, `Ir al panel`, `Emitir otro`,
	} {
		if !strings.Contains(html, pieza) {
			t.Errorf("falta %q", pieza)
		}
	}
	// Y el aviso de pie que sustituye no puede seguir ahí.
	if strings.Contains(html, `id="form-success"`) {
		t.Error("quedó el aviso de pie que el diálogo sustituye")
	}
	// El diálogo lo construye el layout compartido, así que la página no lleva
	// marcado propio: tenerlo significaría que hay dos implementaciones.
	if strings.Contains(html, `id="emitido-modal"`) {
		t.Error("quedó el diálogo propio de la página")
	}
	if !strings.Contains(html, ".modal-overlay.open") {
		t.Error("el layout no trae los estilos del diálogo")
	}
}

// Endoso y cesión contaban el resultado a pie de página, y el endoso además
// mentía: decía "endosado correctamente" cuando la firma estaba pendiente y la
// operación no había llegado al libro.
func TestEndosarYCeder_UsanElDialogo(t *testing.T) {
	casos := map[string]func() string{
		"endoso": func() string {
			var b strings.Builder
			Endosar(&CurrentUser{Username: "ana", Role: "user"}).Render(context.Background(), &b)
			return b.String()
		},
		"cesión": func() string {
			var b strings.Builder
			Ceder(&CurrentUser{Username: "ana", Role: "user"}).Render(context.Background(), &b)
			return b.String()
		},
	}
	for nombre, render := range casos {
		t.Run(nombre, func(t *testing.T) {
			html := render()
			if !strings.Contains(html, "window.dialogoResultado") {
				t.Error("no usa el diálogo compartido")
			}
			if !strings.Contains(html, "no surtirá efecto hasta que firmes") {
				t.Error("no dice que sin firma la operación no surte efecto")
			}
			if strings.Contains(html, "endosado correctamente") {
				t.Error("sigue afirmando que la operación se hizo sin comprobar la firma")
			}
			for _, muerto := range []string{`id="endoso-ok"`, `id="cesion-ok"`} {
				if strings.Contains(html, muerto) {
					t.Errorf("quedó %s, el aviso de pie que ya no se muestra", muerto)
				}
			}
		})
	}
}

// El paginador toma la página en curso de la que se pidió, no de
// count.pages.current: el libro devuelve ahí el índice del primer registro de
// la página, no su número. Con doce por página, la segunda contesta 13, y
// usarlo daba "Página 13 de 2" con el enlace anterior apuntando a la 12.
func TestDashboard_PaginadorNoUsaElIndiceDelLibro(t *testing.T) {
	var b strings.Builder
	if err := Dashboard(&CurrentUser{Username: "ana", Role: "user"}).Render(context.Background(), &b); err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := b.String()

	if !strings.Contains(html, "function paginador(count, actual)") {
		t.Error("el paginador tiene que recibir la página en curso, no deducirla de la respuesta")
	}
	if !strings.Contains(html, "paginador(data.count, PAGINA)") {
		t.Error("no se le está pasando la página que se pidió")
	}
	if strings.Contains(html, "var actual = p.current") {
		t.Error("vuelve a tomar la página del índice que devuelve el libro")
	}
	// Y el panel pide menos de los 25 por defecto: eran demasiadas tarjetas.
	if !strings.Contains(html, "var POR_PAGINA = 12") {
		t.Error("el panel tiene que acotar cuántas tarjetas caben en una página")
	}
	if !strings.Contains(html, "per_page=' + POR_PAGINA") {
		t.Error("no está pidiendo su propio tamaño de página")
	}
}

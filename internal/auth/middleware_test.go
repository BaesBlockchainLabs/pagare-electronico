package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func pide(t *testing.T, activo bool, p *Principal) int {
	t.Helper()
	llamado := false
	h := ExigirVerificacion(activo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llamado = true
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/pagares", nil)
	r = r.WithContext(ContextWithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code == http.StatusOK && !llamado {
		t.Fatal("respondió 200 sin llegar al handler")
	}
	return w.Code
}

// El equivalente para páginas manda a validar en vez de devolver JSON: un
// redirect es lo único que un navegador entiende aquí.
func TestExigirVerificacionPagina(t *testing.T) {
	casos := []struct {
		nombre    string
		activo    bool
		principal *Principal
		esperado  int
		destino   string
	}{
		{"sin verificación configurada no estorba", false,
			&Principal{UserID: "1", Role: RoleUser}, http.StatusOK, ""},
		{"usuario sin verificar va a validarse", true,
			&Principal{UserID: "1", Role: RoleUser}, http.StatusSeeOther, "/verificacion"},
		{"usuario verificado pasa", true,
			&Principal{UserID: "1", Role: RoleUser, Verificado: true}, http.StatusOK, ""},
		{"el administrador pasa", true,
			&Principal{UserID: "1", Role: RoleAdmin}, http.StatusOK, ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			h := ExigirVerificacionPagina(c.activo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			r := httptest.NewRequest(http.MethodGet, "/pagares/nuevo", nil)
			r = r.WithContext(ContextWithPrincipal(r.Context(), c.principal))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != c.esperado {
				t.Errorf("código = %d, se esperaba %d", w.Code, c.esperado)
			}
			if c.destino != "" && w.Header().Get("Location") != c.destino {
				t.Errorf("destino = %q, se esperaba %q", w.Header().Get("Location"), c.destino)
			}
		})
	}
}

func TestExigirVerificacion(t *testing.T) {
	casos := []struct {
		nombre    string
		activo    bool
		principal *Principal
		esperado  int
	}{
		{"sin verificación configurada no estorba", false,
			&Principal{UserID: "1", Role: RoleUser}, http.StatusOK},
		{"usuario sin verificar queda bloqueado", true,
			&Principal{UserID: "1", Role: RoleUser}, http.StatusForbidden},
		{"usuario verificado pasa", true,
			&Principal{UserID: "1", Role: RoleUser, Verificado: true}, http.StatusOK},
		{"el administrador pasa para poder desatascar", true,
			&Principal{UserID: "1", Role: RoleAdmin}, http.StatusOK},
		// Sin sesión no es cosa de este guardia: lo resuelve quien exige
		// autenticación, más abajo.
		{"sin sesión se deja pasar al handler", true, nil, http.StatusOK},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := pide(t, c.activo, c.principal); got != c.esperado {
				t.Errorf("código = %d, se esperaba %d", got, c.esperado)
			}
		})
	}
}

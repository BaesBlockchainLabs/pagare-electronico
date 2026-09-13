package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pideContacto(t *testing.T, s *Store, userID, cuerpo string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	h := NewHandlers(s, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/contacto", strings.NewReader(cuerpo))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(ContextWithPrincipal(r.Context(),
		&Principal{UserID: userID, Username: "antiguo", Role: RoleUser}))
	w := httptest.NewRecorder()
	h.ActualizarContacto(w, r)

	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

// Una cuenta anterior a la validación no tiene correo ni móvil, así que no
// puede ni empezar. Completarlos no puede costarle el resto de su perfil.
func TestActualizarContacto_NoBorraElRestoDelPerfil(t *testing.T) {
	s := newTestStore(t)
	u := &User{
		Username: "antiguo", Role: RoleUser,
		Nombre: "Carlos", Apellido: "Ruiz", NIF: "87654321X",
		Direccion: "Calle Mayor 5", Localidad: "Alicante", Pais: "ES",
		DisplayName: "Carlos Ruiz",
	}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatal(err)
	}
	if faltan := FaltaEnContacto(u); len(faltan) != 3 {
		t.Fatalf("falta = %v, se esperaban los tres", faltan)
	}

	w, res := pideContacto(t, s, u.ID,
		`{"email":"carlos@example.com","telefono":"+34600000000","codigo_postal":"03001"}`)
	if w.Code != http.StatusOK || res["ok"] != true {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "carlos@example.com" || got.Telefono != "+34600000000" || got.CodigoPostal != "03001" {
		t.Errorf("contacto = %q / %q / %q", got.Email, got.Telefono, got.CodigoPostal)
	}
	// Lo que no se tocaba no puede haberse perdido: usar UpdateProfile para
	// esto lo habría borrado.
	for _, c := range []struct{ campo, obtenido, esperado string }{
		{"nombre", got.Nombre, "Carlos"},
		{"apellido", got.Apellido, "Ruiz"},
		{"nif", got.NIF, "87654321X"},
		{"dirección", got.Direccion, "Calle Mayor 5"},
		{"localidad", got.Localidad, "Alicante"},
		{"display_name", got.DisplayName, "Carlos Ruiz"},
	} {
		if c.obtenido != c.esperado {
			t.Errorf("%s = %q, se esperaba %q", c.campo, c.obtenido, c.esperado)
		}
	}
	if faltan := FaltaEnContacto(got); len(faltan) != 0 {
		t.Errorf("todavía falta %v", faltan)
	}
}

func TestActualizarContacto_Validaciones(t *testing.T) {
	casos := map[string]string{
		"sin email":      `{"email":"","telefono":"+34600000000","codigo_postal":"03001"}`,
		"email inválido": `{"email":"no-es-un-email","telefono":"+34600000000","codigo_postal":"03001"}`,
		"sin móvil":      `{"email":"c@example.com","telefono":"","codigo_postal":"03001"}`,
		"sin CP":         `{"email":"c@example.com","telefono":"+34600000000","codigo_postal":""}`,
		"CP con letras":  `{"email":"c@example.com","telefono":"+34600000000","codigo_postal":"0300A"}`,
	}
	for nombre, cuerpo := range casos {
		t.Run(nombre, func(t *testing.T) {
			s := newTestStore(t)
			u := &User{Username: "antiguo", Role: RoleUser}
			if err := s.CreateUser(u, "secreto"); err != nil {
				t.Fatal(err)
			}
			w, _ := pideContacto(t, s, u.ID, cuerpo)
			if w.Code != http.StatusBadRequest {
				t.Errorf("código = %d, se esperaba 400: %s", w.Code, w.Body.String())
			}
			got, _ := s.GetByID(u.ID)
			if got.Email != "" || got.Telefono != "" || got.CodigoPostal != "" {
				t.Errorf("una petición rechazada no puede guardar nada: %+v", got)
			}
		})
	}
}

func TestActualizarContacto_SinSesion(t *testing.T) {
	s := newTestStore(t)
	h := NewHandlers(s, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/contacto", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ActualizarContacto(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("código = %d, se esperaba 401", w.Code)
	}
}

// El estado de la validación dice qué falta, que es de lo que tira la pantalla
// para pedirlo en vez de mandar al usuario al perfil.
func TestEstadoVerificacion_DiceQueFalta(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "antiguo", Role: RoleUser, Email: "c@example.com"}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(s, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/auth/verificacion", nil)
	r = r.WithContext(ContextWithPrincipal(r.Context(),
		&Principal{UserID: u.ID, Username: "antiguo", Role: RoleUser}))
	w := httptest.NewRecorder()
	h.EstadoVerificacionHandler(w, r)

	var res struct {
		OK     bool     `json:"ok"`
		Faltan []string `json:"faltan"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("respuesta = %s", w.Body.String())
	}
	// Tiene correo, así que sólo le faltan los otros dos.
	if len(res.Faltan) != 2 || res.Faltan[0] != "movil" || res.Faltan[1] != "codigo_postal" {
		t.Errorf("faltan = %v, se esperaba [movil codigo_postal]", res.Faltan)
	}
}

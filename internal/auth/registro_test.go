package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pideAlta(t *testing.T, s *Store, cuerpo string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	h := NewHandlers(s, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(cuerpo))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, r)

	var res map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &res)
	return w, res
}

// El código postal se pide en el alta porque el chip del DNI no lo lleva: sin
// él, todo usuario verificado se atasca al llegar al formulario del pagaré con
// un campo obligatorio, de sólo lectura y vacío.
func TestRegister_GuardaElCodigoPostal(t *testing.T) {
	s := newTestStore(t)
	w, res := pideAlta(t, s, `{"username":"ana","password":"secreto","email":"ana@example.com",
		"telefono":"+34600000000","codigo_postal":"03640"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	if res["ok"] != true {
		t.Fatalf("respuesta = %v", res)
	}

	u, err := s.GetByID(buscaID(t, s, "ana"))
	if err != nil {
		t.Fatal(err)
	}
	if u.CodigoPostal != "03640" {
		t.Errorf("código postal = %q", u.CodigoPostal)
	}
	if u.Email != "ana@example.com" || u.Telefono != "+34600000000" {
		t.Errorf("contacto = %q / %q", u.Email, u.Telefono)
	}
	// Y nada de lo que aporta el DNI se inventa en el alta.
	if u.NIF != "" || u.Nombre != "" || u.Direccion != "" {
		t.Errorf("el alta no puede rellenar lo que trae el DNI: %+v", u)
	}
}

// Un código postal que no lo es se rechaza en el alta, no más tarde.
func TestRegister_CodigoPostalInvalido(t *testing.T) {
	casos := map[string]string{
		"vacío":        "",
		"pocas cifras": "364",
		"con letras":   "0364A",
		"demasiadas":   "036400",
		"con espacios": " 03640 x",
	}
	for nombre, cp := range casos {
		t.Run(nombre, func(t *testing.T) {
			s := newTestStore(t)
			cuerpo := `{"username":"ana","password":"secreto","email":"ana@example.com",
				"telefono":"+34600000000","codigo_postal":"` + cp + `"}`
			w, res := pideAlta(t, s, cuerpo)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("código = %d, se esperaba 400: %s", w.Code, w.Body.String())
			}
			if msg, _ := res["msg"].(string); !strings.Contains(msg, "código postal") {
				t.Errorf("el mensaje tiene que explicar qué falta: %q", msg)
			}
			// Y no puede haber quedado usuario a medias.
			if len(s.List()) != 0 {
				t.Error("un alta rechazada no puede crear el usuario")
			}
		})
	}
}

// Los espacios alrededor no invalidan un código postal bueno.
func TestRegister_CodigoPostalConEspacios(t *testing.T) {
	s := newTestStore(t)
	w, _ := pideAlta(t, s, `{"username":"ana","password":"secreto","email":"ana@example.com",
		"telefono":"+34600000000","codigo_postal":"  03640  "}`)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}
	u, _ := s.GetByID(buscaID(t, s, "ana"))
	if u.CodigoPostal != "03640" {
		t.Errorf("código postal = %q, se esperaba sin espacios", u.CodigoPostal)
	}
}

// El código postal sobrevive a la verificación: el DNI no lo trae, así que no
// puede borrarlo. Es la garantía que hace útil pedirlo en el alta.
func TestRegister_ElCodigoPostalSobreviveAlDNI(t *testing.T) {
	s := newTestStore(t)
	if w, _ := pideAlta(t, s, `{"username":"ana","password":"secreto","email":"ana@example.com",
		"telefono":"+34600000000","codigo_postal":"03640"}`); w.Code != http.StatusOK {
		t.Fatalf("alta: %d", w.Code)
	}
	id := buscaID(t, s, "ana")

	v := &Verificacion{UserID: id, Referencia: id}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolverVerificacion(v.ID, id, *v, CamposIdentidad{
		NIF: "22133609L", Nombre: "RAMON", Apellido: "MARTINEZ",
		Direccion: "POL. NO EL BULL 25", Localidad: "MONOVAR",
		Provincia: "ALICANTE/ALACANT", Pais: "ES",
	}); err != nil {
		t.Fatal(err)
	}

	u, _ := s.GetByID(id)
	if u.CodigoPostal != "03640" {
		t.Errorf("código postal = %q: la verificación lo borró", u.CodigoPostal)
	}
	// Y ya tiene todo lo que el formulario del pagaré exige del firmante.
	for _, c := range []struct{ campo, valor string }{
		{"nombre", u.Nombre}, {"nif", u.NIF}, {"dirección", u.Direccion},
		{"localidad", u.Localidad}, {"código postal", u.CodigoPostal}, {"país", u.Pais},
	} {
		if c.valor == "" {
			t.Errorf("falta %s: no podría emitir", c.campo)
		}
	}
}

func buscaID(t *testing.T, s *Store, username string) string {
	t.Helper()
	for _, u := range s.List() {
		if u.Username == username {
			return u.ID
		}
	}
	t.Fatalf("no se creó el usuario %q", username)
	return ""
}

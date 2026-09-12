package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// El perfil llega en JSON con claves de guion bajo. Sin etiquetas en
// ProfileInput, encoding/json las descartaba —empareja sin mirar mayúsculas,
// pero los guiones bajos sí cuentan— y como aquí se asigna sin condiciones, el
// guardado borraba el valor en lugar de dejarlo como estaba.
func TestUpdateProfile_GuardaLasClavesConGuionBajo(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "rampa", Role: RoleUser}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(s, nil)

	cuerpo := `{"display_name":"Ramón Martínez","nombre":"RAMON","apellido":"MARTINEZ",
		"nif":"22133609L","email":"r@example.com","telefono":"+34600000000",
		"direccion":"Calle Mayor 1","localidad":"Monóvar","provincia":"Alicante",
		"codigo_postal":"03640","pais":"ES"}`
	r := httptest.NewRequest(http.MethodPost, "/api/perfil", strings.NewReader(cuerpo))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(ContextWithPrincipal(r.Context(),
		&Principal{UserID: u.ID, Username: "rampa", Role: RoleUser}))
	w := httptest.NewRecorder()

	h.UpdateProfile(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", w.Code, w.Body.String())
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ campo, obtenido, esperado string }{
		{"código postal", got.CodigoPostal, "03640"},
		{"display_name", got.DisplayName, "Ramón Martínez"},
		{"provincia", got.Provincia, "Alicante"},
		{"localidad", got.Localidad, "Monóvar"},
		{"dirección", got.Direccion, "Calle Mayor 1"},
	} {
		if c.obtenido != c.esperado {
			t.Errorf("%s = %q, se esperaba %q", c.campo, c.obtenido, c.esperado)
		}
	}
}

// Un usuario verificado puede completar el código postal, que su DNI no trae,
// sin que se le toquen los datos del documento.
func TestUpdateProfile_VerificadoCompletaElCodigoPostal(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "verificado", Role: RoleUser}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatal(err)
	}
	v := &Verificacion{UserID: u.ID, Referencia: u.ID}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolverVerificacion(v.ID, u.ID, *v, CamposIdentidad{
		NIF: "22133609L", Nombre: "RAMON", Apellido: "MARTINEZ PALOMARES",
		Direccion: "POL. NO EL BULL 25", Localidad: "MONOVAR", Pais: "ES",
	}); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(s, nil)
	r := httptest.NewRequest(http.MethodPost, "/api/perfil",
		strings.NewReader(`{"nombre":"Otro","nif":"99999999R","codigo_postal":"03640",
			"direccion":"POL. NO EL BULL 25","localidad":"MONOVAR","pais":"ES",
			"email":"r@example.com"}`))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(ContextWithPrincipal(r.Context(),
		&Principal{UserID: u.ID, Username: "verificado", Role: RoleUser, Verificado: true}))
	w := httptest.NewRecorder()
	h.UpdateProfile(w, r)

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CodigoPostal != "03640" {
		t.Errorf("código postal = %q: sin poder completarlo no se puede emitir un pagaré", got.CodigoPostal)
	}
	if got.NIF != "22133609L" || got.Nombre != "RAMON" {
		t.Errorf("los datos del DNI se reescribieron: nif=%q nombre=%q", got.NIF, got.Nombre)
	}
}

// Y el perfil que se devuelve tiene que llevar el código postal, o el
// formulario del pagaré no lo puede prerrellenar.
func TestProfile_DevuelveElCodigoPostal(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "rampa", Role: RoleUser, CodigoPostal: "03640", Provincia: "Alicante"}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(s, nil)

	r := httptest.NewRequest(http.MethodGet, "/api/perfil", nil)
	r = r.WithContext(ContextWithPrincipal(r.Context(),
		&Principal{UserID: u.ID, Username: "rampa", Role: RoleUser}))
	w := httptest.NewRecorder()
	h.Profile(w, r)

	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res["codigo_postal"] != "03640" {
		t.Errorf("codigo_postal = %v", res["codigo_postal"])
	}
	if res["provincia"] != "Alicante" {
		t.Errorf("provincia = %v", res["provincia"])
	}
}

package auth

import (
	"errors"
	"testing"
)

func usuarioSinVerificar(t *testing.T, s *Store, nombre string) *User {
	t.Helper()
	u := &User{Username: nombre, Role: RoleUser, Email: nombre + "@example.com", Telefono: "+34600000000"}
	if err := s.CreateUser(u, "secreto"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u
}

func TestVerificacion_AltaNaceSinVerificar(t *testing.T) {
	s := newTestStore(t)
	u := usuarioSinVerificar(t, s, "ana")

	estado, err := s.EstadoVerificacionDe(u.ID)
	if err != nil {
		t.Fatalf("EstadoVerificacionDe: %v", err)
	}
	if estado != VerificacionNoIniciada {
		t.Errorf("estado = %q, se esperaba vacío", estado)
	}

	p, err := s.Authenticate("ana", "secreto")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if p.Verificado {
		t.Error("una cuenta recién creada no puede estar verificada")
	}

	if _, err := s.UltimaVerificacion(u.ID); !errors.Is(err, ErrVerificacionNoEncontrada) {
		t.Errorf("se esperaba ErrVerificacionNoEncontrada, se obtuvo %v", err)
	}
}

// Una verificación superada fija en el usuario los datos del documento y deja
// al principal habilitado para operar.
func TestVerificacion_ResolverVuelcaLosDatosDelDNI(t *testing.T) {
	s := newTestStore(t)
	u := usuarioSinVerificar(t, s, "bea")

	v := &Verificacion{UserID: u.ID, Referencia: u.ID, URL: "https://portal/validar"}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	if estado, _ := s.EstadoVerificacionDe(u.ID); estado != VerificacionPendiente {
		t.Errorf("tras abrirla el estado = %q, se esperaba pendiente", estado)
	}

	if err := s.AnotarGUID(v.ID, "GUID-1"); err != nil {
		t.Fatalf("AnotarGUID: %v", err)
	}
	v.GUID = "GUID-1"
	v.TokenID = "001002-...par"
	v.HashDeclaracion = "abc123"

	if err := s.ResolverVerificacion(v.ID, u.ID, *v, CamposIdentidad{
		NIF:             "00000000T",
		Nombre:          "NOMBRE",
		Apellido:        "APELLIDOUNO APELLIDODOS",
		Direccion:       "CALLE FALSA 1",
		Nacionalidad:    "ESP",
		FechaNacimiento: "1980-03-04",
		DocTipo:         "DNI",
		DocNumero:       "ABC000000",
		DocCaducidad:    "2030-01-02",
	}); err != nil {
		t.Fatalf("ResolverVerificacion: %v", err)
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	casos := []struct{ campo, obtenido, esperado string }{
		{"nif", got.NIF, "00000000T"},
		{"nombre", got.Nombre, "NOMBRE"},
		{"apellido", got.Apellido, "APELLIDOUNO APELLIDODOS"},
		{"display_name", got.DisplayName, "NOMBRE APELLIDOUNO APELLIDODOS"},
		{"dirección", got.Direccion, "CALLE FALSA 1"},
		{"nacionalidad", got.Nacionalidad, "ESP"},
		{"fecha de nacimiento", got.FechaNacimiento, "1980-03-04"},
		{"tipo de documento", got.DocTipo, "DNI"},
		{"número de soporte", got.DocNumero, "ABC000000"},
		{"caducidad", got.DocCaducidad, "2030-01-02"},
	}
	for _, c := range casos {
		if c.obtenido != c.esperado {
			t.Errorf("%s = %q, se esperaba %q", c.campo, c.obtenido, c.esperado)
		}
	}
	if got.Verificacion != VerificacionVerificada {
		t.Errorf("estado del usuario = %q, se esperaba verificada", got.Verificacion)
	}
	if got.VerificadoAt == nil {
		t.Error("falta la fecha de verificación")
	}

	p, err := s.GetPrincipalByID(u.ID)
	if err != nil {
		t.Fatalf("GetPrincipalByID: %v", err)
	}
	if !p.Verificado {
		t.Error("el principal tiene que quedar verificado")
	}

	// La evidencia queda anotada para poder volver a pedir el certificado.
	ultima, err := s.UltimaVerificacion(u.ID)
	if err != nil {
		t.Fatalf("UltimaVerificacion: %v", err)
	}
	if ultima.GUID != "GUID-1" || ultima.TokenID == "" || ultima.HashDeclaracion == "" {
		t.Errorf("la evidencia no se guardó: %+v", ultima)
	}
	if ultima.ResueltaAt == nil {
		t.Error("falta la fecha de resolución")
	}
}

// Un intento fallido no verifica a nadie, pero se conserva con su motivo.
func TestVerificacion_FallarConservaElMotivo(t *testing.T) {
	s := newTestStore(t)
	u := usuarioSinVerificar(t, s, "carlos")

	v := &Verificacion{UserID: u.ID, Referencia: u.ID}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	if err := s.FallarVerificacion(v.ID, u.ID, "Chip NFC ilegible"); err != nil {
		t.Fatalf("FallarVerificacion: %v", err)
	}

	p, err := s.GetPrincipalByID(u.ID)
	if err != nil {
		t.Fatalf("GetPrincipalByID: %v", err)
	}
	if p.Verificado {
		t.Error("un intento fallido no puede dejar al usuario verificado")
	}

	ultima, err := s.UltimaVerificacion(u.ID)
	if err != nil {
		t.Fatalf("UltimaVerificacion: %v", err)
	}
	if ultima.Estado != VerificacionFallida {
		t.Errorf("estado = %q, se esperaba fallida", ultima.Estado)
	}
	if ultima.Motivo != "Chip NFC ilegible" {
		t.Errorf("motivo = %q", ultima.Motivo)
	}
	if ultima.Pendiente() {
		t.Error("un intento fallido ya no está pendiente")
	}
}

// Los datos que fijó el DNI no se pueden reescribir a mano desde el perfil:
// si se pudiera, la verificación no garantizaría nada.
func TestVerificacion_ElPerfilNoReescribeLoVerificado(t *testing.T) {
	s := newTestStore(t)
	u := usuarioSinVerificar(t, s, "diana")

	v := &Verificacion{UserID: u.ID, Referencia: u.ID}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	if err := s.ResolverVerificacion(v.ID, u.ID, *v, CamposIdentidad{
		NIF: "00000000T", Nombre: "NOMBRE", Apellido: "APELLIDOUNO", Direccion: "CALLE FALSA 1",
	}); err != nil {
		t.Fatalf("ResolverVerificacion: %v", err)
	}

	if err := s.UpdateProfile(u.ID, ProfileInput{
		DisplayName: "Otro", Nombre: "Otro", Apellido: "Distinto", NIF: "99999999R",
		Email: "diana@example.com", Telefono: "+34600111222",
		Direccion: "CALLE NUEVA 2", Localidad: "Valencia", CodigoPostal: "46001", Pais: "ES",
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.NIF != "00000000T" || got.Nombre != "NOMBRE" || got.Apellido != "APELLIDOUNO" {
		t.Errorf("los datos del documento se reescribieron: nif=%q nombre=%q apellido=%q",
			got.NIF, got.Nombre, got.Apellido)
	}
	// Contacto y dirección sí son suyos: la del DNI suele estar desfasada.
	if got.Direccion != "CALLE NUEVA 2" || got.Localidad != "Valencia" || got.Telefono != "+34600111222" {
		t.Errorf("la dirección y el contacto tienen que poder cambiarse: %+v", got)
	}
}

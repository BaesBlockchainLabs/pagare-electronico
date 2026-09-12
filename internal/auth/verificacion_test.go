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

	v := &Verificacion{UserID: u.ID, Referencia: u.ID}
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
		Localidad:       "CIUDAD",
		Provincia:       "PROVINCIA",
		Pais:            "ES",
		Nacionalidad:    "España",
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
		{"localidad", got.Localidad, "CIUDAD"},
		{"provincia", got.Provincia, "PROVINCIA"},
		{"país", got.Pais, "ES"},
		{"nacionalidad", got.Nacionalidad, "España"},
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

	if got.CodigoPostal != "" {
		t.Errorf("el certificado no trae código postal; no debería inventarse uno: %q", got.CodigoPostal)
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

// El refresco de administración trabaja sobre esta lista, así que sólo puede
// traer las que todavía pueden resolverse.
func TestVerificacionesPendientes(t *testing.T) {
	s := newTestStore(t)

	pendiente := usuarioSinVerificar(t, s, "eva")
	vPendiente := &Verificacion{UserID: pendiente.ID, Referencia: pendiente.ID}
	if err := s.CrearVerificacion(vPendiente); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}

	fallida := usuarioSinVerificar(t, s, "fran")
	vFallida := &Verificacion{UserID: fallida.ID, Referencia: fallida.ID}
	if err := s.CrearVerificacion(vFallida); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	if err := s.FallarVerificacion(vFallida.ID, fallida.ID, "Tiempo Expirado"); err != nil {
		t.Fatalf("FallarVerificacion: %v", err)
	}

	verificada := usuarioSinVerificar(t, s, "gema")
	vHecha := &Verificacion{UserID: verificada.ID, Referencia: verificada.ID}
	if err := s.CrearVerificacion(vHecha); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	if err := s.ResolverVerificacion(vHecha.ID, verificada.ID, *vHecha,
		CamposIdentidad{NIF: "00000000T", Nombre: "NOMBRE"}); err != nil {
		t.Fatalf("ResolverVerificacion: %v", err)
	}

	// Un usuario que nunca lo intentó no aparece: no hay nada que consultar.
	usuarioSinVerificar(t, s, "hugo")

	pendientes, err := s.VerificacionesPendientes()
	if err != nil {
		t.Fatalf("VerificacionesPendientes: %v", err)
	}
	if len(pendientes) != 1 {
		t.Fatalf("pendientes = %d, se esperaba 1", len(pendientes))
	}
	if pendientes[0].UserID != pendiente.ID {
		t.Errorf("pendiente = %q, se esperaba %q", pendientes[0].UserID, pendiente.ID)
	}
	if pendientes[0].Referencia == "" {
		t.Error("la referencia hace falta para consultar el envío en el portal")
	}
}

// El chip del DNI no lleva código postal, así que el que haya puesto el usuario
// tiene que sobrevivir a la verificación: si no, se le borra un dato bueno.
func TestVerificacion_NoBorraLoQueElDNINoTrae(t *testing.T) {
	s := newTestStore(t)
	u := usuarioSinVerificar(t, s, "ivan")
	if err := s.UpdateProfile(u.ID, ProfileInput{
		Email: "ivan@example.com", CodigoPostal: "03640", Localidad: "A MANO", Pais: "PT",
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	v := &Verificacion{UserID: u.ID, Referencia: u.ID}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatalf("CrearVerificacion: %v", err)
	}
	// Una verificación que no trae localidad ni país tampoco puede vaciarlos.
	if err := s.ResolverVerificacion(v.ID, u.ID, *v, CamposIdentidad{
		NIF: "00000000T", Nombre: "NOMBRE", Direccion: "CALLE FALSA 1",
	}); err != nil {
		t.Fatalf("ResolverVerificacion: %v", err)
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.CodigoPostal != "03640" {
		t.Errorf("código postal = %q, se esperaba que sobreviviera", got.CodigoPostal)
	}
	if got.Localidad != "A MANO" || got.Pais != "PT" {
		t.Errorf("un campo que el certificado no trae no puede vaciarse: localidad=%q pais=%q",
			got.Localidad, got.Pais)
	}
}

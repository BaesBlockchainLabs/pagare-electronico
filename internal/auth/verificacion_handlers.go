package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"pagare/internal/identidad"
)

// SetIdentidad conecta el servicio que valida identidades contra el DNI. Un
// servicio nil deja la verificación desactivada, que es el estado en el que
// corren el desarrollo y los tests.
func (h *Handlers) SetIdentidad(s *identidad.Servicio) { h.identidad = s }

// VerificacionActiva indica si hay verificación de identidad configurada.
func (h *Handlers) VerificacionActiva() bool { return h.identidad.Activo() }

// IniciarVerificacion crea el envío de validación de identidad del usuario en
// sesión y devuelve la URL a la que tiene que ir a leer su DNI.
//
// Es idempotente mientras haya un intento en curso: devuelve la URL que ya se
// creó en lugar de abrir otro envío, para que recargar la página no multiplique
// los envíos en el portal.
func (h *Handlers) IniciarVerificacion(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}
	if !h.identidad.Activo() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"ok": false, "msg": "la verificación de identidad no está configurada"})
		return
	}

	v, err := h.EnviarVerificacion(r.Context(), p.UserID)
	if err != nil {
		writeJSON(w, EstadoHTTPVerificacion(err), map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, respuestaVerificacion(v, ""))
}

// ErrYaVerificado y ErrSinMovil son los dos rechazos que no son avería y que
// quien llama tiene que poder distinguir de un fallo del portal.
var (
	ErrYaVerificado = errors.New("la identidad de este usuario ya está verificada")
	ErrSinMovil     = errors.New("hace falta un móvil para validar la identidad")
)

// EnviarVerificacion arranca la validación de identidad de un usuario, o
// devuelve la que ya está en curso.
//
// No duplica un intento pendiente a propósito: cada envío es otro SMS al
// usuario, y el portal acabaría con dos validaciones vivas para la misma
// persona.
func (h *Handlers) EnviarVerificacion(ctx context.Context, userID string) (*Verificacion, error) {
	if !h.identidad.Activo() {
		return nil, identidad.ErrDesactivado
	}

	if v, err := h.store.UltimaVerificacion(userID); err == nil {
		if v.Estado == VerificacionVerificada {
			return nil, ErrYaVerificado
		}
		if v.Pendiente() {
			return v, nil
		}
	}

	u, err := h.store.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if u.Telefono == "" {
		return nil, ErrSinMovil
	}

	envio, err := h.identidad.Iniciar(ctx, identidad.Solicitud{
		// La referencia es el id del usuario: es lo que permite seguir el
		// envío antes de que el portal le asigne GUID.
		Referencia: u.ID,
		Nombre:     nombreParaElPortal(u),
		Email:      u.Email,
		Movil:      u.Telefono,
	})
	if err != nil {
		return nil, err
	}

	v := &Verificacion{
		UserID:     u.ID,
		Referencia: envio.Referencia,
		GUID:       envio.GUID,
		Estado:     VerificacionPendiente,
	}
	if err := h.store.CrearVerificacion(v); err != nil {
		return nil, err
	}
	return v, nil
}

// RefrescarVerificaciones consulta en el portal todas las validaciones
// pendientes y resuelve las que ya han terminado. Devuelve cuántas ha mirado y
// cuántas han dejado de estar pendientes.
//
// Es lo que un administrador necesita para no depender de que cada usuario
// entre en su pantalla: el portal tarda en registrar el resultado y nadie está
// mirando.
func (h *Handlers) RefrescarVerificaciones(ctx context.Context) (revisadas, resueltas int, err error) {
	if !h.identidad.Activo() {
		return 0, 0, identidad.ErrDesactivado
	}
	pendientes, err := h.store.VerificacionesPendientes()
	if err != nil {
		return 0, 0, err
	}
	for _, v := range pendientes {
		revisadas++
		h.refrescar(ctx, v)
		if !v.Pendiente() {
			resueltas++
		}
	}
	return revisadas, resueltas, nil
}

// EstadoHTTPVerificacion traduce los rechazos conocidos; lo demás se lo achaca
// al portal, que es de donde vienen los errores que no hemos previsto.
func EstadoHTTPVerificacion(err error) int {
	switch {
	case errors.Is(err, identidad.ErrDesactivado):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrYaVerificado), errors.Is(err, ErrSinMovil):
		return http.StatusBadRequest
	case errors.Is(err, ErrUserNotFound):
		return http.StatusNotFound
	default:
		return http.StatusBadGateway
	}
}

// EstadoVerificacionHandler informa de cómo va la verificación del usuario en
// sesión, preguntándole al portal si hace falta.
//
// Se consulta aquí, bajo demanda, en lugar de en un proceso de fondo: el
// usuario vuelve a esta pantalla justo después de validarse, que es
// exactamente cuando hay algo nuevo que mirar.
func (h *Handlers) EstadoVerificacionHandler(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	// Lo que le falte de contacto viaja en la respuesta: sin correo y móvil no
	// se puede ni empezar a validar, y sin código postal no se podrá emitir
	// después. Las cuentas anteriores a todo esto no tienen ninguno de los tres.
	faltan := h.faltaEnContacto(p.UserID)

	v, err := h.store.UltimaVerificacion(p.UserID)
	if errors.Is(err, ErrVerificacionNoEncontrada) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"activa":  h.identidad.Activo(),
			"estado":  string(VerificacionNoIniciada),
			"mensaje": "Todavía no has validado tu identidad.",
			"faltan":  faltan,
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo consultar la validación"})
		return
	}

	if v.Pendiente() && h.identidad.Activo() {
		h.refrescar(r.Context(), v)
	}

	res := respuestaVerificacion(v, "")
	res["faltan"] = faltan
	writeJSON(w, http.StatusOK, res)
}

// refrescar consulta el envío en el portal y, si ya terminó, lo resuelve. Los
// errores de red no cambian el estado guardado: la verificación sigue
// pendiente y se volverá a mirar.
func (h *Handlers) refrescar(ctx context.Context, v *Verificacion) {
	sit, err := h.identidad.Consultar(ctx, v.Referencia)
	if err != nil {
		return
	}
	if sit.GUID != "" && sit.GUID != v.GUID {
		v.GUID = sit.GUID
		_ = h.store.AnotarGUID(v.ID, sit.GUID)
	}
	if !sit.Terminado {
		return
	}
	if !sit.Aceptado {
		v.Estado = VerificacionFallida
		v.Motivo = sit.Descripcion
		_ = h.store.FallarVerificacion(v.ID, v.UserID, v.Motivo)
		return
	}
	if v.GUID == "" {
		// Terminado y aceptado pero sin GUID no debería pasar; sin él no hay
		// certificado que descargar, así que se deja pendiente y se reintenta.
		return
	}

	datos, err := h.identidad.Recoger(ctx, v.GUID)
	if err != nil {
		var noSuperada *identidad.ErrNoSuperada
		if errors.As(err, &noSuperada) {
			v.Estado = VerificacionFallida
			v.Motivo = noSuperada.Error()
			_ = h.store.FallarVerificacion(v.ID, v.UserID, v.Motivo)
		}
		// Cualquier otro error es de transporte: no se cierra el intento.
		return
	}

	v.TokenID = datos.TokenID
	v.ValidationID = datos.ValidationID
	v.HashDeclaracion = datos.HashDeclaracion
	if err := h.store.ResolverVerificacion(v.ID, v.UserID, *v, CamposIdentidad{
		NIF:             datos.NIF,
		Nombre:          datos.Nombre,
		Apellido:        datos.Apellidos(),
		Direccion:       datos.Direccion,
		Localidad:       datos.Localidad,
		Provincia:       datos.Provincia,
		Pais:            datos.Pais,
		Nacionalidad:    datos.Nacionalidad,
		FechaNacimiento: datos.FechaNacimiento,
		DocTipo:         datos.Documento,
		DocNumero:       datos.NumeroSoporte,
		DocCaducidad:    datos.Caducidad,
	}); err != nil {
		return
	}
	v.Estado = VerificacionVerificada
	v.Motivo = ""
}

// respuestaVerificacion es el cuerpo que ven el alta y la pantalla de
// verificación. No lleva ningún dato del documento: sólo el estado y, si
// falló, por qué.
func respuestaVerificacion(v *Verificacion, mensaje string) map[string]interface{} {
	if mensaje == "" {
		mensaje = mensajeDe(v)
	}
	res := map[string]interface{}{
		"ok":      true,
		"activa":  true,
		"estado":  string(v.Estado),
		"mensaje": mensaje,
	}
	if v.Motivo != "" {
		res["motivo"] = v.Motivo
	}
	return res
}

func mensajeDe(v *Verificacion) string {
	switch v.Estado {
	case VerificacionVerificada:
		return "Identidad verificada."
	case VerificacionFallida:
		return "La validación no se completó. Puedes volver a intentarlo."
	case VerificacionPendiente:
		return "Te hemos enviado un enlace por SMS y correo: ábrelo y lee tu DNI con el móvil."
	default:
		return "Todavía no has validado tu identidad."
	}
}

// nombreParaElPortal es cómo se dirige Logalty al usuario mientras valida. El
// nombre de verdad llega después, del propio documento; hasta entonces lo
// único que hay es el usuario con el que se dio de alta.
func nombreParaElPortal(u *User) string {
	if u.Nombre != "" {
		return fmt.Sprintf("%s %s", u.Nombre, u.Apellido)
	}
	return u.Username
}

// faltaEnContacto es lo que le falta al usuario de correo, móvil y código
// postal. Una lista vacía significa que puede validarse y, después, emitir.
func (h *Handlers) faltaEnContacto(userID string) []string {
	u, err := h.store.GetByID(userID)
	if err != nil {
		return nil
	}
	faltan := FaltaEnContacto(u)
	if faltan == nil {
		// Se serializa como lista vacía, no como null: la pantalla distingue
		// "no falta nada" de "no se pudo mirar".
		return []string{}
	}
	return faltan
}

// ActualizarContacto completa el correo, el móvil y el código postal de quien
// ya tiene cuenta.
//
// Las cuentas anteriores a la validación de identidad no tienen ninguno de los
// tres, así que no pueden ni empezar a validarse. Mandarlas al perfil a
// rellenarlos funciona, pero es un desvío que nadie entiende cuando lo único
// que quería era entrar; esto permite pedírselos en la propia pantalla de
// validación.
//
// No reutiliza UpdateProfile a propósito: aquél reemplaza el perfil entero, así
// que mandar sólo el contacto borraría el nombre, el NIF y la dirección.
func (h *Handlers) ActualizarContacto(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
		return
	}

	var req struct {
		Email        string `json:"email"`
		Telefono     string `json:"telefono"`
		CodigoPostal string `json:"codigo_postal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "invalid body"})
		return
	}
	req.Email = strings.TrimSpace(req.Email)
	req.Telefono = strings.TrimSpace(req.Telefono)
	req.CodigoPostal = strings.TrimSpace(req.CodigoPostal)

	if !validEmail(req.Email) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "hace falta un email con formato válido"})
		return
	}
	if req.Telefono == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "hace falta un móvil: es por donde se valida el DNI"})
		return
	}
	if !codigoPostalES.MatchString(req.CodigoPostal) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false,
			"msg": "hace falta un código postal de cinco dígitos: el chip del DNI no lo lleva, " +
				"así que es el único dato del domicilio que tienes que poner tú",
		})
		return
	}

	if err := h.store.ActualizarContacto(p.UserID, Contacto{
		Email: req.Email, Telefono: req.Telefono, CodigoPostal: req.CodigoPostal,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "msg": "Datos guardados.", "faltan": []string{}})
}

// Evidencia devuelve el certificado entero de la validación de un usuario, tal
// como lo emitió el portal.
//
// Sirve para auditar: enseña en qué se apoyó la verificación —las
// comprobaciones con su umbral, las puntuaciones biométricas, los
// consentimientos y las huellas de los artefactos firmados— y no sólo su
// conclusión. Nada de eso se guarda: se pide al portal en el momento.
//
// Todo lo que devuelve es dato personal del sujeto, así que es cosa de
// administradores y no tiene sitio en un log.
func (h *Handlers) Evidencia(ctx context.Context, userID string) (*identidad.Evidencia, error) {
	if !h.identidad.Activo() {
		return nil, identidad.ErrDesactivado
	}
	reg, err := h.store.UltimaVerificacion(userID)
	if err != nil {
		return nil, err
	}
	if reg.GUID == "" {
		return nil, fmt.Errorf("esta validación no llegó a tener envío en el portal, "+
			"así que no hay certificado que pedir (estado %q)", reg.Estado)
	}
	return h.identidad.Evidencia(ctx, reg.GUID)
}

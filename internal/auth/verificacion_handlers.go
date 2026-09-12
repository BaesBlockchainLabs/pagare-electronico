package auth

import (
	"errors"
	"fmt"
	"net/http"

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

	if v, err := h.store.UltimaVerificacion(p.UserID); err == nil {
		if v.Estado == VerificacionVerificada {
			writeJSON(w, http.StatusOK, respuestaVerificacion(v, "Tu identidad ya está verificada."))
			return
		}
		// Un intento en curso no se duplica: reenviar crearía otro envío en el
		// portal y otro SMS al usuario.
		if v.Pendiente() {
			writeJSON(w, http.StatusOK, respuestaVerificacion(v, "Ya tienes una validación en curso: revisa tu móvil."))
			return
		}
	}

	u, err := h.store.GetByID(p.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo cargar el usuario"})
		return
	}
	if u.Telefono == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"ok": false, "msg": "hace falta un móvil para validar la identidad; añádelo en tu perfil"})
		return
	}

	envio, err := h.identidad.Iniciar(r.Context(), identidad.Solicitud{
		// La referencia es el id del usuario: es lo que permite seguir el
		// envío antes de que el portal le asigne GUID.
		Referencia: u.ID,
		Nombre:     nombreParaElPortal(u),
		Email:      u.Email,
		Movil:      u.Telefono,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	v := &Verificacion{
		UserID:     u.ID,
		Referencia: envio.Referencia,
		GUID:       envio.GUID,
		Estado:     VerificacionPendiente,
	}
	if err := h.store.CrearVerificacion(v); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo registrar la validación"})
		return
	}

	writeJSON(w, http.StatusOK, respuestaVerificacion(v, ""))
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

	v, err := h.store.UltimaVerificacion(p.UserID)
	if errors.Is(err, ErrVerificacionNoEncontrada) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"activa":  h.identidad.Activo(),
			"estado":  string(VerificacionNoIniciada),
			"mensaje": "Todavía no has validado tu identidad.",
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "no se pudo consultar la validación"})
		return
	}

	if v.Pendiente() && h.identidad.Activo() {
		h.refrescar(r, v)
	}

	writeJSON(w, http.StatusOK, respuestaVerificacion(v, ""))
}

// refrescar consulta el envío en el portal y, si ya terminó, lo resuelve. Los
// errores de red no cambian el estado guardado: la verificación sigue
// pendiente y se volverá a mirar.
func (h *Handlers) refrescar(r *http.Request, v *Verificacion) {
	sit, err := h.identidad.Consultar(r.Context(), v.Referencia)
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

	datos, err := h.identidad.Recoger(r.Context(), v.GUID)
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

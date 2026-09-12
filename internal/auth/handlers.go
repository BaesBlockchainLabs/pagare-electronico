package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"pagare/internal/crypto"
	"pagare/internal/identidad"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// validEmail does a lightweight format check (not full RFC 5322).
func validEmail(s string) bool { return emailRe.MatchString(s) }

type Handlers struct {
	store  *Store
	crypto *crypto.Service
	// identidad valida la identidad contra el chip del DNI. Nil cuando no hay
	// configuración de Logalty; ver SetIdentidad.
	identidad *identidad.Servicio
}

func NewHandlers(store *Store, cryptoSvc *crypto.Service) *Handlers {
	return &Handlers{store: store, crypto: cryptoSvc}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	OK       bool   `json:"ok"`
	Username string `json:"username,omitempty"`
	Role     Role   `json:"role,omitempty"`
	Msg      string `json:"msg,omitempty"`
}

// Login authenticates and sets the session cookie on success.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "invalid body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "username and password required"})
		return
	}

	principal, err := h.store.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "invalid credentials"})
		return
	}

	if err := SetSessionCookie(w, principal); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "failed to create session"})
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		OK:       true,
		Username: principal.Username,
		Role:     principal.Role,
	})
}

// registerRequest es lo poco que se le pide a quien se da de alta. Nombre,
// apellidos, NIF y dirección no están aquí a propósito: los aporta la
// validación del DNI, que es la única fuente en la que se puede confiar para
// identificar a las partes de un pagaré.
//
// El móvil es obligatorio porque el tipo de envío de validación lo exige: es
// por donde el portal lleva al usuario a leer el chip.
type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Telefono string `json:"telefono"`
}

// Register da de alta a un usuario (rol=user), le provisiona su par de claves,
// le inicia sesión y arranca la validación de su identidad contra el DNI.
//
// La cuenta nace en estado pendiente: existe y se puede entrar en ella, pero no
// opera con pagarés hasta que la validación se supera y trae los datos
// personales. La respuesta lleva la URL a la que hay que llevar al usuario.
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(req.Email)
	req.Telefono = strings.TrimSpace(req.Telefono)
	if req.Username == "" || len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "usuario obligatorio y contraseña de al menos 6 caracteres"})
		return
	}
	if req.Email == "" || !validEmail(req.Email) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "hace falta un email con formato válido"})
		return
	}
	// Sin móvil no hay validación posible, así que se exige en el alta en vez
	// de dejar al usuario atascado en la pantalla siguiente.
	if h.identidad.Activo() && req.Telefono == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "hace falta un móvil: es por donde se valida el DNI"})
		return
	}

	u := &User{
		Username:     req.Username,
		Role:         RoleUser,
		Email:        req.Email,
		Telefono:     req.Telefono,
		DisplayName:  req.Username,
		Verificacion: VerificacionNoIniciada,
	}
	if err := h.store.CreateUser(u, req.Password); err != nil {
		if err == ErrUserAlreadyExists {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "msg": "ese usuario ya existe"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}

	// Provision the identity keypair (idempotent). Non-fatal: the account still
	// works; a keypair can be provisioned later.
	if _, err := h.store.EnsureUserKeypair(u.ID); err != nil {
		fmt.Printf("[register] no se pudo generar keypair para %s: %v\n", u.Username, err)
	}

	principal, err := h.store.GetPrincipalByID(u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "usuario creado pero no se pudo iniciar sesión; entra manualmente"})
		return
	}
	if err := SetSessionCookie(w, principal); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "usuario creado pero no se pudo iniciar sesión; entra manualmente"})
		return
	}

	res := map[string]interface{}{
		"ok":       true,
		"username": principal.Username,
		"role":     principal.Role,
	}
	// Arrancar la validación no es crítico para el alta: si el portal falla, la
	// cuenta existe igual y se reintenta desde la pantalla de verificación.
	if v := h.arrancarVerificacion(r, u); v != nil {
		res["verificacion"] = v
	}
	writeJSON(w, http.StatusOK, res)
}

// arrancarVerificacion crea el envío de validación del recién registrado y
// devuelve lo que el alta le enseña. Devuelve nil sólo cuando la verificación
// no está configurada.
//
// Que el portal falle no puede dejar la respuesta sin este bloque: es lo que
// le dice al alta que mande al usuario a /verificacion, y ahí es donde puede
// reintentar y ver qué ha pasado. Sin él se quedaría dentro de la aplicación
// sin poder hacer nada ni saber por qué.
func (h *Handlers) arrancarVerificacion(r *http.Request, u *User) map[string]interface{} {
	if !h.identidad.Activo() {
		return nil
	}

	falloAlArrancar := func(err error) map[string]interface{} {
		fmt.Printf("[register] no se pudo iniciar la validación de %s: %v\n", u.Username, err)
		return map[string]interface{}{
			"ok":      true,
			"activa":  true,
			"estado":  string(VerificacionNoIniciada),
			"mensaje": "No se pudo empezar la validación de tu identidad. Puedes reintentarlo desde aquí.",
		}
	}

	envio, err := h.identidad.Iniciar(r.Context(), identidad.Solicitud{
		Referencia: u.ID,
		Nombre:     nombreParaElPortal(u),
		Email:      u.Email,
		Movil:      u.Telefono,
	})
	if err != nil {
		return falloAlArrancar(err)
	}
	v := &Verificacion{
		UserID:     u.ID,
		Referencia: envio.Referencia,
		GUID:       envio.GUID,
		Estado:     VerificacionPendiente,
	}
	if err := h.store.CrearVerificacion(v); err != nil {
		return falloAlArrancar(err)
	}
	return respuestaVerificacion(v, "")
}

// Logout clears the session cookie.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// Me returns basic information about the current authenticated user.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":         true,
		"id":         p.UserID,
		"username":   p.Username,
		"role":       p.Role,
		"pub_keys":   p.PubKeys,
		"verificado": p.Verificado,
	})
}

// ClaimPubRequest supports two ways to claim a pubkey (consistent with how the rest of the app works):
// 1. Convenience (like other forms): provide pub + pvt temporarily. Server claims without storing pvt.
// 2. Pure cryptographic: provide pub + challenge (from /claim/challenge) + signature (user signed client-side or externally).
type ClaimPubRequest struct {
	Pub       string `json:"pub"`
	Pvt       string `json:"pvt,omitempty"` // convenience path (pvt never persisted)
	Challenge string `json:"challenge,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// IssueClaimChallenge returns a fresh challenge that the user must sign with the private key
// corresponding to the pub they want to claim.
func (h *Handlers) IssueClaimChallenge(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}

	pub := r.URL.Query().Get("pub")
	if pub == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "pub query param is required"})
		return
	}

	// Challenge includes the user ID to bind it to this account and a timestamp for freshness.
	challenge := fmt.Sprintf("claim:%s:%s:%d", p.UserID, pub, time.Now().Unix())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"challenge": challenge,
		"note":      "Sign this exact string with the private key for the pub. Then POST it with the signature to /api/auth/claim.",
	})
}

// ClaimPub associates a public key with the logged-in user.
// Supports the pvt convenience path (consistent with emit/endoso forms) or a full
// challenge+signature cryptographic proof (preferred when possible).
func (h *Handlers) ClaimPub(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}

	var req ClaimPubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Pub == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "pub is required"})
		return
	}

	if req.Pvt != "" {
		// Convenience path: user sends pvt temporarily (exactly like they do for asset operations).
		// We do not store the pvt. This proves control for the purpose of linking the pub.
		// (In a future iteration we can have the server sign a challenge with it and verify.)
		if err := h.store.AddPubKey(p.UserID, req.Pub); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "failed to claim key"})
			return
		}
		updated, _ := h.store.GetPrincipalByID(p.UserID)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":       true,
			"username": updated.Username,
			"pub_keys": updated.PubKeys,
			"msg":      "pubkey claimed (pvt convenience path)",
		})
		return
	}

	// Cryptographic path: challenge + signature
	if req.Challenge == "" || req.Signature == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "for cryptographic claim provide challenge + signature, or use pvt convenience"})
		return
	}

	expectedPrefix := "claim:" + p.UserID + ":" + req.Pub + ":"
	if !strings.HasPrefix(req.Challenge, expectedPrefix) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "challenge is not for this user/pub"})
		return
	}

	// Verify using existing crypto service (pub as verify key). Reuses BCF /did/verify.
	if h.crypto != nil {
		_, err := h.crypto.VerifySignature(req.Challenge, req.Pub)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "signature verification failed: " + err.Error()})
			return
		}
	} else {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "crypto service not available"})
		return
	}

	if err := h.store.AddPubKey(p.UserID, req.Pub); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "msg": "failed to claim key"})
		return
	}

	updated, _ := h.store.GetPrincipalByID(p.UserID)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"username": updated.Username,
		"pub_keys": updated.PubKeys,
		"msg":      "pubkey claimed after cryptographic verification",
	})
}

// writeJSON is a tiny local helper so the auth package does not create an import cycle
// with the main handler package.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

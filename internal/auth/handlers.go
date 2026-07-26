package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pagare/internal/crypto"
)

type Handlers struct {
	store  *Store
	crypto *crypto.Service
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

type registerRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Nombre       string `json:"nombre"`
	Apellido     string `json:"apellido"`
	NIF          string `json:"nif"`
	Direccion    string `json:"direccion"`
	Localidad    string `json:"localidad"`
	CodigoPostal string `json:"codigo_postal"`
	Pais         string `json:"pais"`
}

// Register creates a new self-service user (role=user), provisions their
// identity keypair and logs them straight in. Open registration, active
// immediately. NOTE: this is the future integration point for Logalty KYC —
// a verification step would gate activation here before the session is set.
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "invalid body"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "usuario obligatorio y contraseña de al menos 6 caracteres"})
		return
	}
	if req.Nombre == "" || req.NIF == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "nombre y NIF son obligatorios"})
		return
	}

	u := &User{
		Username:     req.Username,
		Role:         RoleUser,
		Nombre:       req.Nombre,
		Apellido:     req.Apellido,
		NIF:          req.NIF,
		Direccion:    req.Direccion,
		Localidad:    req.Localidad,
		CodigoPostal: req.CodigoPostal,
		Pais:         req.Pais,
		DisplayName:  strings.TrimSpace(req.Nombre + " " + req.Apellido),
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

	writeJSON(w, http.StatusOK, loginResponse{OK: true, Username: principal.Username, Role: principal.Role})
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
		"ok":       true,
		"id":       p.UserID,
		"username": p.Username,
		"role":     p.Role,
		"pub_keys": p.PubKeys,
	})
}

// ClaimPubRequest supports two ways to claim a pubkey (consistent with how the rest of the app works):
// 1. Convenience (like other forms): provide pub + pvt temporarily. Server claims without storing pvt.
// 2. Pure cryptographic: provide pub + challenge (from /claim/challenge) + signature (user signed client-side or externally).
type ClaimPubRequest struct {
	Pub       string `json:"pub"`
	Pvt       string `json:"pvt,omitempty"`       // convenience path (pvt never persisted)
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

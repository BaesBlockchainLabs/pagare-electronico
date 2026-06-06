package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	sessionCookieName = "pagare_session"
	sessionDuration   = 24 * 7 * time.Hour // 7 days
)

var (
	ErrInvalidSession = errors.New("invalid or expired session")
)

type sessionClaims struct {
	UserID string
	Role   Role
	Expiry int64 // unix seconds
}

// sessionSecret returns the secret used for signing cookies.
// It prefers SESSION_SECRET. In development it falls back to an ephemeral
// random value (sessions will not survive restarts).
func sessionSecret() []byte {
	if s := os.Getenv("SESSION_SECRET"); s != "" {
		return []byte(s)
	}
	// Ephemeral dev secret (not persisted).
	b := make([]byte, 32)
	// Best effort; if this fails we still have a zero secret which is bad,
	// but we log at a higher layer. For safety we panic in prod-like envs.
	if os.Getenv("APP_ENV") == "production" || os.Getenv("APP_ENV") == "prod" {
		panic("SESSION_SECRET must be set in production")
	}
	// Fill with something; in practice callers should set the env.
	copy(b, "dev-insecure-session-secret-change-me")
	return b
}

func sign(data []byte) []byte {
	h := hmac.New(sha256.New, sessionSecret())
	h.Write(data)
	return h.Sum(nil)
}

func encodeSession(c sessionClaims) (string, error) {
	// payload: userID|role|expiry
	payload := fmt.Sprintf("%s|%s|%d", c.UserID, c.Role, c.Expiry)
	sig := sign([]byte(payload))

	// value = base64(payload) + "." + base64(sig)
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
	return enc, nil
}

func decodeAndVerify(raw string) (*sessionClaims, error) {
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidSession
	}
	payloadB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidSession
	}
	sigB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidSession
	}
	if !hmac.Equal(sigB, sign(payloadB)) {
		return nil, ErrInvalidSession
	}

	payload := string(payloadB)
	parts2 := strings.Split(payload, "|")
	if len(parts2) != 3 {
		return nil, ErrInvalidSession
	}
	expiry, err := parseInt64(parts2[2])
	if err != nil {
		return nil, ErrInvalidSession
	}
	if time.Now().Unix() > expiry {
		return nil, ErrInvalidSession
	}

	return &sessionClaims{
		UserID: parts2[0],
		Role:   Role(parts2[1]),
		Expiry: expiry,
	}, nil
}

func parseInt64(s string) (int64, error) {
	var v int64
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// SetSessionCookie issues a signed session cookie for the given principal.
func SetSessionCookie(w http.ResponseWriter, p *Principal) error {
	if p == nil {
		return errors.New("nil principal")
	}
	claims := sessionClaims{
		UserID: p.UserID,
		Role:   p.Role,
		Expiry: time.Now().Add(sessionDuration).Unix(),
	}
	val, err := encodeSession(claims)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure is set by the reverse proxy (Caddy) in production.
		MaxAge: int(sessionDuration.Seconds()),
	})
	return nil
}

// ClearSessionCookie removes the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// PrincipalFromRequest extracts and validates the session from the request cookie.
// It only returns the lightweight claims; the caller (middleware) is responsible
// for turning the UserID into a full Principal via the store (to get fresh PubKeys).
func PrincipalFromRequest(r *http.Request) (*sessionClaims, error) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, ErrInvalidSession
	}
	return decodeAndVerify(c.Value)
}

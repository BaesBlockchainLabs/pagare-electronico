package auth

import (
	"context"
	"net/http"
)

type contextKey string

const principalKey contextKey = "auth.principal"

// AuthMiddleware attempts to authenticate the request using the session cookie
// and stores the resulting *Principal (or nil) in the request context.
func AuthMiddleware(store *Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := PrincipalFromRequest(r)
			if err != nil || claims == nil {
				// No valid session — continue with no principal.
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, (*Principal)(nil))))
				return
			}

			p, err := store.GetPrincipalByID(claims.UserID)
			if err != nil {
				// Stale user in cookie — treat as unauthenticated.
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, (*Principal)(nil))))
				return
			}

			// Refresh pubkeys from the live principal (they may have changed since cookie was issued).
			ctx := context.WithValue(r.Context(), principalKey, p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ContextWithPrincipal returns a copy of ctx carrying the given principal.
// Exported so handlers (and tests) can set a principal explicitly without going
// through the cookie-based middleware.
func ContextWithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// GetPrincipal returns the authenticated principal from context, or nil if unauthenticated.
func GetPrincipal(r *http.Request) *Principal {
	if p, ok := r.Context().Value(principalKey).(*Principal); ok {
		return p
	}
	return nil
}

// RequireAuth is a simple guard that can be used inside handlers.
// For page handlers we usually prefer explicit redirect in the handler for clarity.
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if GetPrincipal(r) == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ExigirVerificacionPagina manda a validar su identidad a quien entre a una
// página sin haberlo hecho.
//
// activo dice si la verificación está configurada: cuando no lo está —en
// desarrollo y en los tests— el guardia no estorba, porque nadie podría
// verificarse aunque quisiera. Los administradores quedan fuera: son quienes
// tienen que poder desatascar una cuenta.
func ExigirVerificacionPagina(activo bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := GetPrincipal(r)
			if !activo || p == nil || p.IsAdmin() || p.Verificado {
				next.ServeHTTP(w, r)
				return
			}
			// Sin identidad validada no hay nada que hacer en la aplicación, así
			// que se va a la única pantalla que sí sirve de algo. Ojo al montarlo:
			// /verificacion y /perfil tienen que quedar fuera de este grupo o el
			// redirect se muerde la cola.
			http.Redirect(w, r, "/verificacion", http.StatusSeeOther)
		})
	}
}

// ExigirVerificacion bloquea las llamadas de API que operan sobre pagarés a
// quien no tenga la identidad validada. Es el equivalente en JSON de
// ExigirVerificacionPagina: un redirect no le sirve de nada a un fetch.
func ExigirVerificacion(activo bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := GetPrincipal(r)
			if !activo || p == nil || p.IsAdmin() || p.Verificado {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"ok":                     false,
				"msg":                    "tienes que validar tu identidad con tu DNI antes de operar con pagarés",
				"verificacion_pendiente": true,
			})
		})
	}
}

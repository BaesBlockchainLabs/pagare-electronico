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

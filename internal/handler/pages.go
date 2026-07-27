package handler

import (
	"net/http"

	"pagare/internal/auth"
	"pagare/internal/templates"
)

type PageHandler struct {
	isDev bool
}

func NewPageHandler(isDev bool) *PageHandler {
	return &PageHandler{isDev: isDev}
}

// requirePrincipal redirects to /login if there is no authenticated user.
// This keeps the admin experience 100% unchanged once logged in.
func (p *PageHandler) requirePrincipal(w http.ResponseWriter, r *http.Request) *auth.Principal {
	pr := auth.GetPrincipal(r)
	if pr == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}
	return pr
}

func (p *PageHandler) Login(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, go straight to the dashboard.
	if auth.GetPrincipal(r) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Login(nil).Render(r.Context(), w)
}

func (p *PageHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{
		Username: principal.Username,
		Role:     string(principal.Role),
		IsAdmin:  principal.IsAdmin(),
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Dashboard(user).Render(r.Context(), w)
}

func (p *PageHandler) NuevoPagare(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{Username: principal.Username, Role: string(principal.Role), IsAdmin: principal.IsAdmin()}
	w.Header().Set("Content-Type", "text/html")
	templates.NuevoPagare(p.isDev, user).Render(r.Context(), w)
}

func (p *PageHandler) Historico(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	id := r.URL.Query().Get("id")
	user := &templates.CurrentUser{Username: principal.Username, Role: string(principal.Role), IsAdmin: principal.IsAdmin()}
	w.Header().Set("Content-Type", "text/html")
	templates.Historico(id, user).Render(r.Context(), w)
}

func (p *PageHandler) Verificar(w http.ResponseWriter, r *http.Request) {
	// Public verification page remains accessible without login.
	id := r.URL.Query().Get("id")
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "test"
	}
	// Public page, but render the nav consistently for logged-in users.
	var user *templates.CurrentUser
	if pr := auth.GetPrincipal(r); pr != nil {
		user = &templates.CurrentUser{Username: pr.Username, Role: string(pr.Role), IsAdmin: pr.IsAdmin()}
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Verificar(id, network, user).Render(r.Context(), w)
}

func (p *PageHandler) Admin(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	if !principal.IsAdmin() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	user := &templates.CurrentUser{
		Username: principal.Username,
		Role:     string(principal.Role),
		IsAdmin:  principal.IsAdmin(),
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Admin(user).Render(r.Context(), w)
}

func (p *PageHandler) Perfil(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{
		Username: principal.Username,
		Role:     string(principal.Role),
		IsAdmin:  principal.IsAdmin(),
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Perfil(user).Render(r.Context(), w)
}

func (p *PageHandler) Endosar(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{Username: principal.Username, Role: string(principal.Role), IsAdmin: principal.IsAdmin()}
	w.Header().Set("Content-Type", "text/html")
	templates.Endosar(user).Render(r.Context(), w)
}

func (p *PageHandler) PagarAnular(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{Username: principal.Username, Role: string(principal.Role), IsAdmin: principal.IsAdmin()}
	w.Header().Set("Content-Type", "text/html")
	templates.PagarAnular(user).Render(r.Context(), w)
}

// Ceder renders the ordinary-assignment form, the route left to a pagaré the
// «no a la orden» clause bars from endoso.
func (p *PageHandler) Ceder(w http.ResponseWriter, r *http.Request) {
	principal := p.requirePrincipal(w, r)
	if principal == nil {
		return
	}
	user := &templates.CurrentUser{Username: principal.Username, Role: string(principal.Role), IsAdmin: principal.IsAdmin()}
	w.Header().Set("Content-Type", "text/html")
	templates.Ceder(user).Render(r.Context(), w)
}

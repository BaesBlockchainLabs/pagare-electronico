package handler

import (
	"net/http"

	"pagare/internal/templates"
)

type PageHandler struct{}

func NewPageHandler() *PageHandler {
	return &PageHandler{}
}

func (p *PageHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	templates.Dashboard().Render(r.Context(), w)
}

func (p *PageHandler) NuevoPagare(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	templates.NuevoPagare().Render(r.Context(), w)
}

func (p *PageHandler) Historico(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	w.Header().Set("Content-Type", "text/html")
	templates.Historico(id).Render(r.Context(), w)
}

func (p *PageHandler) Verificar(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	network := r.URL.Query().Get("network")
	if network == "" {
		network = "test"
	}
	w.Header().Set("Content-Type", "text/html")
	templates.Verificar(id, network).Render(r.Context(), w)
}

func (p *PageHandler) Identidades(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	templates.Identidades().Render(r.Context(), w)
}

func (p *PageHandler) Endosar(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	templates.Endosar().Render(r.Context(), w)
}

func (p *PageHandler) PagarAnular(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	templates.PagarAnular().Render(r.Context(), w)
}

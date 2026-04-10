package main

import (
	"fmt"
	"log"
	"net/http"

	"pagare/internal/bcfclient"
	"pagare/internal/config"
	"pagare/internal/crypto"
	"pagare/internal/handler"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}

	bcfClient := bcfclient.New(cfg.Blockchain)
	cryptoSvc := crypto.NewService(bcfClient)

	pageHandler := handler.NewPageHandler()
	identidadHandler := handler.NewIdentidadHandler(bcfClient)
	consultaHandler := handler.NewConsultaHandler(bcfClient)
	pagareHandler := handler.NewPagareHandler(bcfClient, cryptoSvc)

	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)

	r.Get("/", pageHandler.Dashboard)
	r.Get("/pagares", pageHandler.Dashboard)
	r.Get("/pagares/nuevo", pageHandler.NuevoPagare)
	r.Get("/pagares/historico", pageHandler.Historico)
	r.Get("/pagares/verificar", pageHandler.Verificar)
	r.Get("/pagares/endosar", pageHandler.Endosar)
	r.Get("/pagares/pagar", pageHandler.PagarAnular)
	r.Get("/identidades", pageHandler.Identidades)
	r.Get("/health", handler.New().Health)

	r.Route("/api", func(r chi.Router) {
		r.Get("/system/hello", proxyToBCF(bcfClient, "Hello"))
		r.Get("/system/time", proxyToBCF(bcfClient, "Time"))

		r.Route("/identidades", func(r chi.Router) {
			r.Post("/keypair", identidadHandler.GenerateKeypair)
			r.Get("/keypair/application", identidadHandler.GetApplicationKeypair)
			r.Put("/keypair/pub", identidadHandler.AddPubKey)
			r.Post("/did", identidadHandler.GenerateDID)
		})

		r.Route("/pagares", func(r chi.Router) {
			r.Post("/", pagareHandler.Emitir)
			r.Put("/endoso", pagareHandler.Endosar)
			r.Delete("/", pagareHandler.PagarAnular)
			r.Get("/", consultaHandler.ListPagares)
			r.Get("/buscar", consultaHandler.GetPagare)
			r.Get("/historico", consultaHandler.GetHistorico)
			r.Get("/propietario", consultaHandler.GetPropietario)
			r.Get("/public", consultaHandler.GetPublicAsset)
		})
	})

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	log.Printf("Iniciando servidor en %s (env: %s, network: %s)", addr, cfg.Server.Env, cfg.Blockchain.Network)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Error iniciando servidor: %v", err)
	}
}

func proxyToBCF(client *bcfclient.Client, method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		var status int
		var err error

		switch method {
		case "Hello":
			body, status, err = client.Hello()
		case "Time":
			body, status, err = client.Time()
		default:
			handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "método desconocido"})
			return
		}

		if err != nil {
			handler.WriteJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "msg": err.Error()})
			return
		}
		handler.WriteRaw(w, status, body)
	}
}

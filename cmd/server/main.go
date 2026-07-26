package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pagare/internal/auth"
	"pagare/internal/bcfclient"
	"pagare/internal/config"
	"pagare/internal/crypto"
	"pagare/internal/handler"
	"pagare/internal/keyvault"
	"pagare/internal/scheduler"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func main() {
	seedUsers := flag.Int("seed", 0, "provisiona N usuarios de desarrollo (con keypair) y sale; sólo en APP_ENV=development")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Error cargando configuración: %v", err)
	}

	bcfClient := bcfclient.New(cfg.Blockchain)
	cryptoSvc := crypto.NewService(bcfClient)

	pageHandler := handler.NewPageHandler(cfg.IsDevelopment())
	identidadHandler := handler.NewIdentidadHandler(bcfClient)
	consultaHandler := handler.NewConsultaHandler(bcfClient)
	checker := scheduler.NewChecker(bcfClient)

	// Auth store (file-backed) + bootstrap first admin if requested via env.
	authStore, err := auth.NewStore("")
	if err != nil {
		log.Fatalf("Error inicializando almacén de usuarios: %v", err)
	}

	// Keyvault: seals users' private keys at rest (AES-256-GCM, master key from
	// KEYS_MASTER_KEY). Mandatory in production; falls back to an insecure dev
	// key otherwise so local runs need no setup.
	vault, err := keyvault.LoadFromEnv(os.Getenv("KEYS_MASTER_KEY"), cfg.IsDevelopment())
	if err != nil {
		log.Fatalf("Error inicializando keyvault: %v", err)
	}
	if vault.UsingDevKey() {
		log.Printf("⚠️  keyvault usando clave de DESARROLLO insegura (define KEYS_MASTER_KEY en producción)")
	}
	authStore.SetVault(vault)
	// Provision blockchain keypairs automatically on user creation.
	authStore.SetKeyProvisioner(cryptoSvc)
	// Resolve blockchain participants (firmante/endosatario) to registered users
	// from their public key, for the PDF.
	consultaHandler.SetUsers(authStore)

	// Pagaré handler signs on behalf of the logged-in user using their sealed
	// private key resolved from the store (no private key handled client-side).
	pagareHandler := handler.NewPagareHandler(bcfClient, cryptoSvc, authStore)

	// Development seed: provision N users with keypairs, then exit. Never in prod.
	if *seedUsers > 0 {
		if !cfg.IsDevelopment() {
			log.Fatalf("El seed de usuarios sólo está permitido en desarrollo (APP_ENV=development)")
		}
		log.Printf("Seed: provisionando %d usuarios de desarrollo…", *seedUsers)
		created, err := authStore.SeedDevUsers(*seedUsers)
		if err != nil {
			log.Fatalf("Seed falló tras crear %d usuarios: %v", created, err)
		}
		log.Printf("Seed completado: %d usuarios nuevos (contraseña 'seed1234').", created)
		return
	}

	if err := authStore.BootstrapAdmin(); err != nil {
		log.Printf("Aviso: no se pudo hacer bootstrap del admin: %v", err)
	}

	r := chi.NewRouter()

	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)

	// Populates principal from session cookie for all subsequent handlers (never blocks).
	r.Use(auth.AuthMiddleware(authStore))

	// Public routes (no login required)
	r.Get("/login", pageHandler.Login)
	r.Get("/pagares/verificar", pageHandler.Verificar)
	r.Get("/health", handler.New().Health)

	// Protected pages (require authentication; admin sees everything exactly as before)
	r.Get("/", pageHandler.Dashboard)
	r.Get("/pagares", pageHandler.Dashboard)
	r.Get("/pagares/nuevo", pageHandler.NuevoPagare)
	r.Get("/pagares/historico", pageHandler.Historico)
	r.Get("/pagares/endosar", pageHandler.Endosar)
	r.Get("/pagares/pagar", pageHandler.PagarAnular)
	r.Get("/perfil", pageHandler.Perfil)

	// requireAdmin guards admin-only action endpoints (JSON 403 for non-admins).
	requireAdmin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pr := auth.GetPrincipal(r)
			if pr == nil || !pr.IsAdmin() {
				handler.WriteJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "msg": "solo admin"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// Administration area: page + admin-only action endpoints, all under /admin.
	r.Route("/admin", func(r chi.Router) {
		// Admin page (the handler itself redirects non-admins to /).
		r.Get("/", pageHandler.Admin)

		// Admin-only actions (guarded by requireAdmin → 403 for non-admins).
		// All endpoints return JSON; the admin UI renders client-side.
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)

			// Application keypair (sensitive: the app's own blockchain credentials).
			r.Get("/keypair/application", identidadHandler.GetApplicationKeypair)

			// Full user list (all fields) for the admin management table.
			r.Get("/usuarios", func(w http.ResponseWriter, r *http.Request) {
				raw := authStore.List()
				views := make([]map[string]interface{}, 0, len(raw))
				for _, u := range raw {
					views = append(views, adminUserView(u))
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "usuarios": views})
			})

			// Create a platform user (JSON or form).
			r.Post("/usuarios", func(w http.ResponseWriter, r *http.Request) {
				in := parseUserInput(r)
				u := &auth.User{
					Username:     in.Username,
					Role:         auth.Role(in.Role),
					Nombre:       in.Nombre,
					Apellido:     in.Apellido,
					NIF:          in.NIF,
					Direccion:    in.Direccion,
					Localidad:    in.Localidad,
					CodigoPostal: in.CodigoPostal,
					Pais:         in.Pais,
				}
				if u.Role != auth.RoleAdmin {
					u.Role = auth.RoleUser
				}
				if err := authStore.CreateUser(u, in.Password); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				// Auto-provision the user's identity keypair (idempotent).
				if _, err := authStore.EnsureUserKeypair(u.ID); err != nil {
					log.Printf("Aviso: no se pudo generar keypair para %s: %v", u.Username, err)
					handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Usuario creado (sin keypair: " + err.Error() + ")"})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Usuario creado"})
			})

			// Update a user's personal data and role (password/pubkeys untouched).
			r.Post("/usuarios/{id}/update", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				u, err := authStore.GetByID(id)
				if err != nil {
					handler.WriteJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "msg": "usuario no encontrado"})
					return
				}
				in := parseUserInput(r)
				newRole := auth.Role(in.Role)
				if newRole != auth.RoleAdmin {
					newRole = auth.RoleUser
				}
				// Protect against demoting the last admin.
				if u.Role == auth.RoleAdmin && newRole != auth.RoleAdmin {
					if last, _ := authStore.IsLastAdmin(id); last {
						handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "no puedes quitar admin al último administrador"})
						return
					}
				}
				u.Role = newRole
				u.Nombre = in.Nombre
				u.Apellido = in.Apellido
				u.NIF = in.NIF
				u.Direccion = in.Direccion
				u.Localidad = in.Localidad
				u.CodigoPostal = in.CodigoPostal
				u.Pais = in.Pais
				u.DisplayName = in.DisplayName
				if err := authStore.UpdateUser(u); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Usuario actualizado"})
			})

			// Reset a user's password (admin sets a new one).
			r.Post("/usuarios/{id}/password", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				in := parseUserInput(r)
				if len(in.Password) < 6 {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "la contraseña debe tener al menos 6 caracteres"})
					return
				}
				if err := authStore.SetPassword(id, in.Password); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Contraseña restablecida"})
			})

			// Delete a user, with safeguards.
			r.Post("/usuarios/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
				pr := auth.GetPrincipal(r)
				id := chi.URLParam(r, "id")
				if id == pr.UserID {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "no puedes borrar tu propia cuenta de administrador"})
					return
				}
				if last, err := authStore.IsLastAdmin(id); err == nil && last {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "no puedes borrar el último administrador"})
					return
				}
				if err := authStore.DeleteUser(id); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Usuario eliminado"})
			})

			// Add a pubkey to a user.
			r.Post("/usuarios/{id}/pubkeys", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				var body struct {
					Pub string `json:"pub"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if body.Pub == "" {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "pub vacía"})
					return
				}
				if err := authStore.AddPubKey(id, body.Pub); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Clave añadida"})
			})

			// Remove a pubkey from a user.
			r.Post("/usuarios/{id}/pubkeys/remove", func(w http.ResponseWriter, r *http.Request) {
				id := chi.URLParam(r, "id")
				var body struct {
					Pub string `json:"pub"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if body.Pub == "" {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "pub vacía"})
					return
				}
				if err := authStore.RemovePubKey(id, body.Pub); err != nil {
					handler.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
					return
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Clave eliminada"})
			})
		})
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/system/hello", proxyToBCF(bcfClient, "Hello"))
		r.Get("/system/time", proxyToBCF(bcfClient, "Time"))

		// Authentication endpoints must be reachable without a prior session.
		authH := auth.NewHandlers(authStore, cryptoSvc)
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", authH.Login)
			r.Post("/register", authH.Register)
			r.Post("/logout", authH.Logout)
			r.Get("/me", authH.Me)
			r.Post("/claim/challenge", authH.IssueClaimChallenge)
			r.Post("/claim", authH.ClaimPub)
		})

		// Self-service profile (any authenticated user; handlers check the principal).
		r.Get("/perfil", authH.Profile)
		r.Post("/perfil", authH.UpdateProfile)
		r.Post("/perfil/password", authH.ChangePassword)

		// Usuarios de la plataforma (para selects en formularios, evita teclear a mano)
		// Cualquier usuario logueado puede listar (para rellenar beneficiario/firmante)
		r.Get("/usuarios", func(w http.ResponseWriter, r *http.Request) {
			principal := auth.GetPrincipal(r)
			if principal == nil {
				handler.WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
				return
			}
			users := authStore.List()
			// Public directory view for populating form dropdowns. Carries the
			// fiscal/address fields the pagaré forms autofill (firmante needs
			// address) but NEVER the private key.
			type UserView struct {
				ID           string   `json:"id"`
				Username     string   `json:"username"`
				DisplayName  string   `json:"display_name"`
				Nombre       string   `json:"nombre"`
				Apellido     string   `json:"apellido"`
				NIF          string   `json:"nif"`
				Direccion    string   `json:"direccion"`
				Localidad    string   `json:"localidad"`
				CodigoPostal string   `json:"codigo_postal"`
				Pais         string   `json:"pais"`
				PubKeys      []string `json:"pub_keys"`
			}
			views := make([]UserView, 0, len(users))
			for _, u := range users {
				pubs := u.PubKeys
				if pubs == nil {
					pubs = []string{}
				}
				views = append(views, UserView{
					ID:           u.ID,
					Username:     u.Username,
					DisplayName:  u.DisplayName,
					Nombre:       u.Nombre,
					Apellido:     u.Apellido,
					NIF:          u.NIF,
					Direccion:    u.Direccion,
					Localidad:    u.Localidad,
					CodigoPostal: u.CodigoPostal,
					Pais:         u.Pais,
					PubKeys:      pubs,
				})
			}
			handler.WriteJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "usuarios": views})
		})

		r.Route("/pagares", func(r chi.Router) {
			r.Post("/", pagareHandler.Emitir)
			r.Put("/endoso", pagareHandler.Endosar)
			r.Delete("/", pagareHandler.PagarAnular)
			r.Get("/", consultaHandler.ListPagares)
			r.Get("/buscar", consultaHandler.GetPagare)
			r.Get("/historico", consultaHandler.GetHistorico)
			r.Get("/pdf", consultaHandler.DescargarPDF)
			r.Get("/propietario", consultaHandler.GetPropietario)
			r.Get("/public", consultaHandler.GetPublicAsset)
			r.Get("/alertas", func(w http.ResponseWriter, r *http.Request) {
				principal := auth.GetPrincipal(r)
				if principal == nil {
					handler.WriteJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "autenticación requerida"})
					return
				}
				alertas, lastRun := checker.Alertas()
				// Regular users only see alerts for pagarés they own; admins see all.
				if !principal.IsAdmin() {
					mias := make([]scheduler.Alerta, 0, len(alertas))
					for _, a := range alertas {
						if consultaHandler.OwnsAsset(a.ID, principal) {
							mias = append(mias, a)
						}
					}
					alertas = mias
				}
				var last interface{}
				if !lastRun.IsZero() {
					last = lastRun.Format(time.RFC3339)
				}
				handler.WriteJSON(w, http.StatusOK, map[string]interface{}{
					"ok": true, "alertas": alertas, "count": len(alertas), "last_run": last,
				})
			})
		})
	})

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Chequeo periódico de pagarés vencidos/prescritos (solo lectura).
	go checker.Run(ctx, cfg.Server.CronInterval)

	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: r}

	go func() {
		log.Printf("Iniciando servidor en %s (env: %s, network: %s)", addr, cfg.Server.Env, cfg.Blockchain.Network)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error iniciando servidor: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("Apagando servidor…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error en apagado: %v", err)
	}
}

// adminUserView is the full per-user payload for the admin management table.
func adminUserView(u *auth.User) map[string]interface{} {
	pubKeys := u.PubKeys
	if pubKeys == nil {
		pubKeys = []string{}
	}
	return map[string]interface{}{
		"id":            u.ID,
		"username":      u.Username,
		"role":          string(u.Role),
		"display_name":  u.DisplayName,
		"nombre":        u.Nombre,
		"apellido":      u.Apellido,
		"nif":           u.NIF,
		"direccion":     u.Direccion,
		"localidad":     u.Localidad,
		"codigo_postal": u.CodigoPostal,
		"pais":          u.Pais,
		"pub_keys":      pubKeys,
		"created_at":    u.CreatedAt.Format(time.RFC3339),
	}
}

// userInput carries the editable fields submitted by the admin (JSON or form).
type userInput struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	DisplayName  string `json:"display_name"`
	Nombre       string `json:"nombre"`
	Apellido     string `json:"apellido"`
	NIF          string `json:"nif"`
	Direccion    string `json:"direccion"`
	Localidad    string `json:"localidad"`
	CodigoPostal string `json:"codigo_postal"`
	Pais         string `json:"pais"`
}

// parseUserInput reads a userInput from a JSON body or form values.
func parseUserInput(r *http.Request) userInput {
	in := userInput{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		json.NewDecoder(r.Body).Decode(&in)
		return in
	}
	_ = r.ParseForm()
	return userInput{
		Username:     r.FormValue("username"),
		Password:     r.FormValue("password"),
		Role:         r.FormValue("role"),
		DisplayName:  r.FormValue("display_name"),
		Nombre:       r.FormValue("nombre"),
		Apellido:     r.FormValue("apellido"),
		NIF:          r.FormValue("nif"),
		Direccion:    r.FormValue("direccion"),
		Localidad:    r.FormValue("localidad"),
		CodigoPostal: r.FormValue("codigo_postal"),
		Pais:         r.FormValue("pais"),
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

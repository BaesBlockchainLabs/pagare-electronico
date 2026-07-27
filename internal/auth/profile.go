package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Profile returns the current user's editable personal data.
func (h *Handlers) Profile(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}
	u, err := h.store.GetByID(p.UserID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "msg": "usuario no encontrado"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"username":      u.Username,
		"role":          u.Role,
		"display_name":  u.DisplayName,
		"nombre":        u.Nombre,
		"apellido":      u.Apellido,
		"nif":           u.NIF,
		"email":         u.Email,
		"telefono":      u.Telefono,
		"direccion":     u.Direccion,
		"localidad":     u.Localidad,
		"codigo_postal": u.CodigoPostal,
		"pais":          u.Pais,
		"pub_keys":      u.PubKeys,
	})
}

// UpdateProfile updates the current user's personal data. Accepts form values
// or JSON. Username, role and password are never changed through this path.
func (h *Handlers) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}

	in := ProfileInput{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&in)
	} else {
		_ = r.ParseForm()
		in = ProfileInput{
			DisplayName:  r.FormValue("display_name"),
			Nombre:       r.FormValue("nombre"),
			Apellido:     r.FormValue("apellido"),
			NIF:          r.FormValue("nif"),
			Email:        r.FormValue("email"),
			Telefono:     r.FormValue("telefono"),
			Direccion:    r.FormValue("direccion"),
			Localidad:    r.FormValue("localidad"),
			CodigoPostal: r.FormValue("codigo_postal"),
			Pais:         r.FormValue("pais"),
		}
	}

	if err := h.store.UpdateProfile(p.UserID, in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Datos actualizados"})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword verifies the current password and sets a new one for the
// authenticated user.
func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	p := GetPrincipal(r)
	if p == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "msg": "not authenticated"})
		return
	}

	var req changePasswordRequest
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&req)
	} else {
		_ = r.ParseForm()
		req.CurrentPassword = r.FormValue("current_password")
		req.NewPassword = r.FormValue("new_password")
	}

	if req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "la nueva contraseña es obligatoria"})
		return
	}
	if len(req.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "la nueva contraseña debe tener al menos 6 caracteres"})
		return
	}

	// Verify the current password by re-authenticating.
	if _, err := h.store.Authenticate(p.Username, req.CurrentPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": "la contraseña actual no es correcta"})
		return
	}

	if err := h.store.SetPassword(p.UserID, req.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "msg": "Contraseña actualizada"})
}

package auth

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Role         Role      `json:"role"`
	DisplayName  string    `json:"display_name,omitempty"`
	NIF          string    `json:"nif,omitempty"`

	// Datos personales completos (para usar en formularios de pagarés sin teclear a mano)
	Nombre       string `json:"nombre,omitempty"`
	Apellido     string `json:"apellido,omitempty"`
	Direccion    string `json:"direccion,omitempty"`
	Localidad    string `json:"localidad,omitempty"`
	CodigoPostal string `json:"codigo_postal,omitempty"`
	Pais         string `json:"pais,omitempty"`

	PubKeys   []string  `json:"pub_keys,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Principal is the authenticated identity carried in request context / session.
type Principal struct {
	UserID   string
	Username string
	Role     Role
	PubKeys  []string
}

func (p *Principal) IsAdmin() bool {
	return p != nil && p.Role == RoleAdmin
}

// HasPubKey reports whether the principal controls the given public key.
func (p *Principal) HasPubKey(pub string) bool {
	if p == nil {
		return false
	}
	for _, k := range p.PubKeys {
		if k == pub {
			return true
		}
	}
	return false
}

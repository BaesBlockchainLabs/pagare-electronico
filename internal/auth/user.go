package auth

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Role         Role   `json:"role"`
	DisplayName  string `json:"display_name,omitempty"`
	NIF          string `json:"nif,omitempty"`
	Email        string `json:"email,omitempty"`
	Telefono     string `json:"telefono,omitempty"`

	// Datos personales completos (para usar en formularios de pagarés sin teclear a mano)
	Nombre       string `json:"nombre,omitempty"`
	Apellido     string `json:"apellido,omitempty"`
	Direccion    string `json:"direccion,omitempty"`
	Localidad    string `json:"localidad,omitempty"`
	CodigoPostal string `json:"codigo_postal,omitempty"`
	Pais         string `json:"pais,omitempty"`

	// Identidad verificada contra el chip del DNI. Nombre, Apellido, NIF y
	// Direccion los fija esa verificación cuando se supera; los cinco campos
	// siguientes sólo existen porque los aporta el documento.
	Verificacion    EstadoVerificacion `json:"verificacion,omitempty"`
	VerificadoAt    *time.Time         `json:"verificado_at,omitempty"`
	FechaNacimiento string             `json:"fecha_nacimiento,omitempty"`
	Nacionalidad    string             `json:"nacionalidad,omitempty"`
	DocTipo         string             `json:"doc_tipo,omitempty"`
	DocNumero       string             `json:"doc_numero,omitempty"`
	DocCaducidad    string             `json:"doc_caducidad,omitempty"`

	PubKeys   []string  `json:"pub_keys,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Principal is the authenticated identity carried in request context / session.
type Principal struct {
	UserID   string
	Username string
	Role     Role
	PubKeys  []string
	// Verificado dice si la identidad del usuario está validada contra su DNI.
	// Va en el principal para que el guardia de las operaciones no tenga que
	// consultar la base de datos en cada petición.
	Verificado bool
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

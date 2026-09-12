package auth

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// EstadoVerificacion es en qué punto está la verificación de identidad de un
// usuario. El cero —cadena vacía— es "ni siquiera se ha intentado", que es lo
// que tienen los usuarios creados antes de que esto existiera.
type EstadoVerificacion string

const (
	VerificacionNoIniciada EstadoVerificacion = ""
	VerificacionPendiente  EstadoVerificacion = "pendiente"
	VerificacionVerificada EstadoVerificacion = "verificada"
	VerificacionFallida    EstadoVerificacion = "fallida"
)

// Verificacion es un intento de validar la identidad de un usuario contra el
// chip de su DNI. Se guarda uno por intento: un rechazo no se borra, porque
// forma parte del historial de la cuenta.
//
// Referencia es el identificador propio con el que se creó el envío, y GUID el
// que le asigna el portal, que puede no existir todavía cuando se abre.
type Verificacion struct {
	ID         string             `json:"id"`
	UserID     string             `json:"user_id"`
	Referencia string             `json:"referencia"`
	GUID       string             `json:"guid,omitempty"`
	Estado     EstadoVerificacion `json:"estado"`
	// Motivo explica un estado fallido en palabras que se le puedan enseñar al
	// usuario.
	Motivo string `json:"motivo,omitempty"`

	// Los tres siguientes son la trazabilidad de la evidencia en Logalty.
	TokenID         string `json:"token_id,omitempty"`
	ValidationID    string `json:"validation_id,omitempty"`
	HashDeclaracion string `json:"hash_declaracion,omitempty"`

	CreadaAt   time.Time  `json:"creada_at"`
	ResueltaAt *time.Time `json:"resuelta_at,omitempty"`
}

// Pendiente indica si esta verificación todavía puede resolverse.
func (v *Verificacion) Pendiente() bool {
	return v != nil && v.Estado == VerificacionPendiente
}

// CamposIdentidad son los datos que una verificación superada fija en el
// usuario. Vienen del documento, así que sustituyen a lo que hubiera: son más
// fiables que cualquier cosa tecleada.
type CamposIdentidad struct {
	NIF      string
	Nombre   string
	Apellido string

	// El domicilio del documento. No trae código postal —el chip del DNI no lo
	// lleva—, así que ése se queda como estaba y lo completa el usuario.
	Direccion string
	Localidad string
	Provincia string
	Pais      string

	Nacionalidad    string
	FechaNacimiento string
	DocTipo         string
	DocNumero       string
	DocCaducidad    string
}

// ErrVerificacionNoEncontrada es lo que devuelve una consulta sin resultado.
var ErrVerificacionNoEncontrada = errors.New("verificación no encontrada")

// CrearVerificacion abre un intento de verificación y le asigna id y fecha.
func (s *Store) CrearVerificacion(v *Verificacion) error {
	if v.UserID == "" || v.Referencia == "" {
		return errors.New("la verificación necesita usuario y referencia")
	}
	v.ID = newID()
	v.CreadaAt = time.Now().UTC()
	if v.Estado == "" {
		v.Estado = VerificacionPendiente
	}

	if _, err := s.db.Exec(`
		INSERT INTO verificaciones_identidad
		(id, user_id, referencia, guid, estado, motivo, token_id, validation_id,
		 hash_declaracion, creada_at, resuelta_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
	`, v.ID, v.UserID, v.Referencia, v.GUID, string(v.Estado), v.Motivo,
		v.TokenID, v.ValidationID, v.HashDeclaracion, v.CreadaAt); err != nil {
		return err
	}
	return s.fijarEstadoVerificacion(v.UserID, v.Estado, nil)
}

// UltimaVerificacion devuelve el intento más reciente de un usuario.
func (s *Store) UltimaVerificacion(userID string) (*Verificacion, error) {
	var v Verificacion
	var estado string
	var resuelta sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, user_id, referencia, COALESCE(guid, ''), estado, COALESCE(motivo, ''),
		       COALESCE(token_id, ''), COALESCE(validation_id, ''),
		       COALESCE(hash_declaracion, ''), creada_at, resuelta_at
		FROM verificaciones_identidad
		WHERE user_id = ?
		ORDER BY creada_at DESC
		LIMIT 1
	`, userID).Scan(&v.ID, &v.UserID, &v.Referencia, &v.GUID, &estado, &v.Motivo,
		&v.TokenID, &v.ValidationID, &v.HashDeclaracion, &v.CreadaAt, &resuelta)
	if err == sql.ErrNoRows {
		return nil, ErrVerificacionNoEncontrada
	}
	if err != nil {
		return nil, err
	}
	v.Estado = EstadoVerificacion(estado)
	if resuelta.Valid {
		t := resuelta.Time.UTC()
		v.ResueltaAt = &t
	}
	return &v, nil
}

// VerificacionesPendientes devuelve los intentos que todavía pueden resolverse,
// del más antiguo al más reciente: son los que hay que ir a consultar al
// portal.
func (s *Store) VerificacionesPendientes() ([]*Verificacion, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, referencia, COALESCE(guid, ''), estado, COALESCE(motivo, ''),
		       COALESCE(token_id, ''), COALESCE(validation_id, ''),
		       COALESCE(hash_declaracion, ''), creada_at
		FROM verificaciones_identidad
		WHERE estado = ?
		ORDER BY creada_at
	`, string(VerificacionPendiente))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Verificacion
	for rows.Next() {
		var v Verificacion
		var estado string
		if err := rows.Scan(&v.ID, &v.UserID, &v.Referencia, &v.GUID, &estado, &v.Motivo,
			&v.TokenID, &v.ValidationID, &v.HashDeclaracion, &v.CreadaAt); err != nil {
			continue
		}
		v.Estado = EstadoVerificacion(estado)
		out = append(out, &v)
	}
	return out, rows.Err()
}

// AnotarGUID guarda el GUID que el portal asigna al envío, que en los envíos
// asíncronos no está disponible hasta pasado un rato.
func (s *Store) AnotarGUID(verificacionID, guid string) error {
	if guid == "" {
		return nil
	}
	_, err := s.db.Exec(`UPDATE verificaciones_identidad SET guid = ? WHERE id = ?`,
		guid, verificacionID)
	return err
}

// FallarVerificacion cierra un intento sin éxito, dejando dicho por qué.
func (s *Store) FallarVerificacion(verificacionID, userID, motivo string) error {
	ahora := time.Now().UTC()
	if _, err := s.db.Exec(`
		UPDATE verificaciones_identidad SET estado = ?, motivo = ?, resuelta_at = ?
		WHERE id = ?
	`, string(VerificacionFallida), motivo, ahora, verificacionID); err != nil {
		return err
	}
	return s.fijarEstadoVerificacion(userID, VerificacionFallida, nil)
}

// ResolverVerificacion cierra un intento con éxito: anota la evidencia y
// vuelca sobre el usuario los datos leídos del documento.
//
// Las dos escrituras van en una transacción porque un usuario marcado como
// verificado sin sus datos —o al revés— es peor que un intento que se puede
// repetir.
func (s *Store) ResolverVerificacion(verificacionID, userID string, v Verificacion, c CamposIdentidad) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ahora := time.Now().UTC()
	if _, err := tx.Exec(`
		UPDATE verificaciones_identidad
		SET estado = ?, motivo = '', guid = ?, token_id = ?, validation_id = ?,
		    hash_declaracion = ?, resuelta_at = ?
		WHERE id = ?
	`, string(VerificacionVerificada), v.GUID, v.TokenID, v.ValidationID,
		v.HashDeclaracion, ahora, verificacionID); err != nil {
		return err
	}

	display := strings.TrimSpace(c.Nombre + " " + c.Apellido)
	if _, err := tx.Exec(`
		UPDATE users SET
			nif = ?, nombre = ?, apellido = ?, display_name = ?,
			direccion = COALESCE(NULLIF(?, ''), direccion),
			localidad = COALESCE(NULLIF(?, ''), localidad),
			provincia = COALESCE(NULLIF(?, ''), provincia),
			pais      = COALESCE(NULLIF(?, ''), pais),
			nacionalidad = ?, fecha_nacimiento = ?,
			doc_tipo = ?, doc_numero = ?, doc_caducidad = ?,
			verificacion_estado = ?, verificado_at = ?
		WHERE id = ?
	`, c.NIF, c.Nombre, c.Apellido, display,
		c.Direccion, c.Localidad, c.Provincia, c.Pais,
		c.Nacionalidad, c.FechaNacimiento,
		c.DocTipo, c.DocNumero, c.DocCaducidad,
		string(VerificacionVerificada), ahora, userID); err != nil {
		return err
	}

	return tx.Commit()
}

// EstadoVerificacionDe devuelve en qué punto está la verificación de un
// usuario sin cargar el usuario entero.
func (s *Store) EstadoVerificacionDe(userID string) (EstadoVerificacion, error) {
	var estado string
	err := s.db.QueryRow(
		`SELECT COALESCE(verificacion_estado, '') FROM users WHERE id = ?`, userID).Scan(&estado)
	if err == sql.ErrNoRows {
		return VerificacionNoIniciada, ErrUserNotFound
	}
	if err != nil {
		return VerificacionNoIniciada, err
	}
	return EstadoVerificacion(estado), nil
}

// fijarEstadoVerificacion refleja en el usuario el estado del intento en
// curso. verificado_at sólo se toca cuando se llega a verificado.
func (s *Store) fijarEstadoVerificacion(userID string, estado EstadoVerificacion, en *time.Time) error {
	if en == nil {
		_, err := s.db.Exec(
			`UPDATE users SET verificacion_estado = ? WHERE id = ?`, string(estado), userID)
		return err
	}
	_, err := s.db.Exec(
		`UPDATE users SET verificacion_estado = ?, verificado_at = ? WHERE id = ?`,
		string(estado), *en, userID)
	return err
}

package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"

	"pagare/internal/keyvault"
)

const (
	defaultDataDir   = "data"
	dbFileName       = "users.db"
	usersJSONBackup  = "users.json"
	defaultAdminRole = RoleAdmin
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user already exists")
	ErrInvalidPassword   = errors.New("invalid password")
)

type Store struct {
	db     *sql.DB
	vault  *keyvault.Vault // seals/opens users' private keys; see keys.go
	keygen KeyProvisioner  // provisions a keypair on user creation; see keys.go
}

// NewStore opens/creates the SQLite DB for users in the data dir.
// Migrates from old users.json if present (one-time).
func NewStore(dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, dbFileName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	// One-time migration from JSON if exists
	if err := s.migrateFromJSONIfNeeded(dataDir); err != nil {
		// non-fatal, continue with DB
	}

	return s, nil
}

func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT,
			role TEXT NOT NULL,
			display_name TEXT,
			nif TEXT,
			email TEXT,
			telefono TEXT,
			nombre TEXT,
			apellido TEXT,
			direccion TEXT,
			localidad TEXT,
			codigo_postal TEXT,
			pais TEXT,
			pub_keys TEXT,  -- stored as JSON array
			created_at DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

		-- Encrypted private keys, one row per public key. pvt_sealed is produced
		-- by internal/keyvault (never plaintext). enc_version/enc_algo make the
		-- table self-describing so the sealing scheme can evolve (A -> B) without
		-- breaking previously stored rows.
		CREATE TABLE IF NOT EXISTS user_keys (
			pub         TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			pvt_sealed  TEXT NOT NULL,
			enc_version INTEGER NOT NULL,
			enc_algo    TEXT NOT NULL,
			created_at  DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_user_keys_user ON user_keys(user_id);
	`)
	if err != nil {
		return err
	}
	// Migraciones aditivas idempotentes para BBDD ya existentes.
	s.ensureColumn("users", "email", "TEXT")
	s.ensureColumn("users", "telefono", "TEXT")
	return nil
}

// ensureColumn adds a column to a table if it does not already exist (SQLite has
// no "ADD COLUMN IF NOT EXISTS"). Safe to call on every start.
func (s *Store) ensureColumn(table, col, typ string) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == col {
			return // ya existe
		}
	}
	_, _ = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + typ)
}

func (s *Store) migrateFromJSONIfNeeded(dataDir string) error {
	jsonPath := filepath.Join(dataDir, usersJSONBackup)
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return nil
	}

	b, err := os.ReadFile(jsonPath)
	if err != nil {
		return err
	}
	var list []*User
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}

	for _, u := range list {
		pubJSON, _ := json.Marshal(u.PubKeys)
		_, err := s.db.Exec(`
			INSERT OR IGNORE INTO users 
			(id, username, password_hash, role, display_name, nif, nombre, apellido, direccion, localidad, codigo_postal, pais, pub_keys, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.NIF, u.Nombre, u.Apellido, u.Direccion, u.Localidad, u.CodigoPostal, u.Pais, string(pubJSON), u.CreatedAt)
		if err != nil {
			return err
		}
	}

	// Optional: backup the JSON
	_ = os.Rename(jsonPath, jsonPath+".migrated-"+time.Now().Format("20060102"))

	return nil
}

// BootstrapAdmin ensures the admin user (from ADMIN_USERNAME / ADMIN_PASSWORD)
// and optionally a test regular user (TEST_USER / TEST_PASSWORD) exist.
// Safe and idempotent using INSERT OR IGNORE.
func (s *Store) BootstrapAdmin() error {
	// Admin
	if username := os.Getenv("ADMIN_USERNAME"); username != "" {
		if password := os.Getenv("ADMIN_PASSWORD"); password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hashing admin password: %w", err)
			}
			id := newID()
			createdAt := time.Now().UTC()
			_, err = s.db.Exec(`
				INSERT OR IGNORE INTO users 
				(id, username, password_hash, role, display_name, pub_keys, created_at)
				VALUES (?, ?, ?, ?, ?, '[]', ?)
			`, id, username, string(hash), defaultAdminRole, "Administrator", createdAt)
			if err != nil {
				return err
			}
		}
	}

	// Optional test regular user
	if testUser := os.Getenv("TEST_USER"); testUser != "" {
		if testPass := os.Getenv("TEST_PASSWORD"); testPass != "" {
			thash, err := bcrypt.GenerateFromPassword([]byte(testPass), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hashing test user password: %w", err)
			}
			id := newID()
			createdAt := time.Now().UTC()
			_, err = s.db.Exec(`
				INSERT OR IGNORE INTO users 
				(id, username, password_hash, role, display_name, pub_keys, created_at)
				VALUES (?, ?, ?, ?, ?, '[]', ?)
			`, id, testUser, string(thash), RoleUser, "Usuario de prueba", createdAt)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Authenticate verifies username + password and returns a Principal on success.
func (s *Store) Authenticate(username, password string) (*Principal, error) {
	var id, pwHash, roleStr, pubJSON string
	err := s.db.QueryRow(`
		SELECT id, password_hash, role, COALESCE(pub_keys, '[]') 
		FROM users WHERE username = ?
	`, username).Scan(&id, &pwHash, &roleStr, &pubJSON)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(pwHash), []byte(password)); err != nil {
		return nil, ErrInvalidPassword
	}

	var pubs []string
	json.Unmarshal([]byte(pubJSON), &pubs)

	return &Principal{
		UserID:   id,
		Username: username,
		Role:     Role(roleStr),
		PubKeys:  pubs,
	}, nil
}

// GetPrincipalByID returns a Principal for an already-authenticated user ID.
func (s *Store) GetPrincipalByID(id string) (*Principal, error) {
	var username, roleStr, pubJSON string
	err := s.db.QueryRow(`
		SELECT username, role, COALESCE(pub_keys, '[]') 
		FROM users WHERE id = ?
	`, id).Scan(&username, &roleStr, &pubJSON)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	var pubs []string
	json.Unmarshal([]byte(pubJSON), &pubs)

	return &Principal{
		UserID:   id,
		Username: username,
		Role:     Role(roleStr),
		PubKeys:  pubs,
	}, nil
}

// AddPubKey claims a public key for the given user (idempotent).
func (s *Store) AddPubKey(userID, pub string) error {
	if pub == "" {
		return errors.New("pub key is required")
	}

	var pubJSON string
	err := s.db.QueryRow(`SELECT COALESCE(pub_keys, '[]') FROM users WHERE id = ?`, userID).Scan(&pubJSON)
	if err == sql.ErrNoRows {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}

	var pubs []string
	json.Unmarshal([]byte(pubJSON), &pubs)

	for _, existing := range pubs {
		if existing == pub {
			return nil
		}
	}
	pubs = append(pubs, pub)

	newJSON, _ := json.Marshal(pubs)
	_, err = s.db.Exec(`UPDATE users SET pub_keys = ? WHERE id = ?`, string(newJSON), userID)
	return err
}

// RemovePubKey removes a claimed public key (no-op if not present).
func (s *Store) RemovePubKey(userID, pub string) error {
	var pubJSON string
	err := s.db.QueryRow(`SELECT COALESCE(pub_keys, '[]') FROM users WHERE id = ?`, userID).Scan(&pubJSON)
	if err == sql.ErrNoRows {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}

	var pubs []string
	json.Unmarshal([]byte(pubJSON), &pubs)

	out := make([]string, 0, len(pubs))
	for _, k := range pubs {
		if k != pub {
			out = append(out, k)
		}
	}

	newJSON, _ := json.Marshal(out)
	_, err = s.db.Exec(`UPDATE users SET pub_keys = ? WHERE id = ?`, string(newJSON), userID)
	return err
}

// List returns all users (password_hash stripped for safety).
func (s *Store) List() []*User {
	rows, err := s.db.Query(`
		SELECT id, username, role, COALESCE(display_name, ''), COALESCE(nif, ''),
		       COALESCE(email, ''), COALESCE(telefono, ''),
		       COALESCE(nombre, ''), COALESCE(apellido, ''), COALESCE(direccion, ''),
		       COALESCE(localidad, ''), COALESCE(codigo_postal, ''), COALESCE(pais, ''),
		       COALESCE(pub_keys, '[]'), created_at
		FROM users
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		var u User
		var pubJSON string
		err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.NIF,
			&u.Email, &u.Telefono,
			&u.Nombre, &u.Apellido, &u.Direccion, &u.Localidad, &u.CodigoPostal,
			&u.Pais, &pubJSON, &u.CreatedAt)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(pubJSON), &u.PubKeys)
		// PasswordHash left empty
		out = append(out, &u)
	}
	return out
}

// GetByID returns a user (password_hash stripped).
func (s *Store) GetByID(id string) (*User, error) {
	var u User
	var pubJSON string
	err := s.db.QueryRow(`
		SELECT id, username, role, COALESCE(display_name, ''), COALESCE(nif, ''),
		       COALESCE(email, ''), COALESCE(telefono, ''),
		       COALESCE(nombre, ''), COALESCE(apellido, ''), COALESCE(direccion, ''),
		       COALESCE(localidad, ''), COALESCE(codigo_postal, ''), COALESCE(pais, ''),
		       COALESCE(pub_keys, '[]'), created_at
		FROM users WHERE id = ?
	`, id).Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.NIF,
		&u.Email, &u.Telefono,
		&u.Nombre, &u.Apellido, &u.Direccion, &u.Localidad, &u.CodigoPostal,
		&u.Pais, &pubJSON, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(pubJSON), &u.PubKeys)
	return &u, nil
}

// CreateUser creates a new platform user. If plainPassword provided, hashes it.
func (s *Store) CreateUser(u *User, plainPassword string) error {
	if u.Username == "" {
		return errors.New("username is required")
	}
	if plainPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		u.PasswordHash = string(hash)
	}
	u.ID = newID()
	u.CreatedAt = time.Now().UTC()

	pubJSON, _ := json.Marshal(u.PubKeys)

	_, err := s.db.Exec(`
		INSERT INTO users
		(id, username, password_hash, role, display_name, nif, email, telefono, nombre, apellido,
		 direccion, localidad, codigo_postal, pais, pub_keys, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, u.ID, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.NIF, u.Email, u.Telefono, u.Nombre, u.Apellido,
		u.Direccion, u.Localidad, u.CodigoPostal, u.Pais, string(pubJSON), u.CreatedAt)

	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrUserAlreadyExists
		}
		return err
	}
	return nil
}

// UpdateUser updates an existing user. PasswordHash preserved if empty.
func (s *Store) UpdateUser(u *User) error {
	// get existing hash if needed
	var existingHash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&existingHash)
	if err == sql.ErrNoRows {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}

	if u.PasswordHash == "" {
		u.PasswordHash = existingHash
	}

	pubJSON, _ := json.Marshal(u.PubKeys)

	_, err = s.db.Exec(`
		UPDATE users SET
			username = ?, password_hash = ?, role = ?, display_name = ?, nif = ?,
			email = ?, telefono = ?,
			nombre = ?, apellido = ?, direccion = ?, localidad = ?, codigo_postal = ?,
			pais = ?, pub_keys = ?, created_at = ?
		WHERE id = ?
	`, u.Username, u.PasswordHash, u.Role, u.DisplayName, u.NIF,
		u.Email, u.Telefono,
		u.Nombre, u.Apellido, u.Direccion, u.Localidad, u.CodigoPostal,
		u.Pais, string(pubJSON), u.CreatedAt, u.ID)

	return err
}

// SetPassword hashes and stores a new password for the given user.
func (s *Store) SetPassword(userID, plainPassword string) error {
	if plainPassword == "" {
		return errors.New("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ProfileInput carries the self-editable personal fields of a user's profile.
// It deliberately excludes username, role and password so a user can never
// escalate privileges or change their login identity through this path.
type ProfileInput struct {
	DisplayName  string
	Nombre       string
	Apellido     string
	NIF          string
	Email        string
	Telefono     string
	Direccion    string
	Localidad    string
	CodigoPostal string
	Pais         string
}

// UpdateProfile updates only the personal fields of the user's own profile.
// Username, role, password and pub_keys are preserved untouched.
func (s *Store) UpdateProfile(userID string, p ProfileInput) error {
	u, err := s.GetByID(userID)
	if err != nil {
		return err
	}
	u.DisplayName = p.DisplayName
	u.Nombre = p.Nombre
	u.Apellido = p.Apellido
	u.NIF = p.NIF
	u.Email = p.Email
	u.Telefono = p.Telefono
	u.Direccion = p.Direccion
	u.Localidad = p.Localidad
	u.CodigoPostal = p.CodigoPostal
	u.Pais = p.Pais
	return s.UpdateUser(u)
}

// DeleteUser removes a user completely (admin action), including any stored
// encrypted private keys.
func (s *Store) DeleteUser(id string) error {
	if _, err := s.db.Exec(`DELETE FROM user_keys WHERE user_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// GetUserRole returns the role for safeguards (e.g. prevent deleting last admin).
func (s *Store) GetUserRole(id string) (string, error) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role)
	return role, err
}

// IsLastAdmin returns true if this user is an admin and there is only one admin total.
func (s *Store) IsLastAdmin(id string) (bool, error) {
	role, err := s.GetUserRole(id)
	if err != nil {
		return false, err
	}
	if role != "admin" {
		return false, nil
	}
	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count <= 1, nil
}

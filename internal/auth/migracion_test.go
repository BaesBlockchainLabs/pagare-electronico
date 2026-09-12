package auth

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// esquemaAnterior es la tabla users tal como era antes de la validación de
// identidad: sin verificacion_estado, sin los campos que aporta el documento y
// sin provincia, y sin la tabla de verificaciones.
//
// Está copiado literalmente en lugar de generado para que este test siga
// probando la migración desde la base de datos que hay en producción, y no
// desde lo que el código de hoy crearía.
const esquemaAnterior = `
	CREATE TABLE users (
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
		pub_keys TEXT,
		created_at DATETIME
	);
	CREATE INDEX idx_users_username ON users(username);
	CREATE TABLE user_keys (
		pub         TEXT PRIMARY KEY,
		user_id     TEXT NOT NULL,
		pvt_sealed  TEXT NOT NULL,
		enc_version INTEGER NOT NULL,
		enc_algo    TEXT NOT NULL,
		created_at  DATETIME
	);
`

// Abrir una base de datos anterior tiene que migrarla sola: es lo que decide si
// una subida a producción necesita tocar la base de datos a mano o no.
func TestMigracionDesdeElEsquemaAnterior(t *testing.T) {
	dir := t.TempDir()
	ruta := filepath.Join(dir, dbFileName)

	db, err := sql.Open("sqlite", ruta)
	if err != nil {
		t.Fatalf("creando la base de datos anterior: %v", err)
	}
	if _, err := db.Exec(esquemaAnterior); err != nil {
		t.Fatalf("esquema anterior: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO users (id, username, password_hash, role, display_name, nif, nombre,
		                   apellido, direccion, localidad, codigo_postal, pais, pub_keys, created_at)
		VALUES ('u1', 'antiguo', '', 'user', 'Ana López', '12345678Z', 'Ana', 'López',
		        'Calle Mayor 10', 'Madrid', '28001', 'ES', '[]', '2026-01-01T00:00:00Z')
	`); err != nil {
		t.Fatalf("usuario anterior: %v", err)
	}
	db.Close()

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore sobre la base de datos anterior: %v", err)
	}

	// Lo que ya había sigue ahí y se lee sin tropezar con las columnas nuevas.
	u, err := s.GetByID("u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.Username != "antiguo" || u.NIF != "12345678Z" || u.CodigoPostal != "28001" {
		t.Errorf("los datos anteriores no se conservaron: %+v", u)
	}
	if u.Verificacion != VerificacionNoIniciada {
		t.Errorf("un usuario anterior tiene que quedar sin validar, no %q", u.Verificacion)
	}
	if len(s.List()) != 1 {
		t.Errorf("List devolvió %d usuarios, se esperaba 1", len(s.List()))
	}

	// Y las columnas y la tabla nuevas ya se pueden usar.
	v := &Verificacion{UserID: "u1", Referencia: "u1"}
	if err := s.CrearVerificacion(v); err != nil {
		t.Fatalf("CrearVerificacion tras migrar: %v", err)
	}
	if err := s.ResolverVerificacion(v.ID, "u1", *v, CamposIdentidad{
		NIF: "00000000T", Nombre: "NOMBRE", Apellido: "APELLIDOUNO",
		Direccion: "CALLE FALSA 1", Localidad: "CIUDAD", Provincia: "PROVINCIA", Pais: "ES",
		Nacionalidad: "España", FechaNacimiento: "1980-03-04", DocTipo: "DNI",
	}); err != nil {
		t.Fatalf("ResolverVerificacion tras migrar: %v", err)
	}

	got, err := s.GetByID("u1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Provincia != "PROVINCIA" || got.Nacionalidad != "España" || got.DocTipo != "DNI" {
		t.Errorf("las columnas nuevas no se escribieron: %+v", got)
	}
	if got.Verificacion != VerificacionVerificada {
		t.Errorf("estado = %q, se esperaba verificada", got.Verificacion)
	}

	// Reabrir no puede volver a migrar ni perder nada: el arranque la ejecuta
	// cada vez.
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("segundo NewStore: %v", err)
	}
	otra, err := s.GetByID("u1")
	if err != nil || otra.Provincia != "PROVINCIA" {
		t.Errorf("la migración no es idempotente: %v, %+v", err, otra)
	}
}

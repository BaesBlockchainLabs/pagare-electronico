package firma

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
)

// Estado es en qué punto está la firma de un documento.
type Estado string

const (
	Pendiente Estado = "pendiente"
	Firmada   Estado = "firmada"
	Fallida   Estado = "fallida"
)

// Operacion es qué se estaba haciendo con el pagaré cuando se pidió la firma.
type Operacion string

const (
	Emision Operacion = "emision"
	Endoso  Operacion = "endoso"
	Cesion  Operacion = "cesion"
)

// ErrNoEncontrada es lo que devuelve una consulta sin resultado.
var ErrNoEncontrada = errors.New("firma: no encontrada")

// Registro es una firma pedida sobre el PDF de un pagaré.
//
// Lleva lo que falta por hacer cuando llegue —la entrega al beneficiario en una
// emisión, el cambio de titularidad en un endoso— porque la operación no se
// completa hasta que el firmante firma, y entre las dos cosas pasa el tiempo
// que tarde una persona en leer su DNI.
type Registro struct {
	ID        string    `json:"id"`
	AssetID   string    `json:"asset_id"`
	Operacion Operacion `json:"operacion"`
	// UserID es quien tiene que firmar.
	UserID     string `json:"user_id"`
	Referencia string `json:"referencia"`
	GUID       string `json:"guid,omitempty"`
	Estado     Estado `json:"estado"`
	Motivo     string `json:"motivo,omitempty"`

	// HashOriginal es el SHA-256 del PDF que se mandó a firmar, y HashFirmado
	// el del que devolvió el portal. El primero es lo que ata la firma al
	// contenido que generamos.
	HashOriginal string `json:"hash_original"`
	HashFirmado  string `json:"hash_firmado,omitempty"`
	// RutaPDF es donde está guardado el PDF firmado, y RutaOriginal donde está
	// el que se mandó a firmar.
	RutaPDF      string `json:"ruta_pdf,omitempty"`
	RutaOriginal string `json:"ruta_original,omitempty"`

	// Pendiente es la operación en espera, serializada. Su forma la decide
	// quien la creó; este paquete sólo la guarda y la devuelve.
	Pendiente json.RawMessage `json:"pendiente,omitempty"`

	CreadaAt   time.Time  `json:"creada_at"`
	ResueltaAt *time.Time `json:"resuelta_at,omitempty"`
	// EjecutadaAt es cuándo se hizo lo que esperaba a la firma. Una firma
	// resuelta sin esto está a medias: el documento está firmado y guardado,
	// pero la operación no llegó a la cadena.
	EjecutadaAt *time.Time `json:"ejecutada_at,omitempty"`
}

// AMedias indica que la firma está buena pero su operación no se ejecutó. Se
// puede reintentar sin volver a firmar.
func (r *Registro) AMedias() bool {
	return r != nil && r.Estado == Firmada && r.EjecutadaAt == nil
}

// EnCurso indica si esta firma todavía puede resolverse.
func (r *Registro) EnCurso() bool { return r != nil && r.Estado == Pendiente }

// Registros guarda las firmas pedidas y los PDFs firmados.
//
// Va en su propia base de datos y no con los usuarios porque no es lo mismo:
// esto es el rastro de las operaciones sobre los títulos, y crece con ellas.
type Registros struct {
	db  *sql.DB
	dir string
}

// AbrirRegistros abre —creando si hace falta— el almacén de firmas en dataDir.
func AbrirRegistros(dataDir string) (*Registros, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "firmas"), 0700); err != nil {
		return nil, fmt.Errorf("firma: creando el directorio de firmas: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "firmas.db"))
	if err != nil {
		return nil, fmt.Errorf("firma: abriendo la base de datos: %w", err)
	}
	r := &Registros{db: db, dir: dataDir}
	if err := r.esquema(); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *Registros) esquema() error {
	_, err := r.db.Exec(`
		-- Firmas cualificadas pedidas sobre el PDF de un pagaré. Una por
		-- operación; las fallidas se conservan, porque forman parte del
		-- historial del título.
		CREATE TABLE IF NOT EXISTS firmas_pagare (
			id            TEXT PRIMARY KEY,
			asset_id      TEXT NOT NULL,
			operacion     TEXT NOT NULL,
			user_id       TEXT NOT NULL,
			referencia    TEXT NOT NULL UNIQUE,
			guid          TEXT,
			estado        TEXT NOT NULL,
			motivo        TEXT,
			hash_original TEXT NOT NULL,
			hash_firmado  TEXT,
			ruta_pdf      TEXT,
			-- El PDF que se mandó a firmar. Se guarda para que un reintento haga
			-- firmar el mismo documento y para que hash_original sea cotejable
			-- contra un fichero que tenemos, no sólo contra lo que devuelve el
			-- portal.
			ruta_original TEXT,
			pendiente     TEXT,
			creada_at     DATETIME,
			resuelta_at   DATETIME,
			-- Cuándo se ejecutó la operación que esperaba a la firma. Firmada
			-- sin esto es una firma buena con la operación a medias, que es lo
			-- que se puede reintentar sin volver a firmar.
			ejecutada_at  DATETIME
		);
		CREATE INDEX IF NOT EXISTS idx_firmas_asset  ON firmas_pagare(asset_id);
		CREATE INDEX IF NOT EXISTS idx_firmas_estado ON firmas_pagare(estado);
	`)
	if err != nil {
		return err
	}
	// Migración aditiva para las bases de datos creadas antes de distinguir la
	// firma a medias.
	r.asegurarColumna("firmas_pagare", "ejecutada_at", "DATETIME")
	r.asegurarColumna("firmas_pagare", "ruta_original", "TEXT")
	return nil
}

// asegurarColumna añade una columna si no está: SQLite no tiene
// "ADD COLUMN IF NOT EXISTS". Se puede llamar en cada arranque.
func (r *Registros) asegurarColumna(tabla, columna, tipo string) {
	filas, err := r.db.Query("PRAGMA table_info(" + tabla + ")")
	if err != nil {
		return
	}
	defer filas.Close()
	for filas.Next() {
		var cid, notnull, pk int
		var nombre, ctipo string
		var porDefecto any
		if err := filas.Scan(&cid, &nombre, &ctipo, &notnull, &porDefecto, &pk); err != nil {
			continue
		}
		if nombre == columna {
			return
		}
	}
	_, _ = r.db.Exec("ALTER TABLE " + tabla + " ADD COLUMN " + columna + " " + tipo)
}

// Cerrar suelta la base de datos.
func (r *Registros) Cerrar() error { return r.db.Close() }

// Crear abre una firma pendiente, guardando el documento que se mandó a firmar.
// Asigna id, fecha y la ruta del original.
//
// El original se escribe antes de la fila, por lo mismo que el firmado: un
// registro que dice tener un documento y no lo tiene miente, mientras que un
// fichero huérfano sólo ocupa sitio.
func (r *Registros) Crear(reg *Registro, original []byte) error {
	if reg.AssetID == "" || reg.Referencia == "" || reg.HashOriginal == "" {
		return errors.New("firma: la firma necesita pagaré, referencia y hash del original")
	}
	if len(original) == 0 {
		return errors.New("firma: falta el documento que se mandó a firmar")
	}
	reg.ID = nuevoID()
	reg.CreadaAt = time.Now().UTC()
	if reg.Estado == "" {
		reg.Estado = Pendiente
	}

	ruta := r.ruta(reg, "original")
	if err := os.WriteFile(ruta, original, 0600); err != nil {
		return fmt.Errorf("firma: guardando el documento a firmar: %w", err)
	}

	if _, err := r.db.Exec(`
		INSERT INTO firmas_pagare
		(id, asset_id, operacion, user_id, referencia, guid, estado, motivo,
		 hash_original, hash_firmado, ruta_pdf, ruta_original, pendiente, creada_at,
		 resuelta_at, ejecutada_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, '', '', ?, ?, ?, NULL, NULL)
	`, reg.ID, reg.AssetID, string(reg.Operacion), reg.UserID, reg.Referencia,
		reg.GUID, string(reg.Estado), reg.HashOriginal, ruta, string(reg.Pendiente),
		reg.CreadaAt); err != nil {
		os.Remove(ruta)
		return err
	}
	reg.RutaOriginal = ruta
	return nil
}

// ruta es dónde va un documento de una firma.
func (r *Registros) ruta(reg *Registro, clase string) string {
	return filepath.Join(r.dir, "firmas",
		fmt.Sprintf("%s-%s-%s-%s.pdf", reg.AssetID, reg.Operacion, reg.ID, clase))
}

const camposRegistro = `id, asset_id, operacion, user_id, referencia,
	COALESCE(guid, ''), estado, COALESCE(motivo, ''), hash_original,
	COALESCE(hash_firmado, ''), COALESCE(ruta_pdf, ''), COALESCE(ruta_original, ''),
	COALESCE(pendiente, ''), creada_at, resuelta_at, ejecutada_at`

func leerRegistro(escanear func(...any) error) (*Registro, error) {
	var reg Registro
	var operacion, estado, pendiente string
	var resuelta, ejecutada sql.NullTime
	if err := escanear(&reg.ID, &reg.AssetID, &operacion, &reg.UserID, &reg.Referencia,
		&reg.GUID, &estado, &reg.Motivo, &reg.HashOriginal,
		&reg.HashFirmado, &reg.RutaPDF, &reg.RutaOriginal, &pendiente,
		&reg.CreadaAt, &resuelta, &ejecutada); err != nil {
		return nil, err
	}
	reg.Operacion, reg.Estado = Operacion(operacion), Estado(estado)
	if pendiente != "" {
		reg.Pendiente = json.RawMessage(pendiente)
	}
	if resuelta.Valid {
		t := resuelta.Time.UTC()
		reg.ResueltaAt = &t
	}
	if ejecutada.Valid {
		t := ejecutada.Time.UTC()
		reg.EjecutadaAt = &t
	}
	return &reg, nil
}

// Ultima devuelve la firma más reciente de un pagaré.
func (r *Registros) Ultima(assetID string) (*Registro, error) {
	fila := r.db.QueryRow(`SELECT `+camposRegistro+`
		FROM firmas_pagare WHERE asset_id = ? ORDER BY creada_at DESC LIMIT 1`, assetID)
	reg, err := leerRegistro(fila.Scan)
	if err == sql.ErrNoRows {
		return nil, ErrNoEncontrada
	}
	return reg, err
}

// PorReferencia devuelve la firma de una referencia.
func (r *Registros) PorReferencia(referencia string) (*Registro, error) {
	fila := r.db.QueryRow(`SELECT `+camposRegistro+`
		FROM firmas_pagare WHERE referencia = ?`, referencia)
	reg, err := leerRegistro(fila.Scan)
	if err == sql.ErrNoRows {
		return nil, ErrNoEncontrada
	}
	return reg, err
}

// EnEspera devuelve las firmas sobre las que queda algo por hacer, de la más
// antigua a la más reciente: las que aún no se han firmado y las que están
// firmadas pero con su operación a medias.
func (r *Registros) EnEspera() ([]*Registro, error) {
	filas, err := r.db.Query(`SELECT `+camposRegistro+`
		FROM firmas_pagare
		WHERE estado = ? OR (estado = ? AND ejecutada_at IS NULL)
		ORDER BY creada_at`, string(Pendiente), string(Firmada))
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var out []*Registro
	for filas.Next() {
		reg, err := leerRegistro(filas.Scan)
		if err != nil {
			continue
		}
		out = append(out, reg)
	}
	return out, filas.Err()
}

// AnotarGUID guarda el GUID que el portal asigna al envío, que en los envíos
// asíncronos no está disponible hasta pasado un rato.
func (r *Registros) AnotarGUID(id, guid string) error {
	if guid == "" {
		return nil
	}
	_, err := r.db.Exec(`UPDATE firmas_pagare SET guid = ? WHERE id = ?`, guid, id)
	return err
}

// Fallar cierra una firma sin éxito, dejando dicho por qué.
func (r *Registros) Fallar(id, motivo string) error {
	_, err := r.db.Exec(`UPDATE firmas_pagare SET estado = ?, motivo = ?, resuelta_at = ?
		WHERE id = ?`, string(Fallida), motivo, time.Now().UTC(), id)
	return err
}

// Resolver guarda el PDF firmado y cierra la firma con éxito.
//
// El PDF se escribe antes de tocar la fila: una firma marcada como buena sin
// el documento que la respalda no sirve de nada, mientras que un fichero
// huérfano sólo ocupa sitio.
func (r *Registros) Resolver(reg *Registro, f *Firmado) error {
	ruta := r.ruta(reg, "firmado")
	if err := os.WriteFile(ruta, f.PDF, 0600); err != nil {
		return fmt.Errorf("firma: guardando el PDF firmado: %w", err)
	}

	ahora := time.Now().UTC()
	if _, err := r.db.Exec(`UPDATE firmas_pagare
		SET estado = ?, motivo = '', hash_firmado = ?, ruta_pdf = ?, resuelta_at = ?
		WHERE id = ?`, string(Firmada), f.Hash, ruta, ahora, reg.ID); err != nil {
		return err
	}
	reg.Estado, reg.HashFirmado, reg.RutaPDF, reg.ResueltaAt = Firmada, f.Hash, ruta, &ahora
	return nil
}

// MarcarEjecutada anota que la operación que esperaba a la firma ya se hizo.
func (r *Registros) MarcarEjecutada(reg *Registro) error {
	ahora := time.Now().UTC()
	if _, err := r.db.Exec(`UPDATE firmas_pagare SET ejecutada_at = ? WHERE id = ?`,
		ahora, reg.ID); err != nil {
		return err
	}
	reg.EjecutadaAt = &ahora
	return nil
}

// PDFFirmado devuelve el documento firmado de una firma resuelta.
func (r *Registros) PDFFirmado(reg *Registro) ([]byte, error) {
	if reg == nil || reg.RutaPDF == "" {
		return nil, ErrNoEncontrada
	}
	return os.ReadFile(reg.RutaPDF)
}

// PDFOriginal devuelve el documento tal como se mandó a firmar. Es lo que un
// reintento vuelve a mandar, para que se firme el mismo documento y no otro
// generado de nuevo.
func (r *Registros) PDFOriginal(reg *Registro) ([]byte, error) {
	if reg == nil || reg.RutaOriginal == "" {
		return nil, ErrNoEncontrada
	}
	return os.ReadFile(reg.RutaOriginal)
}

func nuevoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand no falla en la práctica; si lo hiciera, un id predecible
		// sería peor que no emitir el registro.
		panic("firma: sin entropía para el id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Todas devuelve las firmas de un pagaré, de la más antigua a la más reciente.
// Son las operaciones firmadas de su historia, incluidos los intentos que no
// llegaron a puerto.
func (r *Registros) Todas(assetID string) ([]*Registro, error) {
	filas, err := r.db.Query(`SELECT `+camposRegistro+`
		FROM firmas_pagare WHERE asset_id = ? ORDER BY creada_at`, assetID)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var out []*Registro
	for filas.Next() {
		reg, err := leerRegistro(filas.Scan)
		if err != nil {
			continue
		}
		out = append(out, reg)
	}
	return out, filas.Err()
}

// UltimasDe devuelve la firma más reciente de cada pagaré de la lista, indexada
// por pagaré. Los que no tienen ninguna no aparecen.
//
// Existe para que un listado pueda enseñar el estado de todos sus pagarés sin
// una consulta por fila, y es sólo lectura: no habla con el portal ni completa
// nada, al contrario que consultar el estado de uno.
func (r *Registros) UltimasDe(assetIDs []string) (map[string]*Registro, error) {
	out := make(map[string]*Registro, len(assetIDs))
	if len(assetIDs) == 0 {
		return out, nil
	}

	marcas := make([]string, len(assetIDs))
	args := make([]any, len(assetIDs))
	for i, id := range assetIDs {
		marcas[i] = "?"
		args[i] = id
	}

	filas, err := r.db.Query(`SELECT `+camposRegistro+`
		FROM firmas_pagare
		WHERE asset_id IN (`+strings.Join(marcas, ",")+`)
		ORDER BY creada_at`, args...)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	for filas.Next() {
		reg, err := leerRegistro(filas.Scan)
		if err != nil {
			continue
		}
		// Ordenadas de la más antigua a la más reciente, la última que se lee
		// de cada pagaré es la que manda.
		out[reg.AssetID] = reg
	}
	return out, filas.Err()
}

package auth

import (
	"database/sql"
	"errors"
	"time"

	"pagare/internal/keyvault"
)

// ErrNoVault is returned when a private-key operation is attempted but the store
// has no keyvault wired in (encryption is mandatory for private keys).
var ErrNoVault = errors.New("keyvault no configurado en el store")

// ErrPrivateKeyNotFound is returned when no stored private key matches.
var ErrPrivateKeyNotFound = errors.New("clave privada no encontrada")

// SetVault wires the encryption seam used to seal/open users' private keys.
// Must be called before any StorePrivateKey/GetPrivateKey.
func (s *Store) SetVault(v *keyvault.Vault) { s.vault = v }

// keyAAD binds a sealed private key to a specific user+pub so a stolen blob
// cannot be replayed onto a different record.
func keyAAD(userID, pub string) string { return userID + "|" + pub }

// StorePrivateKey seals pvt for (userID, pub) and persists it, also registering
// pub in the user's pub_keys list so ownership stays consistent. Idempotent on
// pub: re-storing overwrites the sealed value. The plaintext pvt is never
// written to the DB.
func (s *Store) StorePrivateKey(userID, pub, pvt string) error {
	if s.vault == nil {
		return ErrNoVault
	}
	if userID == "" || pub == "" || pvt == "" {
		return errors.New("userID, pub y pvt son obligatorios")
	}
	sealed, err := s.vault.Seal(pvt, keyAAD(userID, pub))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO user_keys (pub, user_id, pvt_sealed, enc_version, enc_algo, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(pub) DO UPDATE SET
			user_id = excluded.user_id,
			pvt_sealed = excluded.pvt_sealed,
			enc_version = excluded.enc_version,
			enc_algo = excluded.enc_algo
	`, pub, userID, sealed, s.vault.Version(), keyvault.AlgoName, time.Now().UTC())
	if err != nil {
		return err
	}
	// Keep the public side of the identity registered on the user.
	return s.AddPubKey(userID, pub)
}

// GetPrivateKey returns the decrypted private key for (userID, pub).
//
// INTERNAL USE ONLY — for the signing engine. The returned plaintext must never
// be serialized to JSON, logged, or rendered in a template.
func (s *Store) GetPrivateKey(userID, pub string) (string, error) {
	if s.vault == nil {
		return "", ErrNoVault
	}
	var sealed string
	err := s.db.QueryRow(
		`SELECT pvt_sealed FROM user_keys WHERE user_id = ? AND pub = ?`,
		userID, pub,
	).Scan(&sealed)
	if err == sql.ErrNoRows {
		return "", ErrPrivateKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return s.vault.Open(sealed, keyAAD(userID, pub))
}

// HasPrivateKey reports whether a sealed private key exists for (userID, pub),
// without decrypting it.
func (s *Store) HasPrivateKey(userID, pub string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM user_keys WHERE user_id = ? AND pub = ? LIMIT 1`,
		userID, pub,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

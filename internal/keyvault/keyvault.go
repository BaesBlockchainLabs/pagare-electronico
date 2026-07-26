// Package keyvault provides authenticated encryption of secrets at rest
// (currently users' blockchain private keys) for storage in the database.
//
// Scheme A (current): AES-256-GCM with a single master key (KEK) loaded from
// the environment. Each sealed value carries an explicit version prefix so we
// can migrate to scheme B (a key derived from the user's password, unlocked at
// login) later without breaking previously stored data — an Open on an old
// version keeps working while new writes use the new version.
package keyvault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// VersionAESGCM is the sealing scheme A: AES-256-GCM with the app master key.
const VersionAESGCM = 1

// AlgoName is a human/queryable label for the current scheme, stored alongside
// the version so the DB is self-describing for future migrations.
const AlgoName = "aes-256-gcm"

// devMasterMaterial derives a fixed, INSECURE key used only in development when
// no KEYS_MASTER_KEY is set, so local runs and the dev seed work out of the box.
// It must never be reachable in production (LoadFromEnv enforces that).
const devMasterMaterial = "pagare-dev-insecure-master-key-do-not-use-in-prod"

var (
	// ErrNoMasterKey is returned when production starts without a master key.
	ErrNoMasterKey = errors.New("keyvault: falta KEYS_MASTER_KEY (clave maestra de 32 bytes en hex o base64)")
	// ErrBadMasterKey is returned when the provided master key is malformed.
	ErrBadMasterKey = errors.New("keyvault: KEYS_MASTER_KEY inválida: debe ser 32 bytes en hex (64 chars) o base64")
	// ErrMalformedSealed is returned when a stored value is not a valid sealed blob.
	ErrMalformedSealed = errors.New("keyvault: valor cifrado con formato inválido")
	// ErrUnknownVersion is returned when a sealed value uses an unknown scheme.
	ErrUnknownVersion = errors.New("keyvault: versión de cifrado desconocida")
)

// Vault seals and opens secrets with the app master key.
type Vault struct {
	gcm cipher.AEAD
	dev bool // true when running on the insecure dev key
}

// LoadFromEnv builds a Vault from the KEYS_MASTER_KEY environment value.
//
//   - In production a valid 32-byte master key is mandatory; a missing or
//     malformed key is a fatal misconfiguration.
//   - In development, a missing key falls back to a fixed insecure key (with the
//     caller expected to warn) so local work needs no setup. A key that is
//     present but malformed is still an error, even in dev.
func LoadFromEnv(rawKey string, isDev bool) (*Vault, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		if !isDev {
			return nil, ErrNoMasterKey
		}
		sum := sha256.Sum256([]byte(devMasterMaterial))
		v, err := newVault(sum[:])
		if err != nil {
			return nil, err
		}
		v.dev = true
		return v, nil
	}
	key, err := parseMasterKey(rawKey)
	if err != nil {
		return nil, err
	}
	return newVault(key)
}

// UsingDevKey reports whether the vault fell back to the insecure development
// key, so the caller can emit a loud warning at startup.
func (v *Vault) UsingDevKey() bool { return v.dev }

// parseMasterKey accepts a 32-byte key encoded as hex (64 chars) or base64.
func parseMasterKey(s string) ([]byte, error) {
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, ErrBadMasterKey
}

func newVault(key []byte) (*Vault, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keyvault: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keyvault: %w", err)
	}
	return &Vault{gcm: gcm}, nil
}

// Version reports the scheme used for new writes.
func (v *Vault) Version() int { return VersionAESGCM }

// Seal encrypts plaintext, binding it to aad (which must be supplied verbatim to
// Open). aad is authenticated but not stored; use a stable identifier such as
// "<userID>|<pub>" so a sealed value cannot be transplanted onto another record.
// The result is "<version>:<base64(nonce||ciphertext)>".
func (v *Vault) Seal(plaintext, aad string) (string, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("keyvault: nonce: %w", err)
	}
	ct := v.gcm.Seal(nonce, nonce, []byte(plaintext), []byte(aad))
	return strconv.Itoa(VersionAESGCM) + ":" + base64.StdEncoding.EncodeToString(ct), nil
}

// Open reverses Seal. aad must match the value passed to Seal or authentication
// fails. A wrong master key, tampered ciphertext, or mismatched aad all yield an
// error rather than corrupt plaintext.
func (v *Vault) Open(sealed, aad string) (string, error) {
	ver, blob, err := splitSealed(sealed)
	if err != nil {
		return "", err
	}
	if ver != VersionAESGCM {
		return "", fmt.Errorf("%w: %d", ErrUnknownVersion, ver)
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", ErrMalformedSealed
	}
	ns := v.gcm.NonceSize()
	if len(raw) < ns {
		return "", ErrMalformedSealed
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := v.gcm.Open(nil, nonce, ct, []byte(aad))
	if err != nil {
		return "", fmt.Errorf("keyvault: no se pudo descifrar (clave maestra o datos incorrectos): %w", err)
	}
	return string(pt), nil
}

// SealedVersion extracts the scheme version from a stored value without needing
// the key, so callers/migrations can inspect what a record was sealed with.
func SealedVersion(sealed string) (int, error) {
	ver, _, err := splitSealed(sealed)
	return ver, err
}

func splitSealed(sealed string) (int, string, error) {
	i := strings.IndexByte(sealed, ':')
	if i <= 0 {
		return 0, "", ErrMalformedSealed
	}
	ver, err := strconv.Atoi(sealed[:i])
	if err != nil {
		return 0, "", ErrMalformedSealed
	}
	return ver, sealed[i+1:], nil
}

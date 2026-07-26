package keyvault

import (
	"encoding/hex"
	"strings"
	"testing"
)

const testKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func mustVault(t *testing.T) *Vault {
	t.Helper()
	v, err := LoadFromEnv(testKeyHex, false)
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	return v
}

func TestSealOpenRoundTrip(t *testing.T) {
	v := mustVault(t)
	const pvt = "super-secret-ed25519-private-key"
	sealed, err := v.Seal(pvt, "user1|pubABC")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, pvt) {
		t.Fatal("sealed value leaks plaintext")
	}
	if !strings.HasPrefix(sealed, "1:") {
		t.Fatalf("expected version prefix, got %q", sealed)
	}
	got, err := v.Open(sealed, "user1|pubABC")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != pvt {
		t.Fatalf("round trip mismatch: got %q", got)
	}
}

func TestOpenWrongAADFails(t *testing.T) {
	v := mustVault(t)
	sealed, _ := v.Seal("secret", "user1|pubABC")
	if _, err := v.Open(sealed, "user2|pubABC"); err == nil {
		t.Fatal("expected auth failure with mismatched AAD")
	}
}

func TestOpenTamperedFails(t *testing.T) {
	v := mustVault(t)
	sealed, _ := v.Seal("secret", "aad")
	// Flip a character in the base64 body.
	b := []byte(sealed)
	b[len(b)-2] ^= 0x01
	if _, err := v.Open(string(b), "aad"); err == nil {
		t.Fatal("expected failure on tampered ciphertext")
	}
}

func TestOpenWrongMasterKeyFails(t *testing.T) {
	v := mustVault(t)
	sealed, _ := v.Seal("secret", "aad")

	otherKey := strings.Repeat("ab", 32) // different 32-byte hex key
	v2, err := LoadFromEnv(otherKey, false)
	if err != nil {
		t.Fatalf("LoadFromEnv other: %v", err)
	}
	if _, err := v2.Open(sealed, "aad"); err == nil {
		t.Fatal("expected failure decrypting with a different master key")
	}
}

func TestProdRequiresMasterKey(t *testing.T) {
	if _, err := LoadFromEnv("", false); err != ErrNoMasterKey {
		t.Fatalf("expected ErrNoMasterKey in prod, got %v", err)
	}
}

func TestDevFallbackKey(t *testing.T) {
	v, err := LoadFromEnv("", true)
	if err != nil {
		t.Fatalf("dev LoadFromEnv: %v", err)
	}
	if !v.UsingDevKey() {
		t.Fatal("expected UsingDevKey to be true")
	}
	sealed, _ := v.Seal("x", "aad")
	if got, _ := v.Open(sealed, "aad"); got != "x" {
		t.Fatal("dev vault round trip failed")
	}
}

func TestMalformedMasterKey(t *testing.T) {
	if _, err := LoadFromEnv("not-a-valid-key", false); err != ErrBadMasterKey {
		t.Fatalf("expected ErrBadMasterKey, got %v", err)
	}
}

func TestBase64MasterKeyAccepted(t *testing.T) {
	raw, _ := hex.DecodeString(testKeyHex)
	// std base64 of the 32-byte key
	b64 := "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	if _, err := LoadFromEnv(b64, false); err != nil {
		t.Fatalf("base64 key rejected: %v", err)
	}
	_ = raw
}

func TestSealedVersion(t *testing.T) {
	v := mustVault(t)
	sealed, _ := v.Seal("x", "aad")
	ver, err := SealedVersion(sealed)
	if err != nil {
		t.Fatalf("SealedVersion: %v", err)
	}
	if ver != VersionAESGCM {
		t.Fatalf("expected version %d, got %d", VersionAESGCM, ver)
	}
}

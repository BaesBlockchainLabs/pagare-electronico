package auth

import (
	"errors"
	"fmt"
	"testing"

	"pagare/internal/keyvault"
)

func newVaultStore(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	v, err := keyvault.LoadFromEnv("", true) // dev key is fine for tests
	if err != nil {
		t.Fatalf("keyvault: %v", err)
	}
	s.SetVault(v)
	return s
}

func TestStoreAndGetPrivateKey(t *testing.T) {
	s := newVaultStore(t)
	u := &User{Username: "alice", Role: RoleUser}
	if err := s.CreateUser(u, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	const pub, pvt = "pubABC", "the-private-key"
	if err := s.StorePrivateKey(u.ID, pub, pvt); err != nil {
		t.Fatalf("StorePrivateKey: %v", err)
	}

	// pub is registered on the user.
	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if len(got.PubKeys) != 1 || got.PubKeys[0] != pub {
		t.Fatalf("expected pub registered, got %v", got.PubKeys)
	}

	// pvt round-trips through decryption.
	back, err := s.GetPrivateKey(u.ID, pub)
	if err != nil {
		t.Fatalf("GetPrivateKey: %v", err)
	}
	if back != pvt {
		t.Fatalf("pvt mismatch: got %q", back)
	}
}

func TestPrivateKeyNotStoredInPlaintext(t *testing.T) {
	s := newVaultStore(t)
	u := &User{Username: "bob", Role: RoleUser}
	_ = s.CreateUser(u, "pw")
	const pvt = "PLAINTEXT-SENTINEL-9f3a"
	if err := s.StorePrivateKey(u.ID, "pubX", pvt); err != nil {
		t.Fatalf("StorePrivateKey: %v", err)
	}

	var sealed string
	if err := s.db.QueryRow(`SELECT pvt_sealed FROM user_keys WHERE pub = ?`, "pubX").Scan(&sealed); err != nil {
		t.Fatalf("query: %v", err)
	}
	if sealed == pvt || contains(sealed, pvt) {
		t.Fatalf("private key stored in plaintext: %q", sealed)
	}
}

func TestHasPrivateKey(t *testing.T) {
	s := newVaultStore(t)
	u := &User{Username: "carol", Role: RoleUser}
	_ = s.CreateUser(u, "pw")

	if ok, _ := s.HasPrivateKey(u.ID, "nope"); ok {
		t.Fatal("expected no key")
	}
	_ = s.StorePrivateKey(u.ID, "pubY", "k")
	if ok, err := s.HasPrivateKey(u.ID, "pubY"); err != nil || !ok {
		t.Fatalf("expected key present, ok=%v err=%v", ok, err)
	}
}

func TestGetPrivateKeyMissing(t *testing.T) {
	s := newVaultStore(t)
	if _, err := s.GetPrivateKey("u", "p"); !errors.Is(err, ErrPrivateKeyNotFound) {
		t.Fatalf("expected ErrPrivateKeyNotFound, got %v", err)
	}
}

func TestPrivateKeyRequiresVault(t *testing.T) {
	s := newTestStore(t) // no vault
	if err := s.StorePrivateKey("u", "p", "k"); !errors.Is(err, ErrNoVault) {
		t.Fatalf("expected ErrNoVault, got %v", err)
	}
}

func TestDeleteUserRemovesKeys(t *testing.T) {
	s := newVaultStore(t)
	u := &User{Username: "dave", Role: RoleUser}
	_ = s.CreateUser(u, "pw")
	_ = s.StorePrivateKey(u.ID, "pubZ", "k")

	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if ok, _ := s.HasPrivateKey(u.ID, "pubZ"); ok {
		t.Fatal("key should be gone after user delete")
	}
}

// fakeProvisioner hands out deterministic, unique keypairs and counts calls.
type fakeProvisioner struct{ n int }

func (f *fakeProvisioner) GenerateKeypair() (string, string, error) {
	f.n++
	return fmt.Sprintf("pub-%d", f.n), fmt.Sprintf("pvt-%d", f.n), nil
}

func TestEnsureUserKeypairProvisionsOnce(t *testing.T) {
	s := newVaultStore(t)
	fp := &fakeProvisioner{}
	s.SetKeyProvisioner(fp)

	u := &User{Username: "erin", Role: RoleUser}
	if err := s.CreateUser(u, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	pub1, err := s.EnsureUserKeypair(u.ID)
	if err != nil {
		t.Fatalf("EnsureUserKeypair: %v", err)
	}
	if pub1 == "" {
		t.Fatal("expected a pub")
	}
	// The sealed pvt round-trips.
	if pvt, err := s.GetPrivateKey(u.ID, pub1); err != nil || pvt != "pvt-1" {
		t.Fatalf("GetPrivateKey: pvt=%q err=%v", pvt, err)
	}

	// Idempotent: second call provisions nothing new and returns same pub.
	pub2, err := s.EnsureUserKeypair(u.ID)
	if err != nil {
		t.Fatalf("EnsureUserKeypair 2: %v", err)
	}
	if pub2 != pub1 {
		t.Fatalf("expected same pub, got %q vs %q", pub2, pub1)
	}
	if fp.n != 1 {
		t.Fatalf("expected exactly one provisioning call, got %d", fp.n)
	}
}

func TestEnsureUserKeypairNoProvisioner(t *testing.T) {
	s := newVaultStore(t) // vault but no provisioner
	u := &User{Username: "frank", Role: RoleUser}
	_ = s.CreateUser(u, "pw")
	if _, err := s.EnsureUserKeypair(u.ID); !errors.Is(err, ErrNoKeyProvisioner) {
		t.Fatalf("expected ErrNoKeyProvisioner, got %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

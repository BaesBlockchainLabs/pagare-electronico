package auth

import (
	"testing"

	"pagare/internal/keyvault"
)

func TestSeedDevUsers(t *testing.T) {
	s := newTestStore(t)
	v, _ := keyvault.LoadFromEnv("", true)
	s.SetVault(v)
	s.SetKeyProvisioner(&fakeProvisioner{})

	created, err := s.SeedDevUsers(10)
	if err != nil {
		t.Fatalf("SeedDevUsers: %v", err)
	}
	if created != 10 {
		t.Fatalf("expected 10 created, got %d", created)
	}

	// Each seed user has data and exactly one keypair.
	u, err := s.getByUsername("seed001")
	if err != nil || u == nil {
		t.Fatalf("seed001 missing: %v", err)
	}
	if u.Nombre == "" || u.NIF == "" || u.Localidad == "" || len(u.PubKeys) != 1 {
		t.Fatalf("seed001 incomplete: %+v", u)
	}

	// Seed users can authenticate with the shared dev password.
	if _, err := s.Authenticate("seed001", seedPassword); err != nil {
		t.Fatalf("seed001 login: %v", err)
	}

	// Idempotent: a second run creates nothing new.
	again, err := s.SeedDevUsers(10)
	if err != nil {
		t.Fatalf("SeedDevUsers 2: %v", err)
	}
	if again != 0 {
		t.Fatalf("expected 0 on re-seed, got %d", again)
	}
}

func TestSeedRequiresVaultAndProvisioner(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedDevUsers(1); err != ErrNoVault {
		t.Fatalf("expected ErrNoVault, got %v", err)
	}
	v, _ := keyvault.LoadFromEnv("", true)
	s.SetVault(v)
	if _, err := s.SeedDevUsers(1); err != ErrNoKeyProvisioner {
		t.Fatalf("expected ErrNoKeyProvisioner, got %v", err)
	}
}

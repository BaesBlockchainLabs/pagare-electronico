package auth

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestSetPassword(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "alice", Role: RoleUser}
	if err := s.CreateUser(u, "oldpass"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Old password authenticates.
	if _, err := s.Authenticate("alice", "oldpass"); err != nil {
		t.Fatalf("expected old password to authenticate: %v", err)
	}

	// Change it.
	if err := s.SetPassword(u.ID, "newpass"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	// New password works, old one no longer does.
	if _, err := s.Authenticate("alice", "newpass"); err != nil {
		t.Fatalf("expected new password to authenticate: %v", err)
	}
	if _, err := s.Authenticate("alice", "oldpass"); err == nil {
		t.Fatalf("expected old password to fail after change")
	}

	// Empty password is rejected; unknown user returns ErrUserNotFound.
	if err := s.SetPassword(u.ID, ""); err == nil {
		t.Fatalf("expected error for empty password")
	}
	if err := s.SetPassword("does-not-exist", "x"); err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUpdateProfile_DoesNotChangeRoleOrUsername(t *testing.T) {
	s := newTestStore(t)
	u := &User{Username: "bob", Role: RoleAdmin, Nombre: "Bob", Apellido: "Old"}
	if err := s.CreateUser(u, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	err := s.UpdateProfile(u.ID, ProfileInput{
		Nombre:       "Roberto",
		Apellido:     "Nuevo",
		NIF:          "12345678Z",
		Direccion:    "Calle Mayor 1",
		Localidad:    "Madrid",
		CodigoPostal: "28001",
		Pais:         "ES",
		DisplayName:  "Rob",
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	got, err := s.GetByID(u.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	// Personal fields updated.
	if got.Nombre != "Roberto" || got.Apellido != "Nuevo" || got.NIF != "12345678Z" {
		t.Fatalf("personal fields not updated: %+v", got)
	}
	if got.Direccion != "Calle Mayor 1" || got.Localidad != "Madrid" || got.CodigoPostal != "28001" || got.Pais != "ES" {
		t.Fatalf("address fields not updated: %+v", got)
	}

	// Identity / privilege fields preserved.
	if got.Username != "bob" {
		t.Fatalf("username changed: got %q", got.Username)
	}
	if got.Role != RoleAdmin {
		t.Fatalf("role changed: got %q", got.Role)
	}

	// Password still valid after profile update.
	if _, err := s.Authenticate("bob", "pw"); err != nil {
		t.Fatalf("password broken after UpdateProfile: %v", err)
	}
}

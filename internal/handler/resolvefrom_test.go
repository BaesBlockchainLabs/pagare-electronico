package handler

import (
	"errors"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/models"
)

// fakeKeys is a SigningKeys stub returning a canned private key for a known
// pub, and optionally provisioning one for a user who has none.
type fakeKeys struct {
	pvtByPub map[string]string
	err      error
	// provisiona is the pub handed out to a keyless user; empty means
	// provisioning fails.
	provisiona string
}

func (f fakeKeys) GetPrivateKey(userID, pub string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if pvt, ok := f.pvtByPub[pub]; ok {
		return pvt, nil
	}
	return "", errors.New("not found")
}

func (f fakeKeys) EnsureUserKeypair(userID string) (string, error) {
	if f.provisiona == "" {
		return "", errors.New("sin aprovisionador de claves")
	}
	return f.provisiona, nil
}

func newSigningHandler(keys SigningKeys) *PagareHandler {
	return NewPagareHandler(nil, nil, keys)
}

func TestResolveFrom_ManualPvtUsedVerbatim(t *testing.T) {
	h := newSigningHandler(fakeKeys{})
	p := &auth.Principal{UserID: "u1", PubKeys: []string{"pubA"}}
	in := &models.IdentidadBC{Pub: "external", Pvt: "external-pvt"}

	got, err := h.resolveFrom(p, in)
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got != in {
		t.Fatalf("expected the supplied identity to be used verbatim, got %+v", got)
	}
}

func TestResolveFrom_DefaultsToFirstOwnedKey(t *testing.T) {
	h := newSigningHandler(fakeKeys{pvtByPub: map[string]string{"pubA": "pvtA"}})
	p := &auth.Principal{UserID: "u1", PubKeys: []string{"pubA", "pubB"}}

	got, err := h.resolveFrom(p, nil)
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got == nil || got.Pub != "pubA" || got.Pvt != "pvtA" {
		t.Fatalf("expected server-resolved pubA/pvtA, got %+v", got)
	}
}

func TestResolveFrom_ExplicitOwnedPub(t *testing.T) {
	h := newSigningHandler(fakeKeys{pvtByPub: map[string]string{"pubB": "pvtB"}})
	p := &auth.Principal{UserID: "u1", PubKeys: []string{"pubA", "pubB"}}

	got, err := h.resolveFrom(p, &models.IdentidadBC{Pub: "pubB"})
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got.Pub != "pubB" || got.Pvt != "pvtB" {
		t.Fatalf("expected pubB/pvtB, got %+v", got)
	}
}

func TestResolveFrom_RejectsUnownedPub(t *testing.T) {
	h := newSigningHandler(fakeKeys{pvtByPub: map[string]string{"pubX": "pvtX"}})
	p := &auth.Principal{UserID: "u1", PubKeys: []string{"pubA"}}

	if _, err := h.resolveFrom(p, &models.IdentidadBC{Pub: "pubX"}); err == nil {
		t.Fatal("expected error when pub is not owned by the principal")
	}
}

// A key is provisioned on a best-effort basis at registration, so an account
// can legitimately have none. Since art. 94.7 makes the signature essential,
// such a user gets a key here rather than being turned away.
func TestResolveFrom_ProvisionaClaveAlUsuarioQueNoTiene(t *testing.T) {
	h := newSigningHandler(fakeKeys{
		provisiona: "pubNueva",
		pvtByPub:   map[string]string{"pubNueva": "pvtNueva"},
	})
	p := &auth.Principal{UserID: "u1"} // sin claves

	got, err := h.resolveFrom(p, nil)
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got == nil || got.Pub != "pubNueva" || got.Pvt != "pvtNueva" {
		t.Fatalf("esperaba la clave recién aprovisionada, got %+v", got)
	}
}

func TestResolveFrom_FalloAlAprovisionarEsError(t *testing.T) {
	h := newSigningHandler(fakeKeys{}) // provisiona == "" => falla
	p := &auth.Principal{UserID: "u1"}

	if _, err := h.resolveFrom(p, nil); err == nil {
		t.Fatal("esperaba error cuando no puede aprovisionarse una clave")
	}
}

// Without a key store there is nothing to sign with and nothing to provision;
// the caller decides what to do about it.
func TestResolveFrom_SinAlmacenDeClavesDevuelveNil(t *testing.T) {
	h := newSigningHandler(nil)
	p := &auth.Principal{UserID: "u1"}

	got, err := h.resolveFrom(p, nil)
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got != nil {
		t.Fatalf("esperaba nil, got %+v", got)
	}
}

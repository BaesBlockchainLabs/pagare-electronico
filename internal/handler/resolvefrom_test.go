package handler

import (
	"errors"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/models"
)

// fakeKeys is a SigningKeys stub returning a canned private key for a known pub.
type fakeKeys struct {
	pvtByPub map[string]string
	err      error
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

func TestResolveFrom_NoKeysReturnsNil(t *testing.T) {
	h := newSigningHandler(fakeKeys{})
	p := &auth.Principal{UserID: "u1"} // no pub keys

	got, err := h.resolveFrom(p, nil)
	if err != nil {
		t.Fatalf("resolveFrom: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil (nothing to sign with), got %+v", got)
	}
}

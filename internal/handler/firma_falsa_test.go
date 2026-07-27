package handler

import (
	"encoding/json"
	"net/http"
)

// conFirma adds the ledger's signing endpoint with a canned answer.
//
// Since art. 94.7 LCCH makes the firmante's signature essential, emission now
// refuses to proceed without one. Tests that exercise something other than the
// cryptography still need a signature to exist, and this gives them one without
// modelling real signing — for that, see the ed25519 double in
// verificacion_test.go.
func conFirma(mux *http.ServeMux) *http.ServeMux {
	mux.HandleFunc("/did/sign", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "message": "firma-de-prueba",
		})
	})
	return mux
}

// clavesDePrueba gives the handler a key store that provisions an identity for
// the keyless principal the tests use, as *auth.Store does in production.
func clavesDePrueba() fakeKeys {
	return fakeKeys{
		provisiona: "pub-de-prueba",
		pvtByPub:   map[string]string{"pub-de-prueba": "pvt-de-prueba"},
	}
}

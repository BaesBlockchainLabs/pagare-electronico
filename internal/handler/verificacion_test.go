package handler

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/crypto"
	"pagare/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBCF stands in for BlockchainFUE with real ed25519 signing, reproducing
// the wire contract observed against the live API: /did/sign returns base64 of
// signature||message, and /did/verify gives the message back.
type fakeBCF struct {
	pub     ed25519.PublicKey
	pvt     ed25519.PrivateKey
	stored  map[string]interface{} // asset data as the ledger holds it
	assetID string
}

func newFakeBCF(t *testing.T) *fakeBCF {
	t.Helper()
	pub, pvt, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return &fakeBCF{pub: pub, pvt: pvt, assetID: "asset-de-prueba"}
}

func (f *fakeBCF) pubStr() string { return base64.StdEncoding.EncodeToString(f.pub) }
func (f *fakeBCF) pvtStr() string { return base64.StdEncoding.EncodeToString(f.pvt) }

func (f *fakeBCF) mux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/did/sign", func(w http.ResponseWriter, r *http.Request) {
		var req models.SignRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		pvt, err := base64.StdEncoding.DecodeString(req.SignKey)
		require.NoError(t, err)
		signed := append(ed25519.Sign(ed25519.PrivateKey(pvt), []byte(req.Message)), []byte(req.Message)...)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "message": base64.StdEncoding.EncodeToString(signed),
		})
	})

	mux.HandleFunc("/did/verify", func(w http.ResponseWriter, r *http.Request) {
		var req models.VerifyRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		blob, err := base64.StdEncoding.DecodeString(req.Message)
		if err != nil || len(blob) < ed25519.SignatureSize {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": "firma ilegible"})
			return
		}
		pub, err := base64.StdEncoding.DecodeString(req.VerifyKey)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": "clave ilegible"})
			return
		}
		sig, msg := blob[:ed25519.SignatureSize], blob[ed25519.SignatureSize:]
		if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": "firma no válida"})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": string(msg)})
	})

	// Emission: keep the data exactly as the platform sent it, then add the
	// fields the real ledger adds on its own.
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Asset struct {
				Data map[string]interface{} `json:"data"`
			} `json:"asset"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		f.stored = req.Asset.Data
		f.stored["app"] = "pagare"
		f.stored["from"] = f.pubStr()
		f.stored["namespace"] = "test"
		f.stored["token"] = false
		f.stored["created_at"] = 1785156671724
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "msg": "Asset created", "id": f.assetID, "cost": 0.05,
		})
	})

	mux.HandleFunc("/public/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "asset": map[string]interface{}{"id": f.assetID, "data": f.stored},
		})
	})
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "history": []interface{}{}})
	})
	mux.HandleFunc("/asset/owners", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "owners": []map[string]interface{}{{"pub": f.pubStr(), "amount": 1}},
		})
	})

	return mux
}

// emitir issues a pagaré through the handler against the fake ledger.
func (f *fakeBCF) emitir(t *testing.T, ph *PagareHandler) {
	t.Helper()
	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion":      "PAGARÉ",
				"promesa_pago":      true,
				"importe":           2000.0,
				"moneda":            "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-06-30"},
				"localidad_pago":    "Valencia",
				"beneficiario":      map[string]interface{}{"nombre": "Pedro", "nif": "12345678Z"},
				"localidad_emision": "Madrid",
				"fecha_emision":     "2026-04-10",
				"firmante": map[string]interface{}{
					"nombre": "Maria", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "C/2", "localidad": "Madrid", "codigo_postal": "28001", "pais": "ES",
					},
				},
			},
		},
		"from": map[string]string{"pub": f.pubStr(), "pvt": f.pvtStr()},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// verificar consults the public endpoint and returns the verification block.
func verificar(t *testing.T, ch *ConsultaHandler, id string) Verificacion {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/public?network=test&id="+id, nil)
	w := httptest.NewRecorder()
	ch.GetPublicAsset(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Asset struct {
			Verificacion Verificacion `json:"verificacion"`
		} `json:"asset"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Asset.Verificacion
}

func setup(t *testing.T) (*fakeBCF, *PagareHandler, *ConsultaHandler, func()) {
	t.Helper()
	f := newFakeBCF(t)
	client, server := newTestBCFClient(f.mux(t))
	cryptoSvc := crypto.NewService(client)
	ph := NewPagareHandler(client, cryptoSvc, nil)
	ch := NewConsultaHandler(client)
	ch.SetCrypto(cryptoSvc)
	return f, ph, ch, server.Close
}

func TestVerificacion_PagareIntactoVerifica(t *testing.T) {
	f, ph, ch, done := setup(t)
	defer done()

	f.emitir(t, ph)

	firmante, ok := f.stored["firmante"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, firmante["firma_digital"], "la emisión debe firmar el contenido")

	v := verificar(t, ch, f.assetID)
	assert.True(t, v.Firmado)
	assert.True(t, v.Integro)
	assert.Equal(t, f.pubStr(), v.Clave)
}

func TestVerificacion_DetectaContenidoAlterado(t *testing.T) {
	casos := map[string]func(map[string]interface{}){
		"importe": func(d map[string]interface{}) { d["importe"] = 9999.0 },
		"beneficiario": func(d map[string]interface{}) {
			d["beneficiario"].(map[string]interface{})["nif"] = "00000000T"
		},
		"vencimiento": func(d map[string]interface{}) {
			d["vencimiento"].(map[string]interface{})["fecha"] = "2030-01-01"
		},
		"no a la orden": func(d map[string]interface{}) { d["no_a_la_orden"] = true },
	}

	for nombre, alterar := range casos {
		t.Run(nombre, func(t *testing.T) {
			f, ph, ch, done := setup(t)
			defer done()

			f.emitir(t, ph)
			alterar(f.stored) // manipulación posterior a la emisión

			v := verificar(t, ch, f.assetID)
			assert.True(t, v.Firmado, "la firma sigue siendo del emisor")
			assert.False(t, v.Integro, "pero el contenido ya no es el firmado")
			assert.Contains(t, v.Msg, "ATENCIÓN")
		})
	}
}

func TestVerificacion_DetectaFirmaDeOtraClave(t *testing.T) {
	f, ph, ch, done := setup(t)
	defer done()

	f.emitir(t, ph)

	// Alguien sustituye la clave emisora por otra: la firma deja de validar.
	otraPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	f.stored["from"] = base64.StdEncoding.EncodeToString(otraPub)

	v := verificar(t, ch, f.assetID)
	assert.False(t, v.Firmado)
	assert.False(t, v.Integro)
}

func TestVerificacion_PagareSinFirmaNoEsPagareAlterado(t *testing.T) {
	f, ph, ch, done := setup(t)
	defer done()

	f.emitir(t, ph)
	delete(f.stored["firmante"].(map[string]interface{}), "firma_digital")

	v := verificar(t, ch, f.assetID)
	assert.False(t, v.Firmado)
	assert.False(t, v.Integro)
	assert.Contains(t, v.Msg, "sin firma")
}

// Without a signing identity the emission still goes through, unsigned; the
// keyless path must not be reported as tampering.
func TestVerificacion_EmisionSinClaveNoFirma(t *testing.T) {
	f := newFakeBCF(t)
	client, server := newTestBCFClient(f.mux(t))
	defer server.Close()

	ph := NewPagareHandler(client, crypto.NewService(client), nil)
	ch := NewConsultaHandler(client)
	ch.SetCrypto(crypto.NewService(client))

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true, "importe": 500.0, "moneda": "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-06-30"},
				"localidad_pago":    "Valencia",
				"beneficiario":      map[string]interface{}{"nombre": "Pedro", "nif": "12345678Z"},
				"localidad_emision": "Madrid", "fecha_emision": "2026-04-10",
				"firmante": map[string]interface{}{
					"nombre": "Maria", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "C/2", "localidad": "Madrid", "codigo_postal": "28001", "pais": "ES",
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	v := verificar(t, ch, f.assetID)
	assert.False(t, v.Firmado)
	assert.Contains(t, v.Msg, "sin firma")
}

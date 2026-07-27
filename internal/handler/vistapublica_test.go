package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pagare/internal/auth"
	"pagare/internal/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func datosCompletos() map[string]interface{} {
	return map[string]interface{}{
		"type": "pagare_electronico", "denominacion": "PAGARÉ", "promesa_pago": true,
		"importe": 3200.0, "moneda": "EUR", "no_a_la_orden": false,
		"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": "2027-06-30"},
		"localidad_pago":    "Alicante",
		"localidad_emision": "Alicante",
		"fecha_emision":     "2026-07-27",
		"created_at":        1785170928350,
		"from":              "clave-del-emisor",
		"beneficiario":      map[string]interface{}{"nombre": "Ana", "apellido": "López", "nif": "12345678Z"},
		"firmante": map[string]interface{}{
			"tipo": "juridica", "nombre": "Ferretería Levante, S.L.", "nif": "B12345678",
			"direccion_postal": map[string]interface{}{
				"direccion": "Polígono Las Atalayas 12", "localidad": "Alicante",
				"codigo_postal": "03114", "pais": "ES",
			},
			"representante": map[string]interface{}{
				"nombre": "Luis", "apellido": "Server", "nif": "87654321X", "cargo": "administrador único",
			},
		},
		"aval": map[string]interface{}{
			"avalista": map[string]interface{}{"nombre": "Marta", "nif": "11111111H"},
			"alcance":  "parcial", "importe_parcial": 1000.0,
		},
	}
}

func TestVistaPublica_NoIdentificaALasPartes(t *testing.T) {
	pub := vistaPublica(datosCompletos())
	crudo, err := json.Marshal(pub)
	require.NoError(t, err)
	texto := string(crudo)

	prohibido := []string{
		"Ana", "López", "12345678Z",
		"Ferretería Levante", "B12345678",
		"Polígono Las Atalayas", "03114",
		"Luis", "Server", "87654321X",
		"Marta", "11111111H",
		"clave-del-emisor",
	}
	for _, p := range prohibido {
		assert.NotContains(t, texto, p, "la vista pública no debe revelar %q", p)
	}
}

// El representante no es parte del crédito: no debe salir ni enmascarado.
func TestVistaPublica_ElRepresentanteDesaparece(t *testing.T) {
	pub := vistaPublica(datosCompletos())
	firmante, ok := pub["firmante"].(map[string]interface{})
	require.True(t, ok)
	assert.NotContains(t, firmante, "representante")
	assert.NotContains(t, firmante, "direccion_postal")
	assert.NotContains(t, firmante, "nombre")
}

// Quien tiene el título en la mano debe poder cotejar que corresponde.
func TestVistaPublica_ConservaLoQueAcreditaElTitulo(t *testing.T) {
	pub := vistaPublica(datosCompletos())

	for _, campo := range []string{"denominacion", "importe", "moneda", "vencimiento",
		"localidad_pago", "localidad_emision", "fecha_emision", "no_a_la_orden"} {
		assert.Contains(t, pub, campo, "la verificación necesita %s", campo)
	}
	assert.Equal(t, 3200.0, pub["importe"])

	ben := pub["beneficiario"].(map[string]interface{})
	assert.Equal(t, "******78Z", ben["nif"], "NIF cotejable pero no identificable")

	// Del aval, el alcance importa al crédito; el avalista, no al desconocido.
	aval := pub["aval"].(map[string]interface{})
	assert.Equal(t, "parcial", aval["alcance"])
	assert.Equal(t, 1000.0, aval["importe_parcial"])
	assert.NotContains(t, aval, "avalista")
}

func TestEnmascararNIF(t *testing.T) {
	casos := map[string]string{
		"12345678Z": "******78Z",
		"B12345678": "******678",
		"AB1":       "***",
		"":          "",
	}
	for entrada, esperado := range casos {
		assert.Equal(t, esperado, enmascararNIF(entrada), "entrada %q", entrada)
	}
}

// ledgerVista sirve un pagaré y su titularidad, para probar el endpoint entero.
type ledgerVista struct{ dueño string }

func (l *ledgerVista) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/public/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "asset": map[string]interface{}{"id": "asset-1", "data": datosCompletos()},
		})
	})
	mux.HandleFunc("/asset/owners", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "owners": []map[string]interface{}{{"pub": l.dueño}},
		})
	})
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "history": []interface{}{}})
	})
	return mux
}

func consultarPublico(t *testing.T, principal *auth.Principal) map[string]interface{} {
	t.Helper()
	l := &ledgerVista{dueño: "clave-del-tenedor"}
	client, server := newTestBCFClient(l.mux())
	defer server.Close()

	h := NewConsultaHandler(client)
	h.SetCrypto(crypto.NewService(client))

	req := httptest.NewRequest(http.MethodGet, "/public?network=test&id=asset-1", nil)
	if principal != nil {
		req = req.WithContext(auth.ContextWithPrincipal(req.Context(), principal))
	}
	w := httptest.NewRecorder()
	h.GetPublicAsset(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp["asset"].(map[string]interface{})
}

func TestGetPublicAsset_ElDesconocidoNoVeALasPartes(t *testing.T) {
	asset := consultarPublico(t, nil)
	assert.Equal(t, "publica", asset["vista"], "la vista recortada debe declararse")

	crudo, _ := json.Marshal(asset)
	assert.NotContains(t, string(crudo), "12345678Z")
	assert.NotContains(t, string(crudo), "Polígono Las Atalayas")
	assert.NotContains(t, string(crudo), "propietario_actual")
}

// Un usuario autenticado que no es parte tampoco: la sesión no da interés.
func TestGetPublicAsset_ElAutenticadoAjenoTampoco(t *testing.T) {
	asset := consultarPublico(t, &auth.Principal{UserID: "u9", PubKeys: []string{"otra-clave"}})
	assert.Equal(t, "publica", asset["vista"])
}

func TestGetPublicAsset_ElTenedorVeElTitulo(t *testing.T) {
	asset := consultarPublico(t, &auth.Principal{UserID: "u1", PubKeys: []string{"clave-del-tenedor"}})
	assert.Equal(t, "completa", asset["vista"])

	data := asset["data"].(map[string]interface{})
	ben := data["beneficiario"].(map[string]interface{})
	assert.Equal(t, "12345678Z", ben["nif"])
	assert.Equal(t, "clave-del-tenedor", data["propietario_actual"])
}

// La verificación se calcula sobre el contenido completo, que es lo que se
// firmó; recortarlo antes la rompería, y un desconocido dejaría de poder
// comprobar lo único que la vista pública existe para comprobar.
func TestGetPublicAsset_LaVerificacionSobreviveAlRecorte(t *testing.T) {
	asset := consultarPublico(t, nil)
	ver, ok := asset["verificacion"].(map[string]interface{})
	require.True(t, ok, "el desconocido debe seguir recibiendo el resultado de la verificación")
	assert.NotEmpty(t, ver["msg"])
}

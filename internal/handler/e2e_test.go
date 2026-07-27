//go:build e2e

// Ciclo completo contra la red real de BlockchainFUE: emisión firmada, entrega
// al beneficiario y verificación pública. Graba en la blockchain, así que no
// entra en `go test ./...`; se ejecuta a propósito:
//
//	set -a; . ./.env; set +a
//	go test -tags e2e ./internal/handler/ -run TestE2E -v
//
// El pagaré emitido queda en la red: no se quema al terminar.
package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"pagare/internal/bcfclient"
	"pagare/internal/config"
	"pagare/internal/crypto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_EmisionEntregaYVerificacion(t *testing.T) {
	appID, appKey := os.Getenv("BCF_APP_ID"), os.Getenv("BCF_APP_KEY")
	if appID == "" || appKey == "" {
		t.Skip("sin BCF_APP_ID/BCF_APP_KEY: carga el .env antes de ejecutar")
	}
	network := os.Getenv("BCF_NETWORK")
	if network == "" {
		network = "test"
	}
	baseURL := os.Getenv("BCF_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.blockchainfue.com/api"
	}

	client := bcfclient.New(config.BlockchainConfig{
		BaseURL: baseURL, AppID: appID, AppKey: appKey, Network: network,
	})
	cryptoSvc := crypto.NewService(client)

	// Dos identidades reales: quien firma y quien recibe.
	emisorPub, emisorPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err, "generando identidad del emisor")
	beneficiarioPub, _, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err, "generando identidad del beneficiario")
	t.Logf("emisor       %s", emisorPub)
	t.Logf("beneficiario %s", beneficiarioPub)

	ph := NewPagareHandler(client, cryptoSvc, nil)
	ch := NewConsultaHandler(client)
	ch.SetCrypto(cryptoSvc)

	venc := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true,
				"importe": 1500.75, "moneda": "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": venc},
				"localidad_pago":    "Alicante",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "apellido": "López", "nif": "12345678Z"},
				"localidad_emision": "Alicante",
				"fecha_emision":     time.Now().Format("2006-01-02"),
				"firmante": map[string]interface{}{
					"nombre": "Carlos", "apellido": "Ruiz", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "Calle Mayor 5", "localidad": "Alicante",
						"codigo_postal": "03001", "pais": "ES",
					},
				},
			},
		},
		"from": map[string]string{"pub": emisorPub, "pvt": emisorPvt},
		"to":   beneficiarioPub,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var emision struct {
		Ok      bool    `json:"ok"`
		Msg     string  `json:"msg"`
		ID      string  `json:"id"`
		Cost    float64 `json:"cost"`
		Entrega Entrega `json:"entrega"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &emision))
	t.Logf("emitido %s (coste %.6f) — %s", emision.ID, emision.Cost, emision.Msg)

	require.True(t, emision.Ok)
	require.NotEmpty(t, emision.ID)
	assert.True(t, emision.Entrega.Entregado, "entrega: %s", emision.Entrega.Msg)
	assert.Equal(t, beneficiarioPub, emision.Entrega.A)

	// El control debe estar en el beneficiario, no en quien firmó.
	ownersBody, status, err := client.GetAssetOwners(emision.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	var owners struct {
		Owners []struct {
			Pub string `json:"pub"`
		} `json:"owners"`
	}
	require.NoError(t, json.Unmarshal(ownersBody, &owners))
	require.Len(t, owners.Owners, 1)
	assert.Equal(t, beneficiarioPub, owners.Owners[0].Pub,
		"tras la entrega el titular debe ser el beneficiario")

	// Verificación pública: sin autenticación, como la haría un tercero.
	vreq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/public?network=%s&id=%s", network, emision.ID), nil)
	vw := httptest.NewRecorder()
	ch.GetPublicAsset(vw, vreq)
	require.Equal(t, http.StatusOK, vw.Code, vw.Body.String())

	var publico struct {
		Asset struct {
			Data         map[string]interface{} `json:"data"`
			Verificacion Verificacion           `json:"verificacion"`
		} `json:"asset"`
	}
	require.NoError(t, json.Unmarshal(vw.Body.Bytes(), &publico))
	t.Logf("verificación: firmado=%v integro=%v — %s",
		publico.Asset.Verificacion.Firmado,
		publico.Asset.Verificacion.Integro,
		publico.Asset.Verificacion.Msg)

	assert.True(t, publico.Asset.Verificacion.Firmado, "la firma debe validar")
	assert.True(t, publico.Asset.Verificacion.Integro, "el contenido debe estar íntegro")
	assert.Equal(t, emisorPub, publico.Asset.Verificacion.Clave)

	// La entrega no debe leerse como endoso.
	assert.NotEqual(t, "ENDOSADO", publico.Asset.Data["estado"],
		"un pagaré recién emitido no está endosado")

	t.Logf("verificable en /pagares/verificar?id=%s", emision.ID)
}

// Un pagaré emitido «no a la orden» no puede endosarse (art. 14 LCCH). Emite
// uno real y comprueba que la red no llega a transferirlo.
func TestE2E_NoALaOrdenNoSeEndosa(t *testing.T) {
	appID, appKey := os.Getenv("BCF_APP_ID"), os.Getenv("BCF_APP_KEY")
	if appID == "" || appKey == "" {
		t.Skip("sin BCF_APP_ID/BCF_APP_KEY: carga el .env antes de ejecutar")
	}
	baseURL := os.Getenv("BCF_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.blockchainfue.com/api"
	}

	client := bcfclient.New(config.BlockchainConfig{BaseURL: baseURL, AppID: appID, AppKey: appKey})
	cryptoSvc := crypto.NewService(client)

	emisorPub, emisorPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)
	beneficiarioPub, beneficiarioPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)

	ph := NewPagareHandler(client, cryptoSvc, nil)

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true,
				"importe": 300.0, "moneda": "EUR", "no_a_la_orden": true,
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": time.Now().AddDate(1, 0, 0).Format("2006-01-02")},
				"localidad_pago":    "Alicante",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "nif": "12345678Z"},
				"localidad_emision": "Alicante", "fecha_emision": time.Now().Format("2006-01-02"),
				"firmante": map[string]interface{}{
					"nombre": "Carlos", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "Calle Mayor 5", "localidad": "Alicante",
						"codigo_postal": "03001", "pais": "ES",
					},
				},
			},
		},
		"from": map[string]string{"pub": emisorPub, "pvt": emisorPvt},
		"to":   beneficiarioPub,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var emision struct {
		ID      string  `json:"id"`
		Entrega Entrega `json:"entrega"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &emision))
	require.True(t, emision.Entrega.Entregado)
	t.Logf("emitido «no a la orden» %s", emision.ID)

	// El tenedor intenta endosarlo: debe rechazarse.
	eb, _ := json.Marshal(map[string]interface{}{
		"id": emision.ID, "to": emisorPub,
		"metadata": map[string]interface{}{"tipo_endoso": "en_propiedad"},
		"from":     map[string]string{"pub": beneficiarioPub, "pvt": beneficiarioPvt},
	})
	ereq := httptest.NewRequest(http.MethodPut, "/api/pagares/endoso", bytes.NewReader(eb))
	ew := httptest.NewRecorder()
	ph.Endosar(ew, withPrincipal(ereq))

	assert.Equal(t, http.StatusBadRequest, ew.Code, ew.Body.String())
	var rechazo map[string]interface{}
	require.NoError(t, json.Unmarshal(ew.Body.Bytes(), &rechazo))
	assert.Equal(t, "art. 14 LCCH", rechazo["articulo_lcch"])
	t.Logf("endoso rechazado: %v", rechazo["msg"])

	// Y el control sigue donde estaba.
	ownersBody, status, err := client.GetAssetOwners(emision.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	var owners struct {
		Owners []struct {
			Pub string `json:"pub"`
		} `json:"owners"`
	}
	require.NoError(t, json.Unmarshal(ownersBody, &owners))
	require.Len(t, owners.Owners, 1)
	assert.Equal(t, beneficiarioPub, owners.Owners[0].Pub, "el endoso no debe haber movido el título")
}

// La cesión ordinaria es la vía que le queda al pagaré «no a la orden». Emite
// uno real, lo cede, y comprueba que el crédito cambia de manos sin que el
// título figure como endosado.
func TestE2E_CesionDePagareNoALaOrden(t *testing.T) {
	appID, appKey := os.Getenv("BCF_APP_ID"), os.Getenv("BCF_APP_KEY")
	if appID == "" || appKey == "" {
		t.Skip("sin BCF_APP_ID/BCF_APP_KEY: carga el .env antes de ejecutar")
	}
	baseURL := os.Getenv("BCF_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.blockchainfue.com/api"
	}

	client := bcfclient.New(config.BlockchainConfig{BaseURL: baseURL, AppID: appID, AppKey: appKey})
	cryptoSvc := crypto.NewService(client)

	emisorPub, emisorPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)
	tenedorPub, tenedorPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)
	cesionarioPub, _, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)

	ph := NewPagareHandler(client, cryptoSvc, nil)
	ch := NewConsultaHandler(client)
	ch.SetCrypto(cryptoSvc)

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true,
				"importe": 750.0, "moneda": "EUR", "no_a_la_orden": true,
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": time.Now().AddDate(1, 0, 0).Format("2006-01-02")},
				"localidad_pago":    "Alicante",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "nif": "12345678Z"},
				"localidad_emision": "Alicante", "fecha_emision": time.Now().Format("2006-01-02"),
				"firmante": map[string]interface{}{
					"nombre": "Carlos", "nif": "87654321X",
					"direccion_postal": map[string]interface{}{
						"direccion": "Calle Mayor 5", "localidad": "Alicante",
						"codigo_postal": "03001", "pais": "ES",
					},
				},
			},
		},
		"from": map[string]string{"pub": emisorPub, "pvt": emisorPvt},
		"to":   tenedorPub,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var emision struct {
		ID      string  `json:"id"`
		Entrega Entrega `json:"entrega"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &emision))
	require.True(t, emision.Entrega.Entregado)
	t.Logf("emitido «no a la orden» %s", emision.ID)

	// El tenedor lo cede: es la única vía que le queda.
	cb, _ := json.Marshal(map[string]interface{}{
		"id": emision.ID, "to": cesionarioPub,
		"cesionario":         map[string]interface{}{"nombre": "Bea", "apellido": "Soler", "nif": "11111111H"},
		"notificacion_fecha": time.Now().Format("2006-01-02"),
		"notificacion_medio": "burofax",
		"from":               map[string]string{"pub": tenedorPub, "pvt": tenedorPvt},
	})
	creq := httptest.NewRequest(http.MethodPut, "/api/pagares/cesion", bytes.NewReader(cb))
	cw := httptest.NewRecorder()
	ph.Ceder(cw, withPrincipal(creq))
	require.Equal(t, http.StatusOK, cw.Code, cw.Body.String())
	t.Logf("cedido a %s", cesionarioPub)

	// El crédito está en manos del cesionario.
	ownersBody, status, err := client.GetAssetOwners(emision.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	var owners struct {
		Owners []struct {
			Pub string `json:"pub"`
		} `json:"owners"`
	}
	require.NoError(t, json.Unmarshal(ownersBody, &owners))
	require.Len(t, owners.Owners, 1)
	assert.Equal(t, cesionarioPub, owners.Owners[0].Pub)

	// Pero el título no está endosado: la cesión no es un endoso.
	histBody, hs, err := client.GetAssetHistory(emision.ID)
	require.NoError(t, err)
	require.Equal(t, 200, hs)

	endosos, cesiones, _ := ch.parseHistoryCompleto(histBody)
	assert.Empty(t, endosos, "la cesión no pertenece a la cadena de endosos")
	require.Len(t, cesiones, 1)
	assert.Equal(t, "Bea Soler", cesiones[0].Cesionario)
	assert.Equal(t, "burofax", cesiones[0].NotificacionMedio)

	estado := ch.resolveEstado(emision.ID, map[string]string{"ENDOSO": "ENDOSADO"})
	assert.Equal(t, "CEDIDO", estado)
	t.Logf("estado: %s · cesiones: %d · endosos: %d", estado, len(cesiones), len(endosos))
}

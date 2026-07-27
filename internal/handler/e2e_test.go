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

// Un pagaré cuya entrega no pudo completarse queda pendiente; la operación de
// entrega lo lleva a su beneficiario después, sin cambiar de ID.
func TestE2E_EntregaPendienteSeCompletaDespues(t *testing.T) {
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
	beneficiarioPub, _, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)

	ph := NewPagareHandler(client, cryptoSvc, nil)
	ch := NewConsultaHandler(client)

	// Emisión sin destino: el beneficiario no tiene identidad en la plataforma.
	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true,
				"importe": 420.0, "moneda": "EUR",
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
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/pagares", bytes.NewReader(body))
	w := httptest.NewRecorder()
	ph.Emitir(w, withPrincipal(req))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var emision struct {
		ID      string  `json:"id"`
		Msg     string  `json:"msg"`
		Entrega Entrega `json:"entrega"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &emision))
	require.False(t, emision.Entrega.Entregado, "sin destino no debe entregarse")
	t.Logf("emitido pendiente %s — %s", emision.ID, emision.Msg)

	assert.Equal(t, "PENDIENTE_ENTREGA",
		ch.resolveEstado(emision.ID, map[string]string{"ENDOSO": "ENDOSADO"}))

	// Más tarde se conoce la clave del beneficiario y se completa la entrega.
	eb, _ := json.Marshal(map[string]interface{}{
		"id": emision.ID, "to": beneficiarioPub,
		"from": map[string]string{"pub": emisorPub, "pvt": emisorPvt},
	})
	ereq := httptest.NewRequest(http.MethodPut, "/api/pagares/entrega", bytes.NewReader(eb))
	ew := httptest.NewRecorder()
	ph.Entregar(ew, withPrincipal(ereq))
	require.Equal(t, http.StatusOK, ew.Code, ew.Body.String())
	t.Logf("entregado después a %s", beneficiarioPub)

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
	assert.Equal(t, beneficiarioPub, owners.Owners[0].Pub)

	// Y ya no está pendiente, ni figura como endosado.
	estado := ch.resolveEstado(emision.ID, map[string]string{"ENDOSO": "ENDOSADO"})
	assert.NotEqual(t, "PENDIENTE_ENTREGA", estado)
	assert.NotEqual(t, "ENDOSADO", estado)

	// Un segundo intento debe rechazarse: ya no sería una entrega.
	ereq2 := httptest.NewRequest(http.MethodPut, "/api/pagares/entrega", bytes.NewReader(eb))
	ew2 := httptest.NewRecorder()
	ph.Entregar(ew2, withPrincipal(ereq2))
	assert.Equal(t, http.StatusBadRequest, ew2.Code)
	t.Logf("segundo intento rechazado, estado final: %q", estado)
}

// Pagaré emitido por una sociedad: la representación entra en el contenido
// firmado, y los pagarés ya emitidos antes de que existiera siguen validando.
func TestE2E_PagareDeSociedad(t *testing.T) {
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

	emisorPub, emisorPvt, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)
	beneficiarioPub, _, err := cryptoSvc.GenerateKeypair()
	require.NoError(t, err)

	ph := NewPagareHandler(client, cryptoSvc, nil)
	ch := NewConsultaHandler(client)
	ch.SetCrypto(cryptoSvc)

	payload := map[string]interface{}{
		"asset": map[string]interface{}{
			"data": map[string]interface{}{
				"denominacion": "PAGARÉ", "promesa_pago": true,
				"importe": 3200.0, "moneda": "EUR",
				"vencimiento":       map[string]interface{}{"tipo": "fecha_fija", "fecha": time.Now().AddDate(1, 0, 0).Format("2006-01-02")},
				"localidad_pago":    "Alicante",
				"beneficiario":      map[string]interface{}{"nombre": "Ana", "apellido": "López", "nif": "12345678Z"},
				"localidad_emision": "Alicante", "fecha_emision": time.Now().Format("2006-01-02"),
				"firmante": map[string]interface{}{
					"tipo":   "juridica",
					"nombre": "Ferretería Levante, S.L.",
					"nif":    "B12345678",
					"direccion_postal": map[string]interface{}{
						"direccion": "Polígono Las Atalayas 12", "localidad": "Alicante",
						"codigo_postal": "03114", "pais": "ES",
					},
					"representante": map[string]interface{}{
						"nombre": "Luis", "apellido": "Server", "nif": "87654321X",
						"cargo": "administrador único", "acreditacion": "copia autorizada de escritura",
						"referencia": "protocolo 1234", "fecha": "2025-03-10",
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
	t.Logf("emitido por sociedad %s", emision.ID)

	// La firma cubre la representación: verificar debe salir íntegro.
	vreq := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/public?network=%s&id=%s", network, emision.ID), nil)
	vw := httptest.NewRecorder()
	ch.GetPublicAsset(vw, vreq)
	require.Equal(t, http.StatusOK, vw.Code)

	var publico struct {
		Asset struct {
			Data         map[string]interface{} `json:"data"`
			Verificacion Verificacion           `json:"verificacion"`
		} `json:"asset"`
	}
	require.NoError(t, json.Unmarshal(vw.Body.Bytes(), &publico))
	assert.True(t, publico.Asset.Verificacion.Firmado)
	assert.True(t, publico.Asset.Verificacion.Integro)

	firmante, _ := publico.Asset.Data["firmante"].(map[string]interface{})
	require.NotNil(t, firmante)
	assert.Equal(t, "juridica", firmante["tipo"])
	rep, _ := firmante["representante"].(map[string]interface{})
	require.NotNil(t, rep, "la representación debe constar en el título")
	assert.Equal(t, "administrador único", rep["cargo"])
	t.Logf("verificación: firmado=%v integro=%v · firma por %s (%s)",
		publico.Asset.Verificacion.Firmado, publico.Asset.Verificacion.Integro,
		rep["nombre"], rep["cargo"])
}

// Los pagarés firmados antes de existir la representación deben seguir
// verificando: añadir campos no puede invalidar firmas ya emitidas.
func TestE2E_PagaresAnterioresSiguenVerificando(t *testing.T) {
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
	ch := NewConsultaHandler(client)
	ch.SetCrypto(crypto.NewService(client))

	// Emitidos en fases anteriores de este mismo trabajo.
	anteriores := []string{
		"798bdc87b98af78e33bd279e5c7b12ea302082e463ca3dbf28722e45d4900be1",
		"11ff30119afc6b9d39ab4ccdb99dd0e838ee20df0f0d26948612dd526450c947",
		"c90bf3b47473045f2085b6f4cab5c86cb3921a087faa4ea279023f8695d9be9e",
	}
	for _, id := range anteriores {
		req := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/public?network=%s&id=%s", network, id), nil)
		w := httptest.NewRecorder()
		ch.GetPublicAsset(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var publico struct {
			Asset struct {
				Verificacion Verificacion `json:"verificacion"`
			} `json:"asset"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &publico))
		assert.True(t, publico.Asset.Verificacion.Integro,
			"el pagaré %s dejó de verificar: %s", id[:12], publico.Asset.Verificacion.Msg)
	}
}

// Contra la red real: el pagaré de sociedad emitido en las pruebas exponía el
// NIF y la dirección del firmante, y la identidad de su representante, a
// cualquiera con el ID. La vista pública ya no debe hacerlo.
func TestE2E_LaVistaPublicaNoExponeALasPartes(t *testing.T) {
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
	ch := NewConsultaHandler(client)
	ch.SetCrypto(crypto.NewService(client))

	const idSociedad = "eeee8519b35b27fb54bd8092f443da4ce078dcdae19490659f458d91ef5c393c"

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/public?network=%s&id=%s", network, idSociedad), nil)
	w := httptest.NewRecorder()
	ch.GetPublicAsset(w, req) // sin principal: un desconocido
	require.Equal(t, http.StatusOK, w.Code)

	cuerpo := w.Body.String()
	for _, dato := range []string{
		"B12345678",             // CIF de la sociedad
		"Polígono Las Atalayas", // dirección del firmante
		"87654321X",             // NIF del representante
		"Server",                // apellido del representante
		"12345678Z",             // NIF del beneficiario
	} {
		assert.NotContains(t, cuerpo, dato, "un desconocido no debe ver %q", dato)
	}

	// Pero sigue pudiendo comprobar lo que la verificación existe para probar.
	var resp struct {
		Asset struct {
			Vista        string                 `json:"vista"`
			Data         map[string]interface{} `json:"data"`
			Verificacion Verificacion           `json:"verificacion"`
		} `json:"asset"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "publica", resp.Asset.Vista)
	assert.True(t, resp.Asset.Verificacion.Integro, "la verificación debe seguir funcionando")
	assert.Equal(t, 3200.0, resp.Asset.Data["importe"])
	t.Logf("vista=%s · importe=%v · verificación: %s",
		resp.Asset.Vista, resp.Asset.Data["importe"], resp.Asset.Verificacion.Msg)
}

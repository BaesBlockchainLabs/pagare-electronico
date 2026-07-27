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

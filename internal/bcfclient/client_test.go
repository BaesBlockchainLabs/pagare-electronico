package bcfclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"pagare/internal/config"
)

func newTestClient(handler http.Handler) (*Client, *httptest.Server) {
	server := httptest.NewServer(handler)
	return &Client{
		baseURL:    server.URL,
		appID:      "test-app-id",
		appKey:     "test-app-key",
		httpClient: server.Client(),
	}, server
}

func assertOKResponse(t *testing.T, body []byte, status int) {
	t.Helper()
	assert.Equal(t, http.StatusOK, status)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(body, &resp))
	assert.True(t, resp["ok"].(bool))
}

func TestHello(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/system/hello", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "test-app-id", r.Header.Get("X-App-Id"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Hello World"})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.Hello()
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestTime(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/system/time", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "timestamp": 1580748337})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.Time()
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestCreateAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		assert.NotNil(t, req["asset"])
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true, "msg": "Asset created successfully", "id": "abc123", "cost": 0.11,
		})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.CreateAsset(map[string]interface{}{
		"asset": map[string]interface{}{"data": map[string]interface{}{"type": "pagare_electronico"}},
	})
	assert.NoError(t, err)
	assertOKResponse(t, body, status)

	var resp map[string]interface{}
	json.Unmarshal(body, &resp)
	assert.Equal(t, "abc123", resp["id"])
}

func TestUpdateAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been transferred"})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.UpdateAsset(map[string]interface{}{
		"id": "abc123", "to": "pubkey456", "metadata": map[string]interface{}{"action": "ENDOSO"},
	})
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestBurnAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "Asset has been burnt"})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.BurnAsset(map[string]interface{}{"id": "abc123"})
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestGetAssetHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/history", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"history": []map[string]interface{}{
				{"id": "abc123", "metadata": map[string]interface{}{"action": "CREATE"}},
			},
		})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.GetAssetHistory("abc123")
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestGenerateKeypair(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/keypair", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "pub": "testpub", "pvt": "testpvt"})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.GenerateKeypair("my-seed", "1234")
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestDIDSign(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/did/sign", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "message": "signedmsg"})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.SignMessage(map[string]interface{}{
		"message": "test", "sign_key": "sk", "verify_key": "vk",
	})
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestGetPublicAsset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/public/main/abc123", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    true,
			"asset": map[string]interface{}{"id": "abc123"},
		})
	})

	c, server := newTestClient(mux)
	defer server.Close()

	body, status, err := c.GetPublicAsset("main", "abc123")
	assert.NoError(t, err)
	assertOKResponse(t, body, status)
}

func TestNew(t *testing.T) {
	cfg := config.BlockchainConfig{
		BaseURL: "https://api.blockchainfue.com/api",
		AppID:   "test-id",
		AppKey:  "test-key",
		Network: "test",
	}
	client := New(cfg)
	assert.NotNil(t, client)
	assert.Equal(t, "https://api.blockchainfue.com/api", client.baseURL)
	assert.Equal(t, "test-id", client.appID)
}

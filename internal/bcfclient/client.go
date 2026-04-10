package bcfclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"pagare/internal/config"
)

type Client struct {
	baseURL    string
	appID      string
	appKey     string
	httpClient *http.Client
}

func New(cfg config.BlockchainConfig) *Client {
	return &Client{
		baseURL: cfg.BaseURL,
		appID:   cfg.AppID,
		appKey:  cfg.AppKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func NewTestClient(baseURL, appID, appKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		appID:      appID,
		appKey:     appKey,
		httpClient: httpClient,
	}
}

func (c *Client) doRequest(method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("error marshaling body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-App-Id", c.appID)
	req.Header.Set("X-App-Key", c.appKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func (c *Client) doQueryGet(path string, query interface{}) ([]byte, int, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, 0, fmt.Errorf("error marshaling query: %w", err)
	}

	encoded := url.QueryEscape(string(queryJSON))
	fullPath := fmt.Sprintf("%s?query=%s", path, encoded)

	return c.doRequest(http.MethodGet, fullPath, nil)
}

func (c *Client) Hello() ([]byte, int, error) {
	return c.doRequest(http.MethodGet, "/system/hello", nil)
}

func (c *Client) Time() ([]byte, int, error) {
	return c.doRequest(http.MethodGet, "/system/time", nil)
}

func (c *Client) CreateAsset(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/asset", body)
}

func (c *Client) UpdateAsset(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPut, "/asset", body)
}

func (c *Client) BurnAsset(query interface{}) ([]byte, int, error) {
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return nil, 0, fmt.Errorf("error marshaling query: %w", err)
	}

	encoded := url.QueryEscape(string(queryJSON))
	return c.doRequest(http.MethodDelete, fmt.Sprintf("/asset?query=%s", encoded), nil)
}

func (c *Client) GetAsset(query interface{}) ([]byte, int, error) {
	return c.doQueryGet("/asset", query)
}

func (c *Client) GetAssetHistory(assetID string) ([]byte, int, error) {
	return c.doQueryGet("/asset/history", map[string]string{"id": assetID})
}

func (c *Client) GetAssetOwners(assetID string) ([]byte, int, error) {
	return c.doQueryGet("/asset/owners", map[string]string{"id": assetID})
}

func (c *Client) GenerateKeypair(seed, pin string) ([]byte, int, error) {
	return c.doQueryGet("/keypair", map[string]string{
		"seed": seed,
		"pin":  pin,
	})
}

func (c *Client) GetApplicationKeypair() ([]byte, int, error) {
	return c.doRequest(http.MethodGet, "/keypair/application", nil)
}

func (c *Client) AddPubKey(pub string) ([]byte, int, error) {
	return c.doRequest(http.MethodPut, "/key/add/pub", map[string]string{"pub": pub})
}

func (c *Client) GenerateDID(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/did", body)
}

func (c *Client) SignMessage(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/did/sign", body)
}

func (c *Client) VerifySignature(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/did/verify", body)
}

func (c *Client) EncryptMessage(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/did/encrypt", body)
}

func (c *Client) DecryptMessage(body interface{}) ([]byte, int, error) {
	return c.doRequest(http.MethodPost, "/did/decrypt", body)
}

func (c *Client) GetPublicAsset(network, id string) ([]byte, int, error) {
	return c.doRequest(http.MethodGet, fmt.Sprintf("/public/%s/%s", network, id), nil)
}

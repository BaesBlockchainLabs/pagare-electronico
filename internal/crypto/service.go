package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"pagare/internal/bcfclient"
	"pagare/internal/models"
)

type Service struct {
	client *bcfclient.Client
}

func NewService(client *bcfclient.Client) *Service {
	return &Service{client: client}
}

// GenerateKeypair provisions a fresh ed25519 keypair for a platform user via the
// blockchain API, deriving it from a random seed+pin. The pin is not persisted:
// the private key itself is stored (sealed) by the caller, so the pin is only a
// throwaway input to the derivation. Returns the public and private key strings.
func (s *Service) GenerateKeypair() (pub string, pvt string, err error) {
	seed, err := randomHex(24) // 48 hex chars, comfortably over the >16 hint
	if err != nil {
		return "", "", err
	}
	pin, err := randomHex(4) // 8 hex chars, within the 4-16 alphanumeric range
	if err != nil {
		return "", "", err
	}

	respBody, status, err := s.client.GenerateKeypair(seed, pin)
	if err != nil {
		return "", "", err
	}
	if status != 200 {
		return "", "", parseError(respBody, status)
	}

	var resp models.BCFKeypairResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", "", err
	}
	if !resp.Ok || resp.Pub == "" || resp.Pvt == "" {
		return "", "", fmt.Errorf("respuesta de keypair inválida: %s", resp.Msg)
	}
	return resp.Pub, resp.Pvt, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Service) GenerateDID(seed string) (*models.IdentidadDID, error) {
	body := models.DIDGenerateRequest{Seed: seed}
	snakeCase := true
	if seed == "" {
		snakeCase = true
	}
	body.SnakeCase = &snakeCase

	respBody, status, err := s.client.GenerateDID(body)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, parseError(respBody, status)
	}

	var resp models.BCFDIDResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, err
	}
	return resp.Identity, nil
}

func (s *Service) SignMessage(message, signKey, verifyKey string) (string, error) {
	req := models.SignRequest{
		Message:   message,
		SignKey:   signKey,
		VerifyKey: verifyKey,
	}

	respBody, status, err := s.client.SignMessage(req)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", parseError(respBody, status)
	}

	var resp models.BCFCryptoResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", err
	}
	return resp.Message, nil
}

func (s *Service) VerifySignature(message, verifyKey string) (string, error) {
	req := models.VerifyRequest{
		Message:   message,
		VerifyKey: verifyKey,
	}

	respBody, status, err := s.client.VerifySignature(req)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", parseError(respBody, status)
	}

	var resp models.BCFCryptoResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", err
	}
	return resp.Message, nil
}

func (s *Service) Encrypt(message, fromPrivateKey, toPublicKey string) (encrypted string, nonce string, err error) {
	req := models.EncryptRequest{
		Message:        message,
		FromPrivateKey: fromPrivateKey,
		ToPublicKey:    toPublicKey,
	}

	respBody, status, err := s.client.EncryptMessage(req)
	if err != nil {
		return "", "", err
	}
	if status != 200 {
		return "", "", parseError(respBody, status)
	}

	var resp models.BCFCryptoResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", "", err
	}
	return resp.Message, resp.Nonce, nil
}

func (s *Service) Decrypt(message, nonce, fromPublicKey, toPrivateKey string) (string, error) {
	req := models.DecryptRequest{
		Message:       message,
		Nonce:         nonce,
		FromPublicKey: fromPublicKey,
		ToPrivateKey:  toPrivateKey,
	}

	respBody, status, err := s.client.DecryptMessage(req)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", parseError(respBody, status)
	}

	var resp models.BCFCryptoResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", err
	}
	return resp.Message, nil
}

func (s *Service) SignPagareContent(pagareJSON, signKey, verifyKey string) (string, error) {
	return s.SignMessage(pagareJSON, signKey, verifyKey)
}

func (s *Service) VerifyPagareSignature(signedMessage, verifyKey string) (string, error) {
	return s.VerifySignature(signedMessage, verifyKey)
}

func parseError(body []byte, status int) error {
	var errResp map[string]interface{}
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg, ok := errResp["msg"].(string); ok {
			return &CryptoError{Status: status, Message: msg}
		}
	}
	return &CryptoError{Status: status, Message: string(body)}
}

type CryptoError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func (e *CryptoError) Error() string {
	return e.Message
}

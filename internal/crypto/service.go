package crypto

import (
	"encoding/json"

	"pagare/internal/bcfclient"
	"pagare/internal/models"
)

type Service struct {
	client *bcfclient.Client
}

func NewService(client *bcfclient.Client) *Service {
	return &Service{client: client}
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

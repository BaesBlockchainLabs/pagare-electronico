package models

type IdentidadBC struct {
	Pub string `json:"pub"`
	Pvt string `json:"pvt,omitempty"`
}

type IdentidadDID struct {
	DID                 string     `json:"did"`
	VerifyKey           string     `json:"verify_key"`
	EncryptionPublicKey string     `json:"encryption_public_key"`
	Secret              *DIDSecret `json:"secret,omitempty"`
}

type DIDSecret struct {
	Seed                 string `json:"seed"`
	SignKey              string `json:"sign_key"`
	EncryptionPrivateKey string `json:"encryption_private_key"`
}

type DIDGenerateRequest struct {
	Seed      string `json:"seed,omitempty"`
	SnakeCase *bool  `json:"snake_case,omitempty"`
}

type SignRequest struct {
	Message   string `json:"message" validate:"required"`
	SignKey   string `json:"sign_key" validate:"required"`
	VerifyKey string `json:"verify_key" validate:"required"`
}

type VerifyRequest struct {
	Message   string `json:"message" validate:"required"`
	VerifyKey string `json:"verify_key" validate:"required"`
}

type EncryptRequest struct {
	Message        string `json:"message" validate:"required"`
	FromPrivateKey string `json:"from_private_key" validate:"required"`
	ToPublicKey    string `json:"to_public_key" validate:"required"`
}

type DecryptRequest struct {
	Message       string `json:"message" validate:"required"`
	Nonce         string `json:"nonce" validate:"required"`
	FromPublicKey string `json:"from_public_key" validate:"required"`
	ToPrivateKey  string `json:"to_private_key" validate:"required"`
}

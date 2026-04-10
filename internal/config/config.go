package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Server     ServerConfig
	Blockchain BlockchainConfig
}

type ServerConfig struct {
	Port string
	Env  string
}

type BlockchainConfig struct {
	BaseURL string
	AppID   string
	AppKey  string
	Network string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	port := getEnv("PORT", "8080")
	env := getEnv("APP_ENV", "development")

	baseURL := getEnv("BCF_BASE_URL", "https://api.blockchainfue.com/api")
	appID := os.Getenv("BCF_APP_ID")
	appKey := os.Getenv("BCF_APP_KEY")
	network := getEnv("BCF_NETWORK", "test")

	if appID == "" || appKey == "" {
		return nil, fmt.Errorf("BCF_APP_ID y BCF_APP_KEY son obligatorios (en .env o variables de entorno)")
	}

	return &Config{
		Server: ServerConfig{
			Port: port,
			Env:  env,
		},
		Blockchain: BlockchainConfig{
			BaseURL: baseURL,
			AppID:   appID,
			AppKey:  appKey,
			Network: network,
		},
	}, nil
}

func (c *Config) IsDevelopment() bool {
	return c.Server.Env == "development"
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

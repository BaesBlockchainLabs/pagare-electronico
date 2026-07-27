package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Server       ServerConfig
	Blockchain   BlockchainConfig
	Certificador CertificadorConfig
}

// CertificadorConfig identifica a quien expide y firma los certificados de
// registro y verificación. Va en configuración porque es una persona con un
// cargo, y ambos cambian.
type CertificadorConfig struct {
	Nombre  string
	Cargo   string
	Entidad string
}

type ServerConfig struct {
	Port string
	Env  string
	// CronInterval es la periodicidad del chequeo de pagarés vencidos/prescritos.
	CronInterval time.Duration
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

	cronInterval, err := time.ParseDuration(getEnv("CRON_INTERVAL", "24h"))
	if err != nil || cronInterval <= 0 {
		cronInterval = 24 * time.Hour
	}

	baseURL := getEnv("BCF_BASE_URL", "https://api.blockchainfue.com/api")
	appID := os.Getenv("BCF_APP_ID")
	appKey := os.Getenv("BCF_APP_KEY")
	network := getEnv("BCF_NETWORK", "test")

	if appID == "" || appKey == "" {
		return nil, fmt.Errorf("BCF_APP_ID y BCF_APP_KEY son obligatorios (en .env o variables de entorno)")
	}

	return &Config{
		Certificador: CertificadorConfig{
			Nombre:  getEnv("CERT_NOMBRE", "La dirección"),
			Cargo:   getEnv("CERT_CARGO", "Director"),
			Entidad: getEnv("CERT_ENTIDAD", "BlockchainFUE"),
		},
		Server: ServerConfig{
			Port:         port,
			Env:          env,
			CronInterval: cronInterval,
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

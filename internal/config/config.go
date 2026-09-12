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
	Logalty      LogaltyConfig
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

// LogaltyConfig son las credenciales del canal de datos de Logalty, con el que
// se valida la identidad del usuario contra el chip de su DNI durante el alta.
//
// Dejarla vacía desactiva la verificación: el alta sigue funcionando y nadie
// queda bloqueado, que es lo que hace falta en desarrollo y en los tests.
type LogaltyConfig struct {
	Endpoint string
	Usuario  string
	Password string
	// Empresa y Tipo identifican a la empresa emisora y el tipo de envío
	// configurado para ella. El tipo de validación de identidad exige que el
	// receptor tenga móvil.
	Empresa string
	Tipo    string
	// TipoContrato es el tipo de envío de contratación, con el que se firman
	// los PDFs. Es necesariamente otro: el envío síncrono —el único que
	// devuelve la URL de firma en el acto— sólo acepta tipos de contratación,
	// y rechaza los demás con el código 122.
	TipoContrato string
	// Portal es la base del portal web de Logalty. Sólo se usa para enlazar
	// desde la administración; el flujo del usuario no pasa por ahí.
	Portal string
	// Remitente es el "Enviado por" del aviso que el portal manda al firmante.
	// Vacío, el portal pone el nombre de la empresa.
	Remitente string
}

// Activa indica si hay configuración suficiente para validar identidades.
func (l LogaltyConfig) Activa() bool {
	return l.credenciales() && l.Tipo != ""
}

// FirmaActiva indica si hay configuración suficiente para firmar PDFs. Es
// independiente de Activa: se puede tener una cosa y no la otra, porque cada
// una necesita su propio tipo de envío.
func (l LogaltyConfig) FirmaActiva() bool {
	return l.credenciales() && l.TipoContrato != ""
}

func (l LogaltyConfig) credenciales() bool {
	return l.Endpoint != "" && l.Usuario != "" && l.Password != "" && l.Empresa != ""
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
		Logalty: LogaltyConfig{
			Endpoint:     os.Getenv("LOGALTY_ENDPOINT"),
			Usuario:      os.Getenv("LOGALTY_USER"),
			Password:     os.Getenv("LOGALTY_PASSWORD"),
			Empresa:      os.Getenv("LOGALTY_COMPANY"),
			Tipo:         os.Getenv("LOGALTY_TYPE"),
			TipoContrato: os.Getenv("LOGALTY_TYPE_CONTRACT"),
			Portal:       os.Getenv("LOGALTY_PORTAL"),
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

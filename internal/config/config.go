package config

import "os"

type Config struct {
	ListenAddr     string
	ServerName     string
	Version        string
	DockerHost     string
	ComposeProject string
	QBitHost       string
	QBitUsername   string
	QBitPassword   string
	GluetunURL     string
	RadarrURL      string
	RadarrAPIKey   string
	SonarrURL      string
	SonarrAPIKey   string
	ProwlarrURL    string
	ProwlarrAPIKey string
	BazarrURL      string
	BazarrAPIKey   string
}

func LoadFromEnv() *Config {
	return &Config{
		ListenAddr:     envOr("LISTEN_ADDR", ":8080"),
		ServerName:     envOr("MCP_SERVER_NAME", "stoganet-mcp"),
		Version:        envOr("MCP_SERVER_VERSION", "dev"),
		DockerHost:     os.Getenv("DOCKER_HOST"),
		ComposeProject: envOr("COMPOSE_PROJECT", "services"),
		QBitHost:       envOr("QBIT_HOST", "http://gluetun:8080"),
		QBitUsername:   os.Getenv("QBIT_USERNAME"),
		QBitPassword:   os.Getenv("QBIT_PASSWORD"),
		GluetunURL:     envOr("GLUETUN_CONTROL_URL", "http://gluetun:8000"),
		RadarrURL:      envOr("RADARR_URL", "http://radarr:7878"),
		RadarrAPIKey:   os.Getenv("RADARR_API_KEY"),
		SonarrURL:      envOr("SONARR_URL", "http://sonarr:8989"),
		SonarrAPIKey:   os.Getenv("SONARR_API_KEY"),
		ProwlarrURL:    envOr("PROWLARR_URL", "http://prowlarr:9696"),
		ProwlarrAPIKey: os.Getenv("PROWLARR_API_KEY"),
		BazarrURL:      envOr("BAZARR_URL", "http://bazarr:6767"),
		BazarrAPIKey:   os.Getenv("BAZARR_API_KEY"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

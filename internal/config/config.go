package config

import "os"

type Config struct {
	ListenAddr     string
	ServerName     string
	Version        string
	DockerHost     string
	ComposeProject string
}

func LoadFromEnv() *Config {
	return &Config{
		ListenAddr:     envOr("LISTEN_ADDR", ":8080"),
		ServerName:     envOr("MCP_SERVER_NAME", "stoganet-mcp"),
		Version:        envOr("MCP_SERVER_VERSION", "dev"),
		DockerHost:     os.Getenv("DOCKER_HOST"),
		ComposeProject: envOr("COMPOSE_PROJECT", "services"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

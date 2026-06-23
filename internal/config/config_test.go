package config_test

import (
	"testing"

	"github.com/Stoganet/mcp/internal/config"
)

func TestLoadFromEnv_defaults(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("MCP_SERVER_NAME", "")
	t.Setenv("MCP_SERVER_VERSION", "")

	cfg := config.LoadFromEnv()

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":8080")
	}
	if cfg.ServerName != "stoganet-mcp" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "stoganet-mcp")
	}
	if cfg.Version != "dev" {
		t.Errorf("Version = %q, want %q", cfg.Version, "dev")
	}
}

func TestLoadFromEnv_overrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("MCP_SERVER_NAME", "test-mcp")
	t.Setenv("MCP_SERVER_VERSION", "1.2.3")

	cfg := config.LoadFromEnv()

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.ServerName != "test-mcp" {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, "test-mcp")
	}
	if cfg.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1.2.3")
	}
}

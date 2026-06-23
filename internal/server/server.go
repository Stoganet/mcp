package server

import (
	"net/http"

	"github.com/Stoganet/mcp/internal/config"
	"github.com/Stoganet/mcp/internal/tools"
	mcpgo "github.com/mark3labs/mcp-go/server"
)

func NewHTTPHandler(cfg *config.Config) http.Handler {
	s := mcpgo.NewMCPServer(cfg.ServerName, cfg.Version,
		mcpgo.WithToolCapabilities(true),
	)

	pingTool, pingHandler := tools.Ping(cfg.ServerName, cfg.Version)
	s.AddTool(pingTool, pingHandler)

	return mcpgo.NewStreamableHTTPServer(s)
}

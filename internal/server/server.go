package server

import (
	"log/slog"
	"net/http"
	"os"

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

	dc, err := tools.NewDockerClient(cfg.DockerHost)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("docker client init failed", "err", err)
		os.Exit(1)
	}
	s.AddTool(tools.ListContainers(dc, cfg.ComposeProject))
	s.AddTool(tools.GetLogs(dc, cfg.ComposeProject))
	s.AddTool(tools.RestartContainer(dc, cfg.ComposeProject))
	s.AddTool(tools.PullImage(dc, cfg.ComposeProject))

	return mcpgo.NewStreamableHTTPServer(s)
}

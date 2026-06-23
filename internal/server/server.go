package server

import (
	"fmt"
	"net/http"

	"github.com/Stoganet/mcp/internal/config"
	"github.com/Stoganet/mcp/internal/tools"
	mcpgo "github.com/mark3labs/mcp-go/server"
)

func NewHTTPHandler(cfg *config.Config) (http.Handler, error) {
	s := mcpgo.NewMCPServer(cfg.ServerName, cfg.Version,
		mcpgo.WithToolCapabilities(true),
	)

	pingTool, pingHandler := tools.Ping(cfg.ServerName, cfg.Version)
	s.AddTool(pingTool, pingHandler)

	dc, err := tools.NewDockerClient(cfg.DockerHost)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	s.AddTool(tools.DockerPS(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerLogs(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerInspect(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerRestart(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerPull(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerExec(dc, cfg.ComposeProject))
	s.AddTool(tools.DockerTop(dc, cfg.ComposeProject))

	return mcpgo.NewStreamableHTTPServer(s), nil
}

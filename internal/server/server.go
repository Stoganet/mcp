package server

import (
	"fmt"
	"log"
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

	if cfg.QBitUsername == "" || cfg.QBitPassword == "" {
		log.Println("QBIT_USERNAME or QBIT_PASSWORD not set — qBittorrent tools disabled")
	} else {
		qc := tools.NewQBitClient(cfg.QBitHost, cfg.QBitUsername, cfg.QBitPassword)
		s.AddTool(tools.QBitTorrents(qc))
		s.AddTool(tools.QBitTorrentDetail(qc))
		s.AddTool(tools.QBitStop(qc))
		s.AddTool(tools.QBitStart(qc))
		s.AddTool(tools.QBitDelete(qc))
		s.AddTool(tools.QBitTransferInfo(qc))
		s.AddTool(tools.QBitPreferences(qc))
	}

	if cfg.RadarrAPIKey != "" {
		rc := tools.NewRadarrClient(cfg.RadarrURL, cfg.RadarrAPIKey)
		s.AddTool(tools.RadarrHealth(rc))
		s.AddTool(tools.RadarrMovie(rc))
		s.AddTool(tools.RadarrQueue(rc))
		s.AddTool(tools.RadarrHistory(rc))
		s.AddTool(tools.RadarrSearch(rc))
	} else {
		log.Println("RADARR_API_KEY not set — radarr tools disabled")
	}

	sr := tools.NewSystemReader()
	s.AddTool(tools.SystemDiskUsage(sr))
	s.AddTool(tools.SystemMountStatus(sr))
	s.AddTool(tools.SystemNetbirdStatus(sr))
	s.AddTool(tools.SystemVPNStatus(cfg.GluetunURL))

	return mcpgo.NewStreamableHTTPServer(s), nil
}

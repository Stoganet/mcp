package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Stoganet/mcp/internal/config"
	"github.com/Stoganet/mcp/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	mcpgo "github.com/mark3labs/mcp-go/server"
)

func toolLoggingHooks() *mcpgo.Hooks {
	var starts sync.Map
	h := &mcpgo.Hooks{}

	h.AddBeforeCallTool(func(_ context.Context, id any, _ *mcp.CallToolRequest) {
		starts.Store(id, time.Now())
	})

	h.AddAfterCallTool(func(_ context.Context, id any, req *mcp.CallToolRequest, result any) {
		dur := time.Duration(0)
		if v, ok := starts.LoadAndDelete(id); ok {
			dur = time.Since(v.(time.Time))
		}
		status := "success"
		if r, ok := result.(*mcp.CallToolResult); ok && r != nil && r.IsError {
			status = "error"
		}
		log.Printf("tool=%s status=%s duration=%s", req.Params.Name, status, dur.Round(time.Millisecond))
	})

	return h
}

func NewHTTPHandler(cfg *config.Config) (http.Handler, error) {
	s := mcpgo.NewMCPServer(cfg.ServerName, cfg.Version,
		mcpgo.WithToolCapabilities(true),
		mcpgo.WithHooks(toolLoggingHooks()),
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
		s.AddTool(tools.RadarrQualityProfiles(rc))
		s.AddTool(tools.RadarrUpdateQualityProfile(rc))
	} else {
		log.Println("RADARR_API_KEY not set — radarr tools disabled")
	}

	if cfg.SonarrAPIKey != "" {
		sc := tools.NewSonarrClient(cfg.SonarrURL, cfg.SonarrAPIKey)
		s.AddTool(tools.SonarrHealth(sc))
		s.AddTool(tools.SonarrSeries(sc))
		s.AddTool(tools.SonarrEpisodes(sc))
		s.AddTool(tools.SonarrQueue(sc))
		s.AddTool(tools.SonarrSearch(sc))
		s.AddTool(tools.SonarrQualityProfiles(sc))
		s.AddTool(tools.SonarrUpdateQualityProfile(sc))
	} else {
		log.Println("SONARR_API_KEY not set — sonarr tools disabled")
	}

	sr := tools.NewSystemReader()
	s.AddTool(tools.SystemDiskUsage(sr))
	s.AddTool(tools.SystemMountStatus(sr))
	s.AddTool(tools.SystemNetbirdStatus(sr))
	s.AddTool(tools.SystemVPNStatus(cfg.GluetunURL))

	return mcpgo.NewStreamableHTTPServer(s), nil
}

package tools

import (
	"context"
	"encoding/json"
	"math"
	"time"

	qbit "github.com/autobrr/go-qbittorrent"
	"github.com/mark3labs/mcp-go/mcp"
)

type QBitClient interface {
	GetTorrentsCtx(ctx context.Context, opts qbit.TorrentFilterOptions) ([]qbit.Torrent, error)
	GetTorrentPropertiesCtx(ctx context.Context, hash string) (qbit.TorrentProperties, error)
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]qbit.TorrentTracker, error)
	GetFilesInformationCtx(ctx context.Context, hash string) (*qbit.TorrentFiles, error)
	StopCtx(ctx context.Context, hashes []string) error
	StartCtx(ctx context.Context, hashes []string) error
	DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error
	GetTransferInfoCtx(ctx context.Context) (*qbit.TransferInfo, error)
	GetAppPreferencesCtx(ctx context.Context) (qbit.AppPreferences, error)
	SetPreferencesCtx(ctx context.Context, prefs map[string]interface{}) error
}

func NewQBitClient(host, username, password string) QBitClient {
	return qbit.NewClient(qbit.Config{
		Host:     host,
		Username: username,
		Password: password,
	})
}

func mapTorrentState(raw qbit.TorrentState) string {
	switch raw {
	case qbit.TorrentStateDownloading, qbit.TorrentStateForcedDl, qbit.TorrentStateMetaDl:
		return "downloading"
	case qbit.TorrentStateUploading, qbit.TorrentStateForcedUp:
		return "seeding"
	case qbit.TorrentStateStoppedDl, qbit.TorrentStateStoppedUp,
		qbit.TorrentStatePausedDl, qbit.TorrentStatePausedUp:
		return "stopped"
	case qbit.TorrentStateStalledDl, qbit.TorrentStateStalledUp:
		return "stalled"
	case qbit.TorrentStateError, qbit.TorrentStateMissingFiles:
		return "errored"
	case qbit.TorrentStateQueuedDl, qbit.TorrentStateQueuedUp:
		return "queued"
	case qbit.TorrentStateCheckingDl, qbit.TorrentStateCheckingUp, qbit.TorrentStateCheckingResumeData:
		return "checking"
	case qbit.TorrentStateMoving:
		return "moving"
	default:
		return string(raw)
	}
}

func bytesToMB(b int64) float64 {
	return math.Round(float64(b)/1024/1024*100) / 100
}

func bytesToKBs(b int64) float64 {
	return math.Round(float64(b)/1024*100) / 100
}

type torrentItem struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	SizeMB       float64 `json:"size_mb"`
	DownloadedMB float64 `json:"downloaded_mb"`
	DlSpeedKBs   float64 `json:"dlspeed_kbs"`
	UpSpeedKBs   float64 `json:"upspeed_kbs"`
	ETASeconds   int64   `json:"eta_seconds"`
	Seeds        int64   `json:"seeds"`
	Peers        int64   `json:"peers"`
	Ratio        float64 `json:"ratio"`
	Category     string  `json:"category"`
	AddedOn      string  `json:"added_on"`
	SavePath     string  `json:"save_path"`
}

type torrentListResult struct {
	Total    int           `json:"total"`
	Filtered int           `json:"filtered"`
	Torrents []torrentItem `json:"torrents"`
}

func QBitTorrents(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_torrents",
		mcp.WithDescription("List torrents with status, progress, speeds, and category. "+
			"Supported status filters: downloading, seeding, stopped, stalled, active, inactive, running, errored, completed."),
		mcp.WithString("status", mcp.Description("Filter by status: downloading, seeding, stopped, stalled, active, inactive, running, errored, completed")),
		mcp.WithString("category", mcp.Description("Filter by category (e.g. radarr, sonarr)")),
		mcp.WithString("sort", mcp.Description("Sort field: added_on, name, progress, size, dlspeed (default: added_on)")),
		mcp.WithNumber("limit", mcp.Description("Max results (default 50)")),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		status := mcp.ParseArgument(req, "status", "").(string)
		category := mcp.ParseArgument(req, "category", "").(string)
		sort := mcp.ParseArgument(req, "sort", "added_on").(string)
		limit := int(mcp.ParseArgument(req, "limit", float64(50)).(float64))

		var filter qbit.TorrentFilter
		switch status {
		case "downloading":
			filter = qbit.TorrentFilterDownloading
		case "seeding", "uploading":
			filter = qbit.TorrentFilterUploading
		case "stopped":
			filter = qbit.TorrentFilterStopped
		case "stalled":
			filter = qbit.TorrentFilterStalled
		case "active":
			filter = qbit.TorrentFilterActive
		case "inactive":
			filter = qbit.TorrentFilterInactive
		case "running":
			filter = qbit.TorrentFilterRunning
		case "errored":
			filter = qbit.TorrentFilterError
		case "completed":
			filter = qbit.TorrentFilterCompleted
		case "":
			filter = qbit.TorrentFilterAll
		default:
			return mcp.NewToolResultError("unknown status filter: " + status), nil //nolint:nilerr
		}

		torrents, err := qc.GetTorrentsCtx(ctx, qbit.TorrentFilterOptions{
			Filter:   filter,
			Category: category,
			Sort:     sort,
			Limit:    limit,
		})
		if err != nil {
			return mcp.NewToolResultError("qbittorrent error: " + err.Error()), nil //nolint:nilerr
		}

		items := make([]torrentItem, 0, len(torrents))
		for _, t := range torrents {
			items = append(items, torrentItem{
				Hash:         t.Hash,
				Name:         t.Name,
				State:        mapTorrentState(t.State),
				Progress:     t.Progress,
				SizeMB:       bytesToMB(t.Size),
				DownloadedMB: bytesToMB(t.Downloaded),
				DlSpeedKBs:   bytesToKBs(t.DlSpeed),
				UpSpeedKBs:   bytesToKBs(t.UpSpeed),
				ETASeconds:   t.ETA,
				Seeds:        t.NumSeeds,
				Peers:        t.NumLeechs,
				Ratio:        t.Ratio,
				Category:     t.Category,
				AddedOn:      time.Unix(t.AddedOn, 0).UTC().Format(time.RFC3339),
				SavePath:     t.SavePath,
			})
		}

		b, err := json.Marshal(torrentListResult{
			Total:    len(items),
			Filtered: len(items),
			Torrents: items,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

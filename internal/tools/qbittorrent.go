package tools

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"sync"
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

type trackerItem struct {
	URL     string `json:"url"`
	Status  int    `json:"status"`
	Seeds   int    `json:"seeds"`
	Peers   int    `json:"peers"`
	Message string `json:"message"`
}

type fileItem struct {
	Name     string  `json:"name"`
	SizeMB   float64 `json:"size_mb"`
	Progress float32 `json:"progress"`
	Priority int     `json:"priority"`
}

type torrentDetailResult struct {
	Hash           string        `json:"hash"`
	Name           string        `json:"name"`
	SavePath       string        `json:"save_path"`
	TotalSizeMB    float64       `json:"total_size_mb"`
	DownloadedMB   float64       `json:"downloaded_mb"`
	UploadedMB     float64       `json:"uploaded_mb"`
	DlSpeedKBs     float64       `json:"dlspeed_kbs"`
	UpSpeedKBs     float64       `json:"upspeed_kbs"`
	ETASeconds     int           `json:"eta_seconds"`
	Seeds          int           `json:"seeds"`
	Peers          int           `json:"peers"`
	ShareRatio     float64       `json:"share_ratio"`
	AdditionDate   int           `json:"addition_date"`
	CompletionDate int           `json:"completion_date"`
	IsPrivate      bool          `json:"is_private"`
	Trackers       []trackerItem `json:"trackers"`
	Files          []fileItem    `json:"files"`
}

func isPseudoTracker(url string) bool {
	return strings.HasPrefix(url, "** [")
}

func QBitTorrentDetail(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_torrent_detail",
		mcp.WithDescription("Detailed info for a single torrent: properties, tracker health, and file list."),
		mcp.WithString("hash", mcp.Required(), mcp.Description("Torrent info hash")),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		hash := mcp.ParseArgument(req, "hash", "").(string)
		if hash == "" {
			return mcp.NewToolResultError("hash is required"), nil //nolint:nilerr
		}

		var (
			props                           qbit.TorrentProperties
			trackers                        []qbit.TorrentTracker
			files                           *qbit.TorrentFiles
			propsErr, trackersErr, filesErr error
			wg                              sync.WaitGroup
		)
		wg.Add(3)
		go func() { defer wg.Done(); props, propsErr = qc.GetTorrentPropertiesCtx(ctx, hash) }()
		go func() { defer wg.Done(); trackers, trackersErr = qc.GetTorrentTrackersCtx(ctx, hash) }()
		go func() { defer wg.Done(); files, filesErr = qc.GetFilesInformationCtx(ctx, hash) }()
		wg.Wait()

		if propsErr != nil {
			return mcp.NewToolResultError("properties error: " + propsErr.Error()), nil //nolint:nilerr
		}
		if trackersErr != nil {
			return mcp.NewToolResultError("trackers error: " + trackersErr.Error()), nil //nolint:nilerr
		}
		if filesErr != nil {
			return mcp.NewToolResultError("files error: " + filesErr.Error()), nil //nolint:nilerr
		}

		trackerItems := make([]trackerItem, 0, len(trackers))
		for _, tr := range trackers {
			if isPseudoTracker(tr.Url) {
				continue
			}
			trackerItems = append(trackerItems, trackerItem{
				URL:     tr.Url,
				Status:  int(tr.Status),
				Seeds:   tr.NumSeeds,
				Peers:   tr.NumPeers,
				Message: tr.Message,
			})
		}

		var fileItems []fileItem
		if files != nil {
			fileItems = make([]fileItem, 0, len(*files))
			for _, f := range *files {
				fileItems = append(fileItems, fileItem{
					Name:     f.Name,
					SizeMB:   bytesToMB(f.Size),
					Progress: f.Progress,
					Priority: f.Priority,
				})
			}
		}

		b, err := json.Marshal(torrentDetailResult{
			Hash:           hash,
			Name:           props.Name,
			SavePath:       props.SavePath,
			TotalSizeMB:    bytesToMB(props.TotalSize),
			DownloadedMB:   bytesToMB(props.TotalDownloaded),
			UploadedMB:     bytesToMB(props.TotalUploaded),
			DlSpeedKBs:     bytesToKBs(int64(props.DlSpeed)),
			UpSpeedKBs:     bytesToKBs(int64(props.UpSpeed)),
			ETASeconds:     props.Eta,
			Seeds:          props.Seeds,
			Peers:          props.Peers,
			ShareRatio:     props.ShareRatio,
			AdditionDate:   props.AdditionDate,
			CompletionDate: props.CompletionDate,
			IsPrivate:      props.IsPrivate,
			Trackers:       trackerItems,
			Files:          fileItems,
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

func QBitStop(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_stop",
		mcp.WithDescription(`Stop one or more torrents. Pass ["all"] to stop everything.`),
		mcp.WithArray("hashes", mcp.Required(), mcp.Description(`Torrent info hashes, or ["all"]`)),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		hashes := parseStringSlice(req, "hashes")
		if len(hashes) == 0 {
			return mcp.NewToolResultError("hashes must not be empty"), nil //nolint:nilerr
		}
		if err := qc.StopCtx(ctx, hashes); err != nil {
			return mcp.NewToolResultError("stop error: " + err.Error()), nil //nolint:nilerr
		}
		result, err := verifyStopState(ctx, qc, hashes)
		if err != nil {
			return mcp.NewToolResultError("verify error: " + err.Error()), nil //nolint:nilerr
		}
		b, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

func QBitStart(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_start",
		mcp.WithDescription(`Start one or more stopped torrents. Pass ["all"] to start everything.`),
		mcp.WithArray("hashes", mcp.Required(), mcp.Description(`Torrent info hashes, or ["all"]`)),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		hashes := parseStringSlice(req, "hashes")
		if len(hashes) == 0 {
			return mcp.NewToolResultError("hashes must not be empty"), nil //nolint:nilerr
		}
		if err := qc.StartCtx(ctx, hashes); err != nil {
			return mcp.NewToolResultError("start error: " + err.Error()), nil //nolint:nilerr
		}
		result, err := verifyStartState(ctx, qc, hashes)
		if err != nil {
			return mcp.NewToolResultError("verify error: " + err.Error()), nil //nolint:nilerr
		}
		b, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

func fetchHashStates(ctx context.Context, qc QBitClient, hashes []string) (map[string]string, error) {
	torrents, err := qc.GetTorrentsCtx(ctx, qbit.TorrentFilterOptions{
		Filter: qbit.TorrentFilterAll,
		Hashes: hashes,
	})
	if err != nil {
		return nil, err
	}
	found := make(map[string]string, len(torrents))
	for _, t := range torrents {
		found[t.Hash] = mapTorrentState(t.State)
	}
	return found, nil
}

func verifyStopState(ctx context.Context, qc QBitClient, hashes []string) (map[string][]string, error) {
	if len(hashes) == 1 && hashes[0] == "all" {
		return map[string][]string{"stopped": {"all"}, "already_stopped": {}, "not_found": {}}, nil
	}

	found, err := fetchHashStates(ctx, qc, hashes)
	if err != nil {
		return nil, err
	}

	stopped, alreadyStopped, notFound := make([]string, 0), make([]string, 0), make([]string, 0)
	for _, h := range hashes {
		state, ok := found[h]
		if !ok {
			notFound = append(notFound, h)
			continue
		}
		if state == "stopped" {
			alreadyStopped = append(alreadyStopped, h)
		} else {
			stopped = append(stopped, h)
		}
	}

	return map[string][]string{
		"stopped":         stopped,
		"already_stopped": alreadyStopped,
		"not_found":       notFound,
	}, nil
}

func verifyStartState(ctx context.Context, qc QBitClient, hashes []string) (map[string][]string, error) {
	if len(hashes) == 1 && hashes[0] == "all" {
		return map[string][]string{"started": {"all"}, "already_active": {}, "not_found": {}}, nil
	}

	found, err := fetchHashStates(ctx, qc, hashes)
	if err != nil {
		return nil, err
	}

	started, alreadyActive, notFound := make([]string, 0), make([]string, 0), make([]string, 0)
	for _, h := range hashes {
		state, ok := found[h]
		if !ok {
			notFound = append(notFound, h)
			continue
		}
		if state == "stopped" {
			started = append(started, h)
		} else {
			alreadyActive = append(alreadyActive, h)
		}
	}

	return map[string][]string{
		"started":        started,
		"already_active": alreadyActive,
		"not_found":      notFound,
	}, nil
}

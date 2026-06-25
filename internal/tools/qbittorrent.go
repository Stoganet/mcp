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

func unixToRFC3339(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
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
		status := mcp.ParseString(req, "status", "")
		category := mcp.ParseString(req, "category", "")
		sort := mcp.ParseString(req, "sort", "added_on")
		limit := mcp.ParseInt(req, "limit", 50)

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
				AddedOn:      unixToRFC3339(t.AddedOn),
				SavePath:     t.SavePath,
			})
		}

		b, err := json.Marshal(torrentListResult{
			Total:    len(items),
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
	AdditionDate   string        `json:"addition_date"`
	CompletionDate string        `json:"completion_date,omitempty"`
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
		hash := mcp.ParseString(req, "hash", "")
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
			AdditionDate:   unixToRFC3339(int64(props.AdditionDate)),
			CompletionDate: unixToRFC3339(int64(props.CompletionDate)),
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
		mcp.WithDescription(`Stop one or more torrents. Pass ["all"] to stop everything. Unknown hashes are silently ignored by qBittorrent.`),
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
		return mcp.NewToolResultText(`{"ok":true}`), nil
	}

	return tool, handler
}

func QBitStart(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_start",
		mcp.WithDescription(`Start one or more stopped torrents. Pass ["all"] to start everything. Unknown hashes are silently ignored by qBittorrent.`),
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
		return mcp.NewToolResultText(`{"ok":true}`), nil
	}

	return tool, handler
}

type deleteResult struct {
	Deleted  []string `json:"deleted"`
	NotFound []string `json:"not_found"`
}

func QBitDelete(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_delete",
		mcp.WithDescription("Delete one or more torrents. Set delete_files=true to also remove downloaded data from disk."),
		mcp.WithArray("hashes", mcp.Required(), mcp.Description("Torrent info hashes")),
		mcp.WithBoolean("delete_files", mcp.Description("Also delete downloaded data from disk (default false)")),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		hashes := parseStringSlice(req, "hashes")
		if len(hashes) == 0 {
			return mcp.NewToolResultError("hashes must not be empty"), nil //nolint:nilerr
		}
		deleteFiles := mcp.ParseBoolean(req, "delete_files", false)

		torrents, err := qc.GetTorrentsCtx(ctx, qbit.TorrentFilterOptions{
			Filter: qbit.TorrentFilterAll,
			Hashes: hashes,
		})
		if err != nil {
			return mcp.NewToolResultError("lookup error: " + err.Error()), nil //nolint:nilerr
		}
		existing := make(map[string]struct{}, len(torrents))
		for _, t := range torrents {
			existing[strings.ToLower(t.Hash)] = struct{}{}
		}

		toDelete := make([]string, 0, len(hashes))
		notFound := make([]string, 0)
		for _, h := range hashes {
			if _, ok := existing[strings.ToLower(h)]; ok {
				toDelete = append(toDelete, h)
			} else {
				notFound = append(notFound, h)
			}
		}

		if len(toDelete) > 0 {
			if err := qc.DeleteTorrentsCtx(ctx, toDelete, deleteFiles); err != nil {
				return mcp.NewToolResultError("delete error: " + err.Error()), nil //nolint:nilerr
			}
		}

		b, err := json.Marshal(deleteResult{Deleted: toDelete, NotFound: notFound})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

type transferInfoResult struct {
	DlSpeedKBs       float64 `json:"dl_speed_kbs"`
	UpSpeedKBs       float64 `json:"up_speed_kbs"`
	DlTotalMB        float64 `json:"dl_total_mb"`
	UpTotalMB        float64 `json:"up_total_mb"`
	DlRateLimitKBs   float64 `json:"dl_rate_limit_kbs"`
	UpRateLimitKBs   float64 `json:"up_rate_limit_kbs"`
	DHTNodes         int64   `json:"dht_nodes"`
	ConnectionStatus string  `json:"connection_status"`
	ActiveDownloads  int     `json:"active_downloads"`
	ActiveUploads    int     `json:"active_uploads"`
	Stopped          int     `json:"stopped"`
	Stalled          int     `json:"stalled"`
	Errored          int     `json:"errored"`
}

func QBitTransferInfo(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_transfer_info",
		mcp.WithDescription("Global transfer stats and VPN connectivity indicator. "+
			"connection_status 'firewalled' or 'disconnected' means VPN is down. "+
			"dht_nodes=0 is normal (DHT disabled by policy)."),
	)

	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var (
			info                 *qbit.TransferInfo
			torrents             []qbit.Torrent
			infoErr, torrentsErr error
			wg                   sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); info, infoErr = qc.GetTransferInfoCtx(ctx) }()
		go func() {
			defer wg.Done()
			torrents, torrentsErr = qc.GetTorrentsCtx(ctx, qbit.TorrentFilterOptions{Filter: qbit.TorrentFilterAll})
		}()
		wg.Wait()

		if infoErr != nil {
			return mcp.NewToolResultError("transfer info error: " + infoErr.Error()), nil //nolint:nilerr
		}
		if torrentsErr != nil {
			return mcp.NewToolResultError("torrents error: " + torrentsErr.Error()), nil //nolint:nilerr
		}
		if info == nil {
			return mcp.NewToolResultError("transfer info unavailable"), nil //nolint:nilerr
		}

		counts := map[string]int{}
		for _, t := range torrents {
			counts[mapTorrentState(t.State)]++
		}

		b, err := json.Marshal(transferInfoResult{
			DlSpeedKBs:       bytesToKBs(info.DlInfoSpeed),
			UpSpeedKBs:       bytesToKBs(info.UpInfoSpeed),
			DlTotalMB:        bytesToMB(info.DlInfoData),
			UpTotalMB:        bytesToMB(info.UpInfoData),
			DlRateLimitKBs:   bytesToKBs(info.DlRateLimit),
			UpRateLimitKBs:   bytesToKBs(info.UpRateLimit),
			DHTNodes:         info.DHTNodes,
			ConnectionStatus: string(info.ConnectionStatus),
			ActiveDownloads:  counts["downloading"],
			ActiveUploads:    counts["seeding"],
			Stopped:          counts["stopped"],
			Stalled:          counts["stalled"],
			Errored:          counts["errored"],
		})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

type prefsReadResult struct {
	Mode        string                 `json:"mode"`
	Preferences map[string]interface{} `json:"preferences"`
}

type prefsWriteResult struct {
	Mode    string                 `json:"mode"`
	Applied map[string]interface{} `json:"applied"`
}

var blockedPrefKeys = map[string]struct{}{
	"web_ui_password":                        {},
	"web_ui_username":                        {},
	"proxy_password":                         {},
	"proxy_username":                         {},
	"dht":                                    {},
	"pex":                                    {},
	"lsd":                                    {},
	"upnp":                                   {},
	"web_ui_csrf_protection_enabled":         {},
	"web_ui_clickjacking_protection_enabled": {},
	"web_ui_secure_cookie_enabled":           {},
}

func QBitPreferences(qc QBitClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("qbit_preferences",
		mcp.WithDescription("Get or set qBittorrent application preferences. get and set are mutually exclusive. "+
			"Blocked keys (credentials, peer discovery, security hardening) are rejected on write."),
		mcp.WithArray("get", mcp.Description("Preference keys to read. Omit for all preferences.")),
		mcp.WithObject("set", mcp.Description("Key-value pairs to set. Providing this makes it a write operation.")),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		getKeys := parseStringSlice(req, "get")

		rawSet := mcp.ParseArgument(req, "set", nil)
		var setMap map[string]interface{}
		if rawSet != nil {
			var ok bool
			setMap, ok = rawSet.(map[string]interface{})
			if !ok {
				return mcp.NewToolResultError("set must be an object"), nil //nolint:nilerr
			}
		}

		if len(getKeys) > 0 && len(setMap) > 0 {
			return mcp.NewToolResultError("get and set are mutually exclusive"), nil //nolint:nilerr
		}

		if len(setMap) > 0 {
			for k := range setMap {
				if _, blocked := blockedPrefKeys[k]; blocked {
					return mcp.NewToolResultError("key is blocked: " + k), nil //nolint:nilerr
				}
			}
			if err := qc.SetPreferencesCtx(ctx, setMap); err != nil {
				return mcp.NewToolResultError("set error: " + err.Error()), nil //nolint:nilerr
			}
			b, err := json.Marshal(prefsWriteResult{Mode: "write", Applied: setMap})
			if err != nil {
				return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
			}
			return mcp.NewToolResultText(string(b)), nil
		}

		prefs, err := qc.GetAppPreferencesCtx(ctx)
		if err != nil {
			return mcp.NewToolResultError("get error: " + err.Error()), nil //nolint:nilerr
		}

		raw, err := json.Marshal(prefs)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		var allPrefs map[string]interface{}
		if err := json.Unmarshal(raw, &allPrefs); err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}

		for k := range blockedPrefKeys {
			delete(allPrefs, k)
		}

		if len(getKeys) > 0 {
			for _, k := range getKeys {
				if _, blocked := blockedPrefKeys[k]; blocked {
					return mcp.NewToolResultError("key is blocked: " + k), nil //nolint:nilerr
				}
			}
			filtered := make(map[string]interface{}, len(getKeys))
			for _, k := range getKeys {
				if v, ok := allPrefs[k]; ok {
					filtered[k] = v
				}
			}
			allPrefs = filtered
		}

		b, err := json.Marshal(prefsReadResult{Mode: "read", Preferences: allPrefs})
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}

	return tool, handler
}

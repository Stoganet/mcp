package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	qbit "github.com/autobrr/go-qbittorrent"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockQBitClient struct {
	getTorrentsFn          func(ctx context.Context, opts qbit.TorrentFilterOptions) ([]qbit.Torrent, error)
	getTorrentPropertiesFn func(ctx context.Context, hash string) (qbit.TorrentProperties, error)
	getTorrentTrackersFn   func(ctx context.Context, hash string) ([]qbit.TorrentTracker, error)
	getFilesInformationFn  func(ctx context.Context, hash string) (*qbit.TorrentFiles, error)
	stopFn                 func(ctx context.Context, hashes []string) error
	startFn                func(ctx context.Context, hashes []string) error
	deleteTorrentsFn       func(ctx context.Context, hashes []string, deleteFiles bool) error
	getTransferInfoFn      func(ctx context.Context) (*qbit.TransferInfo, error)
	getAppPreferencesFn    func(ctx context.Context) (qbit.AppPreferences, error)
	setPreferencesFn       func(ctx context.Context, prefs map[string]interface{}) error
}

func (m *mockQBitClient) GetTorrentsCtx(ctx context.Context, opts qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
	return m.getTorrentsFn(ctx, opts)
}
func (m *mockQBitClient) GetTorrentPropertiesCtx(ctx context.Context, hash string) (qbit.TorrentProperties, error) {
	return m.getTorrentPropertiesFn(ctx, hash)
}
func (m *mockQBitClient) GetTorrentTrackersCtx(ctx context.Context, hash string) ([]qbit.TorrentTracker, error) {
	return m.getTorrentTrackersFn(ctx, hash)
}
func (m *mockQBitClient) GetFilesInformationCtx(ctx context.Context, hash string) (*qbit.TorrentFiles, error) {
	return m.getFilesInformationFn(ctx, hash)
}
func (m *mockQBitClient) StopCtx(ctx context.Context, hashes []string) error {
	return m.stopFn(ctx, hashes)
}
func (m *mockQBitClient) StartCtx(ctx context.Context, hashes []string) error {
	return m.startFn(ctx, hashes)
}
func (m *mockQBitClient) DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error {
	return m.deleteTorrentsFn(ctx, hashes, deleteFiles)
}
func (m *mockQBitClient) GetTransferInfoCtx(ctx context.Context) (*qbit.TransferInfo, error) {
	return m.getTransferInfoFn(ctx)
}
func (m *mockQBitClient) GetAppPreferencesCtx(ctx context.Context) (qbit.AppPreferences, error) {
	return m.getAppPreferencesFn(ctx)
}
func (m *mockQBitClient) SetPreferencesCtx(ctx context.Context, prefs map[string]interface{}) error {
	return m.setPreferencesFn(ctx, prefs)
}

func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned Go error: %v", err)
	}
	return result
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r.IsError {
		t.Fatalf("expected success, got MCP error: %s", r.Content[0].(mcp.TextContent).Text)
	}
	return r.Content[0].(mcp.TextContent).Text
}

func resultError(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if !r.IsError {
		t.Fatal("expected MCP error result, got success")
	}
	return r.Content[0].(mcp.TextContent).Text
}

func TestQBitTorrents_List(t *testing.T) {
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{
				{Hash: "abc123", Name: "Movie.mkv", State: qbit.TorrentStateDownloading, Progress: 0.5, Size: 2 * 1024 * 1024 * 1024, Category: "radarr"},
				{Hash: "def456", Name: "Show.S01.mkv", State: qbit.TorrentStateStoppedDl, Progress: 1.0, Size: 1024 * 1024 * 1024, Category: "sonarr"},
			}, nil
		},
	}

	_, handler := tools.QBitTorrents(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Total    int `json:"total"`
		Filtered int `json:"filtered"`
		Torrents []struct {
			Hash     string  `json:"hash"`
			State    string  `json:"state"`
			SizeMB   float64 `json:"size_mb"`
			Category string  `json:"category"`
		} `json:"torrents"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Total != 2 {
		t.Fatalf("want total=2, got %d", out.Total)
	}
	if out.Torrents[0].State != "downloading" {
		t.Errorf("want state=downloading, got %q", out.Torrents[0].State)
	}
	if out.Torrents[1].State != "stopped" {
		t.Errorf("want state=stopped, got %q", out.Torrents[1].State)
	}
	if out.Torrents[0].SizeMB != 2048 {
		t.Errorf("want size_mb=2048, got %v", out.Torrents[0].SizeMB)
	}
	if out.Torrents[0].Category != "radarr" {
		t.Errorf("want category=radarr, got %q", out.Torrents[0].Category)
	}
}

func TestQBitTorrents_FilterByStatus(t *testing.T) {
	var capturedFilter qbit.TorrentFilter
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, opts qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			capturedFilter = opts.Filter
			return nil, nil
		},
	}

	_, handler := tools.QBitTorrents(mock)
	callTool(t, handler, map[string]any{"status": "stopped"})

	if capturedFilter != qbit.TorrentFilterStopped {
		t.Errorf("want filter=stopped, got %q", capturedFilter)
	}
}

func TestQBitTorrents_UnknownStatus(t *testing.T) {
	mock := &mockQBitClient{}
	_, handler := tools.QBitTorrents(mock)
	r := callTool(t, handler, map[string]any{"status": "bogus"})
	resultError(t, r)
}

func TestQBitTorrents_SDKError(t *testing.T) {
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	_, handler := tools.QBitTorrents(mock)
	r := callTool(t, handler, nil)
	msg := resultError(t, r)
	if !strings.Contains(msg, "connection refused") {
		t.Errorf("want error to contain 'connection refused', got %q", msg)
	}
}

func TestQBitTorrentDetail_MissingHash(t *testing.T) {
	mock := &mockQBitClient{}
	_, handler := tools.QBitTorrentDetail(mock)
	r := callTool(t, handler, map[string]any{"hash": ""})
	resultError(t, r)
}

func TestQBitTorrentDetail_PseudoTrackersFiltered(t *testing.T) {
	files := qbit.TorrentFiles{{Name: "file.mkv", Size: 1024 * 1024 * 1024, Progress: 1.0, Priority: 1}}
	mock := &mockQBitClient{
		getTorrentPropertiesFn: func(_ context.Context, _ string) (qbit.TorrentProperties, error) {
			return qbit.TorrentProperties{Name: "Movie", TotalSize: 1024 * 1024 * 1024}, nil
		},
		getTorrentTrackersFn: func(_ context.Context, _ string) ([]qbit.TorrentTracker, error) {
			return []qbit.TorrentTracker{
				{Url: "** [DHT]", Status: 0},
				{Url: "** [PeX]", Status: 0},
				{Url: "** [LSD]", Status: 0},
				{Url: "https://tracker.example.com/announce", Status: 2, NumSeeds: 10},
			}, nil
		},
		getFilesInformationFn: func(_ context.Context, _ string) (*qbit.TorrentFiles, error) {
			return &files, nil
		},
	}

	_, handler := tools.QBitTorrentDetail(mock)
	r := callTool(t, handler, map[string]any{"hash": "abc123"})
	body := resultText(t, r)

	var out struct {
		Trackers []struct {
			URL   string `json:"url"`
			Seeds int    `json:"seeds"`
		} `json:"trackers"`
		Files []struct {
			Name   string  `json:"name"`
			SizeMB float64 `json:"size_mb"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Trackers) != 1 {
		t.Fatalf("want 1 tracker (pseudo filtered), got %d", len(out.Trackers))
	}
	if out.Trackers[0].URL != "https://tracker.example.com/announce" {
		t.Errorf("wrong tracker url: %q", out.Trackers[0].URL)
	}
	if out.Trackers[0].Seeds != 10 {
		t.Errorf("want seeds=10, got %d", out.Trackers[0].Seeds)
	}
	if len(out.Files) != 1 {
		t.Fatalf("want 1 file, got %d", len(out.Files))
	}
	if out.Files[0].SizeMB != 1024 {
		t.Errorf("want size_mb=1024, got %v", out.Files[0].SizeMB)
	}
}

func TestQBitTorrentDetail_PropertiesError(t *testing.T) {
	mock := &mockQBitClient{
		getTorrentPropertiesFn: func(_ context.Context, _ string) (qbit.TorrentProperties, error) {
			return qbit.TorrentProperties{}, fmt.Errorf("not found")
		},
		getTorrentTrackersFn: func(_ context.Context, _ string) ([]qbit.TorrentTracker, error) {
			return nil, nil
		},
		getFilesInformationFn: func(_ context.Context, _ string) (*qbit.TorrentFiles, error) {
			return nil, nil
		},
	}
	_, handler := tools.QBitTorrentDetail(mock)
	r := callTool(t, handler, map[string]any{"hash": "abc123"})
	resultError(t, r)
}

func TestQBitTorrents_SpeedAndETAMapping(t *testing.T) {
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{
				{
					Hash:       "aaa",
					State:      qbit.TorrentStateDownloading,
					DlSpeed:    5 * 1024,           // 5 KiB/s
					UpSpeed:    2 * 1024,           // 2 KiB/s
					Downloaded: 512 * 1024 * 1024,  // 512 MB
					Size:       1024 * 1024 * 1024, // 1 GB
					ETA:        3600,
				},
			}, nil
		},
	}
	_, handler := tools.QBitTorrents(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Torrents []struct {
			DlSpeedKBs   float64 `json:"dlspeed_kbs"`
			UpSpeedKBs   float64 `json:"upspeed_kbs"`
			DownloadedMB float64 `json:"downloaded_mb"`
			SizeMB       float64 `json:"size_mb"`
			ETASeconds   int64   `json:"eta_seconds"`
		} `json:"torrents"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Torrents) != 1 {
		t.Fatalf("want 1 torrent, got %d", len(out.Torrents))
	}
	tt := out.Torrents[0]
	if tt.DlSpeedKBs != 5 {
		t.Errorf("want dlspeed_kbs=5, got %v", tt.DlSpeedKBs)
	}
	if tt.UpSpeedKBs != 2 {
		t.Errorf("want upspeed_kbs=2, got %v", tt.UpSpeedKBs)
	}
	if tt.DownloadedMB != 512 {
		t.Errorf("want downloaded_mb=512, got %v", tt.DownloadedMB)
	}
	if tt.SizeMB != 1024 {
		t.Errorf("want size_mb=1024, got %v", tt.SizeMB)
	}
	if tt.ETASeconds != 3600 {
		t.Errorf("want eta_seconds=3600, got %v", tt.ETASeconds)
	}
}

func TestQBitStop_EmptyHashes(t *testing.T) {
	mock := &mockQBitClient{}
	_, handler := tools.QBitStop(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{}})
	resultError(t, r)
}

func TestQBitStop_Categorization(t *testing.T) {
	mock := &mockQBitClient{
		stopFn: func(_ context.Context, _ []string) error { return nil },
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{
				{Hash: "aaa", State: qbit.TorrentStateStoppedDl},
				{Hash: "bbb", State: qbit.TorrentStateDownloading},
			}, nil
		},
	}
	_, handler := tools.QBitStop(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{"aaa", "bbb", "ccc"}})
	body := resultText(t, r)

	var out map[string][]string
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out["already_stopped"]) != 1 || out["already_stopped"][0] != "aaa" {
		t.Errorf("want already_stopped=[aaa], got %v", out["already_stopped"])
	}
	if len(out["stopped"]) != 1 || out["stopped"][0] != "bbb" {
		t.Errorf("want stopped=[bbb], got %v", out["stopped"])
	}
	if len(out["not_found"]) != 1 || out["not_found"][0] != "ccc" {
		t.Errorf("want not_found=[ccc], got %v", out["not_found"])
	}
}

func TestQBitStop_SDKError(t *testing.T) {
	mock := &mockQBitClient{
		stopFn: func(_ context.Context, _ []string) error { return fmt.Errorf("timeout") },
	}
	_, handler := tools.QBitStop(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{"aaa"}})
	msg := resultError(t, r)
	if !strings.Contains(msg, "timeout") {
		t.Errorf("want error to contain 'timeout', got %q", msg)
	}
}

func TestQBitStart_EmptyHashes(t *testing.T) {
	mock := &mockQBitClient{}
	_, handler := tools.QBitStart(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{}})
	resultError(t, r)
}

func TestQBitStart_Categorization(t *testing.T) {
	mock := &mockQBitClient{
		startFn: func(_ context.Context, _ []string) error { return nil },
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{
				{Hash: "aaa", State: qbit.TorrentStateStoppedDl},
				{Hash: "bbb", State: qbit.TorrentStateDownloading},
			}, nil
		},
	}
	_, handler := tools.QBitStart(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{"aaa", "bbb", "ccc"}})
	body := resultText(t, r)

	var out map[string][]string
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out["started"]) != 1 || out["started"][0] != "aaa" {
		t.Errorf("want started=[aaa], got %v", out["started"])
	}
	if len(out["already_active"]) != 1 || out["already_active"][0] != "bbb" {
		t.Errorf("want already_active=[bbb], got %v", out["already_active"])
	}
	if len(out["not_found"]) != 1 || out["not_found"][0] != "ccc" {
		t.Errorf("want not_found=[ccc], got %v", out["not_found"])
	}
}

func TestQBitDelete_EmptyHashes(t *testing.T) {
	mock := &mockQBitClient{}
	_, handler := tools.QBitDelete(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{}})
	resultError(t, r)
}

func TestQBitDelete_DeletesFoundSkipsNotFound(t *testing.T) {
	var gotHashes []string
	var gotDeleteFiles bool
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{{Hash: "aaa", State: qbit.TorrentStateStoppedDl}}, nil
		},
		deleteTorrentsFn: func(_ context.Context, hashes []string, deleteFiles bool) error {
			gotHashes = hashes
			gotDeleteFiles = deleteFiles
			return nil
		},
	}
	_, handler := tools.QBitDelete(mock)
	r := callTool(t, handler, map[string]any{"hashes": []any{"aaa", "bbb"}, "delete_files": true})
	body := resultText(t, r)

	var out map[string][]string
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out["deleted"]) != 1 || out["deleted"][0] != "aaa" {
		t.Errorf("want deleted=[aaa], got %v", out["deleted"])
	}
	if len(out["not_found"]) != 1 || out["not_found"][0] != "bbb" {
		t.Errorf("want not_found=[bbb], got %v", out["not_found"])
	}
	if len(gotHashes) != 1 || gotHashes[0] != "aaa" {
		t.Errorf("SDK called with wrong hashes: %v", gotHashes)
	}
	if !gotDeleteFiles {
		t.Errorf("want delete_files=true passed to SDK")
	}
}

func TestQBitDelete_DefaultDeleteFilesIsFalse(t *testing.T) {
	var gotDeleteFiles bool
	mock := &mockQBitClient{
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{{Hash: "aaa", State: qbit.TorrentStateStoppedDl}}, nil
		},
		deleteTorrentsFn: func(_ context.Context, _ []string, deleteFiles bool) error {
			gotDeleteFiles = deleteFiles
			return nil
		},
	}
	_, handler := tools.QBitDelete(mock)
	callTool(t, handler, map[string]any{"hashes": []any{"aaa"}})
	if gotDeleteFiles {
		t.Errorf("want delete_files=false by default")
	}
}

func TestQBitTransferInfo_SpeedsAndCounts(t *testing.T) {
	mock := &mockQBitClient{
		getTransferInfoFn: func(_ context.Context) (*qbit.TransferInfo, error) {
			return &qbit.TransferInfo{
				DlInfoSpeed:      10 * 1024,
				UpInfoSpeed:      2 * 1024,
				DlInfoData:       500 * 1024 * 1024,
				UpInfoData:       100 * 1024 * 1024,
				ConnectionStatus: "connected",
				DHTNodes:         0,
			}, nil
		},
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return []qbit.Torrent{
				{Hash: "a", State: qbit.TorrentStateDownloading},
				{Hash: "b", State: qbit.TorrentStateDownloading},
				{Hash: "c", State: qbit.TorrentStateStoppedDl},
				{Hash: "d", State: qbit.TorrentStateError},
			}, nil
		},
	}

	_, handler := tools.QBitTransferInfo(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		DlSpeedKBs       float64 `json:"dl_speed_kbs"`
		UpSpeedKBs       float64 `json:"up_speed_kbs"`
		DlTotalMB        float64 `json:"dl_total_mb"`
		ConnectionStatus string  `json:"connection_status"`
		ActiveDownloads  int     `json:"active_downloads"`
		Stopped          int     `json:"stopped"`
		Errored          int     `json:"errored"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.DlSpeedKBs != 10 {
		t.Errorf("want dl_speed_kbs=10, got %v", out.DlSpeedKBs)
	}
	if out.UpSpeedKBs != 2 {
		t.Errorf("want up_speed_kbs=2, got %v", out.UpSpeedKBs)
	}
	if out.DlTotalMB != 500 {
		t.Errorf("want dl_total_mb=500, got %v", out.DlTotalMB)
	}
	if out.ConnectionStatus != "connected" {
		t.Errorf("want connection_status=connected, got %q", out.ConnectionStatus)
	}
	if out.ActiveDownloads != 2 {
		t.Errorf("want active_downloads=2, got %d", out.ActiveDownloads)
	}
	if out.Stopped != 1 {
		t.Errorf("want stopped=1, got %d", out.Stopped)
	}
	if out.Errored != 1 {
		t.Errorf("want errored=1, got %d", out.Errored)
	}
}

func TestQBitTransferInfo_SDKError(t *testing.T) {
	mock := &mockQBitClient{
		getTransferInfoFn: func(_ context.Context) (*qbit.TransferInfo, error) {
			return nil, fmt.Errorf("unreachable")
		},
		getTorrentsFn: func(_ context.Context, _ qbit.TorrentFilterOptions) ([]qbit.Torrent, error) {
			return nil, nil
		},
	}
	_, handler := tools.QBitTransferInfo(mock)
	r := callTool(t, handler, nil)
	msg := resultError(t, r)
	if !strings.Contains(msg, "unreachable") {
		t.Errorf("want error to contain 'unreachable', got %q", msg)
	}
}

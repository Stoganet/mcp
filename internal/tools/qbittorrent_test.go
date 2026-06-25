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
		t.Errorf("want total=2, got %d", out.Total)
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

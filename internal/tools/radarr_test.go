package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golift.io/starr"
	"golift.io/starr/radarr"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockRadarrClient struct {
	getIntoFn              func(ctx context.Context, req starr.Request, output any) error
	getSystemStatusFn      func(ctx context.Context) (*radarr.SystemStatus, error)
	getMovieFn             func(ctx context.Context, opts *radarr.GetMovie) ([]*radarr.Movie, error)
	getMovieByIDFn         func(ctx context.Context, id int64) (*radarr.Movie, error)
	getQueueFn             func(ctx context.Context, records, perPage int) (*radarr.Queue, error)
	getHistoryPageFn       func(ctx context.Context, params *starr.PageReq) (*radarr.History, error)
	getQualityProfilesFn   func(ctx context.Context) ([]*radarr.QualityProfile, error)
	getQualityProfileFn    func(ctx context.Context, id int64) (*radarr.QualityProfile, error)
	updateQualityProfileFn func(ctx context.Context, p *radarr.QualityProfile) (*radarr.QualityProfile, error)
	sendCommandFn          func(ctx context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error)
}

var _ tools.RadarrClient = (*mockRadarrClient)(nil)

func (m *mockRadarrClient) GetInto(ctx context.Context, req starr.Request, output any) error {
	return m.getIntoFn(ctx, req, output)
}
func (m *mockRadarrClient) GetSystemStatusContext(ctx context.Context) (*radarr.SystemStatus, error) {
	return m.getSystemStatusFn(ctx)
}
func (m *mockRadarrClient) GetMovieContext(ctx context.Context, opts *radarr.GetMovie) ([]*radarr.Movie, error) {
	return m.getMovieFn(ctx, opts)
}
func (m *mockRadarrClient) GetMovieByIDContext(ctx context.Context, id int64) (*radarr.Movie, error) {
	return m.getMovieByIDFn(ctx, id)
}
func (m *mockRadarrClient) GetQueueContext(ctx context.Context, records, perPage int) (*radarr.Queue, error) {
	return m.getQueueFn(ctx, records, perPage)
}
func (m *mockRadarrClient) GetHistoryPageContext(ctx context.Context, params *starr.PageReq) (*radarr.History, error) {
	return m.getHistoryPageFn(ctx, params)
}
func (m *mockRadarrClient) GetQualityProfilesContext(ctx context.Context) ([]*radarr.QualityProfile, error) {
	return m.getQualityProfilesFn(ctx)
}
func (m *mockRadarrClient) GetQualityProfileContext(ctx context.Context, id int64) (*radarr.QualityProfile, error) {
	return m.getQualityProfileFn(ctx, id)
}
func (m *mockRadarrClient) UpdateQualityProfileContext(ctx context.Context, p *radarr.QualityProfile) (*radarr.QualityProfile, error) {
	return m.updateQualityProfileFn(ctx, p)
}
func (m *mockRadarrClient) SendCommandContext(ctx context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error) {
	return m.sendCommandFn(ctx, cmd)
}

func TestRadarrHealth(t *testing.T) {
	mock := &mockRadarrClient{
		getIntoFn: func(_ context.Context, req starr.Request, output any) error {
			switch req.URI {
			case "/api/v3/health":
				return json.Unmarshal([]byte(`[{"source":"Test","type":"warning","message":"update available"}]`), output)
			case "/api/v3/diskspace":
				return json.Unmarshal([]byte(`[{"path":"/data","freeSpace":107374182400,"totalSpace":429496729600}]`), output)
			default:
				return errors.New("unexpected URI: " + req.URI)
			}
		},
		getSystemStatusFn: func(_ context.Context) (*radarr.SystemStatus, error) {
			return &radarr.SystemStatus{Version: "5.14.0"}, nil
		},
	}

	_, handler := tools.RadarrHealth(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Version string `json:"version"`
		Issues  []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"issues"`
		Disk []struct {
			Path    string  `json:"path"`
			FreeGB  float64 `json:"free_gb"`
			TotalGB float64 `json:"total_gb"`
		} `json:"disk_space"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Version != "5.14.0" {
		t.Errorf("version = %q, want 5.14.0", out.Version)
	}
	if len(out.Issues) != 1 || out.Issues[0].Type != "warning" {
		t.Errorf("unexpected issues: %+v", out.Issues)
	}
	if len(out.Disk) != 1 || out.Disk[0].Path != "/data" {
		t.Errorf("unexpected disk_space: %+v", out.Disk)
	}
	if out.Disk[0].FreeGB != 100 || out.Disk[0].TotalGB != 400 {
		t.Errorf("disk GB = free %.0f total %.0f, want 100/400", out.Disk[0].FreeGB, out.Disk[0].TotalGB)
	}
}

func TestRadarrMovie_ByID(t *testing.T) {
	mock := &mockRadarrClient{
		getMovieByIDFn: func(_ context.Context, id int64) (*radarr.Movie, error) {
			if id != 42 {
				return nil, errors.New("unexpected id")
			}
			return &radarr.Movie{
				ID:      42,
				Title:   "The Dark Knight",
				Year:    2008,
				TmdbID:  155,
				HasFile: true,
				MovieFile: &radarr.MovieFile{
					RelativePath: "The Dark Knight (2008)/The.Dark.Knight.2008.mkv",
					Size:         10 * (1 << 30),
					ReleaseGroup: "FGT",
					Quality: &starr.Quality{
						Quality: &starr.BaseQuality{Name: "Bluray-1080p"},
					},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrMovie(mock)
	r := callTool(t, handler, map[string]any{"id": float64(42)})
	body := resultText(t, r)

	var out []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		File  *struct {
			Quality      string  `json:"quality"`
			SizeGB       float64 `json:"size_gb"`
			ReleaseGroup string  `json:"release_group"`
		} `json:"file"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 result, got %d", len(out))
	}
	if out[0].ID != 42 || out[0].Title != "The Dark Knight" {
		t.Errorf("unexpected movie: %+v", out[0])
	}
	if out[0].File == nil {
		t.Fatal("want file info")
	}
	if out[0].File.Quality != "Bluray-1080p" {
		t.Errorf("quality = %q, want Bluray-1080p", out[0].File.Quality)
	}
	if out[0].File.SizeGB != 10 {
		t.Errorf("size_gb = %v, want 10", out[0].File.SizeGB)
	}
}

func TestRadarrMovie_ByTitle(t *testing.T) {
	mock := &mockRadarrClient{
		getMovieFn: func(_ context.Context, _ *radarr.GetMovie) ([]*radarr.Movie, error) {
			return []*radarr.Movie{
				{ID: 1, Title: "The Dark Knight", Year: 2008},
				{ID: 2, Title: "Batman Begins", Year: 2005},
				{ID: 3, Title: "기생충", OriginalTitle: "Parasite", Year: 2019},
			}, nil
		},
	}

	_, handler := tools.RadarrMovie(mock)

	t.Run("matches title", func(t *testing.T) {
		r := callTool(t, handler, map[string]any{"title": "dark knight"})
		var out []struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].ID != 1 {
			t.Errorf("unexpected results: %+v", out)
		}
	})

	t.Run("matches original title", func(t *testing.T) {
		r := callTool(t, handler, map[string]any{"title": "parasite"})
		var out []struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
			t.Fatal(err)
		}
		if len(out) != 1 || out[0].ID != 3 {
			t.Errorf("unexpected results: %+v", out)
		}
	})
}

func TestRadarrMovie_ByTMDB(t *testing.T) {
	mock := &mockRadarrClient{
		getMovieFn: func(_ context.Context, opts *radarr.GetMovie) ([]*radarr.Movie, error) {
			if opts.TMDBID != 155 {
				return nil, errors.New("unexpected tmdb id")
			}
			return []*radarr.Movie{{ID: 42, Title: "The Dark Knight", TmdbID: 155}}, nil
		},
	}

	_, handler := tools.RadarrMovie(mock)
	r := callTool(t, handler, map[string]any{"tmdb_id": float64(155)})
	body := resultText(t, r)

	var out []struct {
		ID     int64 `json:"id"`
		TmdbID int64 `json:"tmdb_id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].ID != 42 || out[0].TmdbID != 155 {
		t.Errorf("unexpected result: %+v", out)
	}
}

func TestRadarrMovie_ByAlternateTitle(t *testing.T) {
	mock := &mockRadarrClient{
		getMovieFn: func(_ context.Context, _ *radarr.GetMovie) ([]*radarr.Movie, error) {
			return []*radarr.Movie{
				{
					ID:    3,
					Title: "기생충",
					AlternateTitles: []*radarr.AlternativeTitle{
						{Title: "Parasite"},
					},
				},
				{ID: 4, Title: "Batman Begins"},
			}, nil
		},
	}

	_, handler := tools.RadarrMovie(mock)
	r := callTool(t, handler, map[string]any{"title": "parasite"})

	var out []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != 3 {
		t.Errorf("unexpected results: %+v", out)
	}
}

func TestRadarrMovie_NoFile(t *testing.T) {
	mock := &mockRadarrClient{
		getMovieByIDFn: func(_ context.Context, _ int64) (*radarr.Movie, error) {
			return &radarr.Movie{ID: 1, Title: "Unmonitored", HasFile: false}, nil
		},
	}

	_, handler := tools.RadarrMovie(mock)
	r := callTool(t, handler, map[string]any{"id": float64(1)})

	var out []struct {
		HasFile bool      `json:"has_file"`
		File    *struct{} `json:"file"`
	}
	if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 result, got %d", len(out))
	}
	if out[0].HasFile || out[0].File != nil {
		t.Errorf("want has_file=false and no file, got %+v", out[0])
	}
}

func TestRadarrMovie_NoParam(t *testing.T) {
	mock := &mockRadarrClient{}
	_, handler := tools.RadarrMovie(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when no params provided")
	}
}

func TestRadarrQueue(t *testing.T) {
	eta, _ := time.Parse(time.RFC3339, "2026-06-26T18:00:00Z")
	mock := &mockRadarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*radarr.Queue, error) {
			return &radarr.Queue{
				TotalRecords: 1,
				Records: []*radarr.QueueRecord{
					{
						ID:       7,
						MovieID:  42,
						Title:    "The Dark Knight",
						Status:   "downloading",
						Protocol: "torrent",
						Size:     10 * (1 << 30),
						Sizeleft: 4 * (1 << 30),
						Quality: &starr.Quality{
							Quality: &starr.BaseQuality{Name: "Bluray-1080p"},
						},
						Indexer:        "MyIndexer",
						DownloadClient: "qbit-gluetun",
						StatusMessages: []*starr.StatusMessage{
							{Title: "warn", Messages: []string{"stalled", "retry later"}},
						},
						EstimatedCompletionTime: eta,
					},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrQueue(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out []struct {
		ID             int64    `json:"id"`
		MovieID        int64    `json:"movie_id"`
		Title          string   `json:"title"`
		Status         string   `json:"status"`
		Protocol       string   `json:"protocol"`
		Quality        string   `json:"quality"`
		SizeGB         float64  `json:"size_gb"`
		RemainingGB    float64  `json:"remaining_gb"`
		Percent        float64  `json:"percent"`
		ETA            string   `json:"eta"`
		Indexer        string   `json:"indexer"`
		DownloadClient string   `json:"download_client"`
		Warnings       []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 record, got %d", len(out))
	}
	rec := out[0]
	if rec.ID != 7 || rec.MovieID != 42 {
		t.Errorf("id/movie_id mismatch: %+v", rec)
	}
	if rec.Title != "The Dark Knight" || rec.Status != "downloading" {
		t.Errorf("title/status mismatch: %+v", rec)
	}
	if rec.Protocol != "torrent" {
		t.Errorf("protocol = %q, want torrent", rec.Protocol)
	}
	if rec.Quality != "Bluray-1080p" {
		t.Errorf("quality = %q, want Bluray-1080p", rec.Quality)
	}
	if rec.SizeGB != 10 || rec.RemainingGB != 4 {
		t.Errorf("size mismatch: size_gb=%v remaining_gb=%v", rec.SizeGB, rec.RemainingGB)
	}
	if rec.Percent != 60 {
		t.Errorf("percent = %v, want 60", rec.Percent)
	}
	if rec.ETA != "2026-06-26T18:00:00Z" {
		t.Errorf("eta = %q, want 2026-06-26T18:00:00Z", rec.ETA)
	}
	if rec.Indexer != "MyIndexer" {
		t.Errorf("indexer = %q, want MyIndexer", rec.Indexer)
	}
	if rec.DownloadClient != "qbit-gluetun" {
		t.Errorf("download_client = %q, want qbit-gluetun", rec.DownloadClient)
	}
	if len(rec.Warnings) != 2 || rec.Warnings[0] != "stalled" {
		t.Errorf("warnings = %v, want [stalled retry later]", rec.Warnings)
	}
}

func TestRadarrQueue_ZeroETA(t *testing.T) {
	mock := &mockRadarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*radarr.Queue, error) {
			return &radarr.Queue{
				Records: []*radarr.QueueRecord{
					{ID: 1, Title: "Some Movie", Status: "queued"},
					// EstimatedCompletionTime is zero value
				},
			}, nil
		},
	}

	_, handler := tools.RadarrQueue(mock)
	r := callTool(t, handler, nil)

	var raw []map[string]any
	if err := json.Unmarshal([]byte(resultText(t, r)), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("want 1 record, got %d", len(raw))
	}
	if _, ok := raw[0]["eta"]; ok {
		t.Error("eta key must be absent when EstimatedCompletionTime is zero")
	}
}

func TestRadarrQueue_Empty(t *testing.T) {
	mock := &mockRadarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*radarr.Queue, error) {
			return &radarr.Queue{Records: []*radarr.QueueRecord{}}, nil
		},
	}
	_, handler := tools.RadarrQueue(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)
	if body != "[]" {
		t.Errorf("want empty array, got %q", body)
	}
}

func TestRadarrQueue_Error(t *testing.T) {
	mock := &mockRadarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*radarr.Queue, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.RadarrQueue(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error on queue fetch failure")
	}
}

func TestRadarrHistory(t *testing.T) {
	date, _ := time.Parse(time.RFC3339, "2026-06-25T10:00:00Z")
	mock := &mockRadarrClient{
		getHistoryPageFn: func(_ context.Context, _ *starr.PageReq) (*radarr.History, error) {
			return &radarr.History{
				Records: []*radarr.HistoryRecord{
					{
						ID:          99,
						MovieID:     42,
						SourceTitle: "The.Dark.Knight.2008.mkv",
						EventType:   "downloadFolderImported",
						Date:        date,
						Quality: &starr.Quality{
							Quality: &starr.BaseQuality{Name: "Bluray-1080p"},
						},
						Data: struct {
							Age                string         `json:"age"`
							AgeHours           string         `json:"ageHours"`
							AgeMinutes         string         `json:"ageMinutes"`
							DownloadClient     string         `json:"downloadClient"`
							DownloadClientName string         `json:"downloadClientName"`
							DownloadURL        string         `json:"downloadUrl"`
							DroppedPath        string         `json:"droppedPath"`
							FileID             string         `json:"fileId"`
							GUID               string         `json:"guid"`
							ImportedPath       string         `json:"importedPath"`
							Indexer            string         `json:"indexer"`
							IndexerFlags       string         `json:"indexerFlags"`
							IndexerID          string         `json:"indexerId"`
							Message            string         `json:"message"`
							NzbInfoURL         string         `json:"nzbInfoUrl"`
							Protocol           starr.Protocol `json:"protocol"`
							PublishedDate      time.Time      `json:"publishedDate"`
							Reason             string         `json:"reason"`
							ReleaseGroup       string         `json:"releaseGroup"`
							Size               string         `json:"size"`
							TmdbID             string         `json:"tmdbId"`
							TorrentInfoHash    string         `json:"torrentInfoHash"`
						}{Indexer: "NZBgeek"},
					},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrHistory(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out []struct {
		ID        int64  `json:"id"`
		MovieID   int64  `json:"movie_id"`
		Title     string `json:"title"`
		EventType string `json:"event_type"`
		Date      string `json:"date"`
		Quality   string `json:"quality"`
		Indexer   string `json:"indexer"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 record, got %d", len(out))
	}
	rec := out[0]
	if rec.ID != 99 || rec.MovieID != 42 {
		t.Errorf("id/movie_id: %+v", rec)
	}
	if rec.EventType != "downloadFolderImported" {
		t.Errorf("event_type = %q", rec.EventType)
	}
	if rec.Date != "2026-06-25T10:00:00Z" {
		t.Errorf("date = %q", rec.Date)
	}
	if rec.Quality != "Bluray-1080p" {
		t.Errorf("quality = %q", rec.Quality)
	}
	if rec.Indexer != "NZBgeek" {
		t.Errorf("indexer = %q", rec.Indexer)
	}
}

func TestRadarrHistory_MovieIDFilter(t *testing.T) {
	mock := &mockRadarrClient{
		getHistoryPageFn: func(_ context.Context, params *starr.PageReq) (*radarr.History, error) {
			if params.Get("movieId") != "42" {
				return nil, errors.New("unexpected movieId param")
			}
			return &radarr.History{Records: []*radarr.HistoryRecord{}}, nil
		},
	}

	_, handler := tools.RadarrHistory(mock)
	r := callTool(t, handler, map[string]any{"movie_id": float64(42)})
	if r.IsError {
		t.Errorf("unexpected error: %s", resultText(t, r))
	}
}

func TestRadarrHistory_NilQuality(t *testing.T) {
	mock := &mockRadarrClient{
		getHistoryPageFn: func(_ context.Context, _ *starr.PageReq) (*radarr.History, error) {
			return &radarr.History{
				Records: []*radarr.HistoryRecord{
					{ID: 1, EventType: "grabbed", Quality: nil},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrHistory(mock)
	r := callTool(t, handler, nil)

	var raw []map[string]any
	if err := json.Unmarshal([]byte(resultText(t, r)), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw[0]["quality"]; ok {
		t.Error("quality key must be absent when nil")
	}
}

func TestRadarrHistory_Error(t *testing.T) {
	mock := &mockRadarrClient{
		getHistoryPageFn: func(_ context.Context, _ *starr.PageReq) (*radarr.History, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.RadarrHistory(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error on history fetch failure")
	}
}

func TestRadarrSearch(t *testing.T) {
	mock := &mockRadarrClient{
		sendCommandFn: func(_ context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error) {
			if cmd.Name != "MoviesSearch" || len(cmd.MovieIDs) != 1 || cmd.MovieIDs[0] != 42 {
				return nil, errors.New("unexpected command")
			}
			return &radarr.CommandResponse{ID: 7, Status: "queued"}, nil
		},
	}

	_, handler := tools.RadarrSearch(mock)
	r := callTool(t, handler, map[string]any{"id": float64(42)})
	body := resultText(t, r)

	var out struct {
		CommandID int64  `json:"command_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.CommandID != 7 || out.Status != "queued" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestRadarrSearch_MissingID(t *testing.T) {
	mock := &mockRadarrClient{}
	_, handler := tools.RadarrSearch(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when id not provided")
	}
}

func TestRadarrSearch_Error(t *testing.T) {
	mock := &mockRadarrClient{
		sendCommandFn: func(_ context.Context, _ *radarr.CommandRequest) (*radarr.CommandResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.RadarrSearch(mock)
	r := callTool(t, handler, map[string]any{"id": float64(1)})
	if !r.IsError {
		t.Error("want MCP error on command failure")
	}
}

func TestRadarrQualityProfiles(t *testing.T) {
	mock := &mockRadarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*radarr.QualityProfile, error) {
			return []*radarr.QualityProfile{
				{
					ID:             1,
					Name:           "HD-1080p",
					UpgradeAllowed: true,
					Cutoff:         7,
					Qualities: []*starr.Quality{
						{Quality: &starr.BaseQuality{ID: 3, Name: "WEBDL-1080p"}, Allowed: true},
						{Quality: &starr.BaseQuality{ID: 7, Name: "Bluray-1080p"}, Allowed: true},
					},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrQualityProfiles(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out []struct {
		ID             int64  `json:"id"`
		Name           string `json:"name"`
		UpgradeAllowed bool   `json:"upgrade_allowed"`
		Cutoff         string `json:"cutoff"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 profile, got %d", len(out))
	}
	if out[0].ID != 1 || out[0].Name != "HD-1080p" {
		t.Errorf("id/name mismatch: %+v", out[0])
	}
	if out[0].Cutoff != "Bluray-1080p" {
		t.Errorf("cutoff = %q, want Bluray-1080p", out[0].Cutoff)
	}
	if !out[0].UpgradeAllowed {
		t.Error("want upgrade_allowed=true")
	}
}

func TestRadarrQualityProfiles_CutoffGroup(t *testing.T) {
	mock := &mockRadarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*radarr.QualityProfile, error) {
			return []*radarr.QualityProfile{
				{
					ID:     2,
					Name:   "Any",
					Cutoff: 1000,
					Qualities: []*starr.Quality{
						{ID: 1000, Name: "HD Group", Items: []*starr.Quality{
							{Quality: &starr.BaseQuality{ID: 3, Name: "WEBDL-1080p"}},
						}},
					},
				},
			}, nil
		},
	}

	_, handler := tools.RadarrQualityProfiles(mock)
	r := callTool(t, handler, nil)

	var out []struct {
		Cutoff string `json:"cutoff"`
	}
	if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
		t.Fatal(err)
	}
	if out[0].Cutoff != "HD Group" {
		t.Errorf("cutoff = %q, want HD Group", out[0].Cutoff)
	}
}

func TestRadarrQualityProfiles_Error(t *testing.T) {
	mock := &mockRadarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*radarr.QualityProfile, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.RadarrQualityProfiles(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error on fetch failure")
	}
}

func TestRadarrUpdateQualityProfile(t *testing.T) {
	mock := &mockRadarrClient{
		getQualityProfileFn: func(_ context.Context, id int64) (*radarr.QualityProfile, error) {
			if id != 1 {
				return nil, errors.New("unexpected id")
			}
			return &radarr.QualityProfile{
				ID:             1,
				Name:           "HD-1080p",
				UpgradeAllowed: true,
				Cutoff:         7,
				Qualities:      []*starr.Quality{{Quality: &starr.BaseQuality{ID: 7, Name: "Bluray-1080p"}}},
			}, nil
		},
		updateQualityProfileFn: func(_ context.Context, p *radarr.QualityProfile) (*radarr.QualityProfile, error) {
			if p.Name != "HD-1080p Renamed" {
				return nil, errors.New("unexpected name: " + p.Name)
			}
			return p, nil
		},
	}

	_, handler := tools.RadarrUpdateQualityProfile(mock)
	r := callTool(t, handler, map[string]any{"id": float64(1), "name": "HD-1080p Renamed"})
	body := resultText(t, r)

	var out struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Cutoff string `json:"cutoff"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != "HD-1080p Renamed" {
		t.Errorf("name = %q", out.Name)
	}
	if out.Cutoff != "Bluray-1080p" {
		t.Errorf("cutoff = %q, want Bluray-1080p", out.Cutoff)
	}
}

func TestRadarrUpdateQualityProfile_MissingID(t *testing.T) {
	mock := &mockRadarrClient{}
	_, handler := tools.RadarrUpdateQualityProfile(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when id not provided")
	}
}

func TestRadarrUpdateQualityProfile_GetError(t *testing.T) {
	mock := &mockRadarrClient{
		getQualityProfileFn: func(_ context.Context, _ int64) (*radarr.QualityProfile, error) {
			return nil, errors.New("not found")
		},
	}
	_, handler := tools.RadarrUpdateQualityProfile(mock)
	r := callTool(t, handler, map[string]any{"id": float64(99)})
	if !r.IsError {
		t.Error("want MCP error on get failure")
	}
}

func TestRadarrHealth_Error(t *testing.T) {
	mock := &mockRadarrClient{
		getIntoFn: func(_ context.Context, _ starr.Request, _ any) error {
			return errors.New("connection refused")
		},
		getSystemStatusFn: func(_ context.Context) (*radarr.SystemStatus, error) {
			return &radarr.SystemStatus{}, nil
		},
	}

	_, handler := tools.RadarrHealth(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when health endpoint fails")
	}
}

func TestRadarrHealth_AllErrorsJoined(t *testing.T) {
	mock := &mockRadarrClient{
		getIntoFn: func(_ context.Context, req starr.Request, _ any) error {
			switch req.URI {
			case "/api/v3/health":
				return errors.New("health failed")
			case "/api/v3/diskspace":
				return errors.New("diskspace failed")
			default:
				return errors.New("unexpected URI: " + req.URI)
			}
		},
		getSystemStatusFn: func(_ context.Context) (*radarr.SystemStatus, error) {
			return nil, errors.New("status failed")
		},
	}

	_, handler := tools.RadarrHealth(mock)
	r := callTool(t, handler, nil)
	body := resultError(t, r)
	if !strings.Contains(body, "health failed") || !strings.Contains(body, "status failed") || !strings.Contains(body, "diskspace failed") {
		t.Errorf("want all three errors in message, got: %s", body)
	}
}

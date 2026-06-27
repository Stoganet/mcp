package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golift.io/starr"
	"golift.io/starr/sonarr"

	"github.com/Stoganet/mcp/internal/tools"
)

type mockSonarrClient struct {
	getIntoFn              func(ctx context.Context, req starr.Request, output any) error
	getSystemStatusFn      func(ctx context.Context) (*sonarr.SystemStatus, error)
	getSeriesFn            func(ctx context.Context, tvdbID int64) ([]*sonarr.Series, error)
	getSeriesByIDFn        func(ctx context.Context, seriesID int64) (*sonarr.Series, error)
	getSeriesEpisodesFn    func(ctx context.Context, getEpisode *sonarr.GetEpisode) ([]*sonarr.Episode, error)
	getQueueFn             func(ctx context.Context, records, perPage int) (*sonarr.Queue, error)
	getQualityProfilesFn   func(ctx context.Context) ([]*sonarr.QualityProfile, error)
	getQualityProfileFn    func(ctx context.Context, id int64) (*sonarr.QualityProfile, error)
	updateQualityProfileFn func(ctx context.Context, p *sonarr.QualityProfile) (*sonarr.QualityProfile, error)
	sendCommandFn          func(ctx context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error)
}

var _ tools.SonarrClient = (*mockSonarrClient)(nil)

func (m *mockSonarrClient) GetInto(ctx context.Context, req starr.Request, output any) error {
	return m.getIntoFn(ctx, req, output)
}
func (m *mockSonarrClient) GetSystemStatusContext(ctx context.Context) (*sonarr.SystemStatus, error) {
	return m.getSystemStatusFn(ctx)
}
func (m *mockSonarrClient) GetSeriesContext(ctx context.Context, tvdbID int64) ([]*sonarr.Series, error) {
	return m.getSeriesFn(ctx, tvdbID)
}
func (m *mockSonarrClient) GetSeriesByIDContext(ctx context.Context, seriesID int64) (*sonarr.Series, error) {
	return m.getSeriesByIDFn(ctx, seriesID)
}
func (m *mockSonarrClient) GetSeriesEpisodesContext(ctx context.Context, getEpisode *sonarr.GetEpisode) ([]*sonarr.Episode, error) {
	return m.getSeriesEpisodesFn(ctx, getEpisode)
}
func (m *mockSonarrClient) GetQueueContext(ctx context.Context, records, perPage int) (*sonarr.Queue, error) {
	return m.getQueueFn(ctx, records, perPage)
}
func (m *mockSonarrClient) GetQualityProfilesContext(ctx context.Context) ([]*sonarr.QualityProfile, error) {
	return m.getQualityProfilesFn(ctx)
}
func (m *mockSonarrClient) GetQualityProfileContext(ctx context.Context, id int64) (*sonarr.QualityProfile, error) {
	return m.getQualityProfileFn(ctx, id)
}
func (m *mockSonarrClient) UpdateQualityProfileContext(ctx context.Context, p *sonarr.QualityProfile) (*sonarr.QualityProfile, error) {
	return m.updateQualityProfileFn(ctx, p)
}
func (m *mockSonarrClient) SendCommandContext(ctx context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
	return m.sendCommandFn(ctx, cmd)
}

func TestSonarrHealth(t *testing.T) {
	mock := &mockSonarrClient{
		getIntoFn: func(_ context.Context, req starr.Request, output any) error {
			switch req.URI {
			case "/api/v3/health":
				return json.Unmarshal([]byte(`[{"source":"Test","type":"error","message":"indexer unavailable"}]`), output)
			case "/api/v3/diskspace":
				return json.Unmarshal([]byte(`[{"path":"/media","freeSpace":214748364800,"totalSpace":858993459200}]`), output)
			default:
				return errors.New("unexpected URI: " + req.URI)
			}
		},
		getSystemStatusFn: func(_ context.Context) (*sonarr.SystemStatus, error) {
			return &sonarr.SystemStatus{Version: "4.0.9"}, nil
		},
	}

	_, handler := tools.SonarrHealth(mock)
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
	if out.Version != "4.0.9" {
		t.Errorf("version = %q, want 4.0.9", out.Version)
	}
	if len(out.Issues) != 1 || out.Issues[0].Type != "error" {
		t.Errorf("unexpected issues: %+v", out.Issues)
	}
	if len(out.Disk) != 1 || out.Disk[0].Path != "/media" {
		t.Errorf("unexpected disk_space: %+v", out.Disk)
	}
	if out.Disk[0].FreeGB != 200 || out.Disk[0].TotalGB != 800 {
		t.Errorf("disk GB = free %.0f total %.0f, want 200/800", out.Disk[0].FreeGB, out.Disk[0].TotalGB)
	}
}

func TestSonarrHealth_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getIntoFn: func(_ context.Context, _ starr.Request, _ any) error {
			return errors.New("connection refused")
		},
		getSystemStatusFn: func(_ context.Context) (*sonarr.SystemStatus, error) {
			return &sonarr.SystemStatus{}, nil
		},
	}

	_, handler := tools.SonarrHealth(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when health endpoint fails")
	}
}

func TestSonarrHealth_AllErrorsJoined(t *testing.T) {
	mock := &mockSonarrClient{
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
		getSystemStatusFn: func(_ context.Context) (*sonarr.SystemStatus, error) {
			return nil, errors.New("status failed")
		},
	}

	_, handler := tools.SonarrHealth(mock)
	r := callTool(t, handler, nil)
	body := resultError(t, r)
	if !strings.Contains(body, "health failed") || !strings.Contains(body, "status failed") || !strings.Contains(body, "diskspace failed") {
		t.Errorf("want all three errors in message, got: %s", body)
	}
}

func makeSeries(id int64, title string, tvdbID int64) *sonarr.Series {
	return &sonarr.Series{
		ID:               id,
		Title:            title,
		Year:             2020,
		TvdbID:           tvdbID,
		Status:           "continuing",
		Monitored:        true,
		QualityProfileID: 6,
		Seasons: []*sonarr.Season{
			{
				SeasonNumber: 1,
				Monitored:    true,
				Statistics: &sonarr.Statistics{
					EpisodeCount:      10,
					EpisodeFileCount:  10,
					PercentOfEpisodes: 100,
				},
			},
			{
				SeasonNumber: 2,
				Monitored:    true,
				Statistics: &sonarr.Statistics{
					EpisodeCount:      8,
					EpisodeFileCount:  5,
					PercentOfEpisodes: 62.5,
				},
			},
		},
	}
}

func TestSonarrSeries_ByID(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, id int64) (*sonarr.Series, error) {
			if id != 12 {
				return nil, errors.New("unexpected id")
			}
			return makeSeries(12, "Breaking Bad", 81189), nil
		},
	}

	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, map[string]any{"id": float64(12)})
	body := resultText(t, r)

	var out []struct {
		ID              int64   `json:"id"`
		Title           string  `json:"title"`
		TvdbID          int64   `json:"tvdb_id"`
		TotalEpisodes   int     `json:"total_episodes"`
		TotalFiles      int     `json:"total_episode_files"`
		PercentComplete float64 `json:"percent_complete"`
		Seasons         []struct {
			SeasonNumber     int     `json:"season_number"`
			EpisodeCount     int     `json:"episode_count"`
			EpisodeFileCount int     `json:"episode_file_count"`
			PercentComplete  float64 `json:"percent_complete"`
		} `json:"seasons"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 result, got %d", len(out))
	}
	s := out[0]
	if s.ID != 12 || s.Title != "Breaking Bad" || s.TvdbID != 81189 {
		t.Errorf("unexpected series: %+v", s)
	}
	if s.TotalEpisodes != 18 || s.TotalFiles != 15 {
		t.Errorf("totals = %d/%d, want 18/15", s.TotalEpisodes, s.TotalFiles)
	}
	if s.PercentComplete != 83.33 {
		t.Errorf("percent_complete = %v, want 83.33", s.PercentComplete)
	}
	if len(s.Seasons) != 2 || s.Seasons[0].EpisodeCount != 10 || s.Seasons[1].EpisodeFileCount != 5 {
		t.Errorf("unexpected seasons: %+v", s.Seasons)
	}
}

func TestSonarrSeries_ByTVDB(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesFn: func(_ context.Context, tvdbID int64) ([]*sonarr.Series, error) {
			if tvdbID != 81189 {
				return nil, errors.New("unexpected tvdb id")
			}
			return []*sonarr.Series{makeSeries(12, "Breaking Bad", 81189)}, nil
		},
	}

	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, map[string]any{"tvdb_id": float64(81189)})
	body := resultText(t, r)

	var out []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].ID != 12 {
		t.Errorf("unexpected results: %+v", out)
	}
}

func TestSonarrSeries_ByTitle(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesFn: func(_ context.Context, tvdbID int64) ([]*sonarr.Series, error) {
			if tvdbID != 0 {
				return nil, errors.New("expected tvdbID=0 for all-series fetch")
			}
			return []*sonarr.Series{
				makeSeries(1, "Breaking Bad", 81189),
				makeSeries(2, "Better Call Saul", 273181),
				makeSeries(3, "The Wire", 79126),
			}, nil
		},
	}

	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, map[string]any{"title": "breaking"})
	body := resultText(t, r)

	var out []struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].ID != 1 {
		t.Errorf("unexpected results: %+v", out)
	}
}

func TestSonarrSeries_NoParam(t *testing.T) {
	mock := &mockSonarrClient{}
	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when no params provided")
	}
}

func TestSonarrSeries_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, _ int64) (*sonarr.Series, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, map[string]any{"id": float64(99)})
	if !r.IsError {
		t.Error("want MCP error on API failure")
	}
}

func TestSonarrSeries_ByAlternateTitle(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesFn: func(_ context.Context, _ int64) ([]*sonarr.Series, error) {
			return []*sonarr.Series{
				{
					ID:    5,
					Title: "나의 아저씨",
					AlternateTitles: []*sonarr.AlternateTitle{
						{Title: "My Mister"},
					},
				},
				{ID: 6, Title: "Breaking Bad"},
			}, nil
		},
	}

	_, handler := tools.SonarrSeries(mock)
	r := callTool(t, handler, map[string]any{"title": "my mister"})

	var out []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != 5 {
		t.Errorf("unexpected results: %+v", out)
	}
}

func TestSonarrEpisodes(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, _ int64) (*sonarr.Series, error) {
			return &sonarr.Series{Title: "Breaking Bad"}, nil
		},
		getSeriesEpisodesFn: func(_ context.Context, _ *sonarr.GetEpisode) ([]*sonarr.Episode, error) {
			return []*sonarr.Episode{
				{ID: 101, SeriesID: 12, SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot", AirDate: "2008-01-20", HasFile: true, Monitored: true},
				{ID: 102, SeriesID: 12, SeasonNumber: 1, EpisodeNumber: 2, Title: "Cat's in the Bag", AirDate: "2008-01-27", HasFile: false, Monitored: true},
			}, nil
		},
	}

	_, handler := tools.SonarrEpisodes(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(12)})
	body := resultText(t, r)

	var out struct {
		Series   string `json:"series"`
		Episodes []struct {
			ID      int64  `json:"id"`
			Season  int    `json:"season"`
			Episode int    `json:"episode"`
			Title   string `json:"title"`
			AirDate string `json:"air_date"`
			HasFile bool   `json:"has_file"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Series != "Breaking Bad" {
		t.Errorf("series = %q, want Breaking Bad", out.Series)
	}
	if len(out.Episodes) != 2 {
		t.Fatalf("want 2 episodes, got %d", len(out.Episodes))
	}
	if out.Episodes[0].ID != 101 || out.Episodes[0].Title != "Pilot" {
		t.Errorf("unexpected episode 0: %+v", out.Episodes[0])
	}
	if out.Episodes[1].AirDate != "2008-01-27" {
		t.Errorf("air_date = %q, want 2008-01-27", out.Episodes[1].AirDate)
	}
}

func TestSonarrEpisodes_MissingOnly(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, _ int64) (*sonarr.Series, error) {
			return &sonarr.Series{Title: "Breaking Bad"}, nil
		},
		getSeriesEpisodesFn: func(_ context.Context, _ *sonarr.GetEpisode) ([]*sonarr.Episode, error) {
			return []*sonarr.Episode{
				{ID: 101, HasFile: true, Monitored: true},
				{ID: 102, HasFile: false, Monitored: true},
				{ID: 103, HasFile: false, Monitored: true},
			}, nil
		},
	}

	_, handler := tools.SonarrEpisodes(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(12), "missing_only": true})
	body := resultText(t, r)

	var out struct {
		Episodes []struct {
			ID int64 `json:"id"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Episodes) != 2 {
		t.Errorf("want 2 missing episodes, got %d", len(out.Episodes))
	}
	if out.Episodes[0].ID != 102 || out.Episodes[1].ID != 103 {
		t.Errorf("unexpected episode ids: %+v", out.Episodes)
	}
}

func TestSonarrEpisodes_MissingSeriesID(t *testing.T) {
	mock := &mockSonarrClient{}
	_, handler := tools.SonarrEpisodes(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when series_id missing")
	}
}

func TestSonarrEpisodes_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, _ int64) (*sonarr.Series, error) {
			return nil, errors.New("not found")
		},
		getSeriesEpisodesFn: func(_ context.Context, _ *sonarr.GetEpisode) ([]*sonarr.Episode, error) {
			return nil, errors.New("api error")
		},
	}
	_, handler := tools.SonarrEpisodes(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(99)})
	if !r.IsError {
		t.Error("want MCP error on API failure")
	}
}

func TestSonarrQueue(t *testing.T) {
	eta, _ := time.Parse(time.RFC3339, "2026-06-26T20:00:00Z")
	mock := &mockSonarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*sonarr.Queue, error) {
			return &sonarr.Queue{
				TotalRecords: 1,
				Records: []*sonarr.QueueRecord{
					{
						ID:       9,
						Title:    "Breaking.Bad.S02E06.1080p.WEB-DL.mkv",
						Status:   "downloading",
						Protocol: "torrent",
						Size:     float64(1.2 * (1 << 30)),
						Sizeleft: float64(0.3 * (1 << 30)),
						Quality: &starr.Quality{
							Quality: &starr.BaseQuality{Name: "WEBDL-1080p"},
						},
						Indexer:        "MyIndexer",
						DownloadClient: "qbit-gluetun",
						StatusMessages: []*starr.StatusMessage{
							{Title: "warn", Messages: []string{"stalled"}},
						},
						EstimatedCompletionTime: eta,
					},
				},
			}, nil
		},
	}

	_, handler := tools.SonarrQueue(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Count   int `json:"count"`
		Records []struct {
			ID             int64    `json:"id"`
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
		} `json:"records"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 1 || len(out.Records) != 1 {
		t.Fatalf("want count=1 and 1 record, got count=%d records=%d", out.Count, len(out.Records))
	}
	rec := out.Records[0]
	if rec.ID != 9 || rec.Status != "downloading" {
		t.Errorf("id/status mismatch: %+v", rec)
	}
	if rec.Quality != "WEBDL-1080p" {
		t.Errorf("quality = %q, want WEBDL-1080p", rec.Quality)
	}
	if rec.Percent <= 0 || rec.Percent >= 100 {
		t.Errorf("percent = %v, want value between 0 and 100", rec.Percent)
	}
	if rec.ETA != "2026-06-26T20:00:00Z" {
		t.Errorf("eta = %q, want 2026-06-26T20:00:00Z", rec.ETA)
	}
	if rec.Indexer != "MyIndexer" || rec.DownloadClient != "qbit-gluetun" {
		t.Errorf("indexer/download_client mismatch: %+v", rec)
	}
	if len(rec.Warnings) != 1 || rec.Warnings[0] != "stalled" {
		t.Errorf("warnings = %v, want [stalled]", rec.Warnings)
	}
}

func TestSonarrQueue_ZeroETA(t *testing.T) {
	mock := &mockSonarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*sonarr.Queue, error) {
			return &sonarr.Queue{
				Records: []*sonarr.QueueRecord{
					{ID: 1, Title: "Some.Episode.mkv", Status: "queued"},
				},
			}, nil
		},
	}

	_, handler := tools.SonarrQueue(mock)
	r := callTool(t, handler, nil)

	var out struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal([]byte(resultText(t, r)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Records) != 1 {
		t.Fatalf("want 1 record, got %d", len(out.Records))
	}
	if _, ok := out.Records[0]["eta"]; ok {
		t.Error("eta key must be absent when EstimatedCompletionTime is zero")
	}
}

func TestSonarrQueue_Empty(t *testing.T) {
	mock := &mockSonarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*sonarr.Queue, error) {
			return &sonarr.Queue{Records: []*sonarr.QueueRecord{}}, nil
		},
	}
	_, handler := tools.SonarrQueue(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Count   int   `json:"count"`
		Records []any `json:"records"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Count != 0 || len(out.Records) != 0 {
		t.Errorf("want empty queue, got count=%d records=%d", out.Count, len(out.Records))
	}
}

func TestSonarrQueue_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getQueueFn: func(_ context.Context, _, _ int) (*sonarr.Queue, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.SonarrQueue(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error on queue fetch failure")
	}
}

func TestSonarrSearch_BySeries(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, id int64) (*sonarr.Series, error) {
			return &sonarr.Series{ID: id, Title: "Breaking Bad"}, nil
		},
		sendCommandFn: func(_ context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
			if cmd.Name != "SeriesSearch" || cmd.SeriesID != 12 {
				return nil, errors.New("unexpected command")
			}
			return &sonarr.CommandResponse{ID: 100, Status: "started"}, nil
		},
	}

	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(12)})
	body := resultText(t, r)

	var out struct {
		Series    string `json:"series"`
		Scope     string `json:"scope"`
		CommandID int64  `json:"command_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Series != "Breaking Bad" {
		t.Errorf("series = %q, want Breaking Bad", out.Series)
	}
	if out.Scope != "all monitored" {
		t.Errorf("scope = %q, want all monitored", out.Scope)
	}
	if out.CommandID != 100 || out.Status != "started" {
		t.Errorf("command_id/status mismatch: %+v", out)
	}
}

func TestSonarrSearch_BySeason(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, id int64) (*sonarr.Series, error) {
			return &sonarr.Series{ID: id, Title: "Breaking Bad"}, nil
		},
		sendCommandFn: func(_ context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
			if cmd.Name != "SeasonSearch" || cmd.SeriesID != 12 || cmd.SeasonNumber != 2 {
				return nil, errors.New("unexpected command")
			}
			return &sonarr.CommandResponse{ID: 101, Status: "started"}, nil
		},
	}

	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(12), "season": float64(2)})
	body := resultText(t, r)

	var out struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Scope != "season 2" {
		t.Errorf("scope = %q, want season 2", out.Scope)
	}
}

func TestSonarrSearch_ByEpisode(t *testing.T) {
	mock := &mockSonarrClient{
		sendCommandFn: func(_ context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
			if cmd.Name != "EpisodeSearch" || len(cmd.EpisodeIDs) != 1 || cmd.EpisodeIDs[0] != 55 {
				return nil, errors.New("unexpected command")
			}
			return &sonarr.CommandResponse{ID: 102, Status: "started"}, nil
		},
	}

	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"episode_id": float64(55)})
	body := resultText(t, r)

	var out struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Scope != "episode" {
		t.Errorf("scope = %q, want episode", out.Scope)
	}
}

func TestSonarrSearch_ByTitle(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesFn: func(_ context.Context, _ int64) ([]*sonarr.Series, error) {
			return []*sonarr.Series{
				{ID: 1, Title: "The Wire"},
				{ID: 2, Title: "Breaking Bad"},
			}, nil
		},
		sendCommandFn: func(_ context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
			if cmd.SeriesID != 2 {
				return nil, errors.New("expected series 2")
			}
			return &sonarr.CommandResponse{ID: 103, Status: "started"}, nil
		},
	}

	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"title": "breaking"})
	body := resultText(t, r)

	var out struct {
		Series string `json:"series"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Series != "Breaking Bad" {
		t.Errorf("series = %q, want Breaking Bad", out.Series)
	}
}

func TestSonarrSearch_TitleNotFound(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesFn: func(_ context.Context, _ int64) ([]*sonarr.Series, error) {
			return []*sonarr.Series{{ID: 1, Title: "The Wire"}}, nil
		},
	}
	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"title": "breaking"})
	if !r.IsError {
		t.Error("want MCP error when title matches no series")
	}
}

func TestSonarrSearch_NoParam(t *testing.T) {
	mock := &mockSonarrClient{}
	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when no params provided")
	}
}

func TestSonarrSearch_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getSeriesByIDFn: func(_ context.Context, _ int64) (*sonarr.Series, error) {
			return &sonarr.Series{ID: 1, Title: "Breaking Bad"}, nil
		},
		sendCommandFn: func(_ context.Context, _ *sonarr.CommandRequest) (*sonarr.CommandResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.SonarrSearch(mock)
	r := callTool(t, handler, map[string]any{"series_id": float64(1)})
	if !r.IsError {
		t.Error("want MCP error on command failure")
	}
}

func TestSonarrQualityProfiles(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*sonarr.QualityProfile, error) {
			return []*sonarr.QualityProfile{
				{
					ID:             6,
					Name:           "HD - 720p/1080p",
					UpgradeAllowed: true,
					Cutoff:         1002,
					Qualities: []*starr.Quality{
						{ID: 1002, Name: "WEB 1080p", Allowed: true},
					},
				},
			}, nil
		},
	}

	_, handler := tools.SonarrQualityProfiles(mock)
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
	if out[0].ID != 6 || out[0].Name != "HD - 720p/1080p" {
		t.Errorf("unexpected profile: %+v", out[0])
	}
	if !out[0].UpgradeAllowed {
		t.Error("want upgrade_allowed=true")
	}
	if out[0].Cutoff != "WEB 1080p" {
		t.Errorf("cutoff = %q, want WEB 1080p", out[0].Cutoff)
	}
}

func TestSonarrQualityProfiles_CutoffGroup(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*sonarr.QualityProfile, error) {
			return []*sonarr.QualityProfile{
				{
					ID:     3,
					Name:   "Any",
					Cutoff: 1000,
					Qualities: []*starr.Quality{
						{ID: 1000, Name: "HD Group", Items: []*starr.Quality{
							{Quality: &starr.BaseQuality{ID: 7, Name: "WEBDL-1080p"}},
						}},
					},
				},
			}, nil
		},
	}

	_, handler := tools.SonarrQualityProfiles(mock)
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

func TestSonarrQualityProfiles_Error(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfilesFn: func(_ context.Context) ([]*sonarr.QualityProfile, error) {
			return nil, errors.New("connection refused")
		},
	}
	_, handler := tools.SonarrQualityProfiles(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error on API failure")
	}
}

func TestSonarrUpdateQualityProfile(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfileFn: func(_ context.Context, id int64) (*sonarr.QualityProfile, error) {
			return &sonarr.QualityProfile{
				ID:             id,
				Name:           "HD - 720p/1080p",
				UpgradeAllowed: true,
				Cutoff:         1002,
				Qualities: []*starr.Quality{
					{ID: 1002, Name: "WEB 1080p", Allowed: true},
				},
			}, nil
		},
		updateQualityProfileFn: func(_ context.Context, p *sonarr.QualityProfile) (*sonarr.QualityProfile, error) {
			return p, nil
		},
	}

	_, handler := tools.SonarrUpdateQualityProfile(mock)
	r := callTool(t, handler, map[string]any{"id": float64(6), "name": "My HD Profile"})
	body := resultText(t, r)

	var out struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != 6 || out.Name != "My HD Profile" {
		t.Errorf("unexpected result: %+v", out)
	}
}

func TestSonarrUpdateQualityProfile_UpgradeAllowed(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfileFn: func(_ context.Context, id int64) (*sonarr.QualityProfile, error) {
			return &sonarr.QualityProfile{ID: id, Name: "HD", UpgradeAllowed: true}, nil
		},
		updateQualityProfileFn: func(_ context.Context, p *sonarr.QualityProfile) (*sonarr.QualityProfile, error) {
			return p, nil
		},
	}

	_, handler := tools.SonarrUpdateQualityProfile(mock)
	r := callTool(t, handler, map[string]any{"id": float64(6), "upgrade_allowed": false})
	body := resultText(t, r)

	var out struct {
		UpgradeAllowed bool `json:"upgrade_allowed"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.UpgradeAllowed {
		t.Error("want upgrade_allowed=false after update")
	}
}

func TestSonarrUpdateQualityProfile_MissingID(t *testing.T) {
	mock := &mockSonarrClient{}
	_, handler := tools.SonarrUpdateQualityProfile(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when id missing")
	}
}

func TestSonarrUpdateQualityProfile_GetError(t *testing.T) {
	mock := &mockSonarrClient{
		getQualityProfileFn: func(_ context.Context, _ int64) (*sonarr.QualityProfile, error) {
			return nil, errors.New("not found")
		},
	}
	_, handler := tools.SonarrUpdateQualityProfile(mock)
	r := callTool(t, handler, map[string]any{"id": float64(99)})
	if !r.IsError {
		t.Error("want MCP error on get failure")
	}
}

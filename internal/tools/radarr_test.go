package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

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
	getCustomFormatsFn     func(ctx context.Context) ([]*radarr.CustomFormatOutput, error)
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
func (m *mockRadarrClient) GetCustomFormatsContext(ctx context.Context) ([]*radarr.CustomFormatOutput, error) {
	return m.getCustomFormatsFn(ctx)
}
func (m *mockRadarrClient) SendCommandContext(ctx context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error) {
	return m.sendCommandFn(ctx, cmd)
}

func TestRadarrHealth(t *testing.T) {
	mock := &mockRadarrClient{
		getIntoFn: func(_ context.Context, req starr.Request, output any) error {
			if req.URI == "/api/v3/health" {
				return json.Unmarshal([]byte(`[{"source":"Test","type":"warning","message":"update available"}]`), output)
			}
			return errors.New("unexpected URI: " + req.URI)
		},
		getSystemStatusFn: func(_ context.Context) (*radarr.SystemStatus, error) {
			return &radarr.SystemStatus{Version: "5.14.0"}, nil
		},
		getMovieFn: func(_ context.Context, _ *radarr.GetMovie) ([]*radarr.Movie, error) {
			return make([]*radarr.Movie, 139), nil
		},
	}

	_, handler := tools.RadarrHealth(mock)
	r := callTool(t, handler, nil)
	body := resultText(t, r)

	var out struct {
		Version    string `json:"version"`
		MovieCount int    `json:"movie_count"`
		Issues     []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Version != "5.14.0" {
		t.Errorf("version = %q, want 5.14.0", out.Version)
	}
	if out.MovieCount != 139 {
		t.Errorf("movie_count = %d, want 139", out.MovieCount)
	}
	if len(out.Issues) != 1 || out.Issues[0].Type != "warning" {
		t.Errorf("unexpected issues: %+v", out.Issues)
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

func TestRadarrHealth_Error(t *testing.T) {
	mock := &mockRadarrClient{
		getIntoFn: func(_ context.Context, _ starr.Request, _ any) error {
			return errors.New("connection refused")
		},
		getSystemStatusFn: func(_ context.Context) (*radarr.SystemStatus, error) {
			return &radarr.SystemStatus{}, nil
		},
		getMovieFn: func(_ context.Context, _ *radarr.GetMovie) ([]*radarr.Movie, error) {
			return nil, nil
		},
	}

	_, handler := tools.RadarrHealth(mock)
	r := callTool(t, handler, nil)
	if !r.IsError {
		t.Error("want MCP error when health endpoint fails")
	}
}

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

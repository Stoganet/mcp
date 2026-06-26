package tools

import (
	"context"
	"encoding/json"
	"sync"

	"golift.io/starr"
	"golift.io/starr/radarr"

	"github.com/mark3labs/mcp-go/mcp"
)

type RadarrClient interface {
	GetSystemStatusContext(ctx context.Context) (*radarr.SystemStatus, error)
	GetMovieContext(ctx context.Context, getMovie *radarr.GetMovie) ([]*radarr.Movie, error)
	GetMovieByIDContext(ctx context.Context, movieID int64) (*radarr.Movie, error)
	GetQueueContext(ctx context.Context, records, perPage int) (*radarr.Queue, error)
	GetHistoryPageContext(ctx context.Context, params *starr.PageReq) (*radarr.History, error)
	GetQualityProfilesContext(ctx context.Context) ([]*radarr.QualityProfile, error)
	GetQualityProfileContext(ctx context.Context, profileID int64) (*radarr.QualityProfile, error)
	UpdateQualityProfileContext(ctx context.Context, profile *radarr.QualityProfile) (*radarr.QualityProfile, error)
	GetCustomFormatsContext(ctx context.Context) ([]*radarr.CustomFormatOutput, error)
	SendCommandContext(ctx context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error)
	GetInto(ctx context.Context, req starr.Request, output any) error
}

func NewRadarrClient(url, apiKey string) RadarrClient {
	return radarr.New(starr.New(apiKey, url, 0))
}

type healthMessage struct {
	Source  string `json:"source"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

func RadarrHealth(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_health",
		mcp.WithDescription("Radarr health check: version, health warnings, and movie count"),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var (
			health []healthMessage
			status *radarr.SystemStatus
			movies []*radarr.Movie
			errs   [3]error
			wg     sync.WaitGroup
		)

		wg.Add(3)
		go func() {
			defer wg.Done()
			errs[0] = rc.GetInto(ctx, starr.Request{URI: "/api/v3/health"}, &health)
		}()
		go func() {
			defer wg.Done()
			status, errs[1] = rc.GetSystemStatusContext(ctx)
		}()
		go func() {
			defer wg.Done()
			movies, errs[2] = rc.GetMovieContext(ctx, &radarr.GetMovie{})
		}()
		wg.Wait()

		for _, err := range errs {
			if err != nil {
				return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
			}
		}

		out := struct {
			Version    string          `json:"version"`
			Issues     []healthMessage `json:"issues"`
			MovieCount int             `json:"movie_count"`
		}{
			Version:    status.Version,
			Issues:     health,
			MovieCount: len(movies),
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

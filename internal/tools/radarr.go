package tools

import (
	"context"
	"encoding/json"
	"strings"
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

type movieFileOut struct {
	RelativePath string  `json:"relative_path"`
	Quality      string  `json:"quality"`
	SizeGB       float64 `json:"size_gb"`
	ReleaseGroup string  `json:"release_group,omitempty"`
}

type movieOut struct {
	ID               int64         `json:"id"`
	Title            string        `json:"title"`
	Year             int           `json:"year"`
	TmdbID           int64         `json:"tmdb_id"`
	Status           string        `json:"status"`
	Monitored        bool          `json:"monitored"`
	HasFile          bool          `json:"has_file"`
	IsAvailable      bool          `json:"is_available"`
	QualityProfileID int64         `json:"quality_profile_id"`
	File             *movieFileOut `json:"file,omitempty"`
}

func movieMatchesTitle(m *radarr.Movie, lower string) bool {
	if strings.Contains(strings.ToLower(m.Title), lower) {
		return true
	}
	if strings.Contains(strings.ToLower(m.OriginalTitle), lower) {
		return true
	}
	for _, alt := range m.AlternateTitles {
		if strings.Contains(strings.ToLower(alt.Title), lower) {
			return true
		}
	}
	return false
}

func toMovieOut(m *radarr.Movie) movieOut {
	out := movieOut{
		ID:               m.ID,
		Title:            m.Title,
		Year:             m.Year,
		TmdbID:           m.TmdbID,
		Status:           m.Status,
		Monitored:        m.Monitored,
		HasFile:          m.HasFile,
		IsAvailable:      m.IsAvailable,
		QualityProfileID: m.QualityProfileID,
	}
	if m.MovieFile != nil {
		var qualName string
		if m.MovieFile.Quality != nil && m.MovieFile.Quality.Quality != nil {
			qualName = m.MovieFile.Quality.Quality.Name
		}
		const gb = 1 << 30
		out.File = &movieFileOut{
			RelativePath: m.MovieFile.RelativePath,
			Quality:      qualName,
			SizeGB:       round2(float64(m.MovieFile.Size) / gb),
			ReleaseGroup: m.MovieFile.ReleaseGroup,
		}
	}
	return out
}

func RadarrMovie(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_movie",
		mcp.WithDescription("Look up a movie in Radarr by Radarr ID, TMDB ID, or title (partial match). Returns file info, quality, and availability."),
		mcp.WithNumber("id", mcp.Description("Radarr movie ID")),
		mcp.WithNumber("tmdb_id", mcp.Description("TMDB movie ID")),
		mcp.WithString("title", mcp.Description("Movie title (case-insensitive partial match, also searches original and alternate titles)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		radarrID := mcp.ParseFloat64(req, "id", 0)
		tmdbID := mcp.ParseFloat64(req, "tmdb_id", 0)
		title := mcp.ParseString(req, "title", "")

		hasID := radarrID != 0
		hasTMDB := tmdbID != 0
		hasTitle := title != ""

		if !hasID && !hasTMDB && !hasTitle {
			return mcp.NewToolResultError("provide at least one of: id, tmdb_id, title"), nil //nolint:nilerr
		}

		var movies []*radarr.Movie
		var err error

		switch {
		case hasID:
			m, e := rc.GetMovieByIDContext(ctx, int64(radarrID))
			if e != nil {
				return mcp.NewToolResultError("radarr: " + e.Error()), nil //nolint:nilerr
			}
			movies = []*radarr.Movie{m}
		case hasTMDB:
			movies, err = rc.GetMovieContext(ctx, &radarr.GetMovie{TMDBID: int64(tmdbID)})
			if err != nil {
				return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
			}
		default:
			all, e := rc.GetMovieContext(ctx, &radarr.GetMovie{})
			if e != nil {
				return mcp.NewToolResultError("radarr: " + e.Error()), nil //nolint:nilerr
			}
			lower := strings.ToLower(title)
			for _, m := range all {
				if movieMatchesTitle(m, lower) {
					movies = append(movies, m)
				}
			}
		}

		out := make([]movieOut, 0, len(movies))
		for _, m := range movies {
			out = append(out, toMovieOut(m))
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
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

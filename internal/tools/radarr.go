package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

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
	SendCommandContext(ctx context.Context, cmd *radarr.CommandRequest) (*radarr.CommandResponse, error)
	GetInto(ctx context.Context, req starr.Request, output any) error
}

func NewRadarrClient(url, apiKey string) RadarrClient {
	return radarr.New(starr.New(apiKey, url, 0))
}

type movieFileOut struct {
	RelativePath string  `json:"relative_path"`
	Quality      string  `json:"quality,omitempty"`
	SizeGB       float64 `json:"size_gb"`
	ReleaseGroup string  `json:"release_group,omitempty"`
}

type movieOut struct {
	ID               int64         `json:"id"`
	Title            string        `json:"title"`
	Year             int           `json:"year"`
	TmdbID           int64         `json:"tmdb_id,omitempty"`
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

type queueRecordOut struct {
	ID       int64   `json:"id"`
	MovieID  int64   `json:"movie_id,omitempty"`
	Title    string  `json:"title"`
	Status   string  `json:"status"`
	Protocol string  `json:"protocol"`
	ETA      string  `json:"eta,omitempty"`
	SizeGB   float64 `json:"size_gb"`
	LeftGB   float64 `json:"left_gb"`
}

func RadarrQueue(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_queue",
		mcp.WithDescription("List Radarr download queue. Returns all items currently downloading or pending import."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		queue, err := rc.GetQueueContext(ctx, 0, 0)
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		const gb = 1 << 30
		out := make([]queueRecordOut, 0, len(queue.Records))
		for _, r := range queue.Records {
			rec := queueRecordOut{
				ID:       r.ID,
				MovieID:  r.MovieID,
				Title:    r.Title,
				Status:   r.Status,
				Protocol: string(r.Protocol),
				SizeGB:   round2(r.Size / gb),
				LeftGB:   round2(r.Sizeleft / gb),
			}
			if !r.EstimatedCompletionTime.IsZero() {
				rec.ETA = r.EstimatedCompletionTime.UTC().Format(time.RFC3339)
			}
			out = append(out, rec)
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func RadarrSearch(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_search",
		mcp.WithDescription("Trigger a movie search in Radarr by Radarr movie ID."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Radarr movie ID")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(mcp.ParseFloat64(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil //nolint:nilerr
		}

		resp, err := rc.SendCommandContext(ctx, &radarr.CommandRequest{
			Name:     "MoviesSearch",
			MovieIDs: []int64{id},
		})
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		out := struct {
			CommandID int64  `json:"command_id"`
			Status    string `json:"status"`
		}{
			CommandID: resp.ID,
			Status:    resp.Status,
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type historyRecordOut struct {
	ID        int64  `json:"id"`
	MovieID   int64  `json:"movie_id,omitempty"`
	Title     string `json:"title"`
	EventType string `json:"event_type"`
	Date      string `json:"date"`
	Quality   string `json:"quality,omitempty"`
	Indexer   string `json:"indexer,omitempty"`
}

func RadarrHistory(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_history",
		mcp.WithDescription("Recent Radarr history (grabs, imports, failures). Optionally filter by movie ID. Returns up to 25 most recent events."),
		mcp.WithNumber("movie_id", mcp.Description("Radarr movie ID to filter history (optional)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		movieID := int64(mcp.ParseFloat64(req, "movie_id", 0))

		pageReq := &starr.PageReq{
			PageSize: 25,
			Page:     1,
			SortKey:  "date",
			SortDir:  starr.SortDescend,
		}
		if movieID != 0 {
			pageReq.Set("movieId", starr.Str(movieID))
		}

		hist, err := rc.GetHistoryPageContext(ctx, pageReq)
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		out := make([]historyRecordOut, 0, len(hist.Records))
		for _, r := range hist.Records {
			rec := historyRecordOut{
				ID:        r.ID,
				MovieID:   r.MovieID,
				Title:     r.SourceTitle,
				EventType: r.EventType,
				Date:      r.Date.UTC().Format(time.RFC3339),
				Indexer:   r.Data.Indexer,
			}
			if r.Quality != nil && r.Quality.Quality != nil {
				rec.Quality = r.Quality.Quality.Name
			}
			out = append(out, rec)
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type qualityProfileOut struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	UpgradeAllowed bool   `json:"upgrade_allowed"`
	Cutoff         string `json:"cutoff,omitempty"`
}

func resolveCutoff(cutoff int64, items []*starr.Quality) string {
	for _, q := range items {
		if q.Quality != nil && q.Quality.ID == cutoff {
			return q.Quality.Name
		}
		if int64(q.ID) == cutoff && q.Name != "" {
			return q.Name
		}
	}
	return ""
}

func RadarrQualityProfiles(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_quality_profiles",
		mcp.WithDescription("List all Radarr quality profiles."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		profiles, err := rc.GetQualityProfilesContext(ctx)
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		out := make([]qualityProfileOut, 0, len(profiles))
		for _, p := range profiles {
			out = append(out, qualityProfileOut{
				ID:             p.ID,
				Name:           p.Name,
				UpgradeAllowed: p.UpgradeAllowed,
				Cutoff:         resolveCutoff(p.Cutoff, p.Qualities),
			})
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func RadarrUpdateQualityProfile(rc RadarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("radarr_update_quality_profile",
		mcp.WithDescription("Update a Radarr quality profile. Fetches the existing profile and patches the provided fields."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Quality profile ID")),
		mcp.WithString("name", mcp.Description("New profile name")),
		mcp.WithBoolean("upgrade_allowed", mcp.Description("Whether quality upgrades are allowed")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(mcp.ParseFloat64(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil //nolint:nilerr
		}

		profile, err := rc.GetQualityProfileContext(ctx, id)
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		if name := mcp.ParseString(req, "name", ""); name != "" {
			profile.Name = name
		}
		if _, ok := req.GetArguments()["upgrade_allowed"]; ok {
			profile.UpgradeAllowed = mcp.ParseBoolean(req, "upgrade_allowed", profile.UpgradeAllowed)
		}

		updated, err := rc.UpdateQualityProfileContext(ctx, profile)
		if err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		out := qualityProfileOut{
			ID:             updated.ID,
			Name:           updated.Name,
			UpgradeAllowed: updated.UpgradeAllowed,
			Cutoff:         resolveCutoff(updated.Cutoff, updated.Qualities),
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
		mcp.WithDescription("Radarr health check: version and health warnings"),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var (
			health [2]error
			status *radarr.SystemStatus
			msgs   []healthMessage
			wg     sync.WaitGroup
		)

		wg.Add(2)
		go func() {
			defer wg.Done()
			health[0] = rc.GetInto(ctx, starr.Request{URI: "/api/v3/health"}, &msgs)
		}()
		go func() {
			defer wg.Done()
			status, health[1] = rc.GetSystemStatusContext(ctx)
		}()
		wg.Wait()

		if err := errors.Join(health[0], health[1]); err != nil {
			return mcp.NewToolResultError("radarr: " + err.Error()), nil //nolint:nilerr
		}

		out := struct {
			Version string          `json:"version"`
			Issues  []healthMessage `json:"issues"`
		}{
			Version: status.Version,
			Issues:  msgs,
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

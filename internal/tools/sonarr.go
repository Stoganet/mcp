package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"golift.io/starr"
	"golift.io/starr/sonarr"

	"github.com/mark3labs/mcp-go/mcp"
)

type SonarrClient interface {
	GetSystemStatusContext(ctx context.Context) (*sonarr.SystemStatus, error)
	GetSeriesContext(ctx context.Context, tvdbID int64) ([]*sonarr.Series, error)
	GetSeriesByIDContext(ctx context.Context, seriesID int64) (*sonarr.Series, error)
	GetSeriesEpisodesContext(ctx context.Context, getEpisode *sonarr.GetEpisode) ([]*sonarr.Episode, error)
	GetQueueContext(ctx context.Context, records, perPage int) (*sonarr.Queue, error)
	GetQualityProfilesContext(ctx context.Context) ([]*sonarr.QualityProfile, error)
	GetQualityProfileContext(ctx context.Context, profileID int64) (*sonarr.QualityProfile, error)
	UpdateQualityProfileContext(ctx context.Context, profile *sonarr.QualityProfile) (*sonarr.QualityProfile, error)
	SendCommandContext(ctx context.Context, cmd *sonarr.CommandRequest) (*sonarr.CommandResponse, error)
	GetInto(ctx context.Context, req starr.Request, output any) error
}

func NewSonarrClient(url, apiKey string) SonarrClient {
	return sonarr.New(starr.New(apiKey, url, 0))
}

func SonarrHealth(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_health",
		mcp.WithDescription("Sonarr health check: version, health warnings, and disk space"),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var (
			errs   [3]error
			status *sonarr.SystemStatus
			msgs   []healthMessage
			raw    []arrDiskSpace
			wg     sync.WaitGroup
		)

		wg.Add(3)
		go func() {
			defer wg.Done()
			errs[0] = sc.GetInto(ctx, starr.Request{URI: "/api/v3/health"}, &msgs)
		}()
		go func() {
			defer wg.Done()
			status, errs[1] = sc.GetSystemStatusContext(ctx)
		}()
		go func() {
			defer wg.Done()
			errs[2] = sc.GetInto(ctx, starr.Request{URI: "/api/v3/diskspace"}, &raw)
		}()
		wg.Wait()

		if err := errors.Join(errs[0], errs[1], errs[2]); err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
		}

		const gb = 1 << 30
		disk := make([]diskSpaceEntry, 0, len(raw))
		for _, d := range raw {
			disk = append(disk, diskSpaceEntry{
				Path:    d.Path,
				FreeGB:  round2(float64(d.FreeSpace) / gb),
				TotalGB: round2(float64(d.TotalSpace) / gb),
			})
		}

		out := struct {
			Version string           `json:"version"`
			Issues  []healthMessage  `json:"issues"`
			Disk    []diskSpaceEntry `json:"disk_space"`
		}{
			Version: status.Version,
			Issues:  msgs,
			Disk:    disk,
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type seasonOut struct {
	SeasonNumber     int     `json:"season_number"`
	Monitored        bool    `json:"monitored"`
	EpisodeCount     int     `json:"episode_count"`
	EpisodeFileCount int     `json:"episode_file_count"`
	PercentComplete  float64 `json:"percent_complete"`
}

type seriesOut struct {
	ID               int64       `json:"id"`
	Title            string      `json:"title"`
	Year             int         `json:"year,omitempty"`
	TvdbID           int64       `json:"tvdb_id,omitempty"`
	Status           string      `json:"status,omitempty"`
	Monitored        bool        `json:"monitored"`
	QualityProfileID int64       `json:"quality_profile_id"`
	Seasons          []seasonOut `json:"seasons"`
	TotalEpisodes    int         `json:"total_episodes"`
	TotalFiles       int         `json:"total_episode_files"`
	PercentComplete  float64     `json:"percent_complete"`
}

func toSeriesOut(s *sonarr.Series) seriesOut {
	seasons := make([]seasonOut, 0, len(s.Seasons))
	var totalEps, totalFiles int
	for _, season := range s.Seasons {
		so := seasonOut{
			SeasonNumber: season.SeasonNumber,
			Monitored:    season.Monitored,
		}
		if season.Statistics != nil {
			so.EpisodeCount = season.Statistics.EpisodeCount
			so.EpisodeFileCount = season.Statistics.EpisodeFileCount
			so.PercentComplete = round2(season.Statistics.PercentOfEpisodes)
			totalEps += season.Statistics.EpisodeCount
			totalFiles += season.Statistics.EpisodeFileCount
		}
		seasons = append(seasons, so)
	}
	var pct float64
	if totalEps > 0 {
		pct = round2(float64(totalFiles) / float64(totalEps) * 100)
	}
	return seriesOut{
		ID:               s.ID,
		Title:            s.Title,
		Year:             s.Year,
		TvdbID:           s.TvdbID,
		Status:           s.Status,
		Monitored:        s.Monitored,
		QualityProfileID: s.QualityProfileID,
		Seasons:          seasons,
		TotalEpisodes:    totalEps,
		TotalFiles:       totalFiles,
		PercentComplete:  pct,
	}
}

func seriesMatchesTitle(s *sonarr.Series, lower string) bool {
	if strings.Contains(strings.ToLower(s.Title), lower) {
		return true
	}
	for _, alt := range s.AlternateTitles {
		if strings.Contains(strings.ToLower(alt.Title), lower) {
			return true
		}
	}
	return false
}

func SonarrSeries(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_series",
		mcp.WithDescription("Look up a series in Sonarr by Sonarr ID, TVDB ID, or title (partial match). Returns season breakdown with episode file counts."),
		mcp.WithNumber("id", mcp.Description("Sonarr series ID")),
		mcp.WithNumber("tvdb_id", mcp.Description("TVDB series ID")),
		mcp.WithString("title", mcp.Description("Series title (case-insensitive partial match)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sonarrID := mcp.ParseFloat64(req, "id", 0)
		tvdbID := mcp.ParseFloat64(req, "tvdb_id", 0)
		title := mcp.ParseString(req, "title", "")

		if sonarrID == 0 && tvdbID == 0 && title == "" {
			return mcp.NewToolResultError("provide at least one of: id, tvdb_id, title"), nil //nolint:nilerr
		}

		var series []*sonarr.Series
		var err error

		switch {
		case sonarrID != 0:
			s, e := sc.GetSeriesByIDContext(ctx, int64(sonarrID))
			if e != nil {
				return mcp.NewToolResultError("sonarr: " + e.Error()), nil //nolint:nilerr
			}
			series = []*sonarr.Series{s}
		case tvdbID != 0:
			series, err = sc.GetSeriesContext(ctx, int64(tvdbID))
			if err != nil {
				return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
			}
		default:
			all, e := sc.GetSeriesContext(ctx, 0)
			if e != nil {
				return mcp.NewToolResultError("sonarr: " + e.Error()), nil //nolint:nilerr
			}
			lower := strings.ToLower(title)
			for _, s := range all {
				if seriesMatchesTitle(s, lower) {
					series = append(series, s)
				}
			}
		}

		out := make([]seriesOut, 0, len(series))
		for _, s := range series {
			out = append(out, toSeriesOut(s))
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type episodeOut struct {
	ID        int64  `json:"id"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title"`
	AirDate   string `json:"air_date,omitempty"`
	HasFile   bool   `json:"has_file"`
	Monitored bool   `json:"monitored"`
}

func SonarrEpisodes(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_episodes",
		mcp.WithDescription("List episodes for a Sonarr series. Optionally filter by season number or show only missing episodes."),
		mcp.WithNumber("series_id", mcp.Required(), mcp.Description("Sonarr series ID")),
		mcp.WithNumber("season", mcp.Description("Filter to this season number")),
		mcp.WithBoolean("missing_only", mcp.Description("Only return episodes without files")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seriesID := int64(mcp.ParseFloat64(req, "series_id", 0))
		if seriesID == 0 {
			return mcp.NewToolResultError("series_id is required"), nil //nolint:nilerr
		}
		seasonNum := int(mcp.ParseFloat64(req, "season", 0))
		missingOnly := mcp.ParseBoolean(req, "missing_only", false)

		var (
			seriesTitle string
			episodes    []*sonarr.Episode
			errs        [2]error
			wg          sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			s, e := sc.GetSeriesByIDContext(ctx, seriesID)
			if e != nil {
				errs[0] = e
				return
			}
			seriesTitle = s.Title
		}()
		go func() {
			defer wg.Done()
			episodes, errs[1] = sc.GetSeriesEpisodesContext(ctx, &sonarr.GetEpisode{
				SeriesID:     seriesID,
				SeasonNumber: seasonNum,
			})
		}()
		wg.Wait()

		if err := errors.Join(errs[0], errs[1]); err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
		}

		out := make([]episodeOut, 0, len(episodes))
		for _, ep := range episodes {
			if missingOnly && ep.HasFile {
				continue
			}
			out = append(out, episodeOut{
				ID:        ep.ID,
				Season:    ep.SeasonNumber,
				Episode:   ep.EpisodeNumber,
				Title:     ep.Title,
				AirDate:   ep.AirDate,
				HasFile:   ep.HasFile,
				Monitored: ep.Monitored,
			})
		}

		result := struct {
			Series   string       `json:"series"`
			Episodes []episodeOut `json:"episodes"`
		}{
			Series:   seriesTitle,
			Episodes: out,
		}

		b, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

type sonarrQueueRecordOut struct {
	ID             int64    `json:"id"`
	Title          string   `json:"title"`
	Status         string   `json:"status"`
	Protocol       string   `json:"protocol"`
	Quality        string   `json:"quality,omitempty"`
	SizeGB         float64  `json:"size_gb"`
	RemainingGB    float64  `json:"remaining_gb"`
	Percent        float64  `json:"percent,omitempty"`
	ETA            string   `json:"eta,omitempty"`
	Indexer        string   `json:"indexer,omitempty"`
	DownloadClient string   `json:"download_client,omitempty"`
	Warnings       []string `json:"warnings"`
}

func SonarrQueue(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_queue",
		mcp.WithDescription("List Sonarr download queue. Returns all items currently downloading or pending import."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		queue, err := sc.GetQueueContext(ctx, 0, 0)
		if err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
		}

		const gb = 1 << 30
		records := make([]sonarrQueueRecordOut, 0, len(queue.Records))
		for _, r := range queue.Records {
			rec := sonarrQueueRecordOut{
				ID:             r.ID,
				Title:          r.Title,
				Status:         r.Status,
				Protocol:       string(r.Protocol),
				SizeGB:         round2(r.Size / gb),
				RemainingGB:    round2(r.Sizeleft / gb),
				Indexer:        r.Indexer,
				DownloadClient: r.DownloadClient,
				Warnings:       []string{},
			}
			if r.Quality != nil && r.Quality.Quality != nil {
				rec.Quality = r.Quality.Quality.Name
			}
			if r.Size > 0 {
				rec.Percent = round2((r.Size - r.Sizeleft) / r.Size * 100)
			}
			if !r.EstimatedCompletionTime.IsZero() {
				rec.ETA = r.EstimatedCompletionTime.UTC().Format(time.RFC3339)
			}
			for _, sm := range r.StatusMessages {
				rec.Warnings = append(rec.Warnings, sm.Messages...)
			}
			records = append(records, rec)
		}

		out := struct {
			Count   int                    `json:"count"`
			Records []sonarrQueueRecordOut `json:"records"`
		}{
			Count:   len(records),
			Records: records,
		}

		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError("marshal error"), nil //nolint:nilerr
		}
		return mcp.NewToolResultText(string(b)), nil
	}
	return tool, handler
}

func SonarrSearch(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_search",
		mcp.WithDescription("Trigger a search in Sonarr. Searches at series, season, or episode level. Requires series_id or title. Add season to limit to a season, or episode_id for a single episode."),
		mcp.WithNumber("series_id", mcp.Description("Sonarr series ID")),
		mcp.WithString("title", mcp.Description("Series title (resolved to ID if series_id not provided)")),
		mcp.WithNumber("season", mcp.Description("Season number (triggers season-level search)")),
		mcp.WithNumber("episode_id", mcp.Description("Episode ID (triggers episode-level search)")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seriesID := int64(mcp.ParseFloat64(req, "series_id", 0))
		title := mcp.ParseString(req, "title", "")
		seasonNum := int(mcp.ParseFloat64(req, "season", 0))
		episodeID := int64(mcp.ParseFloat64(req, "episode_id", 0))

		if seriesID == 0 && title == "" && episodeID == 0 {
			return mcp.NewToolResultError("provide series_id, title, or episode_id"), nil //nolint:nilerr
		}

		var seriesTitle string

		if episodeID == 0 {
			if seriesID == 0 {
				all, e := sc.GetSeriesContext(ctx, 0)
				if e != nil {
					return mcp.NewToolResultError("sonarr: " + e.Error()), nil //nolint:nilerr
				}
				lower := strings.ToLower(title)
				for _, s := range all {
					if seriesMatchesTitle(s, lower) {
						seriesID = s.ID
						seriesTitle = s.Title
						break
					}
				}
				if seriesID == 0 {
					return mcp.NewToolResultError("no series found matching title: " + title), nil //nolint:nilerr
				}
			} else {
				s, e := sc.GetSeriesByIDContext(ctx, seriesID)
				if e != nil {
					return mcp.NewToolResultError("sonarr: " + e.Error()), nil //nolint:nilerr
				}
				seriesTitle = s.Title
			}
		}

		var cmd *sonarr.CommandRequest
		var scope string

		switch {
		case episodeID != 0:
			cmd = &sonarr.CommandRequest{Name: "EpisodeSearch", EpisodeIDs: []int64{episodeID}}
			scope = "episode"
		case seasonNum != 0:
			cmd = &sonarr.CommandRequest{Name: "SeasonSearch", SeriesID: seriesID, SeasonNumber: seasonNum}
			scope = "season " + starr.Str(int64(seasonNum))
		default:
			cmd = &sonarr.CommandRequest{Name: "SeriesSearch", SeriesID: seriesID}
			scope = "all monitored"
		}

		resp, err := sc.SendCommandContext(ctx, cmd)
		if err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
		}

		out := struct {
			Series    string `json:"series,omitempty"`
			Scope     string `json:"scope"`
			CommandID int64  `json:"command_id"`
			Status    string `json:"status"`
		}{
			Series:    seriesTitle,
			Scope:     scope,
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

func SonarrQualityProfiles(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_quality_profiles",
		mcp.WithDescription("List all Sonarr quality profiles."),
	)
	handler := func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		profiles, err := sc.GetQualityProfilesContext(ctx)
		if err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
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

func SonarrUpdateQualityProfile(sc SonarrClient) (mcp.Tool, func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("sonarr_update_quality_profile",
		mcp.WithDescription("Update a Sonarr quality profile. Fetches the existing profile and patches the provided fields."),
		mcp.WithNumber("id", mcp.Required(), mcp.Description("Quality profile ID")),
		mcp.WithString("name", mcp.Description("New profile name")),
		mcp.WithBoolean("upgrade_allowed", mcp.Description("Whether quality upgrades are allowed")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(mcp.ParseFloat64(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil //nolint:nilerr
		}

		profile, err := sc.GetQualityProfileContext(ctx, id)
		if err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
		}

		if name := mcp.ParseString(req, "name", ""); name != "" {
			profile.Name = name
		}
		if _, ok := req.GetArguments()["upgrade_allowed"]; ok {
			profile.UpgradeAllowed = mcp.ParseBoolean(req, "upgrade_allowed", profile.UpgradeAllowed)
		}

		updated, err := sc.UpdateQualityProfileContext(ctx, profile)
		if err != nil {
			return mcp.NewToolResultError("sonarr: " + err.Error()), nil //nolint:nilerr
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

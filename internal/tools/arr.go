package tools

import "golift.io/starr"

type healthMessage struct {
	Source  string `json:"source"`
	Type    string `json:"type"`
	Message string `json:"message"`
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

type diskSpaceEntry struct {
	Path    string  `json:"path"`
	FreeGB  float64 `json:"free_gb"`
	TotalGB float64 `json:"total_gb"`
}

type arrDiskSpace struct {
	Path       string `json:"path"`
	FreeSpace  int64  `json:"freeSpace"`
	TotalSpace int64  `json:"totalSpace"`
}

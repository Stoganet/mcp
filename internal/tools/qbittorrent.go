package tools

import (
	"context"
	"math"

	qbit "github.com/autobrr/go-qbittorrent"
)

type QBitClient interface {
	GetTorrentsCtx(ctx context.Context, opts qbit.TorrentFilterOptions) ([]qbit.Torrent, error)
	GetTorrentPropertiesCtx(ctx context.Context, hash string) (qbit.TorrentProperties, error)
	GetTorrentTrackersCtx(ctx context.Context, hash string) ([]qbit.TorrentTracker, error)
	GetFilesInformationCtx(ctx context.Context, hash string) (*qbit.TorrentFiles, error)
	StopCtx(ctx context.Context, hashes []string) error
	StartCtx(ctx context.Context, hashes []string) error
	DeleteTorrentsCtx(ctx context.Context, hashes []string, deleteFiles bool) error
	GetTransferInfoCtx(ctx context.Context) (*qbit.TransferInfo, error)
	GetAppPreferencesCtx(ctx context.Context) (qbit.AppPreferences, error)
	SetPreferencesCtx(ctx context.Context, prefs map[string]interface{}) error
}

func NewQBitClient(host, username, password string) QBitClient {
	return qbit.NewClient(qbit.Config{
		Host:     host,
		Username: username,
		Password: password,
	})
}

func mapTorrentState(raw qbit.TorrentState) string {
	switch raw {
	case qbit.TorrentStateDownloading, qbit.TorrentStateForcedDl, qbit.TorrentStateMetaDl:
		return "downloading"
	case qbit.TorrentStateUploading, qbit.TorrentStateForcedUp:
		return "seeding"
	case qbit.TorrentStateStoppedDl, qbit.TorrentStateStoppedUp,
		qbit.TorrentStatePausedDl, qbit.TorrentStatePausedUp:
		return "stopped"
	case qbit.TorrentStateStalledDl, qbit.TorrentStateStalledUp:
		return "stalled"
	case qbit.TorrentStateError, qbit.TorrentStateMissingFiles:
		return "errored"
	case qbit.TorrentStateQueuedDl, qbit.TorrentStateQueuedUp:
		return "queued"
	case qbit.TorrentStateCheckingDl, qbit.TorrentStateCheckingUp, qbit.TorrentStateCheckingResumeData:
		return "checking"
	case qbit.TorrentStateMoving:
		return "moving"
	default:
		return string(raw)
	}
}

func bytesToMB(b int64) float64 {
	return math.Round(float64(b)/1024/1024*100) / 100
}

func bytesToKBs(b int64) float64 {
	return math.Round(float64(b)/1024*100) / 100
}

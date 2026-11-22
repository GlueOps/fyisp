package store

import (
	"context"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/store/blob"
)

// HourSummary is one series' UTC hour: its samples, losses and round trips
// (ms; zero when N is 0). P50 is the lower median, as P95 rounds up.
type HourSummary struct {
	Key                                model.SeriesKey
	Hour                               int64 // unix hours
	N, Lost                            int64
	MinMs, P50Ms, P95Ms, MaxMs, MeanMs float64
}

// HourSummaries calls fn with the hourly summary of every key and hour that
// starts in [from, to], stored or still in memory (the current hour is
// partial), in no particular order.
func (s *SQLite) HourSummaries(ctx context.Context, keys []model.SeriesKey, from, to time.Time, fn func(HourSummary)) error {
	const ms = float64(blob.Unit) / 1e6
	return s.hourlySummaries(ctx, keys, from, to, func(k model.SeriesKey, hour int64, u *summary) {
		h := HourSummary{Key: k, Hour: hour, N: u.n, Lost: u.lost}
		if u.n > 0 {
			h.MinMs, h.P50Ms, h.P95Ms, h.MaxMs = float64(u.min)*ms, float64(u.p50)*ms, float64(u.p95)*ms, float64(u.max)*ms
			h.MeanMs = float64(u.sum) / float64(u.n) * ms
		}
		fn(h)
	})
}

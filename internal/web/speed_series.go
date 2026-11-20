package web

import (
	"math"
	"slices"
	"sort"
	"time"

	"github.com/glueops/fyisp/internal/callq"
	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/speedtest"
	"github.com/glueops/fyisp/internal/store"
)

// Speed tests over time (api/speed?part=series and part=tests): the chart
// on Details → Speed. The lines are made of scheduled tests only, like the
// patterns (Test now and sweeps cluster at the hours people look), but
// unlike them they keep backup servers, lower bounds and results capped by
// the server: the chart shows what was measured, in bits per second.

const (
	// seriesMinTests is the fewest finished tests a usual point holds: the
	// step grows until every line has that many. 4 gives a region tested
	// hourly 6 h points (never 3 h, which would hold two tests of one of its
	// two providers and one of the other) and a server tested every few
	// hours 12 h points.
	seriesMinTests = 4
	// seriesPooledStep is the pooled line's finest step: in mode regions
	// most regions alternate two providers hourly, so an hour holds half of
	// the servers and two hours hold them all.
	seriesPooledStep = 2 * time.Hour
	// seriesDailyOver is the range above which points are days (a day is
	// too narrow to read its hours).
	seriesDailyOver = 60 * 24 * time.Hour
	// seriesPooledQ is the pooled line: the faster tests of each point,
	// mostly the nearest servers, which follow the line's own speed. A
	// median would sit on a far server and miss a slowdown above its speed.
	seriesPooledQ = 0.9
)

var seriesSteps = []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour}

// speedSeriesJSON is api/speed?part=series. Point i covers t[i] up to
// t[i+1] (the last one up to end): points start on local wall-clock hours,
// so they are not all step long across a DST change.
type speedSeriesJSON struct {
	At      int64       `json:"at"`
	From    int64       `json:"from"`
	To      int64       `json:"to"`
	Step    int64       `json:"step"`
	T       []int64     `json:"t"`
	End     int64       `json:"end"`
	DownMax *float64    `json:"down_max"` // the fastest single test of the lines
	UpMax   *float64    `json:"up_max"`
	Series  []speedLine `json:"series"`
}

// speedLine is one line: every scheduled test (kind pooled: the faster
// tests of each point), a region or a server (the median).
type speedLine struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Usual is the median number of finished tests in a point that has
	// any. A pooled point with under half of it has no value: it holds a
	// different mix of servers.
	Usual   int        `json:"usual"`
	DownBps []*float64 `json:"down_bps"`
	UpBps   []*float64 `json:"up_bps"`
	N       []int      `json:"n"`       // tests with a download
	UpN     []int      `json:"up_n"`    // tests with an upload
	Failed  []int      `json:"failed"`  // busy, unreachable or failed before a download
	DownLB  []int      `json:"down_lb"` // of n: lower bounds
	UpLB    []int      `json:"up_lb"`
}

type seriesCell struct {
	down, up             []float64
	failed, downLB, upLB int
}

// seriesCounts reports a test the lines may use.
func seriesCounts(t *model.SpeedTest) bool {
	return !t.OnDemand() && t.Outcome != model.SpeedCancelled
}

// seriesBounds are the points' edges: every step-th wall-clock hour of
// each local day, from the last one at or before from to the first after
// to. A day with a DST change has a point an hour longer or shorter; when
// the change skips a step hour (midnight in a few zones), the point before
// it runs on to the next one.
func seriesBounds(from, to time.Time, step time.Duration, loc *time.Location) []time.Time {
	y, m, d := from.In(loc).Date()
	var out []time.Time
	for day := -1; ; day++ { // from the day before: its last hour may be the one at or before from
		for h := 0; h < 24; h += int(step / time.Hour) {
			b := time.Date(y, m, d+day, h, 0, 0, 0, loc)
			if b.Hour() != h {
				continue // an hour a DST change skips: the point before it runs on
			}
			if !b.After(from) {
				out = out[:0]
			}
			out = append(out, b)
			if b.After(to) {
				return out
			}
		}
	}
}

func seriesCells(ts []*model.SpeedTest, bounds []time.Time) []seriesCell {
	cells := make([]seriesCell, len(bounds)-1)
	for _, t := range ts {
		i := sort.Search(len(bounds), func(i int) bool { return bounds[i].After(t.Start) }) - 1
		if i < 0 || i >= len(cells) {
			continue
		}
		c := &cells[i]
		if t.DownBps == nil {
			c.failed++
			continue
		}
		lbDown, lbUp := speedtest.LowerBounds(t)
		c.down = append(c.down, *t.DownBps)
		if lbDown {
			c.downLB++
		}
		if t.UpBps != nil { // a failed upload keeps its download
			c.up = append(c.up, *t.UpBps)
			if lbUp {
				c.upLB++
			}
		}
	}
	return cells
}

// seriesUsual is the median number of finished tests in the cells that
// have any (0: none has).
func seriesUsual(cells []seriesCell) int {
	var ns []float64
	for i := range cells {
		if n := len(cells[i].down); n > 0 {
			ns = append(ns, float64(n))
		}
	}
	if len(ns) == 0 {
		return 0
	}
	return int(math.Ceil(callq.Median(ns)))
}

// speedTarget is a known region or server id's kind and label.
func speedTarget(id string, st speedtest.State, names speedNames) (kind, label string) {
	if i := slices.IndexFunc(st.Regions, func(r speedtest.Region) bool { return r.ID == id }); i >= 0 {
		return "region", st.Regions[i].Title
	}
	return "server", names.ref(id).Label
}

func speedSeriesOf(all []model.SpeedTest, p speedParams, st speedtest.State, names speedNames, now time.Time) *speedSeriesJSON {
	type group struct {
		line speedLine
		ts   []*model.SpeedTest
	}
	groups := []group{{line: speedLine{Kind: "pooled"}}}
	if len(p.targets) > 0 {
		groups = groups[:0]
		for _, id := range p.targets {
			kind, label := speedTarget(id, st, names)
			groups = append(groups, group{line: speedLine{ID: id, Kind: kind, Label: label}})
		}
	}
	out := &speedSeriesJSON{At: now.UnixMilli(), From: p.from.UnixMilli(), To: p.to.UnixMilli()}
	top := func(m **float64, v *float64) {
		if v != nil && (*m == nil || *v > **m) {
			x := math.Round(*v)
			*m = &x
		}
	}
	for k := range all {
		t := &all[k]
		if !seriesCounts(t) {
			continue
		}
		for g := range groups {
			l := &groups[g].line
			if l.Kind == "pooled" || (l.Kind == "region" && t.Region == l.ID) || (l.Kind == "server" && t.Server == l.ID) {
				groups[g].ts = append(groups[g].ts, t)
				top(&out.DownMax, t.DownBps)
				top(&out.UpMax, t.UpBps)
			}
		}
	}
	step := store.BucketStep(p.from, p.to, p.points, store.TierHourly)
	if len(p.targets) == 0 {
		step = max(step, seriesPooledStep)
	}
	if p.to.Sub(p.from) > seriesDailyOver {
		step = 24 * time.Hour
	}
	var bounds []time.Time
	cells := make([][]seriesCell, len(groups))
	for {
		bounds = seriesBounds(p.from, p.to, step, p.zone.loc)
		thin := false
		for g := range groups {
			cells[g] = seriesCells(groups[g].ts, bounds)
			groups[g].line.Usual = seriesUsual(cells[g])
			thin = thin || (groups[g].line.Usual > 0 && groups[g].line.Usual < seriesMinTests)
		}
		i := slices.Index(seriesSteps, step)
		if !thin || i < 0 || i == len(seriesSteps)-1 {
			break
		}
		step = seriesSteps[i+1]
	}
	out.Step = step.Milliseconds()
	out.T = make([]int64, len(bounds)-1)
	for i := range out.T {
		out.T[i] = bounds[i].UnixMilli()
	}
	out.End = bounds[len(bounds)-1].UnixMilli()
	for g := range groups {
		l := groups[g].line
		n := len(out.T)
		l.DownBps, l.UpBps = make([]*float64, n), make([]*float64, n)
		l.N, l.UpN, l.Failed, l.DownLB, l.UpLB = make([]int, n), make([]int, n), make([]int, n), make([]int, n), make([]int, n)
		value := func(v []float64) *float64 {
			if len(v) == 0 || (l.Kind == "pooled" && 2*len(v) < l.Usual) {
				return nil
			}
			x := callq.Median(v)
			if l.Kind == "pooled" {
				x = callq.Quantile(v, seriesPooledQ)
			}
			x = math.Round(x)
			return &x
		}
		for i, c := range cells[g] {
			l.DownBps[i], l.UpBps[i] = value(c.down), value(c.up)
			l.N[i], l.UpN[i], l.Failed[i], l.DownLB[i], l.UpLB[i] = len(c.down), len(c.up), c.failed, c.downLB, c.upLB
		}
		out.Series = append(out.Series, l)
	}
	return out
}

// speedPointsJSON is api/speed?part=tests: one region's or server's tests
// as the chart draws them one by one (oldest first; the newest
// speedTestsCap, complete from complete_from). Unlike the lines it has the
// tests the owner asked for, marked on_demand.
type speedPointsJSON struct {
	At           int64                       `json:"at"`
	From         int64                       `json:"from"`
	To           int64                       `json:"to"`
	ID           string                      `json:"id"`
	Kind         string                      `json:"kind"`
	Label        string                      `json:"label"`
	Servers      map[string]speedPointServer `json:"servers"`
	Tests        []speedPoint                `json:"tests"`
	Capped       bool                        `json:"capped"`
	CompleteFrom int64                       `json:"complete_from"`
}

type speedPointServer struct {
	Label    string `json:"label"`
	Provider string `json:"provider"`
}

type speedPoint struct {
	ID       int64    `json:"id"`
	Start    int64    `json:"start"`
	Outcome  string   `json:"outcome"`
	Server   string   `json:"server"`
	DownBps  *float64 `json:"down_bps"`
	UpBps    *float64 `json:"up_bps"`
	DownLB   bool     `json:"down_lb,omitempty"`
	UpLB     bool     `json:"up_lb,omitempty"`
	Fallback bool     `json:"fallback,omitempty"`
	OnDemand bool     `json:"on_demand,omitempty"`
	Capped   bool     `json:"capped_by_server,omitempty"`
}

func speedPoints(all []model.SpeedTest, p speedParams, st speedtest.State, names speedNames, now time.Time) *speedPointsJSON {
	kind, label := speedTarget(p.row, st, names)
	out := &speedPointsJSON{At: now.UnixMilli(), From: p.from.UnixMilli(), To: p.to.UnixMilli(), ID: p.row, Kind: kind, Label: label,
		Servers: map[string]speedPointServer{}, Tests: []speedPoint{}, CompleteFrom: p.from.UnixMilli()}
	var ts []model.SpeedTest
	for _, t := range all {
		if t.Outcome == model.SpeedCancelled {
			continue
		}
		if (kind == "region" && t.Region == p.row) || (kind == "server" && t.Server == p.row) {
			ts = append(ts, t)
		}
	}
	if len(ts) > speedTestsCap {
		ts = ts[len(ts)-speedTestsCap:]
		out.Capped, out.CompleteFrom = true, ts[0].Start.UnixMilli()
	}
	for i := range ts {
		t := &ts[i]
		lbDown, lbUp := speedtest.LowerBounds(t)
		out.Tests = append(out.Tests, speedPoint{ID: t.ID, Start: t.Start.UnixMilli(), Outcome: t.Outcome, Server: t.Server,
			DownBps: roundP(t.DownBps, 1), UpBps: roundP(t.UpBps, 1), DownLB: lbDown, UpLB: lbUp,
			Fallback: t.Fallback, OnDemand: t.OnDemand(), Capped: t.CappedByServer})
		if _, ok := out.Servers[t.Server]; !ok && t.Server != "" {
			ref, _, provider, _ := names.server(t.Server)
			out.Servers[t.Server] = speedPointServer{Label: ref.Label, Provider: provider}
		}
	}
	return out
}

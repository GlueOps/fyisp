package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/speedtest"
)

// Points start on local wall-clock hours on both sides of a DST change.
func TestSeriesBounds(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	cairo, err := time.LoadLocation("Africa/Cairo")
	if err != nil {
		t.Fatal(err)
	}
	at := func(m time.Month, d, h int) time.Time { return time.Date(2026, m, d, h, 0, 0, 0, berlin) }
	for _, c := range []struct {
		name     string
		from, to time.Time
		step     time.Duration
		n        int
		longest  time.Duration
	}{
		// 25 October 2026 has 25 hours: 03:00 CEST goes back to 02:00 CET.
		{"autumn 6 h", at(10, 24, 12), at(10, 26, 12), 6 * time.Hour, 10, 7 * time.Hour},
		{"autumn days", at(10, 20, 9), at(10, 30, 9), 24 * time.Hour, 12, 25 * time.Hour},
		// 29 March 2026 has no 02:00.
		{"spring hours", at(3, 29, 0), at(3, 29, 6), time.Hour, 7, time.Hour},
		{"spring 2 h", at(3, 28, 22), at(3, 29, 8), 2 * time.Hour, 6, 3 * time.Hour},
		// 24 April 2026 has no midnight in Cairo: the day before's point runs on.
		{"no midnight", time.Date(2026, 4, 24, 1, 17, 0, 0, cairo), time.Date(2026, 4, 25, 1, 17, 0, 0, cairo), 24 * time.Hour, 3, 47 * time.Hour},
		{"no midnight 2 h", time.Date(2026, 4, 24, 1, 17, 0, 0, cairo), time.Date(2026, 4, 24, 5, 0, 0, 0, cairo), 2 * time.Hour, 4, 3 * time.Hour},
		{"utc", time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC), time.Date(2026, 1, 2, 5, 30, 0, 0, time.UTC), 3 * time.Hour, 10, 3 * time.Hour},
	} {
		loc := c.from.Location()
		b := seriesBounds(c.from, c.to, c.step, loc)
		if len(b) != c.n || b[0].After(c.from) || !b[len(b)-1].After(c.to) || (len(b) > 1 && !b[1].After(c.from)) {
			t.Errorf("%s: %d bounds %v", c.name, len(b), b)
			continue
		}
		var longest time.Duration
		for i, x := range b {
			if x.In(loc).Hour()%int(c.step/time.Hour) != 0 || x.In(loc).Minute() != 0 {
				t.Errorf("%s: bound %v is not on a step hour", c.name, x.In(loc))
			}
			if i > 0 {
				if !x.After(b[i-1]) {
					t.Errorf("%s: bounds not increasing at %d", c.name, i)
				}
				longest = max(longest, x.Sub(b[i-1]))
			}
		}
		if longest != c.longest {
			t.Errorf("%s: longest point %v, want %v", c.name, longest, c.longest)
		}
	}
}

func TestSpeedSeriesRules(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := func(v float64) *float64 { return &v }
	st := speedtest.State{Regions: []speedtest.Region{{ID: "uk", Title: "UK"}, {ID: "us-east", Title: "US East"}}}
	names := newSpeedNames(st, nil)
	zone, _ := parseZone("0", day)
	ok := func(at time.Time, region string, down float64) model.SpeedTest {
		return model.SpeedTest{Start: at, Trigger: "schedule", Outcome: model.SpeedOK, Region: region, Server: region + "-1", DownBps: f(down), UpBps: f(down / 10)}
	}
	var ts []model.SpeedTest
	// Two days, 8 tests an hour: 4 fast (uk) and 4 slow (us-east).
	for h := range 48 {
		at := day.Add(time.Duration(h) * time.Hour)
		for i := range 4 {
			if h/2 == 5 && i > 0 {
				continue // hours 10 and 11: 4 tests instead of 16
			}
			ts = append(ts, ok(at.Add(time.Duration(i)*time.Minute), "uk", 900e6), ok(at.Add(time.Duration(30+i)*time.Minute), "us-east", 300e6))
		}
	}
	extra := func(at time.Time, mut func(*model.SpeedTest)) {
		x := ok(at, "uk", 900e6)
		mut(&x)
		ts = append(ts, x)
	}
	extra(day.Add(90*time.Minute), func(x *model.SpeedTest) { x.Trigger, x.DownBps = "manual", f(5e9) })
	extra(day.Add(91*time.Minute), func(x *model.SpeedTest) { x.Trigger, x.DownBps = "sweep", f(5e9) })
	extra(day.Add(92*time.Minute), func(x *model.SpeedTest) { x.Outcome, x.DownBps, x.UpBps = model.SpeedCancelled, nil, nil })
	extra(day.Add(93*time.Minute), func(x *model.SpeedTest) { x.Outcome, x.DownBps, x.UpBps = model.SpeedBusy, nil, nil })
	extra(day.Add(94*time.Minute), func(x *model.SpeedTest) { x.Outcome, x.UpBps = model.SpeedFailed, nil }) // the upload failed
	extra(day.Add(95*time.Minute), func(x *model.SpeedTest) {
		x.Extra = `{"rate_method":["peak90","whole"],"lower_bound":[true,false]}`
	})

	out := speedSeriesOf(ts, speedParams{from: day, to: day.Add(48 * time.Hour), points: 1000, zone: zone}, st, names, day)
	if len(out.Series) != 1 || out.Series[0].Kind != "pooled" || out.Step != (2*time.Hour).Milliseconds() || len(out.T) != 25 || out.T[0] != day.UnixMilli() {
		t.Fatalf("pooled: step %d, %d points, %+v", out.Step, len(out.T), out.Series)
	}
	l := out.Series[0]
	if l.Usual != 16 || *out.DownMax != 900e6 || *out.UpMax != 90e6 {
		t.Errorf("usual %d, max %v %v", l.Usual, *out.DownMax, *out.UpMax)
	}
	// The first point: 16 tests and the two extras with a download; the faster tests are uk's.
	if l.N[0] != 18 || l.UpN[0] != 17 || l.Failed[0] != 1 || l.DownLB[0] != 1 || l.UpLB[0] != 0 || l.DownBps[0] == nil || *l.DownBps[0] != 900e6 {
		t.Errorf("first point: n %d up_n %d failed %d lb %d/%d down %v", l.N[0], l.UpN[0], l.Failed[0], l.DownLB[0], l.UpLB[0], l.DownBps[0])
	}
	// Under half the usual tests: counted, but no value.
	if l.N[5] != 4 || l.DownBps[5] != nil || l.UpBps[5] != nil || l.DownBps[4] == nil || l.DownBps[6] == nil {
		t.Errorf("thin point: n %d down %v", l.N[5], l.DownBps[5])
	}
	if l.N[24] != 0 || l.DownBps[24] != nil {
		t.Errorf("the point after to: %d %v", l.N[24], l.DownBps[24])
	}

	// Targets: the median, on a step with 4 tests in a usual point; several lines share the coarsest.
	out = speedSeriesOf(ts, speedParams{from: day, to: day.Add(48 * time.Hour), points: 1000, zone: zone, targets: []string{"us-east", "uk"}}, st, names, day)
	if len(out.Series) != 2 || out.Series[0].ID != "us-east" || out.Series[0].Kind != "region" || out.Series[1].Label != "UK" || out.Step != time.Hour.Milliseconds() {
		t.Fatalf("targets: step %d %+v", out.Step, out.Series)
	}
	if u := out.Series[0]; u.Usual != 4 || *u.DownBps[10] != 300e6 || u.N[10] != 1 {
		t.Errorf("us-east: usual %d, thin point %v n %d", u.Usual, u.DownBps[5], u.N[5])
	}
	var hourly []model.SpeedTest
	for h := range 72 {
		hourly = append(hourly, ok(day.Add(time.Duration(h)*time.Hour), "uk", 900e6))
	}
	out = speedSeriesOf(hourly, speedParams{from: day, to: day.Add(72 * time.Hour), points: 1000, zone: zone, targets: []string{"uk"}}, st, names, day)
	if out.Step != (6*time.Hour).Milliseconds() || out.Series[0].Usual != 6 {
		t.Errorf("one test an hour: step %d usual %d", out.Step, out.Series[0].Usual)
	}
	// Few points asked for: the step follows them.
	out = speedSeriesOf(ts, speedParams{from: day, to: day.Add(48 * time.Hour), points: 10, zone: zone}, st, names, day)
	if out.Step != (6 * time.Hour).Milliseconds() {
		t.Errorf("points=10: step %d", out.Step)
	}
	// Over 60 days: days.
	out = speedSeriesOf(ts, speedParams{from: day.Add(-70 * 24 * time.Hour), to: day.Add(48 * time.Hour), points: 2000, zone: zone}, st, names, day)
	if out.Step != (24*time.Hour).Milliseconds() || len(out.T) != 73 {
		t.Errorf("72 days: step %d, %d points", out.Step, len(out.T))
	}
	// Nothing stored.
	out = speedSeriesOf(nil, speedParams{from: day, to: day.Add(48 * time.Hour), points: 1000, zone: zone}, st, names, day)
	if out.DownMax != nil || out.Series[0].Usual != 0 || len(out.T) != 25 || out.Series[0].DownBps[0] != nil {
		t.Errorf("empty: %+v", out)
	}
}

func TestSpeedSeriesHTTP(t *testing.T) {
	loc, _ := speedHandlers(t, newSpeedRig(t, time.Hour), LocalOptions{})
	get := func(path string, v any) {
		t.Helper()
		w := do(loc, "GET", path, speedHost)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	var s speedSeriesJSON
	get("/api/speed?part=series&from=now-7d&tz=Europe/Berlin", &s)
	if len(s.Series) != 1 || s.Series[0].Kind != "pooled" || s.Step != (2*time.Hour).Milliseconds() || s.To != speedNow.UnixMilli() || s.DownMax == nil {
		t.Errorf("pooled: step %d, %+v", s.Step, s.Series)
	}
	// The rig's Test now results (900 Mbit/s, its own server) are not in the lines.
	if *s.DownMax > 450e6 {
		t.Errorf("down_max %v has an on-demand test", *s.DownMax)
	}
	own := url.QueryEscape("192.168.1.20:5201")
	s = speedSeriesJSON{}
	get("/api/speed?part=series&from=now-7d&targets=uk,"+own, &s)
	if len(s.Series) != 2 || s.Series[0].Kind != "region" || s.Series[1].Kind != "server" || s.Series[1].Label != "Your server" || s.Series[1].Usual != 0 || s.Step != (6*time.Hour).Milliseconds() {
		t.Errorf("targets: step %d, %+v", s.Step, s.Series)
	}
	// A server's line holds its own tests only: fewer than its region's, which rotates providers.
	s = speedSeriesJSON{}
	get("/api/speed?part=series&from=now-7d&targets=uk,Clouvider-London", &s)
	sum := func(v []int) (n int) {
		for _, x := range v {
			n += x
		}
		return n
	}
	if reg, srv := sum(s.Series[0].N), sum(s.Series[1].N); len(s.Series) != 2 || s.Series[1].Kind != "server" || s.Series[1].Label != "Clouvider London" || srv == 0 || srv >= reg {
		t.Errorf("server line: %d of the region's %d tests, %+v", srv, reg, s.Series[1])
	}
	var p speedPointsJSON
	get("/api/speed?part=tests&row=uk&from=now-2d", &p)
	if p.Kind != "region" || p.Label == "" || len(p.Tests) < 40 || p.Capped || p.CompleteFrom != p.From || len(p.Servers) == 0 {
		t.Errorf("tests: %s %q %d tests, capped %v", p.Kind, p.Label, len(p.Tests), p.Capped)
	}
	for i, x := range p.Tests {
		if i > 0 && x.Start < p.Tests[i-1].Start {
			t.Fatalf("tests not oldest first at %d", i)
		}
		if _, ok := p.Servers[x.Server]; !ok {
			t.Errorf("no server entry for %q", x.Server)
		}
	}
	p = speedPointsJSON{}
	get("/api/speed?part=tests&server="+own+"&from=now-30d", &p)
	if p.Kind != "server" || len(p.Tests) == 0 || !p.Tests[0].OnDemand || *p.Tests[0].DownBps != 900e6 {
		t.Errorf("own server: %+v", p)
	}
	// 30 days of hourly tests of one region stay under the cap; the body has no address fields.
	w := do(loc, "GET", "/api/speed?part=tests&row=uk&from=now-30d", speedHost)
	for _, k := range []string{`"port"`, `"handoff"`, `"latency"`} {
		if strings.Contains(w.Body.String(), k) {
			t.Errorf("part=tests has %s", k)
		}
	}
	nine := strings.TrimSuffix(strings.Repeat("uk,", 9), ",")
	for path, code := range map[string]int{
		"/api/speed?part=series&targets=Nope":                         404,
		"/api/speed?part=series&targets=":                             400,
		"/api/speed?part=series&targets=uk,uk":                        400,
		"/api/speed?part=series&targets=uk,,us-east":                  400,
		"/api/speed?part=series&targets=" + nine:                      400,
		"/api/speed?part=series&targets=" + strings.Repeat("a,", 400): 400,
		"/api/speed?part=series&days=7":                               400,
		"/api/speed?part=series&row=uk":                               400,
		"/api/speed?part=series&points=x":                             400,
		"/api/speed?part=series&from=1000000000000":                   400,
		"/api/speed?part=series&from=now&to=now-1h":                   400,
		"/api/speed?part=series&tz=Mars/Olympus":                      400,
		"/api/speed?part=tests":                                       400,
		"/api/speed?part=tests&row=uk&server=uk":                      400,
		"/api/speed?part=tests&row=uk&targets=uk":                     400,
		"/api/speed?part=tests&row=uk&points=100":                     400,
		"/api/speed?part=tests&row=uk&tz=120":                         400,
		"/api/speed?part=tests&row=Nope":                              404,
		"/api/speed?targets=uk":                                       400,
		"/api/speed?part=windows&points=100":                          400,
		"/api/speed?part=queue&targets=uk":                            400,
	} {
		w := do(loc, "GET", path, speedHost)
		var e struct{ Code string }
		json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != code || e.Code != errCode(code) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	// points is clamped, as on api/panel.
	s = speedSeriesJSON{}
	get("/api/speed?part=series&from=now-2d&points=1", &s)
	if s.Step != (6 * time.Hour).Milliseconds() { // 10 points; unclamped it would be a day
		t.Errorf("points=1: step %d", s.Step)
	}
}

// part=tests: the newest speedTestsCap, each with its flags; never a cancelled one.
func TestSpeedPoints(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	f := func(v float64) *float64 { return &v }
	const own = "10.0.0.2:5201"
	st := speedtest.State{Regions: []speedtest.Region{{ID: "uk", Title: "UK"}}, Servers: []speedtest.ServerState{{ID: own, Custom: true}}}
	var ts []model.SpeedTest
	for i := range speedTestsCap + 1 {
		ts = append(ts, model.SpeedTest{ID: int64(i + 1), Start: day.Add(time.Duration(i) * time.Minute), Trigger: "schedule", Outcome: model.SpeedOK,
			Region: "uk", Server: own, DownBps: f(9e8), UpBps: f(4e7)})
	}
	ts[1].Extra = `{"rate_method":["whole","whole"],"lower_bound":[true,false]}`
	ts[2].Extra = `{"rate_method":["whole","whole"],"lower_bound":[false,true]}`
	ts[3].CappedByServer, ts[3].Fallback, ts[3].Trigger = true, true, "sweep"
	last := len(ts) - 1
	ts[last].Outcome, ts[last].DownBps, ts[last].UpBps = model.SpeedCancelled, nil, nil
	ts = append(ts, model.SpeedTest{ID: 9999, Start: day, Trigger: "schedule", Outcome: model.SpeedOK, Region: "us-east", Server: "elsewhere", DownBps: f(1e8)})

	p := speedParams{row: "uk", from: day, to: day.Add(5 * 24 * time.Hour)}
	out := speedPoints(ts, p, st, newSpeedNames(st, nil), day)
	if out.Kind != "region" || out.Label != "UK" || len(out.Tests) != speedTestsCap || out.Capped || out.CompleteFrom != day.UnixMilli() {
		t.Fatalf("under the cap: %s %q, %d tests, capped %v from %d", out.Kind, out.Label, len(out.Tests), out.Capped, out.CompleteFrom)
	}
	if x := out.Tests; !x[1].DownLB || x[1].UpLB || x[2].DownLB || !x[2].UpLB || !x[3].Capped || !x[3].Fallback || !x[3].OnDemand || x[4].Capped || x[4].OnDemand ||
		x[len(x)-1].ID != int64(speedTestsCap) || out.Servers[own].Label != "Your server" || len(out.Servers) != 1 {
		t.Errorf("flags: %+v, last %+v, servers %v", x[1:5], x[len(x)-1], out.Servers)
	}
	// One more: the oldest goes, and complete_from says where the rest start.
	ts[last].Outcome, ts[last].DownBps = model.SpeedOK, f(9e8)
	out = speedPoints(ts, p, st, newSpeedNames(st, nil), day)
	if len(out.Tests) != speedTestsCap || !out.Capped || out.CompleteFrom != day.Add(time.Minute).UnixMilli() || out.Tests[0].ID != 2 {
		t.Errorf("capped: %d tests, capped %v, complete from %d, first %d", len(out.Tests), out.Capped, out.CompleteFrom, out.Tests[0].ID)
	}
	// By server: the same tests.
	p.row = own
	if out = speedPoints(ts, p, st, newSpeedNames(st, nil), day); out.Kind != "server" || out.Label != "Your server" || len(out.Tests) != speedTestsCap {
		t.Errorf("server: %s %q %d", out.Kind, out.Label, len(out.Tests))
	}
}

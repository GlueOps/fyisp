package web

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glueops/fyisp/internal/export"
	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/store"
)

// compareStore is a real store with two hours of Alpha's TCP connects, one
// survey hour of Alpha (two destinations; the main path crosses AS64500,
// then AS15169 twice) and one address period.
func compareStore(t *testing.T) (*store.SQLite, time.Time) {
	t.Helper()
	from := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	st, err := store.Open(t.TempDir(), store.Options{Now: func() time.Time { return from.Add(2*time.Hour + 30*time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key := model.SeriesKey{Target: "Alpha", Kind: model.KindTCP}
	i := 0
	for ts := from; ts.Before(from.Add(2 * time.Hour)); ts = ts.Add(15 * time.Second) {
		s := model.Sample{Key: key, Slot: ts, RTT: time.Duration(10+i%10) * time.Millisecond}
		if i%40 == 0 {
			s = model.Sample{Key: key, Slot: ts, Lost: true, Reason: model.ReasonTimeout}
		}
		st.Observe(s)
		i++
	}
	a := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	hop := func(ttl int, status, ip string, asn uint32, n int) model.SurveyHop {
		h := model.SurveyHop{TTL: ttl, Status: status, ASN: asn, N: n, MinMs: 5, SumMs: 5 * float64(n), MaxMs: 5}
		if ip != "" {
			h.IP = a(ip)
		}
		return h
	}
	st.ObserveSurvey(model.SurveyHour{Target: "Alpha", Hour: from.Unix() / 3600, Host: "alpha.example.com", Port: 443, HopsVia: "tcp",
		Paths: []model.SurveyPath{
			{Dest: a("198.51.100.9"), Rounds: 1, Reached: 1, Hops: []model.SurveyHop{hop(3, model.HopAnswered, "198.51.100.9", 64510, 1)}},
			{Dest: a("198.51.100.7"), Rounds: 3, Reached: 3, Hops: []model.SurveyHop{
				hop(1, model.HopYours, "", 0, 3), hop(2, model.HopProvider, "", 64496, 3),
				hop(3, model.HopAnswered, "203.0.113.1", 64500, 2), hop(3, model.HopAnswered, "203.0.113.2", 64501, 1),
				hop(4, model.HopSilent, "", 0, 3),
				hop(5, model.HopAnswered, "192.0.2.10", 15169, 3), hop(6, model.HopAnswered, "198.51.100.7", 15169, 3),
			}},
		}})
	st.ObserveAddr("alpha.example.com", a("198.51.100.7"), 2)
	if err := st.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st, from
}

func compareGet(t *testing.T, st store.Reader, site string) func(path string) (int, string) {
	t.Helper()
	p := testProfile()
	p.Targets[0].Provider, p.Targets[0].City, p.Targets[0].Country, p.Targets[0].Geo = "acme", "Mumbai", "IN", "as"
	p.Targets[0].Tags = []string{"cdn", "app:x"}
	h := Local(Deps{Profile: func() *model.Profile { return p }, Store: st, Site: site}, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok"})
	return func(path string) (int, string) {
		w := do(h, "GET", path, map[string]string{"Host": "127.0.0.1:3000"})
		return w.Code, w.Body.String()
	}
}

func readCSV(t *testing.T, body string) []map[string]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil || len(recs) == 0 {
		t.Fatalf("csv: %v\n%s", err, body)
	}
	var out []map[string]string
	for _, r := range recs[1:] {
		m := map[string]string{}
		for i, h := range recs[0] {
			m[h] = r[i]
		}
		out = append(out, m)
	}
	return out
}

// TestCompareCSV: a row per target, kind and hour, with the hour's connects,
// the site, the target's place, and the survey's destinations and networks.
func TestCompareCSV(t *testing.T) {
	st, from := compareStore(t)
	get := compareGet(t, st, "mumbai-dc1")
	code, body := get(fmt.Sprintf("/api/compare.csv?from=%d&to=%d", from.UnixMilli(), from.Add(3*time.Hour).UnixMilli()))
	if code != 200 {
		t.Fatalf("code %d: %s", code, body)
	}
	if !strings.HasPrefix(body, strings.Join(CompareHeader, ",")+"\n") {
		t.Fatalf("header: %s", strings.SplitN(body, "\n", 2)[0])
	}
	rows := readCSV(t, body)
	if len(rows) != 2 {
		t.Fatalf("%d rows, want 2 (Alpha tcp, two hours): %s", len(rows), body)
	}
	r := rows[0]
	want := map[string]string{
		"site": "mumbai-dc1", "hour": "2026-09-26T10:00:00Z", "target": "Alpha", "kind": "tcp",
		"provider": "acme", "city": "Mumbai", "country": "IN", "geo": "as", "tags": "cdn;app:x",
		"n": "234", "lost": "6", "loss": "0.0250", "min_ms": "10.000", "max_ms": "19.000", "complete": "1",
		"dest_ips": "198.51.100.7;198.51.100.9", "as_path": "64500 15169", "survey_rounds": "4", "survey_reached": "4",
	}
	for k, v := range want {
		if r[k] != v {
			t.Errorf("%s = %q, want %q", k, r[k], v)
		}
	}
	if rows[1]["hour"] != "2026-09-26T11:00:00Z" || rows[1]["dest_ips"] != "" || rows[1]["as_path"] != "" || rows[1]["survey_rounds"] != "" {
		t.Errorf("second hour (no survey): %v", rows[1])
	}
	// targets= narrows it; a target with no connects in the range has no rows.
	if _, body := get(fmt.Sprintf("/api/compare.csv?from=%d&to=%d&targets=Beta", from.UnixMilli(), from.Add(3*time.Hour).UnixMilli())); len(readCSV(t, body)) != 0 {
		t.Errorf("Beta: %s", body)
	}
}

// TestCompareExports: api/survey.csv and api/addrs.csv are fyisp export's
// --survey and --addrs CSVs, named after the site.
func TestCompareExports(t *testing.T) {
	st, from := compareStore(t)
	p := testProfile()
	h := Local(Deps{Profile: func() *model.Profile { return p }, Store: st, Site: "dc2"}, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok"})
	rng := fmt.Sprintf("from=%d&to=%d", from.UnixMilli(), from.Add(3*time.Hour).UnixMilli())
	w := do(h, "GET", "/api/survey.csv?"+rng, map[string]string{"Host": "127.0.0.1:3000"})
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="dc2-survey.csv"`) {
		t.Fatalf("survey: %d %v", w.Code, w.Header())
	}
	rows := readCSV(t, w.Body.String())
	if len(rows) == 0 || rows[0]["target"] != "Alpha" || rows[0]["dest_ip"] == "" {
		t.Errorf("survey rows: %v", rows)
	}
	w = do(h, "GET", "/api/addrs.csv?"+rng, map[string]string{"Host": "127.0.0.1:3000"})
	if rows := readCSV(t, w.Body.String()); w.Code != 200 || len(rows) != 1 || rows[0]["ip"] != "198.51.100.7" {
		t.Errorf("addrs: %d %v", w.Code, rows)
	}
}

// TestCompareLimits: bad parameters, the admin token, and one export at a
// time.
func TestCompareLimits(t *testing.T) {
	st, _ := compareStore(t)
	get := compareGet(t, st, "")
	for path, code := range map[string]int{
		"/api/compare.csv":                    200,
		"/api/compare.csv?from=now-2h":        200,
		"/api/compare.csv?from=now-30d":       200, // shortened to 7 days
		"/api/compare.csv?from=now&to=now-1h": 400,
		"/api/compare.csv?kind=tcp":           400,
		"/api/addrs.csv?targets=Alpha":        400,
		"/api/survey.csv?targets=Alpha":       200,
	} {
		if c, body := get(path); c != code {
			t.Errorf("%s = %d, want %d: %s", path, c, code, body)
		}
	}
	// Without a site the file is named fyisp-*.
	p := testProfile()
	h := Local(Deps{Profile: func() *model.Profile { return p }, Store: st}, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok", AdminToken: testAdmin})
	if w := do(h, "GET", "/api/compare.csv", map[string]string{"Host": "127.0.0.1:3000"}); w.Code != 401 {
		t.Errorf("no admin token: %d", w.Code)
	}
	w := do(h, "GET", "/api/compare.csv", map[string]string{"Host": "127.0.0.1:3000", HeaderAdmin: testAdmin})
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="fyisp-compare.csv"`) {
		t.Errorf("with the token: %d %v", w.Code, w.Header())
	}
	// Another export running: 429 with Retry-After.
	heavy <- struct{}{}
	w = do(h, "GET", "/api/compare.csv", map[string]string{"Host": "127.0.0.1:3000", HeaderAdmin: testAdmin})
	<-heavy
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), `"busy"`) {
		t.Errorf("busy: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	// A store without the survey readers: no such route.
	if c, _ := compareGet(t, countingStoreOf(st), "")("/api/compare.csv"); c != 404 {
		t.Errorf("plain reader: %d", c)
	}
}

// TestStatusSite: api/status names the site, the profile and the clock.
func TestStatusSite(t *testing.T) {
	p := testProfile()
	h := Local(Deps{Profile: func() *model.Profile { return p }, Site: "dc1"}, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok"})
	w := do(h, "GET", "/api/status", map[string]string{"Host": "127.0.0.1:3000"})
	b := w.Body.String()
	if w.Code != 200 || !strings.Contains(b, `"site":"dc1"`) || !strings.Contains(b, `"profile":"test"`) || !strings.Contains(b, `"now":"`) {
		t.Errorf("status: %d %s", w.Code, b)
	}
}

// countingStoreOf hides st's other methods behind the plain store.Reader.
func countingStoreOf(st store.Reader) store.Reader { return &countingStore{Reader: st} }

// TestCompareRange: RFC 3339 or the API's forms; to clamped to now; a span
// over 7 days shortened from the start.
func TestCompareRange(t *testing.T) {
	now := time.Date(2026, 12, 10, 12, 0, 0, 0, time.UTC)
	from, to, err := compareRange("2026-12-01T13:00:00Z", "2026-12-01T17:00:00Z", now)
	if err != nil || !from.Equal(time.Date(2026, 12, 1, 13, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 12, 1, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("RFC 3339: %v %v %v", from, to, err)
	}
	if from, to, err := compareRange("now-30d", "2027-01-01T00:00:00Z", now); err != nil || !to.Equal(now) || to.Sub(from) != CompareMaxRange {
		t.Errorf("shortened and clamped: %v %v %v", from, to, err)
	}
	if _, _, err := compareRange("2026-12-01", "", now); err == nil {
		t.Error("a bare date accepted")
	}
}

// TestCompareOpenHour: the hour still running is complete=0.
func TestCompareOpenHour(t *testing.T) {
	st, from := compareStore(t)
	p := testProfile()
	s := newServer(Deps{Profile: func() *model.Profile { return p }, Store: st}, "tok")
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, n: 1 << 20}
	if err := s.writeCompare(context.Background(), st, lw, nil, from, from.Add(2*time.Hour), from.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, buf.String())
	if len(rows) != 2 || rows[0]["complete"] != "1" || rows[1]["complete"] != "0" {
		t.Errorf("complete: %v", rows)
	}
}

// TestCompareTooLarge: over the cap is 413, never a cut file.
func TestCompareTooLarge(t *testing.T) {
	st, from := compareStore(t)
	get := compareGet(t, st, "dc1")
	defer func(n int) { compareMaxBytes = n }(compareMaxBytes)
	compareMaxBytes = 300
	rng := fmt.Sprintf("from=%d&to=%d", from.UnixMilli(), from.Add(3*time.Hour).UnixMilli())
	for _, r := range []string{"compare", "survey"} {
		if code, body := get("/api/" + r + ".csv?" + rng); code != 413 || !strings.Contains(body, `"too_large"`) {
			t.Errorf("%s: %d %s", r, code, body)
		}
	}
	// The slot is free again after a refusal.
	compareMaxBytes = 1 << 20
	if code, _ := get("/api/compare.csv?" + rng); code != 200 {
		t.Errorf("after 413: %d", code)
	}
}

// The compare-sites skill parses these CSVs; its header fixture must follow
// any column change (update .claude/skills/compare-sites/scripts/testdata/headers.txt).
func TestCompareSitesSkillHeaders(t *testing.T) {
	b, err := os.ReadFile("../../.claude/skills/compare-sites/scripts/testdata/headers.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := "compare: " + strings.Join(CompareHeader, ",") + "\nsurvey: " + strings.Join(export.SurveyHeader, ",") + "\n"
	if string(b) != want {
		t.Errorf("headers.txt is\n%s\nwant\n%s", b, want)
	}
}

package web

import (
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/glueops/fyisp/internal/export"
	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/store"
)

// The site-comparison routes (docs/COMPARING-SITES.md): api/compare.csv, a
// row per target, kind and hour, and api/survey.csv and api/addrs.csv, the
// CSVs of `fyisp export --survey` and `--addrs`. They read the store as the
// export does, under the admin token like every route.
const (
	// CompareMaxRange caps a request's span (shortened from the start).
	CompareMaxRange = 7 * 24 * time.Hour
	compareRetry    = 5 * time.Second
	// compareSend is how long sending a built CSV may take: the server's
	// WriteTimeout (30 s) would cut a big one on a slow link, and a cut
	// file looks complete.
	compareSend = 5 * time.Minute
)

// compareMaxBytes caps a response: the CSV is built in memory (a cut
// stream would look like a complete file), so a bigger one is refused. A
// variable for tests.
var compareMaxBytes = 64 << 20

// errTooLarge stops an export at compareMaxBytes.
var errTooLarge = errors.New("web: export over the size cap")

// CompareReader is what the comparison routes read (*store.SQLite).
type CompareReader interface {
	export.SurveyReader
	HourSummaries(ctx context.Context, keys []model.SeriesKey, from, to time.Time, fn func(store.HourSummary)) error
}

// CompareHeader is api/compare.csv's header.
var CompareHeader = []string{"site", "hour", "target", "kind", "provider", "city", "country", "geo", "tags",
	"n", "lost", "loss", "min_ms", "p50_ms", "p95_ms", "max_ms", "mean_ms", "complete",
	"dest_ips", "as_path", "survey_rounds", "survey_reached"}

// heavy lets one comparison request read the store at a time: they scan up
// to a week of every target, and the dashboard shares the reader pool.
var heavy = make(chan struct{}, 1)

// serveCompare answers GET api/compare.csv, api/survey.csv and
// api/addrs.csv with from= and to= (default the last 24 hours).
func (s *server) serveCompare(w http.ResponseWriter, r *http.Request, rt route) {
	cr, ok := s.d.Store.(CompareReader)
	if !ok {
		writeErr(w, r, http.StatusNotFound, "not found")
		return
	}
	q := r.URL.Query()
	allowed := []string{"from", "to"}
	if rt != routeAddrsCSV {
		allowed = append(allowed, "targets")
	}
	if err := checkParams(q, allowed...); err != nil {
		writeErr(w, r, http.StatusBadRequest, err.Error())
		return
	}
	now := s.now()
	from, to, err := compareRange(q.Get("from"), q.Get("to"), now)
	if err != nil {
		writeErr(w, r, http.StatusBadRequest, err.Error())
		return
	}
	var targets []string
	if v := q.Get("targets"); v != "" {
		targets = strings.Split(v, ",")
	}
	select {
	case heavy <- struct{}{}:
	default:
		w.Header().Set("Retry-After", strconv.Itoa(int(compareRetry/time.Second)))
		writeErrCode(w, r, http.StatusTooManyRequests, "busy", "another comparison export is running: retry shortly")
		return
	}
	// The slot covers reading the store, not sending: a slow client
	// doesn't hold off the next export.
	var buf bytes.Buffer
	lw := &limitWriter{w: &buf, n: compareMaxBytes}
	var name string
	switch rt {
	case routeCompareCSV:
		name = "compare"
		err = s.writeCompare(r.Context(), cr, lw, targets, from, to, now)
	case routeSurveyCSV:
		name = "survey"
		err = export.WriteSurvey(r.Context(), cr, lw, nil, targets, from, to)
	default:
		name = "addrs"
		err = export.WriteAddrs(r.Context(), cr, lw, nil, from, to)
	}
	<-heavy
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(compareSend))
	switch {
	case lw.over:
		writeErrCode(w, r, http.StatusRequestEntityTooLarge, "too_large", "the CSV would be over 64 MiB: ask for a shorter range or fewer targets")
		return
	case err != nil:
		if r.Context().Err() != nil {
			return
		}
		s.d.log().Warn("compare export", "route", name, "err", err)
		writeErr(w, r, http.StatusServiceUnavailable, "the store could not be read: "+err.Error())
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", `attachment; filename="`+fileSafe(cmp.Or(s.d.Site, "fyisp"))+"-"+name+".csv\"")
	writeBody(w, r, http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

// compareRange parses from and to (default now-24h and now; RFC 3339 as
// well as the API's forms), clamps to to now and shortens a span over
// CompareMaxRange from the start.
func compareRange(fs, ts string, now time.Time) (from, to time.Time, err error) {
	if from, err = compareTime(cmp.Or(fs, "now-24h"), now); err != nil {
		return
	}
	if to, err = compareTime(cmp.Or(ts, "now"), now); err != nil {
		return
	}
	if to.After(now) {
		to = now
	}
	if !from.Before(to) {
		return from, to, badReq("from must be before to")
	}
	if to.Sub(from) > CompareMaxRange {
		from = to.Add(-CompareMaxRange)
	}
	return from, to, nil
}

// compareTime is parseTime, or an RFC 3339 time (2026-12-01T13:00:00Z), as
// fyisp export takes, so a script names its window the same way for both.
func compareTime(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return parseTime(s, now)
}

// writeCompare writes a row per TCP and HTTPS target, kind and hour that
// starts in [from, to]: its connects (from the hourly summaries) and, with
// the path survey, the hour's destination addresses and networks past the
// provider (surveyAt), and the target's provider and place.
func (s *server) writeCompare(ctx context.Context, cr CompareReader, w *limitWriter, names []string, from, to, now time.Time) error {
	p := s.d.Profile()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	byName := map[string]model.Target{}
	var keys []model.SeriesKey
	for _, t := range p.Targets {
		if isPath(t) || len(want) > 0 && !want[t.Name] {
			continue
		}
		for _, k := range t.Kinds {
			if k == model.KindTCP || k == model.KindHTTPS {
				keys = append(keys, model.SeriesKey{Target: t.Name, Kind: k})
				byName[t.Name] = t
			}
		}
	}
	var rows []store.HourSummary
	if err := cr.HourSummaries(ctx, keys, from, to, func(h store.HourSummary) { rows = append(rows, h) }); err != nil {
		return err
	}
	slices.SortFunc(rows, func(a, b store.HourSummary) int {
		return cmp.Or(cmp.Compare(a.Hour, b.Hour), cmp.Compare(a.Key.Target, b.Key.Target), cmp.Compare(a.Key.Kind.String(), b.Key.Kind.String()))
	})
	type hk struct {
		target string
		hour   int64
	}
	survey := map[hk]surveyAt{}
	err := cr.SurveyHours(ctx, from, to, func(h model.SurveyHour) error {
		if _, ok := byName[h.Target]; ok {
			survey[hk{h.Target, h.Hour}] = surveyOf(h)
		}
		return nil
	})
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(CompareHeader); err != nil {
		return err
	}
	site := s.d.Site // --site is a plain label (siteRE)
	open := now.Unix() / 3600
	ms := func(v float64, n int64) string {
		if n == 0 {
			return ""
		}
		return strconv.FormatFloat(v, 'f', 3, 64)
	}
	for _, h := range rows {
		t := byName[h.Key.Target]
		sv := survey[hk{t.Name, h.Hour}]
		loss := ""
		if h.N+h.Lost > 0 {
			loss = strconv.FormatFloat(float64(h.Lost)/float64(h.N+h.Lost), 'f', 4, 64)
		}
		complete := "1"
		if h.Hour >= open {
			complete = "0"
		}
		row := []string{site, time.Unix(h.Hour*3600, 0).UTC().Format(time.RFC3339), csvSafe(t.Name), h.Key.Kind.String(),
			csvSafe(t.Provider), csvSafe(t.City), csvSafe(t.Country), csvSafe(t.Geo), csvSafe(strings.Join(t.Tags, ";")),
			strconv.FormatInt(h.N, 10), strconv.FormatInt(h.Lost, 10), loss,
			ms(h.MinMs, h.N), ms(h.P50Ms, h.N), ms(h.P95Ms, h.N), ms(h.MaxMs, h.N), ms(h.MeanMs, h.N), complete,
			sv.dests, sv.asPath, "", ""}
		if sv.rounds > 0 {
			row[20], row[21] = strconv.Itoa(sv.rounds), strconv.Itoa(sv.reached)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// surveyAt is one target's survey hour for api/compare.csv.
type surveyAt struct {
	dests   string // destination addresses, most rounds first, ";"-joined
	asPath  string // the networks past the provider on the main path, " "-joined
	rounds  int
	reached int
}

// surveyOf sums a target's survey hour: every destination address (most
// rounds first), and the main path's (most rounds) networks past the
// provider: at each TTL the router that answered most, its AS number,
// repeats folded. Hops with no address (yours, provider, private) and
// unknown networks are left out.
func surveyOf(h model.SurveyHour) surveyAt {
	var out surveyAt
	paths := slices.Clone(h.Paths)
	slices.SortStableFunc(paths, func(a, b model.SurveyPath) int { return cmp.Compare(b.Rounds, a.Rounds) })
	var dests []string
	for _, p := range paths {
		out.rounds += p.Rounds
		out.reached += p.Reached
		if p.Dest.IsValid() {
			dests = append(dests, p.Dest.String())
		}
	}
	out.dests = strings.Join(dests, ";")
	if len(paths) == 0 {
		return out
	}
	best := map[int]model.SurveyHop{}
	for _, hp := range paths[0].Hops {
		if hp.Status != model.HopAnswered || hp.ASN == 0 {
			continue
		}
		if b, ok := best[hp.TTL]; !ok || hp.N > b.N || hp.N == b.N && hp.IP.Less(b.IP) {
			best[hp.TTL] = hp
		}
	}
	ttls := make([]int, 0, len(best))
	for ttl := range best {
		ttls = append(ttls, ttl)
	}
	slices.Sort(ttls)
	var asns []string
	var last uint32
	for _, ttl := range ttls {
		if a := best[ttl].ASN; a != last {
			asns = append(asns, strconv.FormatUint(uint64(a), 10))
			last = a
		}
	}
	out.asPath = strings.Join(asns, " ")
	return out
}

// limitWriter stops at n bytes, remembers it did and fails every write
// after, so an export stops reading the store early.
type limitWriter struct {
	w    *bytes.Buffer
	n    int
	over bool
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.over || l.w.Len()+len(p) > l.n {
		l.over = true
		return 0, errTooLarge
	}
	return l.w.Write(p)
}

// fileSafe keeps letters, digits, '.', '-' and '_' of s for a file name.
func fileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, s)
}

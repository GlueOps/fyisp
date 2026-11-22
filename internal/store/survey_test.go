package store

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/glueops/fyisp/internal/model"
)

func addrs(t *testing.T, s *SQLite, from, to time.Time) []AddrPeriod {
	t.Helper()
	var out []AddrPeriod
	if err := s.AddrPeriods(ctx, from, to, func(p AddrPeriod) error { out = append(out, p); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestAddrPeriods: lookups that keep one address extend its period (written
// at most every HopSeenInterval, and once more when it changes); a new
// address starts a period; a restart does too.
func TestAddrPeriods(t *testing.T) {
	dir := t.TempDir()
	now := t0
	s := openT(t, dir, Options{Now: func() time.Time { return now }})
	a, b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	for range 30 { // 30 lookups a minute apart
		s.ObserveAddr("h.example", a, 2)
		now = now.Add(time.Minute)
	}
	s.ObserveAddr("h.example", b, 3)
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	want := []AddrPeriod{
		{Host: "h.example", IP: a, Answers: 2, First: t0, Last: t0.Add(29 * time.Minute)},
		{Host: "h.example", IP: b, Answers: 3, First: t0.Add(30 * time.Minute), Last: t0.Add(30 * time.Minute)},
	}
	if got := addrs(t, s, t0, now); !slices.Equal(got, want) {
		t.Errorf("periods\n%v\nwant\n%v", got, want)
	}
	if got := addrs(t, s, t0.Add(29*time.Minute+time.Second), now); len(got) != 1 || got[0].IP != b {
		t.Errorf("overlap filter: %v", got)
	}
	s.Close()

	now = now.Add(time.Hour)
	s = openT(t, dir, Options{Now: func() time.Time { return now }})
	defer s.Close()
	s.ObserveAddr("h.example", b, 3)
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if got := addrs(t, s, t0, now); len(got) != 3 || !got[2].First.Equal(now) {
		t.Errorf("after a restart: %v, want a third period from %v", got, now)
	}
	if err := s.Prune(ctx, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := addrs(t, s, t0, now); len(got) != 1 || !got[0].First.Equal(now) {
		t.Errorf("after prune: %v, want only the period that ends after the cut", got)
	}
}

func surveys(t *testing.T, s *SQLite, from, to time.Time) []model.SurveyHour {
	t.Helper()
	var out []model.SurveyHour
	if err := s.SurveyHours(ctx, from, to, func(h model.SurveyHour) error { out = append(out, h); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func surveyRound(dest netip.Addr, rtt float64) model.SurveyHour {
	return model.SurveyHour{Target: "T", Hour: t0.Unix() / 3600, Host: "t.example", Port: 443, HopsVia: "tcp",
		Paths: []model.SurveyPath{{Dest: dest, Rounds: 1, Fixed: 1, Reached: 1, Complete: 1, Hops: []model.SurveyHop{
			{TTL: 1, Status: model.HopYours, N: 1, MinMs: 1, SumMs: 1, MaxMs: 1},
			{TTL: 2, Status: model.HopSilent, N: 1},
			{TTL: 3, Status: model.HopAnswered, IP: dest, ASN: 64500, N: 1, MinMs: rtt, SumMs: rtt, MaxMs: rtt},
		}}}}
}

// TestSurveyHours: the latest report of an hour replaces the earlier ones;
// after a restart the hour's earlier rounds are kept and added to.
func TestSurveyHours(t *testing.T) {
	dir := t.TempDir()
	now := t0.Add(10 * time.Minute)
	s := openT(t, dir, Options{Now: func() time.Time { return now }})
	d := netip.MustParseAddr("198.51.100.7")
	h := surveyRound(d, 20)
	s.ObserveSurvey(h)
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	h.Merge(surveyRound(d, 10)) // the tracer's report after round 2
	s.ObserveSurvey(h)
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	got := surveys(t, s, t0, now)
	if len(got) != 1 || got[0].Paths[0].Rounds != 2 {
		t.Fatalf("one hour, two rounds: %+v", got)
	}
	s.Close()

	s = openT(t, dir, Options{Now: func() time.Time { return now }})
	defer s.Close()
	s.ObserveSurvey(surveyRound(d, 30)) // this run's first round of the same hour
	if err := s.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	got = surveys(t, s, t0, now)
	if len(got) != 1 {
		t.Fatalf("hours: %+v", got)
	}
	p := got[0].Paths[0]
	dst := p.Hops[len(p.Hops)-1]
	if p.Rounds != 3 || dst.N != 3 || dst.MinMs != 10 || dst.MaxMs != 30 || dst.SumMs != 60 {
		t.Errorf("after a restart: %+v, want 3 rounds, destination n=3 min 10 max 30 sum 60", p)
	}
	if err := s.Prune(ctx, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := surveys(t, s, t0, now); len(got) != 0 {
		t.Errorf("after prune: %+v", got)
	}
}

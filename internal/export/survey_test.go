package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/store"
)

func surveyStore(t *testing.T) *store.SQLite {
	t.Helper()
	now := t0
	s, err := store.Open(t.TempDir(), store.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.ObserveAddr("svc.example", netip.MustParseAddr("198.51.100.7"), 2)
	now = now.Add(time.Minute)
	s.ObserveAddr("svc.example", netip.MustParseAddr("198.51.100.8"), 2)
	s.ObserveHop(model.HopInfo{IP: netip.MustParseAddr("203.0.113.1"), RDNS: "=ae1.par.example.net", ASN: 64501, Owner: "TRANSIT"})
	s.ObserveSurvey(model.SurveyHour{Target: "svc", Hour: t0.Unix() / 3600, Host: "svc.example", Port: 443, HopsVia: "tcp",
		Paths: []model.SurveyPath{{Dest: netip.MustParseAddr("198.51.100.7"), Rounds: 2, Fixed: 2, Reached: 2, Complete: 1, Hops: []model.SurveyHop{
			{TTL: 1, Status: model.HopYours, N: 2, MinMs: 0.4, SumMs: 1, MaxMs: 0.6},
			{TTL: 2, Status: model.HopProvider, ASN: 64500, N: 2, MinMs: 2, SumMs: 5, MaxMs: 3},
			{TTL: 3, Status: model.HopAnswered, IP: netip.MustParseAddr("203.0.113.1"), ASN: 64501, N: 1, MinMs: 80, SumMs: 80, MaxMs: 80},
			{TTL: 3, Status: model.HopSilent, N: 1},
			{TTL: 4, Status: model.HopAnswered, IP: netip.MustParseAddr("198.51.100.7"), ASN: 64502, N: 2, MinMs: 90, SumMs: 181, MaxMs: 91},
		}}}})
	if err := s.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func csvRows(t *testing.T, b *bytes.Buffer) [][]string {
	t.Helper()
	rows, err := csv.NewReader(b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestExportAddrs: one row per address period, overlapping the range.
func TestExportAddrs(t *testing.T) {
	s := surveyStore(t)
	var b bytes.Buffer
	if err := Write(context.Background(), s, &b, Options{Addrs: true, From: t0, To: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rows := csvRows(t, &b)
	want := [][]string{AddrHeader,
		{"svc.example", "198.51.100.7", "2", "2026-09-01T00:00:00.000Z", "2026-09-01T00:00:00.000Z"},
		{"svc.example", "198.51.100.8", "2", "2026-09-01T00:01:00.000Z", "2026-09-01T00:01:00.000Z"}}
	if !slices.EqualFunc(rows, want, slices.Equal) {
		t.Errorf("rows\n%q\nwant\n%q", rows, want)
	}
}

// TestExportSurvey: one row per hop entry; masked hops have no address,
// rdns or stored owner; answered hops get what fyisp knows of the router;
// a range with nothing prints the header and a note.
func TestExportSurvey(t *testing.T) {
	s := surveyStore(t)
	var b, e bytes.Buffer
	if err := Write(context.Background(), s, &b, Options{Survey: true, From: t0, To: t0.Add(time.Hour), Stderr: &e}); err != nil {
		t.Fatal(err)
	}
	rows := csvRows(t, &b)
	if !slices.Equal(rows[0], SurveyHeader) || len(rows) != 6 {
		t.Fatalf("rows %q", rows)
	}
	col := func(r []string, name string) string { return r[slices.Index(SurveyHeader, name)] }
	for _, r := range rows[1:] {
		if col(r, "hour") != "2026-09-01T00:00:00.000Z" || col(r, "target") != "svc" || col(r, "dest_ip") != "198.51.100.7" ||
			col(r, "rounds") != "2" || col(r, "fixed_flow") != "2" || col(r, "reached") != "2" || col(r, "complete") != "1" {
			t.Errorf("row %q", r)
		}
	}
	type hop struct{ ttl, status, ip, asn, owner, rdns, n, mean string }
	var got []hop
	for _, r := range rows[1:] {
		got = append(got, hop{col(r, "ttl"), col(r, "status"), col(r, "ip"), col(r, "asn"), col(r, "owner"), col(r, "rdns"), col(r, "n"), col(r, "mean_ms")})
	}
	want := []hop{
		{"1", "yours", "", "", "your network", "", "2", "0.5"},
		{"2", "provider", "", "64500", "your provider", "", "2", "2.5"},
		{"3", "answered", "203.0.113.1", "64501", "TRANSIT", "'=ae1.par.example.net", "1", "80"},
		{"3", "silent", "", "", "", "", "1", ""},
		{"4", "answered", "198.51.100.7", "64502", "", "", "2", "90.5"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("hops\n%v\nwant\n%v", got, want)
	}
	if e.Len() != 0 {
		t.Errorf("stderr %q", e.String())
	}

	b.Reset()
	if err := Write(context.Background(), s, &b, Options{Survey: true, From: t0.Add(48 * time.Hour), To: t0.Add(49 * time.Hour), Stderr: &e}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(b.String()) != strings.Join(SurveyHeader, ",") || !strings.Contains(e.String(), "--path-survey") {
		t.Errorf("empty range: %q, stderr %q", b.String(), e.String())
	}
}

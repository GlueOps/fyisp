package main

import (
	"context"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/s3test"
	"github.com/glueops/fyisp/internal/speedtest"
	"github.com/glueops/fyisp/internal/storage"
	"github.com/glueops/fyisp/internal/store"
)

// Storage scenarios (-storage).
const (
	storageNone    = "none"    // no storage source: Storage hidden
	storageEmpty   = "empty"   // no targets yet
	storageTargets = "targets" // a few targets
	storageHistory = "history" // the targets and a past evening schedule's rounds
)

// newFakeStorage is the real storage service and campaigns over a
// temporary data directory (a targets file and a database of their own),
// with the engine's network faked: no request leaves the machine. Names
// ending .lan or .local resolve to the LAN, others to a public address; a
// host with "down" in it times out, one with "auth" refuses keys. Rounds
// wait in q.
func newFakeStorage(ctx context.Context, scenario string, q *speedtest.Queue, log *slog.Logger) (*storage.Service, *storage.Campaigns, *store.SQLite, func()) {
	dir, err := os.MkdirTemp("", "fyisp-webdev-storage-")
	if err != nil {
		log.Error("storage", "err", err)
		return nil, nil, nil, func() {}
	}
	db, err := store.Open(dir, store.Options{Log: log})
	if err != nil {
		log.Error("storage database", "err", err)
		os.RemoveAll(dir)
		return nil, nil, nil, func() {}
	}
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".local") {
			return []netip.Addr{netip.MustParseAddr("192.168.1.20")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("52.216.10.10")}, nil
	}
	e := s3test.New(s3test.Options{Lookup: lookup, Fake: fakeAttempt, IsSelf: func(netip.Addr) bool { return false }})
	s := storage.Open(storage.Options{Dir: dir, Engine: e, Lookup: lookup})
	var ids []string
	if scenario == storageTargets || scenario == storageHistory {
		for _, sp := range []storage.Spec{
			{Name: "AWS Virginia", Endpoint: "https://s3.us-east-1.amazonaws.com", Region: "us-east-1", Bucket: "fyisp-probe"},
			{Name: "Backups (R2)", Endpoint: "https://0123456789abcdef.r2.cloudflarestorage.com", Region: "auto", Bucket: "fyisp-probe", Addressing: "path"},
			{Name: "NAS", Endpoint: "http://nas.lan:9000", Bucket: "probe", Addressing: "path"},
			{Name: "Flaky edge", Endpoint: "https://down.storage.example"},
			{Name: "Wrong keys", Endpoint: "https://auth.storage.example", Bucket: "probe"},
		} {
			ts, err := s.Add(ctx, sp)
			if err != nil {
				log.Error("storage target", "name", sp.Name, "err", err)
				continue
			}
			ids = append(ids, ts.ID)
		}
	}
	if scenario == storageHistory {
		if err := pastSchedule(ctx, db, ids, time.Now()); err != nil {
			log.Error("storage history", "err", err)
		}
	}
	c := storage.NewCampaigns(storage.CampaignOptions{Store: db, Queue: q, Engine: e, Targets: s, Log: log,
		// The line: a little loss at the provider's first router in the evening.
		Line: func(from, to time.Time) (*model.LoadStats, *model.LoadStats) {
			edge := &model.LoadStats{N: 60, P50Ms: 9.5}
			if h := from.Hour(); h >= 19 && h < 23 {
				edge.Lost, edge.Loss = 2, 2.0/60
			}
			return &model.LoadStats{N: 60, P50Ms: 1.2}, edge
		},
		Trace: func(ctx context.Context, ip netip.Addr) ([]model.StorageHop, uint32, bool) {
			select {
			case <-ctx.Done():
				return nil, 0, false
			case <-time.After(2 * time.Second):
			}
			return []model.StorageHop{{IP: "192.168.1.1"}, {IP: "100.64.0.1"}, {IP: "203.0.113.9", ASN: 64500, Owner: "EXAMPLE-ISP"},
				{IP: ""}, {IP: "198.51.100.20", ASN: 64510, Owner: "EXAMPLE-TRANSIT"}, {IP: ip.String(), ASN: 64520, Owner: "EXAMPLE-STORAGE"}}, 64510, true
		}})
	done := make(chan struct{})
	c.Restore(ctx) // before the API serves, as fyisp does
	go func() { defer close(done); c.Run(ctx) }()
	// After ctx ends: the campaigns stop before the database closes (a
	// round in the queue is saved by the scheduler that runs it).
	return s, c, db, func() { <-done; db.Close(); os.RemoveAll(dir) }
}

// fakeAttempt answers one request after a short wait, as a server would:
// an unsigned request for a bucket is refused (403), as by a real one;
// keys sent to a host with "auth" in it are refused.
func fakeAttempt(ctx context.Context, t s3test.Target, phase, op string, signed bool) model.StorageAttempt {
	f := func(v float64) *float64 { return &v }
	a := model.StorageAttempt{Phase: phase, Op: op, At: time.Now(), DNSms: f(3.2)}
	if h := t.Endpoint.Hostname(); !strings.HasSuffix(h, ".lan") && !strings.HasSuffix(h, ".local") {
		// The provider's address as a public target's round keeps it.
		a.EdgeIP = netip.MustParseAddr("52.216.10.10")
		if strings.Contains(h, "down") {
			a.EdgeIP = netip.MustParseAddr("52.216.20.20")
		}
		a.EdgePrefix = edgePrefixOf(a.EdgeIP)
	}
	select {
	case <-ctx.Done():
		return model.StorageAttempt{} // cut off: no class
	case <-time.After(60 * time.Millisecond):
	}
	switch {
	case strings.Contains(t.Endpoint.Hostname(), "down"):
		a.Class, a.Totalms = string(s3test.ClassConnectTimeout), f(5003)
		return a
	case strings.Contains(t.Endpoint.Hostname(), "auth") && signed:
		a.Class, a.HTTPStatus, a.S3Code = string(s3test.ClassHTTPAuth), 403, "SignatureDoesNotMatch"
	case t.Bucket != "" && !signed:
		a.Class, a.HTTPStatus = string(s3test.ClassOK), 403
	default:
		a.Class, a.HTTPStatus = string(s3test.ClassOK), 200
	}
	a.Connectms, a.TTFBms, a.Totalms = f(11.4), f(31.0), f(48.6)
	if phase == model.StoragePhaseDown || (phase == model.StoragePhaseUp && op == "put") {
		// A line that is slower in the evening, as many are.
		bps := 9e7
		if h := time.Now().Hour(); h >= 19 && h < 23 {
			bps = 4e7
		}
		a.GoodputBps, a.Stallms, a.Totalms = f(bps), f(120), f(850)
	}
	if t.Endpoint.Scheme == "https" {
		a.TLSms, a.TLSVersion = f(23.1), "1.3"
	}
	return a
}

// pastSchedule stores a recurring campaign that ended: rounds every 5
// minutes from 19:00 to 23:00 on the 6 weekday evenings before now, in
// UTC. AWS Virginia, the R2 bucket and the NAS had keys; the
// flaky edge only got connection checks. The values are fixed (no clock or
// randomness), so a spec can count on them:
//   - AWS Virginia: first byte about 185 ms, 62 Mbit/s down and 38 up; on the
//     fifth evening, 21:00 to 21:35, the line was slow (every public target
//     takes 2.6 times as long, transfers half the speed);
//   - R2: 140 ms, 48 and 30 Mbit/s; on the fourth evening, 20:10 to 20:45,
//     3 of 4 rounds' fresh connections failed (timeouts);
//   - NAS: 2 ms, 880 and 860 Mbit/s;
//   - the flaky edge: 95 ms, no transfers;
//   - fyisp wasn't running on the second evening from 21:30 to 22:10 (a gap).
func pastSchedule(ctx context.Context, db *store.SQLite, ids []string, now time.Time) error {
	now = now.UTC() // a zone with a name, the same on every machine
	if len(ids) < 4 {
		return nil
	}
	// A public target is over HTTPS; the NAS, on the LAN, over plain HTTP.
	type tg struct {
		id, name, mode string
		ttfb, down, up float64
		public         bool
	}
	ts := []tg{
		{ids[0], "AWS Virginia", "bucket", 185, 62e6, 38e6, true},
		{ids[1], "Backups (R2)", "bucket", 140, 48e6, 30e6, true},
		{ids[2], "NAS", "bucket", 2, 880e6, 860e6, false},
		{ids[3], "Flaky edge", "conn", 95, 0, 0, true},
	}
	f := func(v float64) *float64 { return &v }
	// The 6 weekday evenings before today.
	var days []time.Time
	for d := now.AddDate(0, 0, -1); len(days) < 6; d = d.AddDate(0, 0, -1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			days = append([]time.Time{time.Date(d.Year(), d.Month(), d.Day(), 19, 0, 0, 0, d.Location())}, days...)
		}
	}
	c := &model.StorageCampaign{Created: days[0].Add(-time.Hour), Updated: days[5].Add(4 * time.Hour), Shape: model.StorageRecurring,
		State: model.StorageDone, Stopped: model.StopUntil, Cold: 4, Warm: 2, Down: 64 << 20, Up: 16 << 20, Interval: 5 * time.Minute,
		Days: 0b0011111, FromMin: 19 * 60, ToMin: 23 * 60, TZ: now.Location().String(), Until: days[5].Add(4 * time.Hour), End: days[5].Add(4 * time.Hour)}
	for _, t := range ts {
		c.Targets = append(c.Targets, model.StorageCampaignTarget{ID: t.id, Name: t.name, Mode: t.mode})
	}
	if err := db.SaveStorageCampaign(ctx, c); err != nil {
		return err
	}
	seq := 0
	for wi, day := range days {
		for k := range 48 {
			due := day.Add(time.Duration(k) * 5 * time.Minute)
			min := k * 5
			if wi == 1 && min >= 150 && min < 190 {
				if min == 150 {
					gap := &model.StorageRound{Campaign: c.ID, Due: due, End: due.Add(40 * time.Minute), Outcome: model.RoundGap}
					if err := db.SaveStorageRound(ctx, gap, nil); err != nil {
						return err
					}
				}
				continue
			}
			s := seq
			seq++
			slow := wi == 4 && min >= 120 && min < 155
			r := &model.StorageRound{Campaign: c.ID, Seq: &s, Due: due, Start: due.Add(2 * time.Second), End: due.Add(40 * time.Second), Outcome: model.RoundOK}
			var as []model.StorageAttempt
			wob := float64(k%7-3) / 30 // a little up and down, the same every run
			for _, t := range ts {
				lf := 1.0
				if slow && t.public {
					lf = 2.6
				}
				r2fail := t.name == "Backups (R2)" && wi == 3 && min >= 70 && min < 105 && k%4 != 0
				for n := range c.Cold {
					a := model.StorageAttempt{Target: t.id, Phase: model.StoragePhaseCold, Op: "head", N: n, At: r.Start.Add(time.Duration(n) * time.Second)}
					if t.mode == "bucket" {
						a.Op = "get"
					}
					if r2fail {
						a.Class, a.Totalms = string(s3test.ClassConnectTimeout), f(5003)
						r.Outcome = model.RoundFailed
					} else {
						ttfb := t.ttfb * lf * (1 + wob + float64(n)/50)
						a.Class, a.HTTPStatus = string(s3test.ClassOK), 200
						a.DNSms, a.Connectms, a.TTFBms, a.Totalms = f(ttfb*0.08), f(ttfb*0.3), f(ttfb), f(ttfb*1.2)
						if t.public {
							a.TLSms = f(ttfb * 0.34)
						}
					}
					as = append(as, a)
				}
				if t.mode == "bucket" && !r2fail {
					at := r.Start.Add(10 * time.Second)
					as = append(as,
						model.StorageAttempt{Target: t.id, Phase: model.StoragePhaseDown, Op: "get", At: at, Class: string(s3test.ClassOK), HTTPStatus: 200,
							GoodputBps: f(t.down / lf * (1 - wob/2)), Bytes: c.Down},
						model.StorageAttempt{Target: t.id, Phase: model.StoragePhaseUp, Op: "put", At: at, Class: string(s3test.ClassOK), HTTPStatus: 200,
							GoodputBps: f(t.up / lf * (1 - wob/2)), Bytes: c.Up})
					r.BytesDown, r.BytesUp = r.BytesDown+c.Down, r.BytesUp+c.Up
				}
			}
			if err := db.SaveStorageRound(ctx, r, as); err != nil {
				return err
			}
		}
	}
	return nil
}

func edgePrefixOf(a netip.Addr) string { p, _ := a.Prefix(24); return p.String() }

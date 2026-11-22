package trace

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/netinfo/hops"
)

// testClassify: private addresses are your network, the rest answered.
func testClassify(hs []netip.Addr) []model.SurveyClass {
	out := make([]model.SurveyClass, len(hs))
	for i, h := range hs {
		switch {
		case !h.IsValid():
		case h.IsPrivate():
			out[i] = model.SurveyClass{Status: model.HopYours}
		default:
			out[i] = model.SurveyClass{Status: model.HopAnswered, IP: h, ASN: 64500}
		}
	}
	return out
}

// flowNet is fakeNet with a FlowProber: it records each probe's source port.
type flowNet struct {
	*fakeNet
	mu    sync.Mutex
	ports []uint16
	busy  map[uint16]bool // ports that fail to bind
	nat   int             // from this TTL on, a fixed flow's probes go unanswered (0: never)
	lose  int             // this TTL's first fixed-flow probe goes unanswered (0: none)
	wait  func()          // called for each unanswered fixed-flow probe (its timeout)
}

func (f *flowNet) ProbeFlow(ctx context.Context, dst netip.AddrPort, sport uint16, ttl int, timeout time.Duration) (hops.Hop, error) {
	f.mu.Lock()
	if f.busy[sport] {
		f.mu.Unlock()
		return hops.Hop{}, hops.ErrPortInUse
	}
	f.ports = append(f.ports, sport)
	f.mu.Unlock()
	if f.lose > 0 && ttl == f.lose {
		f.lose = 0
		if f.wait != nil {
			f.wait()
		}
		return hops.Hop{}, nil
	}
	if f.nat > 0 && ttl >= f.nat {
		if f.wait != nil {
			f.wait()
		}
		return hops.Hop{}, nil
	}
	return f.fakeNet.ProbeTransport(ctx, hops.TCP, dst, ttl, timeout)
}

func surveyTargetFor(t model.Target) *surveyTarget { return newSurveyTarget(t, "tcp") }

// TestSurveyRound: a round probes the TCP host on the target's own port,
// one TTL at a time, and records every TTL classified; the hour adds up
// rounds. It writes no samples or routes and leaves the generation alone.
func TestSurveyRound(t *testing.T) {
	f := newFakeNet("192.168.1.1", "62.1.1.1", "", "62.2.2.2")
	tr, _, sink := roundTracer(t, f, Options{})
	sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
	tt := surveyTargetFor(model.Target{Name: "svc", Host: "198.51.100.1", Port: 8443,
		HostOverrides: map[model.ProbeKind]string{model.KindICMP: "198.51.100.99"}})
	var h model.SurveyHour
	for range 2 {
		var ok bool
		if h, ok = tr.surveyRound(context.Background(), sv, tt); !ok {
			t.Fatal("no round")
		}
	}
	if len(h.Paths) != 1 {
		t.Fatalf("paths %+v", h.Paths)
	}
	p := h.Paths[0]
	if p.Dest != a("198.51.100.1") || p.Rounds != 2 || p.Reached != 2 || p.Complete != 2 || p.Fixed != 0 {
		t.Errorf("path %+v", p)
	}
	want := []struct {
		ttl    int
		status string
		ip     string
	}{{1, model.HopYours, ""}, {2, model.HopAnswered, "62.1.1.1"}, {3, model.HopSilent, ""}, {4, model.HopAnswered, "62.2.2.2"}, {5, model.HopAnswered, "198.51.100.1"}}
	if len(p.Hops) != len(want) {
		t.Fatalf("hops %+v", p.Hops)
	}
	for i, w := range want {
		g := p.Hops[i]
		if g.TTL != w.ttl || g.Status != w.status || (w.ip != "" && g.IP != a(w.ip)) || (w.ip == "" && g.IP.IsValid()) || g.N != 2 {
			t.Errorf("hop %d = %+v, want %+v n=2", i, g, w)
		}
	}
	for _, r := range f.log {
		if r.tp != hops.TCP || r.port != 8443 || r.dst != a("198.51.100.1") {
			t.Errorf("probe %+v: want TCP to the TCP host on port 8443", r)
		}
	}
	if smp, routes, changes := sink.take(); len(smp)+len(routes)+len(changes) != 0 || tr.gen.Load() != 0 {
		t.Errorf("survey wrote samples %d routes %d changes %d, gen %d", len(smp), len(routes), len(changes), tr.gen.Load())
	}
}

// TestSurveyFixedFlow: every probe of every round goes from the target's
// one source port; a port in use falls back to ephemeral ports for that
// round (not counted as fixed).
func TestSurveyFixedFlow(t *testing.T) {
	f := &flowNet{fakeNet: newFakeNet("62.1.1.1", "62.2.2.2"), busy: map[uint16]bool{}}
	tr, _, _ := roundTracer(t, f.fakeNet, Options{})
	tr.prober = f
	sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
	tt := surveyTargetFor(model.Target{Name: "svc", Host: "198.51.100.1"})
	tr.surveyRound(context.Background(), sv, tt)
	tr.surveyRound(context.Background(), sv, tt)
	f.busy[tt.sport] = true
	h, _ := tr.surveyRound(context.Background(), sv, tt)
	if len(f.ports) != 6 {
		t.Fatalf("fixed-port probes %v, want 6 (2 rounds of 3 TTLs)", f.ports)
	}
	for _, p := range f.ports {
		if p != tt.sport {
			t.Errorf("port %d, want %d", p, tt.sport)
		}
	}
	if p := h.Paths[0]; p.Rounds != 3 || p.Fixed != 2 || p.Reached != 3 {
		t.Errorf("path %+v, want 3 rounds, 2 fixed", p)
	}
}

// TestSurveyFlowBehindNAT: past a NAT that drops a flow's repeated SYNs,
// two silent TTLs in a row are asked again from fresh ports, under the
// default router cap (once each bucket is full again), and the round goes
// on from fresh ports (not counted as fixed). A silent router right past
// the NAT doesn't stop that; one lost answer, or routers that answer
// neither way, keep the flow.
func TestSurveyFlowBehindNAT(t *testing.T) {
	type tc struct {
		name    string
		routers []string
		nat     int
		lose    int // a fixed-flow TTL whose answer is lost once
		busyTTL int // a TTL whose bucket another trace has just used (the survey must skip it)
		want    []string
		fixed   int
		reached int
	}
	for _, c := range []tc{
		{"nat", []string{"62.1.1.1", "62.2.2.2", "62.3.3.3"}, 2, 0, 0, []string{"1:62.1.1.1", "2:62.2.2.2", "3:62.3.3.3", "4:198.51.100.1"}, 0, 1},
		{"silent router past nat", []string{"62.1.1.1", "", "62.3.3.3"}, 2, 0, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:62.3.3.3", "4:198.51.100.1"}, 0, 1},
		{"one lost answer", []string{"62.1.1.1", "62.2.2.2", "62.3.3.3"}, 0, 2, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:62.3.3.3", "4:198.51.100.1"}, 1, 1},
		{"silent routers", []string{"62.1.1.1", "", "", "62.4.4.4"}, 0, 0, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:invalid IP", "4:62.4.4.4", "5:198.51.100.1"}, 1, 1},
		{"silent routers before nat", []string{"62.1.1.1", "", "", "62.4.4.4", "62.5.5.5", "62.6.6.6"}, 6, 0, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:invalid IP", "4:62.4.4.4", "5:62.5.5.5", "6:62.6.6.6", "7:198.51.100.1"}, 0, 1},
		// a silent destination: its silent tail is asked again twice at most (4 more SYNs)
		{"silent destination", []string{"62.1.1.1", "62.2.2.2"}, 0, 0, 0, nil, 1, 0},
		{"two silent routers past nat", []string{"62.1.1.1", "", "", "62.4.4.4", "62.5.5.5"}, 2, 0, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:invalid IP", "4:62.4.4.4", "5:62.5.5.5", "6:198.51.100.1"}, 0, 1},
		{"silent pair right before nat", []string{"62.1.1.1", "", "", "62.4.4.4", "62.5.5.5", "62.6.6.6"}, 4, 0, 0, []string{"1:62.1.1.1", "2:invalid IP", "3:invalid IP", "4:62.4.4.4", "5:62.5.5.5", "6:62.6.6.6", "7:198.51.100.1"}, 0, 1},
		// TTL 3 is left out (its bucket isn't full): TTLs 2 and 4 are still a silent run, asked again
		// under their own keys, and TTL 3 isn't probed
		{"capped ttl between", []string{"62.1.1.1", "62.2.2.2", "62.3.3.3", "62.4.4.4"}, 2, 0, 3, []string{"1:62.1.1.1", "2:62.2.2.2", "4:62.4.4.4", "5:198.51.100.1"}, 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// a silent TCP probe waits 900 ms (errqTransport's cap), less than a token's refill
				f := &flowNet{fakeNet: newFakeNet(c.routers...), busy: map[uint16]bool{}, nat: c.nat, lose: c.lose,
					wait: func() { time.Sleep(900 * time.Millisecond) }}
				tr, _, _ := roundTracer(t, f.fakeNet, Options{RouterRate: DefaultRouterRate, Now: time.Now})
				tr.prober = f
				if c.busyTTL > 0 {
					tr.lim.take(limitKey{ttl: c.busyTTL}, time.Now())
				}
				if c.want == nil { // the destination never answers
					for ttl := len(c.routers) + 1; ttl <= surveyMaxHops; ttl++ {
						f.silent[ttl] = true
					}
				}
				sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
				h, _ := tr.surveyRound(context.Background(), sv, surveyTargetFor(model.Target{Name: "svc", Host: "198.51.100.1"}))
				p := h.Paths[0]
				var got []string
				for _, hp := range p.Hops {
					got = append(got, fmt.Sprintf("%d:%s", hp.TTL, hp.IP))
				}
				if c.want != nil && !slices.Equal(got, c.want) || p.Fixed != c.fixed || p.Reached != c.reached || p.Rounds != 1 {
					t.Errorf("hops %v fixed %d reached %d, want %v fixed %d reached %d", got, p.Fixed, p.Reached, c.want, c.fixed, c.reached)
				}
				if c.name == "silent destination" {
					if n := len(f.log); n != 2+maxUnknown+4 {
						t.Errorf("%d probes, want %d (2 routers, %d silent TTLs, 2 retries of 2)", n, 2+maxUnknown+4, maxUnknown)
					}
				}
				if c.busyTTL > 0 {
					for _, r := range f.log {
						if r.ttl == c.busyTTL {
							t.Errorf("TTL %d probed despite the cap", c.busyTTL)
						}
					}
				}
			})
		})
	}
}

// TestSurveyYieldsAtCap: a survey probe needs a full bucket. A router
// another trace just probed takes this survey's answer from it within
// surveyReuse; after that the TTL is not measured (not silent) that round.
func TestSurveyYieldsAtCap(t *testing.T) {
	f := newFakeNet("62.1.1.1", "62.2.2.2")
	now := time.Unix(1_800_000_000, 0)
	tr, _, _ := roundTracer(t, f, Options{RouterRate: 1, Now: func() time.Time { return now }})
	sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
	tt := surveyTargetFor(model.Target{Name: "svc", Host: "198.51.100.1"})
	tr.surveyRound(context.Background(), sv, tt) // learns the route
	probes := len(f.log)
	tr.lim.take(limitKey{addr: a("62.1.1.1")}, now)
	h, _ := tr.surveyRound(context.Background(), sv, tt)
	if p := h.Paths[0]; p.Rounds != 2 || p.Complete != 2 || p.Hops[0].N != 2 || len(f.log)-probes != 1 {
		// TTL 2's bucket hasn't refilled either (no time passed): only the
		// destination, which the cap exempts, is probed.
		t.Errorf("recent answer: path %+v, %d probes; want TTLs 1-2 reused (2 rounds complete, n=2, 1 probe)", p, len(f.log)-probes)
	}
	now = now.Add(surveyReuse)
	tr.lim.take(limitKey{addr: a("62.1.1.1")}, now)
	tr.lim.take(limitKey{addr: a("62.1.1.1")}, now)
	h, _ = tr.surveyRound(context.Background(), sv, tt)
	p := h.Paths[0]
	if p.Rounds != 3 || p.Complete != 2 || p.Hops[0].TTL != 1 || p.Hops[0].N != 2 || p.Hops[0].Status != model.HopAnswered {
		t.Errorf("stale answer: path %+v, want 3 rounds, 2 complete, TTL 1 not measured in the third", p)
	}
}

// TestSurveyPacing: 1,300 targets every 15 minutes with the real router cap
// next to an always-on ICMP trace. Every target has a round in the first
// interval and one per interval after it, and the always-on trace keeps
// measuring the shared first hop.
func TestSurveyPacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeNet("192.168.1.1", "62.1.1.1", "62.2.2.2")
		var mu sync.Mutex
		rounds := map[string]int{}
		sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(h model.SurveyHour) {
			mu.Lock()
			rounds[h.Target]++
			mu.Unlock()
		}})
		p := &model.Profile{Name: "big", Targets: []model.Target{{Name: "always", Host: "1.1.1.1", Trace: true, Kinds: []model.ProbeKind{model.KindICMP}}}}
		const n = 1300
		for i := range n {
			p.Targets = append(p.Targets, model.Target{Name: fmt.Sprintf("t%d", i), Host: fmt.Sprintf("198.51.%d.%d", i/250, i%250+1)})
		}
		o := Options{Survey: sv, RouterRate: 1, Timeout: 900 * time.Millisecond,
			Resolve: func(_ context.Context, h string) ([]netip.Addr, error) { return []netip.Addr{a(h)}, nil }}
		_, sink := runTracer(t, f, o, p)
		time.Sleep(SurveyInterval + time.Minute)
		mu.Lock()
		missing := 0
		for i := range n {
			if rounds[fmt.Sprintf("t%d", i)] == 0 {
				missing++
			}
		}
		mu.Unlock()
		if missing > 0 {
			t.Errorf("%d of %d targets without a round after one interval", missing, n)
		}
		if st := sv.Status(); st.State != SurveyOn || st.Covered != n || st.Targets != n {
			t.Errorf("status %+v", st)
		}
		lost := 0
		smp := sink.samplesOf("always")
		for _, s := range smp {
			if s.Key.Hop == 1 && s.Lost {
				lost++
			}
		}
		if len(smp) == 0 || lost > 0 {
			t.Errorf("always-on trace: %d samples, %d hop-1 losses", len(smp), lost)
		}
		time.Sleep(SurveyInterval)
		mu.Lock()
		for i := range n {
			if c := rounds[fmt.Sprintf("t%d", i)]; c < 2 || c > 3 {
				t.Errorf("t%d: %d rounds in two intervals", i, c)
				break
			}
		}
		mu.Unlock()
	})
}

// TestSurveySilentDestination: a destination that never answers doesn't
// make each round go one TTL deeper than the last.
func TestSurveySilentDestination(t *testing.T) {
	f := newFakeNet("62.1.1.1", "62.2.2.2")
	f.silent = map[int]bool{}
	for ttl := 3; ttl <= surveyMaxHops; ttl++ {
		f.silent[ttl] = true
	}
	tr, _, _ := roundTracer(t, f, Options{})
	sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
	tt := surveyTargetFor(model.Target{Name: "svc", Host: "198.51.100.1"})
	for i := range 6 {
		before := len(f.log)
		h, ok := tr.surveyRound(context.Background(), sv, tt)
		if n := len(f.log) - before; !ok || n != 2+maxUnknown {
			t.Fatalf("round %d: %d probes, want %d", i+1, n, 2+maxUnknown)
		}
		if p := h.Paths[0]; p.Reached != 0 {
			t.Errorf("reached %d", p.Reached)
		}
	}
}

// TestSurveyDestinationPrivacy: a target on this network is not traced; one
// in the provider's network is traced, but its address is not kept.
func TestSurveyDestinationPrivacy(t *testing.T) {
	f := newFakeNet("62.1.1.1")
	tr, _, _ := roundTracer(t, f, Options{})
	provider := a("203.0.113.50")
	sv := NewSurvey(SurveyOptions{Sink: func(model.SurveyHour) {}, Classify: func(hs []netip.Addr) []model.SurveyClass {
		out := testClassify(hs)
		for i, h := range hs {
			if h == provider {
				out[i] = model.SurveyClass{Status: model.HopProvider, ASN: 64500}
			}
		}
		return out
	}})
	if _, ok := tr.surveyRound(context.Background(), sv, surveyTargetFor(model.Target{Name: "nas", Host: "192.168.1.20"})); ok {
		t.Error("a LAN target was traced")
	}
	h, ok := tr.surveyRound(context.Background(), sv, surveyTargetFor(model.Target{Name: "home", Host: provider.String()}))
	if !ok || len(h.Paths) != 1 || h.Paths[0].Dest.IsValid() {
		t.Fatalf("provider target: %+v, want one path without a destination address", h)
	}
	for _, hp := range h.Paths[0].Hops {
		if hp.IP == provider {
			t.Errorf("hop %+v keeps the provider address", hp)
		}
	}
}

// TestSurveyTargets: TCP and HTTPS targets only; not the gateway, the ISP
// edge, UDP-only or ICMP-only targets.
func TestSurveyTargets(t *testing.T) {
	p := &model.Profile{Targets: []model.Target{
		{Name: "tcp", Host: "a.example"},
		{Name: "https", Host: "b.example", Kinds: []model.ProbeKind{model.KindHTTPS}},
		{Name: "icmp", Host: "c.example", Kinds: []model.ProbeKind{model.KindICMP}},
		{Name: "stun", Host: "d.example", Port: 3478, Kinds: []model.ProbeKind{model.KindUDP}},
		{Name: "gw", Host: model.HostGateway, Kinds: []model.ProbeKind{model.KindICMP}, Layer: model.LayerGateway},
		{Name: "anycast", Host: "1.1.1.1", Kinds: []model.ProbeKind{model.KindTCP}, Layer: model.LayerAnycast},
	}}
	var got []string
	for _, x := range SurveyTargets(p) {
		got = append(got, x.Name)
	}
	if !slices.Equal(got, []string{"tcp", "https"}) {
		t.Errorf("survey targets %v", got)
	}
}

// TestSurveyStatus: unavailable when the tracer can't start; first_pass
// until every target was tried, a name that never resolves included.
func TestSurveyStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sv := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
		f := newFakeNet("62.1.1.1")
		p := &model.Profile{Targets: []model.Target{{Name: "ok", Host: "198.51.100.1"}, {Name: "nx", Host: "nx.example"}}}
		o := Options{Survey: sv, Resolve: func(_ context.Context, h string) ([]netip.Addr, error) {
			return nil, errors.New("no such host")
		}}
		runTracer(t, f, o, p)
		if st := sv.Status(); st.State != SurveyFirstPass || st.Targets != 2 {
			t.Errorf("at start: %+v", st)
		}
		time.Sleep(SurveyInterval + time.Minute)
		if st := sv.Status(); st.State != SurveyOn || st.Covered != 2 {
			t.Errorf("after one interval: %+v, want on with both tried", st)
		}

		bad := NewSurvey(SurveyOptions{Classify: testClassify, Sink: func(model.SurveyHour) {}})
		tr := newTracer(Options{Survey: bad, Log: quiet(), open: func() (hops.Prober, error) { return nil, errors.New("no ICMP") }})
		if err := tr.Run(context.Background(), p, &recSink{}); err == nil || bad.Status().State != SurveyUnavailable {
			t.Errorf("no ICMP socket: %v, %+v", err, bad.Status())
		}
	})
}

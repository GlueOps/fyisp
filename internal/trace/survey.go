package trace

import (
	"context"
	"errors"
	"hash/fnv"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/netinfo/hops"
)

// The path survey (Options.Survey) traces the TCP path to every TCP and
// HTTPS target of the profile once per Interval, to compare this site with
// another (`fyisp export --survey`). It is separate from the always-on and
// Investigate traces: it keeps no route, writes no per-hop series, never
// bumps the route-change generation and never shares hops, so it cannot
// change what they measure. It only shares the prober and the per-router
// cap, where it yields: a survey probe takes a router's token only when
// the bucket is full, so one is always left for the other traces.
//
// A round probes one TTL at a time towards the target's own TCP host and
// port, on Linux from one source port per target (the same in every
// round: one flow, see hops.FlowProber), and stops at the destination, at
// an unreachable, after maxUnknown silent TTLs past the last that answered,
// or at MaxHops. A capped TTL is not measured (never loss). Each round is
// added to the target's hour (model.SurveyHour), which goes to Sink after
// every round, and classified first (SurveyOptions.Classify) so no address
// of the user's own network or provider is kept.

// Survey defaults.
const (
	DefaultSurveyInterval = 15 * time.Minute
	DefaultSurveyInFlight = 24
	DefaultSurveyMaxHops  = 30
)

// Source ports of survey flows: below Linux's default ephemeral range
// (32768-60999), so they rarely meet a socket of fyisp's own.
const (
	surveyPortBase  = 20000
	surveyPortRange = 12000
)

// SurveyOptions configures the path survey. Classify and Sink are
// required.
type SurveyOptions struct {
	Interval time.Duration // one round per target per Interval (default 15m)
	InFlight int           // targets traced at once (default 24)
	MaxHops  int           // highest TTL (default 30)
	// Classify says how each hop of a round is recorded: hops[i] is the
	// router that answered TTL i+1 (invalid: none, or not measured), the
	// last one the destination when it answered.
	Classify func(hops []netip.Addr) []model.SurveyClass
	// Sink receives a target's hour so far after every round.
	Sink func(model.SurveyHour)
}

// Survey is the path survey's state: create it with NewSurvey, pass it in
// Options.Survey, and read Status while the tracer runs.
type Survey struct {
	o SurveyOptions

	mu      sync.Mutex
	state   string // SurveyOff until the tracer runs it
	targets int
	covered map[string]bool // targets with a round since the start
	last    time.Time       // end of the newest round
}

// Survey states (SurveyStatus.State).
const (
	SurveyOff         = "off"         // not started (or the tracer is not running)
	SurveyUnavailable = "unavailable" // the tracer could not start (no ICMP socket)
	SurveyFirstPass   = "first_pass"  // some targets have no round yet
	SurveyOn          = "on"
)

// SurveyStatus is what api/status shows of the survey.
type SurveyStatus struct {
	State     string    `json:"state"`
	Targets   int       `json:"targets"`
	Covered   int       `json:"covered"` // targets with at least one round since the start
	Interval  int64     `json:"interval_s"`
	LastRound time.Time `json:"last_round,omitzero"`
}

// NewSurvey returns a survey with o's defaults filled in.
func NewSurvey(o SurveyOptions) *Survey {
	if o.Interval <= 0 {
		o.Interval = DefaultSurveyInterval
	}
	if o.InFlight <= 0 {
		o.InFlight = DefaultSurveyInFlight
	}
	if o.MaxHops <= 0 {
		o.MaxHops = DefaultSurveyMaxHops
	}
	o.MaxHops = min(o.MaxHops, 255)
	return &Survey{o: o, state: SurveyOff, covered: map[string]bool{}}
}

// Status reports the survey's progress.
func (sv *Survey) Status() SurveyStatus {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	st := SurveyStatus{State: sv.state, Targets: sv.targets, Covered: len(sv.covered), Interval: int64(sv.o.Interval / time.Second), LastRound: sv.last}
	if st.State == SurveyOn && st.Covered < st.Targets {
		st.State = SurveyFirstPass
	}
	return st
}

// Unavailable marks the survey as unable to run (the tracer failed to start).
func (sv *Survey) Unavailable() {
	sv.mu.Lock()
	sv.state = SurveyUnavailable
	sv.mu.Unlock()
}

// SurveyTargets returns the targets the survey traces: every target with
// a TCP or HTTPS kind, except the gateway and the ISP edge.
func SurveyTargets(p *model.Profile) []model.Target {
	var out []model.Target
	for _, t := range p.Targets {
		host := t.HostFor(model.KindTCP)
		if isSpecial(host) || t.Layer != "" {
			continue
		}
		for _, k := range t.ProbeKinds() {
			if k == model.KindTCP || k == model.KindHTTPS {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// surveyTarget is one target's survey state, owned by the survey loop
// (one round of a target at a time).
type surveyTarget struct {
	st    *target // for dest: the TCP host's sticky resolution
	port  int
	sport uint16
	route []netip.Addr // the last round's routers, for the cap's keys
	hour  model.SurveyHour
	busy  atomic.Bool // a round runs
}

func surveyPort(name string) uint16 {
	h := fnv.New32a()
	h.Write([]byte(name))
	return uint16(surveyPortBase + h.Sum32()%surveyPortRange)
}

// runSurvey traces every target of ts once per Interval, spread evenly
// over it, until ctx ends.
func (tr *tracer) runSurvey(ctx context.Context, sv *Survey, ts []model.Target) {
	via := "tcp"
	if tr.o.support().TCPViaICMP {
		via = "icmp"
	}
	targets := make([]*surveyTarget, 0, len(ts))
	for _, t := range ts {
		port := t.Port
		if port == 0 {
			port = 443
		}
		targets = append(targets, &surveyTarget{
			st:    &target{t: t, mode: model.TraceTCP, host: t.HostFor(model.KindTCP)},
			port:  port,
			sport: surveyPort(t.Name),
			hour:  model.SurveyHour{Target: t.Name, Host: t.HostFor(model.KindTCP), Port: port, HopsVia: via},
		})
	}
	sv.mu.Lock()
	sv.state, sv.targets = SurveyOn, len(targets)
	sv.mu.Unlock()
	defer func() {
		sv.mu.Lock()
		sv.state = SurveyOff
		sv.mu.Unlock()
	}()
	tr.o.Log.Info("path survey", "targets", len(targets), "interval", sv.o.Interval, "in_flight", sv.o.InFlight,
		"max_hops", sv.o.MaxHops, "probes_per_s_estimate", float64(len(targets))*16/sv.o.Interval.Seconds())
	if len(targets) == 0 {
		<-ctx.Done()
		return
	}

	sem := make(chan struct{}, sv.o.InFlight)
	var wg sync.WaitGroup
	defer wg.Wait()
	step := sv.o.Interval / time.Duration(len(targets))
	// Rounds start at multiples of step since the epoch, target i at slot
	// i of each pass, so a restart keeps the spacing.
	next := tr.o.Now().Truncate(step).Add(step)
	i := int(next.UnixNano()/int64(step)) % len(targets)
	for {
		if !sleepUntil(ctx, tr.o.Now, next) {
			return
		}
		next = next.Add(step)
		t := targets[i]
		i = (i + 1) % len(targets)
		if t.busy.Load() { // its last round has not ended: skip this one
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		t.busy.Store(true)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			h, ok := tr.surveyRound(ctx, sv, t)
			t.busy.Store(false)
			if !ok {
				return
			}
			sv.o.Sink(h)
			sv.mu.Lock()
			sv.covered[t.st.t.Name] = true
			sv.last = tr.o.Now()
			sv.mu.Unlock()
		}()
	}
}

func sleepUntil(ctx context.Context, now func() time.Time, at time.Time) bool {
	d := at.Sub(now())
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// surveyRound traces t once and returns its hour so far (ok false: no
// round, e.g. its name did not resolve).
func (tr *tracer) surveyRound(ctx context.Context, sv *Survey, t *surveyTarget) (model.SurveyHour, bool) {
	dst, err := tr.dest(ctx, t.st)
	if err != nil || !dst.Is4() {
		tr.o.Log.Debug("path survey: no address", "target", t.st.t.Name, "err", err)
		return model.SurveyHour{}, false
	}
	tr.mu.Lock()
	pr := tr.prober
	tr.mu.Unlock()
	fp, fixed := pr.(hops.FlowProber)
	tp, _ := pr.(hops.TransportProber)
	if tp == nil && !fixed {
		return model.SurveyHour{}, false
	}
	to := netip.AddrPortFrom(dst, uint16(t.port))

	var (
		answered []netip.Addr // by TTL-1: the router (or destination) that answered
		rtts     []time.Duration
		probed   []bool
		complete = true
		reached  = false
		unknown  = 0
		last     = 0 // highest TTL that answered
	)
	for ttl := 1; ttl <= sv.o.MaxHops && ctx.Err() == nil; ttl++ {
		var expect netip.Addr
		if ttl-1 < len(t.route) {
			expect = t.route[ttl-1]
		}
		if !expect.IsValid() {
			expect = tr.lim.predict(ttl)
		}
		k := limitKey{ttl: ttl}
		if expect.IsValid() {
			k = limitKey{addr: expect}
		}
		now := tr.o.Now()
		var keys []limitKey
		if expect != dst {
			var ok bool
			if keys, ok = tr.lim.takeSpare(k, now); !ok {
				answered, rtts, probed = append(answered, netip.Addr{}), append(rtts, 0), append(probed, false)
				complete = false
				continue
			}
		}
		var h hops.Hop
		if fixed {
			h, err = fp.ProbeFlow(ctx, to, t.sport, ttl, tr.o.Timeout)
			if errors.Is(err, hops.ErrPortInUse) {
				fixed = false // this round goes on from ephemeral ports
				h, err = tp.ProbeTransport(ctx, hops.TCP, to, ttl, tr.o.Timeout)
			}
		} else {
			h, err = tp.ProbeTransport(ctx, hops.TCP, to, ttl, tr.o.Timeout)
		}
		if err != nil {
			tr.o.Log.Debug("path survey: probe failed", "target", t.st.t.Name, "ttl", ttl, "err", err)
			return model.SurveyHour{}, false
		}
		a := h.Addr.Unmap()
		answered, rtts, probed = append(answered, a), append(rtts, h.RTT), append(probed, true)
		if a.IsValid() && !h.Reached {
			tr.lim.answered(ttl, k.addr, a, keys, now)
			tr.hops.seen(a)
		}
		if h.Reached || h.Unreach {
			reached = h.Reached
			break
		}
		if a.IsValid() {
			unknown, last = 0, ttl
		} else if unknown++; unknown >= maxUnknown && ttl > len(t.route) {
			break
		}
	}
	if ctx.Err() != nil {
		return model.SurveyHour{}, false
	}
	if last > 0 || reached {
		t.route = answered
	}

	cls := sv.o.Classify(answered)
	hourNow := tr.o.Now().Unix() / 3600
	if t.hour.Hour != hourNow {
		t.hour.Hour, t.hour.Paths = hourNow, nil
	}
	p := t.hour.PathTo(dst)
	p.Rounds++
	if fixed {
		p.Fixed++
	}
	if reached {
		p.Reached++
	}
	if complete {
		p.Complete++
	}
	for i := range answered {
		if !probed[i] {
			continue
		}
		hp := model.SurveyHop{TTL: i + 1, Status: model.HopSilent, N: 1}
		if answered[i].IsValid() && i < len(cls) {
			ms := float64(rtts[i].Microseconds()) / 1000
			c := cls[i]
			hp = model.SurveyHop{TTL: i + 1, Status: c.Status, IP: c.IP, ASN: c.ASN, N: 1, MinMs: ms, SumMs: ms, MaxMs: ms}
		}
		p.AddHop(hp)
	}
	return t.hour.Clone(), true
}

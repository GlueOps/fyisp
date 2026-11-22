package trace

import (
	"context"
	"errors"
	"hash/fnv"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/netinfo"
	"github.com/glueops/fyisp/internal/netinfo/hops"
)

// The path survey (Options.Survey) traces the TCP path to every TCP and
// HTTPS target of the profile once per SurveyInterval, to compare this
// site with another (`fyisp export --survey`). It is separate from the
// always-on and Investigate traces: it keeps no route of theirs, writes
// no per-hop series, never bumps the route-change generation and never
// uses their shared hops, so it cannot change what they measure. It only
// shares the prober and the per-router cap, where it yields: a survey
// probe takes a router's token only when the bucket is full, so one is
// always left for the other traces.
//
// A round looks the target's TCP host up and probes one TTL at a time
// towards it on the target's own port, on Linux from one source port per
// target (the same in every round: one flow, see hops.FlowProber). It
// stops at the destination, at an unreachable, after maxUnknown silent
// TTLs past the last that answered (and past the last round's route), or
// at surveyMaxHops. A TTL the cap refuses takes the same router's answer
// to another survey round within surveyReuse when this target's last
// route had it there, else it is not measured (never loss). Each round is
// classified (SurveyOptions.Classify, the destination too) so no address
// of the user's own network or provider is kept, and added to the
// target's hour (model.SurveyHour), which goes to Sink after every round.
// A target that resolves to an address on this network is not traced.

// The survey's schedule and depth.
const (
	SurveyInterval = 15 * time.Minute
	surveyInFlight = 24 // targets traced at once
	surveyMaxHops  = 30
	// surveyReuse: a TTL the router cap refuses takes the same router's
	// answer to another survey round this recent instead (the routers
	// every target shares get more rounds a second than the cap allows).
	surveyReuse = time.Minute
	// surveyRetries: silent runs of a round asked again from fresh ports
	// (one may be routers that never answer, before a NAT that drops the flow)
	surveyRetries = 2
)

// Source ports of survey flows: below Linux's default ephemeral range
// (32768-60999), so they rarely meet a socket of fyisp's own.
const (
	surveyPortBase  = 20000
	surveyPortRange = 12000
)

// SurveyOptions configures the path survey. Both fields are required.
type SurveyOptions struct {
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

	interval time.Duration // SurveyInterval (tests: shorter)

	mu      sync.Mutex
	state   string // SurveyOff until the tracer runs it
	targets int
	covered map[string]bool // targets tried since the start
	last    time.Time       // end of the newest round
	answers map[netip.Addr]reuse
}

// reuse is a router's newest answer to a survey probe (see surveyReuse).
type reuse struct {
	rtt time.Duration
	at  time.Time
}

// Survey states (SurveyStatus.State).
const (
	SurveyOff         = "off"         // not started (or the tracer is not running)
	SurveyUnavailable = "unavailable" // the tracer could not start (no ICMP socket)
	SurveyFirstPass   = "first_pass"  // some targets have not been tried yet
	SurveyOn          = "on"
)

// SurveyStatus is what api/status shows of the survey.
type SurveyStatus struct {
	State     string    `json:"state"`
	Targets   int       `json:"targets"`
	Covered   int       `json:"covered"` // targets tried at least once since the start (a name that doesn't resolve counts)
	Interval  int64     `json:"interval_s"`
	LastRound time.Time `json:"last_round,omitzero"`
}

// NewSurvey returns a survey.
func NewSurvey(o SurveyOptions) *Survey {
	return &Survey{o: o, interval: SurveyInterval, state: SurveyOff, covered: map[string]bool{}, answers: map[netip.Addr]reuse{}}
}

// Status reports the survey's progress.
func (sv *Survey) Status() SurveyStatus {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	st := SurveyStatus{State: sv.state, Targets: sv.targets, Covered: len(sv.covered), Interval: int64(sv.interval / time.Second), LastRound: sv.last}
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

func newSurveyTarget(t model.Target, via string) *surveyTarget {
	host, port := t.HostFor(model.KindTCP), t.Port
	if port == 0 {
		port = 443
	}
	return &surveyTarget{st: &target{t: t, mode: model.TraceTCP, host: host}, port: port, sport: surveyPort(t.Name),
		hour: model.SurveyHour{Target: t.Name, Host: host, Port: port, HopsVia: via}}
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
		targets = append(targets, newSurveyTarget(t, via))
	}
	sv.mu.Lock()
	sv.state, sv.targets = SurveyOn, len(targets)
	sv.mu.Unlock()
	defer func() {
		sv.mu.Lock()
		sv.state = SurveyOff
		sv.mu.Unlock()
	}()
	tr.o.Log.Info("path survey", "targets", len(targets), "interval", sv.interval, "in_flight", surveyInFlight,
		"max_hops", surveyMaxHops, "probes_per_s_estimate", float64(len(targets))*16/sv.interval.Seconds())
	if len(targets) == 0 {
		<-ctx.Done()
		return
	}

	sem := make(chan struct{}, surveyInFlight)
	var wg sync.WaitGroup
	defer wg.Wait()
	step := sv.interval / time.Duration(len(targets))
	// Rounds start every step, target i at slot i of each pass (counted
	// from the epoch), so a restart keeps the spacing.
	slot := func(now time.Time) time.Time { return time.Unix(0, (now.UnixNano()/int64(step)+1)*int64(step)) }
	next := slot(tr.o.Now())
	i := int(next.UnixNano()/int64(step)) % len(targets)
	for {
		// The clock was stepped back, or jumped forward (a suspend): carry
		// on from now rather than wait or run the missed rounds back to back.
		if now := tr.o.Now(); next.Sub(now) > step+time.Minute || now.Sub(next) > time.Minute {
			next = slot(now)
		}
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
			if ok {
				sv.o.Sink(h)
			}
			sv.mu.Lock()
			sv.covered[t.st.t.Name] = true
			if ok {
				sv.last = tr.o.Now()
			}
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
// round: its name did not resolve, or to an address on this network).
func (tr *tracer) surveyRound(ctx context.Context, sv *Survey, t *surveyTarget) (model.SurveyHour, bool) {
	// Look the name up now, not in the background as dest would: its last
	// lookup is a round (15 minutes) old.
	if _, err := netip.ParseAddr(t.st.host); err != nil {
		tr.resolve(ctx, t.st)
		t.st.resolveAt = tr.o.Now().Add(sv.interval)
	}
	dst, err := tr.dest(ctx, t.st)
	if err != nil || !dst.Is4() || !netinfo.IsPublic(dst) {
		tr.o.Log.Debug("path survey: no public address", "target", t.st.t.Name, "addr", dst, "err", err)
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
		last     = 0       // highest TTL that answered
		retries  = 0       // silent runs asked again from fresh ports (at most surveyRetries)
		prevK    *limitKey // the previous TTL's key when its fixed-flow probe was silent
	)
	// fresh asks ttl from a fresh port once k's bucket is full again (a
	// silent probe's wait refills most of it).
	fresh := func(ttl int, k limitKey) (hops.Hop, []limitKey, bool) {
		if d := tr.lim.untilFull(k, tr.o.Now()); d > 0 && d <= time.Second {
			select {
			case <-ctx.Done():
			case <-time.After(d):
			}
		}
		if ctx.Err() != nil {
			return hops.Hop{}, nil, false
		}
		keys, ok := tr.lim.takeSpare(k, tr.o.Now())
		if !ok {
			return hops.Hop{}, nil, false
		}
		h, err := tp.ProbeTransport(ctx, hops.TCP, to, ttl, tr.o.Timeout)
		return h, keys, err == nil
	}
	heard := func(h hops.Hop) bool { return h.Addr.IsValid() || h.Reached || h.Unreach }
	for ttl := 1; ttl <= surveyMaxHops && ctx.Err() == nil; ttl++ {
		var expect netip.Addr
		if ttl-1 < len(t.route) {
			expect = t.route[ttl-1]
		}
		own := expect.IsValid() // this target's last route has it
		if !own {
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
				prevK = nil // only TTLs next to each other count as a silent run
				if r, ok := sv.recent(expect, now); ok && own {
					answered, rtts, probed = append(answered, expect), append(rtts, r), append(probed, true)
					unknown, last = 0, ttl
					continue
				}
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
		// Some NATs and firewalls drop a flow's repeated SYNs (or the answers
		// to them) until their state for it expires, so past them a fixed flow
		// goes silent. When two TTLs in a row are silent, both are asked again
		// from fresh ports; when one answers, this round goes on from fresh
		// ports. One lost answer (a router's rate limit) keeps the flow.
		silent := err == nil && fixed && !heard(h)
		retry := silent && prevK != nil && retries < surveyRetries && tp != nil
		if retry {
			hp, kp, okp := fresh(ttl-1, *prevK)
			hc, kc, okc := fresh(ttl, k)
			if okp || okc {
				retries++
			}
			if okp && heard(hp) || okc && heard(hc) {
				fixed = false
				if okp && hp.Addr.IsValid() && !hp.Reached && !hp.Unreach {
					a := hp.Addr.Unmap()
					answered[ttl-2], rtts[ttl-2] = a, hp.RTT
					tr.lim.answered(ttl-1, prevK.addr, a, kp, tr.o.Now())
					tr.hops.seen(a)
					sv.answered(a, hp.RTT, tr.o.Now())
					unknown, last = 0, ttl-1
				}
				if okc {
					h, keys = hc, append(keys, kc...)
				}
			}
		}
		prevK = nil
		if silent && fixed && !retry { // after a retry, the next one needs two more silent TTLs
			kk := k
			prevK = &kk
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
			sv.answered(a, h.RTT, now)
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
	// The route ends at the last TTL that answered (the destination when it
	// did): a silent tail must not make the next round go deeper.
	if last > 0 || reached {
		n := last
		if reached {
			n = len(answered)
		}
		t.route = slices.Clone(answered[:n])
	}

	// The destination is classified like a hop: one in this network's
	// provider's network keeps no address either (it could be the user's
	// own, a server at home reached by its DDNS name).
	cls := sv.o.Classify(append(slices.Clone(answered), dst))
	if dc := cls[len(cls)-1]; dc.Status != model.HopAnswered {
		dst = netip.Addr{}
	}
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

// recent returns router's answer to another survey round within
// surveyReuse.
func (sv *Survey) recent(router netip.Addr, now time.Time) (time.Duration, bool) {
	if !router.IsValid() {
		return 0, false
	}
	sv.mu.Lock()
	defer sv.mu.Unlock()
	r, ok := sv.answers[router]
	return r.rtt, ok && now.Sub(r.at) < surveyReuse
}

func (sv *Survey) answered(router netip.Addr, rtt time.Duration, now time.Time) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	if len(sv.answers) >= maxHopCache {
		for a, r := range sv.answers {
			if now.Sub(r.at) >= surveyReuse {
				delete(sv.answers, a)
			}
		}
	}
	sv.answers[router] = reuse{rtt, now}
}

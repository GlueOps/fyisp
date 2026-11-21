package model

import (
	"cmp"
	"net/netip"
	"slices"
)

// SurveyHour is one target's path survey over one UTC hour: every round's
// TCP path to the target, counted per TTL and answering router. It holds no
// address of the user's own network or provider (Status says which).
type SurveyHour struct {
	Target  string       `json:"target"`
	Hour    int64        `json:"hour"` // unix seconds / 3600
	Host    string       `json:"host"`
	Port    int          `json:"port"`
	HopsVia string       `json:"hops_via"` // "tcp", or "icmp" where routers are found with ICMP (Windows)
	Paths   []SurveyPath `json:"paths"`    // one per destination address, in the order first seen
}

// SurveyPath is the rounds of a SurveyHour that went to one address.
type SurveyPath struct {
	Dest     netip.Addr  `json:"dest"`
	Rounds   int         `json:"rounds"`
	Fixed    int         `json:"fixed"`    // rounds sent from one fixed source port (one flow)
	Reached  int         `json:"reached"`  // rounds the destination answered
	Complete int         `json:"complete"` // rounds with every TTL up to the end probed (none capped)
	Hops     []SurveyHop `json:"hops"`     // by TTL, then Status and IP
}

// Survey hop statuses.
const (
	HopAnswered = "answered" // a router (or the destination) answered: IP is set
	HopYours    = "yours"    // your own network: no IP
	HopProvider = "provider" // your provider's network: no IP
	HopPrivate  = "private"  // a private address past your provider (e.g. inside a transit network): no IP
	HopSilent   = "silent"   // no answer: N counts the unanswered probes
)

// SurveyHop is one TTL's answers from one router (or its unanswered
// probes). RTTs are in milliseconds over the N answers.
type SurveyHop struct {
	TTL    int        `json:"ttl"`
	Status string     `json:"status"`
	IP     netip.Addr `json:"ip,omitzero"`
	ASN    uint32     `json:"asn,omitempty"`
	N      int        `json:"n"`
	MinMs  float64    `json:"min_ms,omitempty"`
	SumMs  float64    `json:"sum_ms,omitempty"`
	MaxMs  float64    `json:"max_ms,omitempty"`
}

// SurveyClass is how a survey round's hop is recorded (see SurveyHop).
type SurveyClass struct {
	Status string
	IP     netip.Addr // HopAnswered only
	ASN    uint32
}

func (h SurveyHop) key() SurveyHop {
	return SurveyHop{TTL: h.TTL, Status: h.Status, IP: h.IP, ASN: h.ASN}
}

func (h *SurveyHop) add(o SurveyHop) {
	if o.N == 0 {
		return
	}
	if h.N == 0 {
		h.MinMs, h.MaxMs = o.MinMs, o.MaxMs
	} else {
		h.MinMs, h.MaxMs = min(h.MinMs, o.MinMs), max(h.MaxMs, o.MaxMs)
	}
	h.SumMs += o.SumMs
	h.N += o.N
}

// Merge adds o's rounds (the same target and hour) to s.
func (s *SurveyHour) Merge(o SurveyHour) {
	for _, op := range o.Paths {
		i := slices.IndexFunc(s.Paths, func(p SurveyPath) bool { return p.Dest == op.Dest })
		if i < 0 {
			s.Paths = append(s.Paths, SurveyPath{Dest: op.Dest})
			i = len(s.Paths) - 1
		}
		p := &s.Paths[i]
		p.Rounds += op.Rounds
		p.Fixed += op.Fixed
		p.Reached += op.Reached
		p.Complete += op.Complete
		for _, h := range op.Hops {
			p.AddHop(h)
		}
	}
}

// AddHop adds h's answers to the matching hop of p (same TTL, status, IP
// and ASN), keeping Hops sorted.
func (p *SurveyPath) AddHop(h SurveyHop) {
	k := h.key()
	i, found := slices.BinarySearchFunc(p.Hops, k, compareHop)
	if !found {
		p.Hops = slices.Insert(p.Hops, i, k)
	}
	p.Hops[i].add(h)
}

func compareHop(a, b SurveyHop) int {
	return cmp.Or(cmp.Compare(a.TTL, b.TTL), cmp.Compare(a.Status, b.Status), a.IP.Compare(b.IP), cmp.Compare(a.ASN, b.ASN))
}

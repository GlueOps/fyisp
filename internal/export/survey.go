package export

import (
	"context"
	"encoding/csv"
	"io"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/glueops/fyisp/internal/asn"
	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/store"
)

// AddrHeader and SurveyHeader are the CSV headers of --addrs and --survey.
var (
	// AddrHeader: one row per period a host's probes used one address;
	// answers is how many addresses DNS returned at the last lookup.
	AddrHeader = []string{"host", "ip", "answers", "first_seen", "last_seen"}
	// SurveyHeader: one row per hour, target, destination, TTL and router
	// (or the TTL's unanswered probes, status silent). n counts the
	// probes; the RTTs are over them. status answered has ip; yours,
	// provider and private have none (see model.SurveyHop).
	SurveyHeader = []string{"hour", "target", "host", "port", "dest_ip", "hops_via", "rounds", "fixed_flow", "reached", "complete",
		"ttl", "status", "ip", "asn", "owner", "rdns", "n", "min_ms", "mean_ms", "max_ms"}
)

// SurveyReader reads the path survey's data (*store.SQLite).
type SurveyReader interface {
	AddrPeriods(ctx context.Context, from, to time.Time, fn func(store.AddrPeriod) error) error
	SurveyHours(ctx context.Context, from, to time.Time, fn func(model.SurveyHour) error) error
	HopInfo(ctx context.Context, ip netip.Addr) (model.HopInfo, bool, error)
}

// noData is the note on stderr when --addrs or --survey finds nothing.
const noData = "fyisp export: nothing in this range (--addrs and --survey need the path survey, which --path-survey=false turns off)\n"

// WriteAddrs writes the address periods that overlap [from, to] as CSV;
// a note goes to stderr when there are none.
func WriteAddrs(ctx context.Context, r SurveyReader, w, stderr io.Writer, from, to time.Time) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(AddrHeader); err != nil {
		return err
	}
	n := 0
	err := r.AddrPeriods(ctx, from, to, func(p store.AddrPeriod) error {
		n++
		return cw.Write([]string{textCell(p.Host), p.IP.String(), strconv.Itoa(p.Answers), p.First.UTC().Format(TimeFormat), p.Last.UTC().Format(TimeFormat)})
	})
	if err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	if n == 0 && stderr != nil {
		io.WriteString(stderr, noData)
	}
	return nil
}

// WriteSurvey writes every target's survey of the hours that overlap
// [from, to] as CSV. owner and rdns come from what fyisp knows of the
// router (owner from the built-in table when it has none stored). A note
// goes to stderr when there is nothing.
func WriteSurvey(ctx context.Context, r SurveyReader, w, stderr io.Writer, targets []string, from, to time.Time) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(SurveyHeader); err != nil {
		return err
	}
	type meta struct{ owner, rdns string }
	known := map[netip.Addr]meta{}
	describe := func(ip netip.Addr) (meta, error) {
		if m, ok := known[ip]; ok {
			return m, nil
		}
		var m meta
		info, ok, err := r.HopInfo(ctx, ip)
		if err != nil {
			return m, err
		}
		if ok {
			m = meta{info.Owner, info.RDNS}
		}
		if m.owner == "" {
			m.owner = asn.Lookup(ip).Owner
		}
		known[ip] = m
		return m, nil
	}
	n := 0
	err := r.SurveyHours(ctx, from, to, func(h model.SurveyHour) error {
		if len(targets) > 0 && !slices.Contains(targets, h.Target) {
			return nil
		}
		hour := time.Unix(h.Hour*3600, 0).UTC().Format(TimeFormat)
		for _, p := range h.Paths {
			dest := "" // in your own or your provider's network
			if p.Dest.IsValid() {
				dest = p.Dest.String()
			}
			for _, hp := range p.Hops {
				n++
				var ip, owner, rdns, asnText, minMs, meanMs, maxMs string
				if hp.IP.IsValid() {
					m, err := describe(hp.IP)
					if err != nil {
						return err
					}
					ip, owner, rdns = hp.IP.String(), m.owner, m.rdns
				}
				switch hp.Status {
				case model.HopYours:
					owner = model.HopYourNetwork
				case model.HopProvider:
					owner = model.HopYourProvider
				}
				if hp.ASN != 0 {
					asnText = strconv.FormatUint(uint64(hp.ASN), 10)
				}
				if hp.Status != model.HopSilent && hp.N > 0 {
					minMs, meanMs, maxMs = fmtMs(hp.MinMs), fmtMs(math.Round(hp.SumMs/float64(hp.N)*1000)/1000), fmtMs(hp.MaxMs)
				}
				// rdns comes from the network: never a spreadsheet formula.
				if err := cw.Write([]string{hour, textCell(h.Target), textCell(h.Host), strconv.Itoa(h.Port), dest, h.HopsVia,
					strconv.Itoa(p.Rounds), strconv.Itoa(p.Fixed), strconv.Itoa(p.Reached), strconv.Itoa(p.Complete),
					strconv.Itoa(hp.TTL), hp.Status, ip, asnText, textCell(owner), textCell(rdns), strconv.Itoa(hp.N), minMs, meanMs, maxMs}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	if n == 0 && stderr != nil {
		io.WriteString(stderr, noData)
	}
	return nil
}

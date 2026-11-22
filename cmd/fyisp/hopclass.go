package main

import (
	"net/netip"

	"github.com/glueops/fyisp/internal/asn"
	"github.com/glueops/fyisp/internal/model"
	"github.com/glueops/fyisp/internal/netinfo"
	"github.com/glueops/fyisp/internal/storage"
	"github.com/glueops/fyisp/internal/store"
)

// classifyRoute says which hops of a route (in TTL order; invalid: no
// answer) may keep their address, for storage-test routes and the path
// survey alike. Hops on this network (private, CGNAT, this machine's own),
// in this connection's provider's network (isp; 0: unknown, then every
// hop is), and of no known network before the route leaves the provider
// keep no address: one could be the user's public address. Past the
// provider, a private address is a router inside another network
// (HopPrivate) and keeps no address either. The rest are HopAnswered with
// their address. Also returned: the first network past the provider.
func classifyRoute(hs []netip.Addr, isp uint32) ([]model.SurveyClass, uint32) {
	out := make([]model.SurveyClass, len(hs))
	var handoff uint32
	seenISP := false
	for i, h := range hs {
		if !h.IsValid() {
			continue
		}
		info := asn.Lookup(h)
		switch {
		case !netinfo.Traceable(h) || storage.IsSelf(h):
			out[i] = model.SurveyClass{Status: model.HopYours}
			if handoff != 0 && !storage.IsSelf(h) {
				out[i].Status = model.HopPrivate
			}
		case isp == 0 || info.ASN == isp || info.ASN == 0 && handoff == 0:
			// The provider's own routers (or an unknown provider: maybe
			// yours; or a router of no known network before the route
			// leaves it: maybe the provider's unannounced space).
			seenISP = true
			out[i] = model.SurveyClass{Status: model.HopProvider, ASN: info.ASN}
		default:
			if seenISP && handoff == 0 && info.ASN != 0 {
				handoff = info.ASN
			}
			out[i] = model.SurveyClass{Status: model.HopAnswered, IP: h, ASN: info.ASN}
		}
	}
	return out, handoff
}

// ispASN is the network of this connection's provider (0: unknown).
func ispASN(path func() netinfo.Path) uint32 {
	if p := path(); p.Edge.IsValid() {
		return asn.Lookup(p.Edge).ASN
	}
	return 0
}

// surveyClassify is the path survey's SurveyOptions.Classify.
func surveyClassify(path func() netinfo.Path) func([]netip.Addr) []model.SurveyClass {
	return func(hs []netip.Addr) []model.SurveyClass {
		out, _ := classifyRoute(hs, ispASN(path))
		return out
	}
}

// keepAddr is the probe runner's OnResolve while the path survey runs: it keeps
// the address each host's probes use (`fyisp export --addrs`), except
// one on this network (a --config target on the LAN, this machine's own)
// or in this connection's provider's network (it could be the user's own,
// a server at home reached by its DDNS name). Like classifyRoute, it keeps
// none while the provider is unknown.
func keepAddr(st *store.SQLite, path func() netinfo.Path) func(host string, ip netip.Addr, answers int) {
	return func(host string, ip netip.Addr, answers int) {
		if !netinfo.IsPublic(ip) || storage.IsSelf(ip) {
			return
		}
		if isp := ispASN(path); isp == 0 || asn.Lookup(ip).ASN == isp {
			return
		}
		st.ObserveAddr(host, ip, answers)
	}
}

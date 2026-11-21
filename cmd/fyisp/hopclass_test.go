package main

import (
	"net/netip"
	"testing"

	"github.com/glueops/fyisp/internal/asn"
	"github.com/glueops/fyisp/internal/model"
)

// TestClassifyRoute: your network and your provider's routers keep no
// address, past the provider a private router is HopPrivate, the rest keep
// theirs; an unknown provider keeps none.
func TestClassifyRoute(t *testing.T) {
	a := netip.MustParseAddr
	provider, other := asn.Lookup(a("1.0.0.1")).ASN, asn.Lookup(a("8.8.4.4")).ASN
	if provider == 0 || other == 0 || provider == other {
		t.Fatalf("test addresses: ASNs %d %d", provider, other)
	}
	route := []netip.Addr{a("192.168.1.1"), a("100.64.0.1"), a("1.0.0.1"), {}, a("8.8.4.4"), a("10.20.30.40"), a("8.8.8.8")}
	got, handoff := classifyRoute(route, provider)
	want := []model.SurveyClass{
		{Status: model.HopYours}, {Status: model.HopYours}, {Status: model.HopProvider, ASN: provider}, {},
		{Status: model.HopAnswered, IP: a("8.8.4.4"), ASN: other}, {Status: model.HopPrivate},
		{Status: model.HopAnswered, IP: a("8.8.8.8"), ASN: other},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hop %d (%v) = %+v, want %+v", i+1, route[i], got[i], want[i])
		}
	}
	if handoff != other {
		t.Errorf("handoff %d, want %d", handoff, other)
	}
	got, _ = classifyRoute(route, 0)
	for i, c := range got {
		if c.IP.IsValid() {
			t.Errorf("unknown provider: hop %d keeps %v", i+1, c.IP)
		}
	}
}

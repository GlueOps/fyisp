package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/glueops/fyisp/internal/model"
)

// fakeDNS is an injected resolver: names map to addresses; others fail.
type fakeDNS struct {
	mu    sync.Mutex
	names map[string][]netip.Addr
	asked []string
}

func (f *fakeDNS) resolve(_ context.Context, host string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, host)
	if a, ok := f.names[host]; ok {
		return a, nil
	}
	return nil, errors.New("no such host")
}

func (f *fakeDNS) lookups() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.asked) }

func newFakeDNS() *fakeDNS {
	return &fakeDNS{names: map[string][]netip.Addr{
		"example.com":       {ip("93.184.215.14")},
		"nas.lan":           {ip("192.168.1.20")},
		"mixed.example":     {ip("93.184.215.14"), ip("10.0.0.7")},
		"v6only.example":    nil,
		"loop.example":      {ip("127.0.0.1")},
		"cgnat.example":     {ip("100.72.1.1")},
		"linklocal.example": {ip("169.254.1.1")},
	}}
}

type invResp struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Code   string `json:"code"`
	Error  string `json:"error"`
}

// Investigate takes a host outside the profile: resolved first, public
// addresses only, traced under its normalised reserved name.
func TestInvestigateHost(t *testing.T) {
	d, ft, _ := traceFixture(t)
	dns := newFakeDNS()
	d.Resolve = dns.resolve
	h := Local(d, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok123"})
	good := sameOriginHdr("127.0.0.1:3000")
	post := func(q string) (int, invResp) {
		w := do(h, "POST", "/api/investigate?"+q, good)
		var r invResp
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		return w.Code, r
	}

	for q, want := range map[string]string{
		"host=Example.COM.":                "host:example.com",
		"host=93.184.215.14&mode=udp":      "host:93.184.215.14",
		"target=host:example.com&mode=tcp": "host:example.com",
	} {
		code, r := post(q)
		if code != 200 || r.Target != want {
			t.Errorf("%s: %d %+v, want %s", q, code, r, want)
		}
	}
	ft.mu.Lock()
	started, modes := append([]string(nil), ft.started...), append([]model.TraceMode(nil), ft.modes...)
	ft.mu.Unlock()
	got := map[string]bool{}
	for i, n := range started {
		got[n+" "+modes[i].String()] = true
	}
	if len(started) != 3 || !got["host:example.com tcp"] || !got["host:93.184.215.14 udp"] || !got["host:example.com icmp"] {
		t.Errorf("tracer asked for %v %v", started, modes)
	}

	// The trace view reads it under its name; only the normalised name.
	w := do(h, "GET", "/api/trace?target=host:example.com&mode=tcp", good)
	var tr struct {
		Target        string             `json:"target"`
		Host          string             `json:"host"`
		Port          int                `json:"port"`
		Traced        bool               `json:"traced"`
		Investigation *investigationJSON `json:"investigation"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &tr)
	if w.Code != 200 || tr.Host != "example.com" || tr.Port != 443 || tr.Traced || tr.Investigation == nil || tr.Investigation.State != "running" {
		t.Errorf("trace = %d %+v", w.Code, tr)
	}
	for _, p := range []string{"/api/trace?target=host:Example.com", "/api/trace?target=host:", "/api/trace/panel?target=host:a%20b"} {
		if w := do(h, "GET", p, good); w.Code != 404 {
			t.Errorf("%s = %d, want 404", p, w.Code)
		}
	}
	if w := do(h, "GET", "/api/trace/panel?target=host:example.com&mode=tcp&hops=1", good); w.Code != 200 {
		t.Errorf("panel = %d", w.Code)
	}

	// Refusals: each with its code and a reason for people; none reaches
	// the tracer.
	refused := map[string][2]any{
		// private, loopback, link-local, CGNAT, multicast and other
		// non-public address literals
		"host=10.0.0.1":        {403, "not_public"},
		"host=172.16.5.5":      {403, "not_public"},
		"host=192.168.1.1":     {403, "not_public"},
		"host=127.0.0.1":       {403, "not_public"},
		"host=169.254.1.1":     {403, "not_public"},
		"host=100.64.0.1":      {403, "not_public"},
		"host=224.0.0.251":     {403, "not_public"},
		"host=255.255.255.255": {403, "not_public"},
		"host=0.0.0.0":         {403, "not_public"},
		"host=192.0.2.1":       {403, "not_public"},
		"host=198.18.0.1":      {403, "not_public"},
		// names that resolve to a non-public address (one is enough)
		"host=nas.lan":           {403, "not_public"},
		"host=mixed.example":     {403, "not_public"},
		"host=loop.example":      {403, "not_public"},
		"host=cgnat.example":     {403, "not_public"},
		"host=linklocal.example": {403, "not_public"},
		"target=host:nas.lan":    {403, "not_public"},
		// no IPv4 address
		"host=v6only.example":  {404, "unresolved"},
		"host=nowhere.example": {404, "unresolved"},
		// bad syntax, IPv6
		"host=exa%20mple.com":                       {400, "bad_host"},
		"host=-a.example":                           {400, "bad_host"},
		"host=a..example":                           {400, "bad_host"},
		"host=a_b.example":                          {400, "bad_host"},
		"host=http://example.com":                   {400, "bad_host"},
		"host=example.com:443":                      {400, "bad_host"},
		"host=example.com/x":                        {400, "bad_host"},
		"host=127.1":                                {400, "bad_host"},
		"host=0x7f.0.0.1":                           {400, "bad_host"},
		"host=::1":                                  {400, "bad_host"},
		"host=2606:4700::1111":                      {400, "bad_host"},
		"host=" + strings.Repeat("a.", 126) + "com": {400, "bad_host"}, // over 253 characters
		// the request shape: one target or host; a port only for TCP, 1 to 65535
		"host=":                                     {400, "bad_request"},
		"host=example.com&target=Alpha":             {400, "bad_request"},
		"host=example.com&host=example.org":         {400, "bad_request"},
		"host=example.com&mode=sctp":                {400, "bad_request"},
		"host=example.com&port=22":                  {400, "bad_request"},
		"host=example.com&mode=udp&port=22":         {400, "bad_request"},
		"host=example.com&mode=tcp&port=0":          {400, "bad_request"},
		"host=example.com&mode=tcp&port=65536":      {400, "bad_request"},
		"host=example.com&mode=tcp&port=x":          {400, "bad_request"},
		"host=example.com&mode=tcp&port=22&port=23": {400, "bad_request"},
		"target=host:example.com:22":                {400, "bad_request"}, // a port name, ICMP
		"target=host:example.com:22&mode=udp":       {400, "bad_request"},
	}
	for q, want := range refused {
		code, r := post(q)
		if code != want[0] || r.Code != want[1] || r.Error == "" {
			t.Errorf("%s: %d %+v, want %v", q, code, r, want)
		}
	}
	ft.mu.Lock()
	if len(ft.started) != 3 {
		t.Errorf("refused hosts reached the tracer: %v", ft.started)
	}
	ft.mu.Unlock()
}

// A long host name (200 characters, under DNS's 253) is traced and read
// under its name.
func TestInvestigateLongHost(t *testing.T) {
	d, _, _ := traceFixture(t)
	dns := newFakeDNS()
	long := strings.Repeat(strings.Repeat("a", 63)+".", 3) + "examples"
	dns.names[long] = []netip.Addr{ip("93.184.215.14")}
	d.Resolve = dns.resolve
	h := Local(d, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok123"})
	good := sameOriginHdr("127.0.0.1:3000")
	if w := do(h, "POST", "/api/investigate?host="+long, good); w.Code != 200 {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/trace?target=host:"+long, good); w.Code != 200 {
		t.Errorf("GET = %d %s", w.Code, w.Body)
	}
}

// A host trace refused for want of a slot is refused before any DNS lookup.
func TestInvestigateHostBusyNoLookup(t *testing.T) {
	d, _, _ := traceFixture(t)
	p := d.Profile()
	for i := range MaxInvestigations {
		p.Targets = append(p.Targets, model.Target{Name: "S" + strconv.Itoa(i), Group: "common"})
	}
	dns := newFakeDNS()
	d.Resolve = dns.resolve
	h := Local(d, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok123"})
	good := sameOriginHdr("127.0.0.1:3000")
	for i := range MaxInvestigations {
		if w := do(h, "POST", "/api/investigate?target=S"+strconv.Itoa(i), good); w.Code != 200 {
			t.Fatalf("start S%d = %d %s", i, w.Code, w.Body)
		}
	}
	w := do(h, "POST", "/api/investigate?host=example.com", good)
	if w.Code != 429 || !strings.Contains(w.Body.String(), `"busy"`) || dns.lookups() != 0 {
		t.Errorf("busy host = %d %s, %d lookups", w.Code, w.Body, dns.lookups())
	}
}

// TestInvestigatePort: port= traces TCP to another port as its own trace
// (host:name:port), a profile target's host included; 443 keeps the plain name.
func TestInvestigatePort(t *testing.T) {
	d, ft, _ := traceFixture(t)
	d.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil
	}
	h := Local(d, LocalOptions{Addr: "127.0.0.1:3000", CSRFToken: "tok123"})
	good := map[string]string{"Host": "127.0.0.1:3000", "Origin": "http://127.0.0.1:3000", "Sec-Fetch-Site": "same-origin", HeaderCSRF: "tok123"}
	post := func(q string) investigationJSON {
		t.Helper()
		w := do(h, "POST", "/api/investigate?"+q, good)
		var r investigationJSON
		_ = json.Unmarshal(w.Body.Bytes(), &r)
		if w.Code != 200 {
			t.Fatalf("%s = %d %s", q, w.Code, w.Body.String())
		}
		return r
	}
	for q, want := range map[string]string{
		"host=example.com&mode=tcp&port=22":     "host:example.com:22",
		"host=example.com&mode=tcp&port=443":    "host:example.com",
		"target=host:example.com:8443&mode=tcp": "host:example.com:8443",
		"target=Beta&mode=tcp&port=2222":        "host:beta.example.com:2222",
	} {
		if r := post(q); r.Target != want || r.Mode != "tcp" {
			t.Errorf("%s: %+v, want %s", q, r, want)
		}
	}
	// A profile target to 443 stays itself (once a slot is free).
	for _, se := range ft.sessions {
		se.advance(InvestigateRounds)
	}
	if r := post("target=Alpha&mode=tcp&port=443"); r.Target != "Alpha" {
		t.Errorf("Alpha to 443: %+v", r)
	}
	// The trace view reads a port's trace by its name, with its port.
	var tr traceResp
	w := do(h, "GET", "/api/trace?target=host:example.com:22&mode=tcp", good)
	_ = json.Unmarshal(w.Body.Bytes(), &tr)
	if w.Code != 200 || tr.Port != 22 || tr.Host != "example.com" {
		t.Errorf("trace host:example.com:22 = %d %s", w.Code, w.Body.String())
	}
	// Names that aren't canonical aren't traces.
	for _, n := range []string{"host:example.com:443", "host:example.com:022", "host:example.com:0", "host:example.com:70000"} {
		if w := do(h, "GET", "/api/trace?target="+url.QueryEscape(n)+"&mode=tcp", good); w.Code != 404 {
			t.Errorf("GET %s = %d, want 404", n, w.Code)
		}
	}
	ft.mu.Lock()
	defer ft.mu.Unlock()
	if len(ft.started) != 5 {
		t.Errorf("started %v", ft.started)
	}
}

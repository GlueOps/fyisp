"""Tests for compare_sites.py and run.sh: python3 -I scripts/test_compare_sites.py (the Dockerfile's skilltest stage)."""

import csv
import gzip
import http.server
import io
import json
import os
import re
import secrets
import socket
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import compare_sites as cs  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
H0 = 492000  # 2026-02-15T00:00Z in unix hours


def put(path, data):
    with open(path, "wb" if isinstance(data, bytes) else "w") as f:
        f.write(data)


def get(path):
    with open(path) as f:
        return f.read()


HEADERS = dict(line.split(": ", 1) for line in get(os.path.join(HERE, "testdata", "headers.txt")).splitlines())
COMPARE_HEAD = HEADERS["compare"].split(",")
SURVEY_HEAD = HEADERS["survey"].split(",")


def hz(h):  # compare.csv's hour format
    return cs.iso(h)


def hms(h):  # survey.csv's hour format (export.TimeFormat)
    return cs.iso(h)[:-1] + ".000Z"


class Fixture:
    """Builds compare.csv and survey.csv rows in fyisp's real headers from a compact spec."""

    def __init__(self, site):
        self.site, self.compare, self.survey = site, [], []

    def conn(self, target, hours, p50, n=240, lost=0, kind="tcp", geo="eu", provider="examplenet", country="DE",
             tags="", dests="192.0.2.10", as_path="64500 64510", complete="1"):
        for h in hours:
            row = dict.fromkeys(COMPARE_HEAD, "")
            loss = f"{lost / (n + lost):.4f}" if n + lost else ""
            ms = "" if n == 0 else f"{p50:.3f}"
            row.update(site=self.site, hour=hz(h), target=target, kind=kind, provider=provider, city="Example",
                       country=country, geo=geo, tags=tags, n=n, lost=lost, loss=loss, min_ms=ms, p50_ms=ms, p95_ms=ms,
                       max_ms=ms, mean_ms=ms, complete=complete, dest_ips=dests if n else "", as_path=as_path if n else "")
            self.compare.append(row)
        return self

    def path(self, target, hours, hops, dest="192.0.2.10", rounds=4, fixed=4, via="tcp"):
        """hops: [(ttl, ip, asn, min_ms)]; ip None is a silent TTL, ip "provider" a hop in the site's provider."""
        for h in hours:
            for ttl, ip, asn, mn in hops:
                status = "provider" if ip == "provider" else ("answered" if ip else "silent")
                row = dict.fromkeys(SURVEY_HEAD, "")
                row.update(hour=hms(h), target=target, host=target.lower() + ".example.com", port=443, dest_ip=dest,
                           hops_via=via, rounds=rounds, fixed_flow=fixed, reached=rounds, complete=rounds, ttl=ttl,
                           status=status, ip=ip if status == "answered" else "", asn=asn or "",
                           owner="your provider" if status == "provider" else (f"OWNER-{asn}" if asn else ""),
                           rdns=f"r{ttl}.example.net" if status == "answered" else "",
                           n=4, min_ms=mn if status != "silent" else "", mean_ms=mn if status != "silent" else "",
                           max_ms=mn if status != "silent" else "")
                self.survey.append(row)
        return self

    @staticmethod
    def gz(head, rows):
        b = io.StringIO()
        w = csv.DictWriter(b, head, lineterminator="\n")
        w.writeheader()
        w.writerows(rows)
        return gzip.compress(b.getvalue().encode())

    def write(self, data):
        f1, f2 = f"{self.site}-compare.csv.gz", f"{self.site}-survey-x.csv.gz"
        put(os.path.join(data, f1), self.gz(COMPARE_HEAD, self.compare))
        put(os.path.join(data, f2), self.gz(SURVEY_HEAD, self.survey))
        return {"files": {"compare": f1, "survey": {f2: {"file": f2, "from": cs.iso(H0), "to": cs.iso(H0 + 12)}}},
                "host": "127.0.0.1:1",
                "status": {"version": "v0.18.2", "site": self.site, "profile": "all", "now": cs.iso(H0),
                           "survey": "on", "fetched": H0 * 3600, "icmp": "udp"}}


HOURS = range(H0, H0 + 12)
WIN = (H0, H0 + 12)
NAMES = ["site-a", "site-b"]


def run_analysis(fa, fb):
    with tempfile.TemporaryDirectory() as d:
        man = {"names": NAMES, "sites": {"site-a": fa.write(d), "site-b": fb.write(d)}}
        warnings = []
        results, owners, rdns, rows = cs.analyze(NAMES, WIN, d, man, warnings)
    paths = cs.common_paths(rows, NAMES, results)
    clusters, fixes = cs.clusters_and_fixes([r for r in results if r.get("cause")], paths)
    return {r["target"]: r for r in results}, warnings, owners, fixes


class Parsing(unittest.TestCase):
    def test_both_hour_formats_are_the_same_hour(self):
        self.assertEqual(cs.hour_of(hz(H0)), H0)
        self.assertEqual(cs.hour_of(hms(H0)), H0)

    def test_cell_quote(self):
        self.assertEqual(cs.cell("'=SUM(A1)"), "=SUM(A1)")
        self.assertEqual(cs.cell("'tis"), "'tis")

    def test_window_drops_the_hour_at_to_and_open_hours(self):
        f = Fixture("site-a").conn("T", range(H0 - 1, H0 + 13), 20).conn("U", [H0], 20, complete="0")
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "c.gz")
            put(p, Fixture.gz(COMPARE_HEAD, f.compare))
            rows, _ = cs.read_compare(p, "site-a", WIN)
        self.assertEqual(sorted(rows[("T", "tcp")]), list(HOURS))
        self.assertNotIn(("U", "tcp"), rows)

    def test_missing_column_blocks(self):
        with tempfile.TemporaryDirectory() as d:
            p = os.path.join(d, "c.gz")
            put(p, gzip.compress(b"site,hour\n"))
            with self.assertRaises(cs.Blocked) as e:
                cs.read_compare(p, "site-a", WIN)
        self.assertIn("lacks", str(e.exception))

    def test_columns_used_exist(self):
        self.assertTrue(set(cs.COMPARE_COLS) <= set(COMPARE_HEAD))
        self.assertTrue(set(cs.SURVEY_COLS) <= set(SURVEY_HEAD))

    def test_clean_strips_markup(self):
        self.assertEqual(cs.clean("evil`](http://x)<script>alert(1)</script>"), "evilhttpxscriptalert1script")

    def test_window_spec(self):
        now = (H0 + 30) * 3600 + 600
        self.assertEqual(cs.window_from("6h", now), (H0 + 24, H0 + 30))
        self.assertEqual(cs.window_from(f"{cs.iso(H0)[:16]}Z..{cs.iso(H0 + 12)[:16]}Z", now), WIN)
        with self.assertRaises(cs.Blocked):
            cs.window_from("25h", now)


class Verdicts(unittest.TestCase):
    def test_latency_ratio_and_tie(self):
        a = Fixture("site-a").conn("Slow", HOURS, 100).conn("Close", HOURS, 100).conn("Fast", HOURS, 30)
        b = Fixture("site-b").conn("Slow", HOURS, 160).conn("Close", HOURS, 112).conn("Fast", HOURS, 30)
        r = run_analysis(a, b)[0]
        self.assertEqual(r["Slow"]["slower"], "site-b")
        self.assertEqual(r["Slow"]["delta_ms"], 60.0)
        self.assertEqual(r["Slow"]["confidence"], 1.0)
        self.assertIsNone(r["Close"]["slower"])  # 12 ms but only 1.12x
        self.assertIsNone(r["Fast"]["slower"])

    def test_pooled_loss(self):
        a = Fixture("site-a").conn("L", HOURS, 50, n=200, lost=0)
        b = Fixture("site-b").conn("L", HOURS[:6], 50, n=200, lost=0).conn("L", HOURS[6:], 50, n=180, lost=20)
        r = run_analysis(a, b)[0]
        self.assertEqual(r["L"]["slower"], "site-b")
        self.assertEqual(r["L"]["decided_by"], "loss")
        self.assertAlmostEqual(r["L"]["loss"]["site-b"], 0.05)

    def test_unreachable_and_thin(self):
        a = Fixture("site-a").conn("Dead", HOURS, 50).conn("Thin", HOURS[:2], 50)
        b = Fixture("site-b").conn("Dead", HOURS, 0, n=0, lost=240).conn("Thin", HOURS[:2], 200)
        r, w, _, _ = run_analysis(a, b)
        self.assertEqual(r["Dead"]["unreachable"], "site-b")
        self.assertEqual(r["Dead"]["cause"], "unreachable")
        self.assertTrue(r["Thin"]["thin"])
        self.assertIn("thin", [x["code"] for x in w])

    def test_https_used_when_no_tcp(self):
        a = Fixture("site-a").conn("H", HOURS, 50, kind="https")
        b = Fixture("site-b").conn("H", HOURS, 90, kind="https")
        self.assertEqual(run_analysis(a, b)[0]["H"]["kind"], "https")


# Paths for the cause tests. Site-a reaches the destination network 64510 via transit 64500, site-b via 64501.
FAST = [(1, "provider", 64400, 1), (3, "192.0.2.1", 64500, 3), (6, "192.0.2.60", 64510, 20), (8, "192.0.2.10", 64510, 21)]


class Causes(unittest.TestCase):
    def pair(self, b_ms, b_hops, b_path="64501 64510", a_path="64500 64510", geo="eu", dests="192.0.2.10", hops_a=FAST):
        a = Fixture("site-a").conn("T", HOURS, 21, geo=geo, as_path=a_path, dests=dests).path("T", HOURS, hops_a, dest=dests)
        b = Fixture("site-b").conn("T", HOURS, b_ms, geo=geo, as_path=b_path, dests=dests).path("T", HOURS, b_hops, dest=dests)
        return run_analysis(a, b)

    def test_geodns(self):
        a = Fixture("site-a").conn("G", HOURS, 20, dests="192.0.2.10;192.0.2.11").path("G", HOURS, FAST)
        b = Fixture("site-b").conn("G", HOURS, 90, dests="198.51.100.7").path("G", HOURS, FAST)
        self.assertEqual(run_analysis(a, b)[0]["G"]["cause"], "geodns")

    def test_round_robin_and_rotating_pools_are_not_geodns(self):
        a = Fixture("site-a").conn("R", HOURS, 20, dests="192.0.2.10").path("R", HOURS, FAST)
        b = Fixture("site-b").conn("R", HOURS, 90, dests="192.0.2.99").path("R", HOURS, FAST)
        self.assertNotEqual(run_analysis(a, b)[0]["R"]["cause"], "geodns")
        a = Fixture("site-a").conn("P", HOURS, 20, dests=";".join(f"10.{i}.0.1" for i in range(8)))
        b = Fixture("site-b").conn("P", HOURS, 90, dests=";".join(f"10.{i}.1.1" for i in range(8, 16)))
        self.assertNotEqual(run_analysis(a, b)[0]["P"]["cause"], "geodns")

    def test_longer_path(self):
        r = self.pair(90, [(3, "198.51.100.1", 64501, 3), (8, "192.0.2.10", 64510, 90)], b_path="64501 64500 64520 64510")[0]
        self.assertEqual(r["T"]["cause"], "longer_path")
        self.assertEqual(r["T"]["evidence"]["network"], "64520")

    def test_latency_added_in_own_transit(self):
        # Site-b's transit 64501 adds 60 ms before 64510; TTL 4 answers slowly (deprioritised replies) and is ignored.
        hops = [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 2), (4, "198.51.100.4", 64501, 95),
                (5, "198.51.100.5", 64501, 62), (6, "192.0.2.61", 64510, 80), (8, "192.0.2.10", 64510, 81)]
        r, _, _, fixes = self.pair(81, hops)
        t = r["T"]
        self.assertEqual(t["cause"], "latency_inside")
        self.assertEqual(t["evidence"]["network"], "64501")
        self.assertEqual(t["evidence"]["router"], "198.51.100.1")  # the router before the rise
        self.assertEqual(fixes[0]["kind"], "new_transit")
        self.assertEqual((fixes[0]["slower_via"], fixes[0]["faster_via"]), ("64501", "64500"))

    def test_shared_long_haul_step_is_not_blamed(self):
        # Both cross 64999 with the same +130 ms; site-b's extra 40 ms is in its provider, before 64999.
        a_hops = [(1, "provider", 64400, 1), (3, "192.0.2.1", 64999, 5), (5, "192.0.2.5", 64999, 135), (7, "192.0.2.10", 64510, 136)]
        b_hops = [(1, "provider", 64401, 41), (3, "198.51.100.1", 64999, 45), (5, "198.51.100.5", 64999, 175),
                  (7, "192.0.2.10", 64510, 176)]
        a = Fixture("site-a").conn("T", HOURS, 136, as_path="64999 64510").path("T", HOURS, a_hops)
        b = Fixture("site-b").conn("T", HOURS, 176, as_path="64999 64510").path("T", HOURS, b_hops)
        r = run_analysis(a, b)[0]
        self.assertEqual(r["T"]["cause"], "latency_inside")
        self.assertEqual(r["T"]["evidence"]["network"], "64401")  # site-b's provider, not 64999
        self.assertIsNone(r["T"]["evidence"]["router"])  # no known router before the provider

    def test_anycast_far_site(self):
        a_hops = [(1, "provider", 64400, 1), (3, "192.0.2.1", 64500, 3), (5, "203.0.113.1", 13335, 5)]
        b_hops = [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 3), (4, "198.51.100.4", 13335, 4),
                  (5, "203.0.113.1", 13335, 60)]
        r = self.pair(60, b_hops, b_path="64501 13335", a_path="64500 13335", geo="global", dests="203.0.113.1",
                      hops_a=a_hops)[0]
        self.assertEqual(r["T"]["cause"], "anycast")

    def test_different_route(self):
        b_hops = [(3, "198.51.100.1", 64501, 60), (8, "192.0.2.10", 64510, 61)]
        r = self.pair(61, b_hops, b_path="64501 64510", a_path="64510",
                      hops_a=[(1, "provider", 64400, 1), (8, "192.0.2.10", 64510, 21)])[0]
        # the time is added before the first shared network: site-b's transit
        self.assertIn(r["T"]["cause"], ("latency_inside", "different_route"))
        self.assertEqual(r["T"]["evidence"]["network"], "64501")

    def test_peer_is_per_target_and_needs_a_reached_destination(self):
        # 3 targets beyond transit 1299, 1 inside it: one prefer/new-transit row and one peer row
        a, b = Fixture("site-a"), Fixture("site-b")
        for i, dnet in enumerate([64601, 64602, 64603, 1299]):
            t, d = f"T{i}", f"192.0.2.{20 + i}"
            ap = f"1299 {dnet}" if dnet != 1299 else "1299"
            a.conn(t, HOURS, 20, as_path=ap, dests=d).path(t, HOURS, [(1, "provider", 64400, 1), (3, "192.0.2.1", 1299, 3), (8, d, dnet, 20)], dest=d)
            b.conn(t, HOURS, 80, as_path=f"64501 {dnet}", dests=d).path(t, HOURS, [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 60), (8, d, dnet, 80)], dest=d)
        fixes = run_analysis(a, b)[3]
        self.assertEqual(sorted((f["kind"], f["targets"]) for f in fixes), [("new_transit", 3), ("peer", 1)])
        # cut short: site-a's trace never reaches the destination, so no fix and no cause from paths
        a2 = Fixture("site-a").conn("C", HOURS, 20, as_path="1299").path("C", HOURS, [(1, "provider", 64400, 1), (3, "192.0.2.1", 1299, 3)])
        b2 = Fixture("site-b").conn("C", HOURS, 80, as_path="64501 64601").path("C", HOURS, [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 60), (8, "192.0.2.10", 64601, 80)])
        r, _, _, fixes = run_analysis(a2, b2)
        self.assertEqual(fixes, [])
        self.assertNotEqual(r["C"]["cause"], "latency_inside")

    def test_peer_fix(self):
        hops = [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 60), (8, "192.0.2.10", 64510, 61)]
        r, _, _, fixes = self.pair(61, hops, b_path="64501 64510", a_path="64510",
                                   hops_a=[(1, "provider", 64400, 1), (8, "192.0.2.10", 64510, 21)])
        self.assertEqual(fixes[0]["kind"], "peer")

    def test_faster_trace_short_of_destination_names_no_network(self):
        a_hops = [(1, "provider", 64400, 1), (2, "provider", 64400, 2)]
        b_hops = [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 3), (8, "192.0.2.10", 64510, 60)]
        r = self.pair(60, b_hops, b_path="64501 64510", a_path="", hops_a=a_hops)[0]
        self.assertNotEqual(r["T"]["cause"], "latency_inside")

    def test_one_sided_survey_is_not_same_path(self):
        a = Fixture("site-a").conn("O", HOURS, 20, dests="", as_path="")
        b = Fixture("site-b").conn("O", HOURS, 90).path("O", HOURS, FAST)
        self.assertEqual(run_analysis(a, b)[0]["O"]["cause"], "no_survey")

    def test_no_survey(self):
        a = Fixture("site-a").conn("N", HOURS, 20, dests="", as_path="")
        b = Fixture("site-b").conn("N", HOURS, 90, dests="", as_path="")
        r, w, _, _ = run_analysis(a, b)
        self.assertEqual(r["N"]["cause"], "no_survey")
        self.assertIn("no_survey_data", [x["code"] for x in w])

    def test_congestion(self):
        a, b = Fixture("site-a"), Fixture("site-b")
        for i, h in enumerate(HOURS):
            a.conn("C", [h], 20)
            b.conn("C", [h], 40 + (60 if i % 2 else 0))
        a.path("C", HOURS, [(4, "192.0.2.1", 64500, 20)])
        b.path("C", HOURS, [(4, "198.51.100.1", 64500, 40)])
        self.assertEqual(run_analysis(a, b)[0]["C"]["cause"], "congestion")

    def test_provider_label_never_names_another_sites_upstream(self):
        a = Fixture("site-a").conn("T", HOURS, 20).path("T", HOURS, [(1, "provider", 64501, 1), (3, "192.0.2.1", 64500, 3)])
        b = Fixture("site-b").conn("T", HOURS, 90, as_path="64501 64510").path("T", HOURS, [(3, "198.51.100.1", 64501, 50)])
        owners = run_analysis(a, b)[2]
        self.assertEqual(owners["64501"], "OWNER-64501")

    def test_survey_blind_site_warned(self):
        a, b = Fixture("site-a"), Fixture("site-b")
        for i in range(25):
            a.conn(f"T{i}", HOURS, 20).path(f"T{i}", HOURS[:1], [(1, "provider", 64400, 1), (3, None, None, None)])
            b.conn(f"T{i}", HOURS, 20).path(f"T{i}", HOURS[:1], FAST)
        w = run_analysis(a, b)[1]
        self.assertEqual([x["site"] for x in w if x["code"] == "survey_blind"], ["site-a"])

    def test_ecmp_flagged(self):
        a = Fixture("site-a").conn("E", HOURS, 10).path("E", HOURS, [(4, "192.0.2.1", 64500, 5)], fixed=2)
        b = Fixture("site-b").conn("E", HOURS, 80).path("E", HOURS, [(4, "198.51.100.1", 64501, 5)])
        self.assertIn("ecmp_or_nat", [x["code"] for x in run_analysis(a, b)[1]])


class Sites(unittest.TestCase):
    TOK = "t" * 40

    def test_errors_name_lines_not_contents(self):
        with self.assertRaises(cs.Blocked) as e:
            cs.parse_sites(f"# c\nsite-a=http://127.0.0.1:3000/#admin=short{self.TOK[:5]}\n", ["site-a"])
        self.assertIn("line 2", str(e.exception))
        self.assertNotIn("short", str(e.exception))

    def test_http_needs_loopback_or_mark(self):
        with self.assertRaises(cs.Blocked):
            cs.parse_sites(f"site-a=http://10.0.0.5:3000/#admin={self.TOK}\n", ["site-a"])
        with self.assertRaises(cs.Blocked):
            cs.parse_sites(f"site-a=http://100.64.1.1:3000/#admin={self.TOK}\n", ["site-a"])
        s = cs.parse_sites(f"site-a=http://100.64.1.1:3000/#admin={self.TOK} http-ok\nsite-b=https://x.example.com/#admin={self.TOK}\n",
                           ["site-a", "site-b"])
        self.assertEqual(s[1]["port"], "443")
        with self.assertRaises(cs.Blocked):
            cs.parse_sites(f"site-a=http://169.254.169.254:80/#admin={self.TOK} http-ok\n", ["site-a"])

    def test_same_address_blocked(self):
        with self.assertRaises(cs.Blocked):
            cs.parse_sites(f"a=http://127.0.0.1:3000/#admin={self.TOK}\nb=http://127.0.0.1:3000/#admin={self.TOK}\n", ["a", "b"])


class Fill(unittest.TestCase):
    def test_fill(self):
        with tempfile.TemporaryDirectory() as d:
            put(os.path.join(d, "report.md"), f"x {cs.PH.format('summary')} y {cs.PH.format('causes')}")
            put(os.path.join(d, "share-summary.md"), cs.PH.format("share"))
            left = cs.fill(d, "@@ summary\nA is faster.\n@@ share\nRouter 192.0.2.1 and fe80::1 <b>slow</b> at 10:30 [[x]]\n")
            self.assertEqual(left, ["report.md: causes"])
            self.assertIn("A is faster.", get(os.path.join(d, "report.md")))
            self.assertEqual(get(os.path.join(d, "share-summary.md")), "Router [address] and [address] slow at 10:30 [x]")


class FakeFyisp(http.server.BaseHTTPRequestHandler):
    """api/status, compare.csv and survey.csv for one site; its server's mode sets how it misbehaves."""
    hits = []

    def log_message(self, *a):
        pass

    def do_GET(self):
        srv = self.server
        FakeFyisp.hits.append((srv.site, self.path))
        if self.headers.get("Authorization") != "Bearer " + srv.token:
            return self.reply(401, b'{"code":"unauthorized","error":"secret text from the server"}', "application/json")
        if srv.mode == "421":
            return self.reply(421, b'{"code":"unknown_host","error":"x"}', "application/json")
        if srv.mode == "302":
            self.send_response(302)
            self.send_header("Location", "http://127.0.0.1:1/")
            self.end_headers()
            return
        if srv.mode == "html502":
            return self.reply(502, b"<html>bad gateway</html>", "text/html")
        if srv.mode == "busy" and srv.busy:
            srv.busy -= 1
            self.send_response(429)
            self.send_header("Retry-After", "1")
            self.end_headers()
            return
        p = self.path.split("?")[0]
        if p == "/api/status":
            st = {"site": srv.site, "profile": "all", "version": "v0.18.2", "survey": {"state": "on"}, "caps": {"icmp": "udp"},
                  "now": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
            return self.reply(200, json.dumps(st).encode(), "application/json")
        if p == "/api/compare.csv":
            return self.reply(200, Fixture.gz(COMPARE_HEAD, srv.fixture.compare), "text/csv", gz=True)
        if p == "/api/survey.csv":
            if srv.mode == "survey413":
                return self.reply(413, b'{"code":"error","error":"too big"}', "application/json")
            if srv.mode == "slowsurvey":
                time.sleep(8)
            return self.reply(200, Fixture.gz(SURVEY_HEAD, srv.fixture.survey), "text/csv", gz=True)
        self.reply(404, b'{"code":"not_found"}', "application/json")

    def reply(self, code, body, ctype, gz=False):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        if gz:
            self.send_header("Content-Encoding", "gzip")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if self.server.mode == "truncate" and code == 200 and gz:
            body = body[: len(body) // 2]  # the connection drops mid-answer
            self.close_connection = True
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass


def serve(site, token, fixture, mode="ok"):
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), FakeFyisp)
    srv.site, srv.token, srv.fixture, srv.mode, srv.busy = site, token, fixture, mode, 2
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


class EndToEnd(unittest.TestCase):
    def setUp(self):
        self.tok = {"site-a": secrets.token_hex(20), "site-b": secrets.token_hex(20)}
        now = int(time.time()) // 3600
        hours = range(now - 12, now)
        fa = Fixture("site-a").conn("Slow", hours, 21).conn("Same", hours, 20).path("Slow", hours, FAST)
        fb = Fixture("site-b").conn("Slow", hours, 95, as_path="64501 64510").conn("Same", hours, 21).path(
            "Slow", hours, [(1, "provider", 64401, 1), (3, "198.51.100.1", 64501, 2), (5, "198.51.100.5", 64501, 75),
                            (6, "192.0.2.61", 64510, 94), (8, "192.0.2.10", 64510, 95)])
        fb.survey[1]["rdns"] = "evil`](http://x)<img src=x>"
        self.fx = {"site-a": fa, "site-b": fb}
        self.tmp = tempfile.TemporaryDirectory()
        self.cwd = os.getcwd()
        os.chdir(self.tmp.name)
        FakeFyisp.hits = []
        self.servers = {}

    def tearDown(self):
        self.stop()
        os.chdir(self.cwd)
        self.tmp.cleanup()

    def stop(self):
        for s in self.servers.values():
            s.shutdown()
            s.server_close()
        self.servers = {}

    def start(self, modes=("ok", "ok")):
        lines = []
        for n, m in zip(NAMES, modes):
            s = serve(n, self.tok[n], self.fx[n], m)
            self.servers[n] = s
            lines.append(f"{n}=http://127.0.0.1:{s.server_address[1]}/#admin={self.tok[n]}")
        put("sites", "\n".join(lines) + "\n")

    def run_main(self, args, deadline=60):
        out, err = io.TextIOWrapper(io.BytesIO()), io.StringIO()
        so, se = sys.stdout, sys.stderr
        sys.stdout, sys.stderr = out, err
        try:
            rc = cs.main(args + ["--deadline", str(time.time() + deadline), "--version", "9.9.9"])
        finally:
            sys.stdout, sys.stderr = so, se
        out.flush()
        raw = out.buffer.getvalue()
        files = {}
        if raw:
            with tarfile.open(fileobj=io.BytesIO(raw)) as tf:
                for m in tf.getmembers():
                    files[m.name] = tf.extractfile(m).read()
        return rc, err.getvalue(), raw, files

    def into_input(self, files):
        """What run.sh sends back in on resume or analyze: the folder's data/."""
        os.makedirs("in/data", exist_ok=True)
        for n, b in files.items():
            if n.startswith("data/"):
                put(os.path.join("in", n), b)

    def no_token(self, *blobs):
        for t in self.tok.values():
            for b in blobs:
                self.assertNotIn(t.encode() if isinstance(b, bytes) else t, b)

    def fetch(self, window="12h", **kw):
        return self.run_main(["fetch", "--names", *NAMES, "--window", window], **kw)

    def test_full_run_then_analyze(self):
        self.start()
        os.environ["HTTP_PROXY"] = os.environ["http_proxy"] = "http://127.0.0.1:9"
        try:
            rc, err, raw, files = self.fetch()
        finally:
            del os.environ["HTTP_PROXY"], os.environ["http_proxy"]
        self.assertEqual(rc, 0, err)
        report = files["report.md"].decode()
        self.assertIn("latency added in one network", report)
        self.assertIn("[[narrative:summary]]", report)
        self.assertNotIn("<img", report)
        self.assertNotRegex(files["share-summary.md"].decode(), r"\d+\.\d+\.\d+\.\d+")
        f = json.loads(files["data/findings.json"])
        self.assertEqual((f["version"], f["window"]["hours"]), ("9.9.9", 12))
        self.no_token(err, raw, *files.values())
        self.into_input(files)
        rc, err, raw, again = self.run_main(["analyze"])
        self.assertEqual(rc, 0, err)
        self.assertEqual(again["report.md"], files["report.md"])

    def test_blocked_paths_never_leak(self):
        for modes, want in [(("ok", "421"), "only answers at its own address"), (("302", "ok"), "redirected"),
                            (("ok", "html502"), "tunnel is down"), (("truncate", "ok"), "connection failed")]:
            with self.subTest(modes=modes):
                self.stop()
                self.start(modes)
                rc, err, raw, files = self.fetch()
                self.assertEqual(rc, cs.BLOCKED)
                self.assertIn(want, err)
                self.assertNotIn("secret text", err)
                self.assertNotIn("compare-sites failed", err)
                self.no_token(err, raw, *files.values())

    def test_wrong_token(self):
        self.start()
        put("sites", get("sites").replace(self.tok["site-b"], "x" * 40))
        rc, err, raw, _ = self.fetch()
        self.assertEqual(rc, cs.BLOCKED)
        self.assertIn("site-b: the admin token was refused", err)
        self.assertNotIn("secret text", err)
        self.no_token(err, raw)

    def test_refused_and_busy(self):
        self.start(("busy", "ok"))
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        closed = sock.getsockname()[1]
        sock.close()
        put("sites", re.sub(r"(site-b=http://127\.0\.0\.1:)\d+", rf"\g<1>{closed}", get("sites")))
        rc, err, raw, files = self.fetch()
        self.assertEqual(rc, cs.BLOCKED)
        self.assertIn("site-b: connection refused", err)
        self.assertIn("retrying", err)
        self.no_token(err, raw, *files.values())

    def test_survey_413_halves_then_marks_partial(self):
        self.start(("survey413", "ok"))
        rc, err, raw, files = self.fetch()
        self.assertEqual(rc, 0, err)
        self.assertEqual(json.loads(files["data/findings.json"])["data_quality"]["survey"], "partial")
        self.assertGreaterEqual(sum(1 for s, p in FakeFyisp.hits if s == "site-a" and p.startswith("/api/survey.csv")), 12)

    def test_out_of_time_keeps_files_and_resume_fetches_only_the_rest(self):
        self.start(("slowsurvey", "ok"))
        rc, err, raw, files = self.fetch(deadline=7)
        self.assertEqual(rc, cs.OUT_OF_TIME, err)
        self.assertIn("data/site-a-compare.csv.gz", files)
        self.assertIn("data/site-b-compare.csv.gz", files)
        self.into_input(files)
        self.servers["site-a"].mode = "ok"
        FakeFyisp.hits = []
        rc, err, raw, files = self.run_main(["fetch"])
        self.assertEqual(rc, 0, err)
        self.assertFalse([p for _, p in FakeFyisp.hits if "compare.csv" in p])
        self.assertTrue([p for s, p in FakeFyisp.hits if s == "site-a" and "survey.csv" in p])
        self.assertFalse([p for s, p in FakeFyisp.hits if s == "site-b" and "survey.csv" in p])
        self.assertEqual(json.loads(files["data/findings.json"])["data_quality"]["survey"], "ok")


class RunSh(unittest.TestCase):
    """run.sh's own checks; Docker isn't needed until the container starts (and isn't in the test image)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.env = {"HOME": self.tmp.name, "PATH": "/usr/local/bin:/usr/bin:/bin"}
        self.link = "http://127.0.0.1:3001/#admin=" + "a" * 255

    def tearDown(self):
        self.tmp.cleanup()

    def sh(self, *args, stdin=""):
        p = subprocess.run(["bash", os.path.join(HERE, "run.sh"), *args], input=stdin, capture_output=True, text=True,
                           env=self.env, cwd=self.tmp.name)
        return p.returncode, p.stderr + p.stdout

    def test_save_site(self):
        self.assertEqual(self.sh("save-site", "site-b", stdin=self.link + " http-ok\n")[0], 0)
        self.assertEqual(self.sh("save-site", "site-b", stdin=self.link + "\n")[0], 0)
        sites = os.path.join(self.tmp.name, ".config", "fyisp", "sites")
        self.assertEqual(get(sites), f"site-b={self.link}\n")
        self.assertEqual(os.stat(sites).st_mode & 0o777, 0o600)
        rc, out = self.sh("save-site", "site-c", stdin="http://127.0.0.1:3001/#admin=tooshort\n")
        self.assertEqual(rc, 3)
        self.assertNotIn("tooshort", out)
        self.assertEqual(self.sh("save-site", "Bad_Name", stdin=self.link)[0], 3)

    def test_arguments(self):
        self.assertIn("Window", self.sh("compare", "a", "b", "25h")[1])
        self.assertIn("two different", self.sh("compare", "a", "a")[1])
        self.assertIn("No sites file", self.sh("compare", "a", "b", "6h")[1])
        self.sh("save-site", "a", stdin=self.link)
        rc, out = self.sh("compare", "a", "b", "6h")
        self.assertEqual(rc, 3)
        self.assertIn("Docker is needed", out)
        self.assertFalse(os.path.exists(os.path.join(self.tmp.name, "fyisp-reports")))

    def test_resume_reads_the_manifest(self):
        self.sh("save-site", "a", stdin=self.link)
        d = os.path.join(self.tmp.name, "fyisp-reports", "a-vs-b-20260101T0000Z", "data")
        os.makedirs(d)
        put(os.path.join(d, "manifest.json"), json.dumps({"names": ["a", "b"], "sites": {}}, indent=1))
        rc, out = self.sh("resume", "fyisp-reports/a-vs-b-20260101T0000Z")
        self.assertIn("Docker is needed", out)  # past the manifest and sites checks
        self.assertIn("not a report folder", self.sh("resume", "../etc")[1])


class Pins(unittest.TestCase):
    """run.sh's image and version must match the Dockerfile's ARG and the release (set by skilltest)."""

    def test_image_and_version(self):
        sh = get(os.path.join(HERE, "run.sh"))
        image = re.search(r"^IMAGE=(\S+)", sh, re.M).group(1)
        version = re.search(r"^VERSION=(\S+)", sh, re.M).group(1)
        if os.environ.get("WANT_IMAGE"):
            self.assertEqual(image, os.environ["WANT_IMAGE"])
        if os.environ.get("RELEASE_MANIFEST"):
            self.assertEqual(version, json.loads(get(os.environ["RELEASE_MANIFEST"]))["."])
        self.assertTrue(os.access(os.path.join(HERE, "run.sh"), os.X_OK))


if __name__ == "__main__":
    unittest.main()

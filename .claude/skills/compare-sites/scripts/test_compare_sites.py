"""Tests for compare_sites.py: python3 -I -m unittest discover -s scripts (the Dockerfile's skilltest stage)."""

import csv
import gzip
import http.server
import io
import json
import os
import re
import secrets
import shutil
import socket
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
        """hops: [(ttl, ip, asn, min_ms)], ip None for a silent TTL."""
        for h in hours:
            for ttl, ip, asn, mn in hops:
                row = dict.fromkeys(SURVEY_HEAD, "")
                row.update(hour=hms(h), target=target, host=target.lower() + ".example.com", port=443, dest_ip=dest,
                           hops_via=via, rounds=rounds, fixed_flow=fixed, reached=rounds, complete=rounds, ttl=ttl,
                           status="answered" if ip else "silent", ip=ip or "", asn=asn or "",
                           owner=f"OWNER-{asn}" if asn else "", rdns=f"r{ttl}.example.net" if ip else "",
                           n=4, min_ms=mn if ip else "", mean_ms=mn if ip else "", max_ms=mn if ip else "")
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
        return {"files": {"compare": f1, "survey": {"x": {"file": f2}}}, "host": "127.0.0.1:1",
                "status": {"version": "v0.18.2", "site": self.site, "profile": "all", "now": cs.iso(H0),
                           "survey": "on", "fetched": H0 * 3600, "icmp": "udp"}}


HOURS = range(H0, H0 + 12)
WIN = (H0, H0 + 12)


def run_analysis(fa, fb):
    d = tempfile.mkdtemp()
    data = os.path.join(d, "data")
    os.makedirs(data)
    man = {"names": ["site-a", "site-b"], "sites": {"site-a": fa.write(data), "site-b": fb.write(data)}}
    warnings = []
    results, owners, rdns, rows = cs.analyze(["site-a", "site-b"], WIN, data, man, warnings)
    shutil.rmtree(d)
    return {r["target"]: r for r in results}, warnings


class Parsing(unittest.TestCase):
    def test_both_hour_formats_are_the_same_hour(self):
        self.assertEqual(cs.hour_of(hz(H0)), H0)
        self.assertEqual(cs.hour_of(hms(H0)), H0)

    def test_cell_quote(self):
        self.assertEqual(cs.cell("'=SUM(A1)"), "=SUM(A1)")
        self.assertEqual(cs.cell("'tis"), "'tis")

    def test_window_drops_the_hour_at_to_and_open_hours(self):
        f = Fixture("site-a").conn("T", range(H0 - 1, H0 + 13), 20).conn("U", [H0], 20, complete="0")
        d = tempfile.mkdtemp()
        p = os.path.join(d, "c.gz")
        put(p, Fixture.gz(COMPARE_HEAD, f.compare))
        rows, _ = cs.read_compare(p, "site-a", WIN)
        self.assertEqual(sorted(rows[("T", "tcp")]), list(HOURS))
        self.assertNotIn(("U", "tcp"), rows)
        shutil.rmtree(d)

    def test_missing_column_blocks(self):
        d = tempfile.mkdtemp()
        p = os.path.join(d, "c.gz")
        put(p, gzip.compress(b"site,hour\n"))
        with self.assertRaises(cs.Blocked) as e:
            cs.read_compare(p, "site-a", WIN)
        self.assertIn("lacks", str(e.exception))
        shutil.rmtree(d)

    def test_columns_used_exist(self):
        self.assertTrue(set(cs.COMPARE_COLS) <= set(COMPARE_HEAD))
        self.assertTrue(set(cs.SURVEY_COLS) <= set(SURVEY_HEAD))

    def test_clean_strips_markup(self):
        self.assertEqual(cs.clean("evil`](http://x)<script>alert(1)</script>"), "evilhttpxscriptalert1script")


class Verdicts(unittest.TestCase):
    def test_latency_ratio_and_tie(self):
        a = Fixture("site-a").conn("Slow", HOURS, 100).conn("Close", HOURS, 100).conn("Fast", HOURS, 30)
        b = Fixture("site-b").conn("Slow", HOURS, 160).conn("Close", HOURS, 112).conn("Fast", HOURS, 30)
        r, _ = run_analysis(a, b)
        self.assertEqual(r["Slow"]["slower"], "site-b")
        self.assertEqual(r["Slow"]["delta_ms"], 60.0)
        self.assertEqual(r["Slow"]["confidence"], 1.0)
        self.assertIsNone(r["Close"]["slower"])  # 12 ms but only 1.12x
        self.assertIsNone(r["Fast"]["slower"])

    def test_pooled_loss(self):
        a = Fixture("site-a").conn("L", HOURS, 50, n=200, lost=0)
        b = Fixture("site-b").conn("L", HOURS[:6], 50, n=200, lost=0).conn("L", HOURS[6:], 50, n=180, lost=20)
        r, _ = run_analysis(a, b)
        self.assertEqual(r["L"]["slower"], "site-b")
        self.assertEqual(r["L"]["decided_by"], "loss")
        self.assertAlmostEqual(r["L"]["loss"]["site-b"], 0.05)

    def test_unreachable_and_thin(self):
        a = Fixture("site-a").conn("Dead", HOURS, 50).conn("Thin", HOURS[:2], 50)
        b = Fixture("site-b").conn("Dead", HOURS, 0, n=0, lost=240).conn("Thin", HOURS[:2], 200)
        r, w = run_analysis(a, b)
        self.assertEqual(r["Dead"]["unreachable"], "site-b")
        self.assertEqual(r["Dead"]["cause"], "unreachable")
        self.assertTrue(r["Thin"]["thin"])
        self.assertIn("thin", [x["code"] for x in w])

    def test_https_used_when_no_tcp(self):
        a = Fixture("site-a").conn("H", HOURS, 50, kind="https")
        b = Fixture("site-b").conn("H", HOURS, 90, kind="https")
        r, _ = run_analysis(a, b)
        self.assertEqual(r["H"]["kind"], "https")


class Causes(unittest.TestCase):
    def test_geodns(self):
        a = Fixture("site-a").conn("G", HOURS, 20, dests="192.0.2.10;192.0.2.11").path("G", HOURS, [(5, "192.0.2.10", 64500, 20)])
        b = Fixture("site-b").conn("G", HOURS, 90, dests="198.51.100.7").path("G", HOURS, [(5, "198.51.100.7", 64501, 90)])
        r, _ = run_analysis(a, b)
        self.assertEqual(r["G"]["cause"], "geodns")

    def test_round_robin_dns_is_not_geodns(self):
        a = Fixture("site-a").conn("R", HOURS, 20, dests="192.0.2.10").path("R", HOURS, [(5, "192.0.2.10", 64500, 20)])
        b = Fixture("site-b").conn("R", HOURS, 90, dests="192.0.2.99").path("R", HOURS, [(5, "192.0.2.99", 64500, 90)])
        r, _ = run_analysis(a, b)
        self.assertNotEqual(r["R"]["cause"], "geodns")

    def test_anycast(self):
        a = Fixture("site-a").conn("Any", HOURS, 5, geo="global", dests="203.0.113.1", as_path="64500 13335") \
            .path("Any", HOURS, [(3, "203.0.113.200", 64500, 2), (4, "203.0.113.1", 13335, 5)], dest="203.0.113.1")
        b = Fixture("site-b").conn("Any", HOURS, 60, geo="global", dests="203.0.113.1", as_path="64501 13335") \
            .path("Any", HOURS, [(3, "198.51.100.1", 64501, 10), (4, "203.0.113.9", 13335, 60)], dest="203.0.113.1")
        r, _ = run_analysis(a, b)
        self.assertEqual(r["Any"]["cause"], "anycast")

    def test_longer_path(self):
        a = Fixture("site-a").conn("P", HOURS, 30, as_path="64500 64510").path("P", HOURS, [(4, "192.0.2.1", 64500, 10), (6, "192.0.2.10", 64510, 30)])
        b = Fixture("site-b").conn("P", HOURS, 90, as_path="64501 64500 64520 64510").path("P", HOURS, [(4, "198.51.100.1", 64501, 10), (6, "192.0.2.10", 64510, 90)])
        r, _ = run_analysis(a, b)
        self.assertEqual(r["P"]["cause"], "longer_path")
        self.assertEqual(r["P"]["evidence"]["network"], "64520")

    def test_latency_inside_with_deprioritised_hop(self):
        # TTL 5 answers slowly (rate-limited replies) but TTL 6 is fast: the envelope ignores TTL 5,
        # and the rise is at TTL 7 in AS64530.
        hops = [(3, "198.51.100.1", 64501, 2), (5, "198.51.100.5", 64501, 80), (6, "198.51.100.6", 64501, 3),
                (7, "198.51.100.7", 64530, 70), (8, "192.0.2.10", 64510, 72)]
        a = Fixture("site-a").conn("J", HOURS, 10, as_path="64500 64510").path("J", HOURS, [(4, "192.0.2.1", 64500, 5), (8, "192.0.2.10", 64510, 10)])
        b = Fixture("site-b").conn("J", HOURS, 72, as_path="64501 64530 64510").path("J", HOURS, hops)
        r, _ = run_analysis(a, b)
        # 64501 64530 64510 vs 64500 64510 meet at 64510 with nothing after on either side: not longer_path.
        self.assertEqual(r["J"]["cause"], "latency_inside")
        self.assertEqual(r["J"]["evidence"]["network"], "64530")
        self.assertEqual(r["J"]["evidence"]["router"], "198.51.100.7")

    def test_no_survey(self):
        a = Fixture("site-a").conn("N", HOURS, 20, dests="", as_path="")
        b = Fixture("site-b").conn("N", HOURS, 90, dests="", as_path="")
        r, w = run_analysis(a, b)
        self.assertEqual(r["N"]["cause"], "no_survey")
        self.assertIn("no_survey_data", [x["code"] for x in w])

    def test_congestion(self):
        a = Fixture("site-a")
        b = Fixture("site-b")
        for i, h in enumerate(HOURS):
            a.conn("C", [h], 20)
            b.conn("C", [h], 40 + (60 if i % 2 else 0))
        a.path("C", HOURS, [(4, "192.0.2.1", 64500, 20)])
        b.path("C", HOURS, [(4, "198.51.100.1", 64500, 40)])
        r, _ = run_analysis(a, b)
        self.assertEqual(r["C"]["cause"], "congestion")

    def test_ecmp_flagged(self):
        a = Fixture("site-a").conn("E", HOURS, 10).path("E", HOURS, [(4, "192.0.2.1", 64500, 5)], fixed=2)
        b = Fixture("site-b").conn("E", HOURS, 80).path("E", HOURS, [(4, "198.51.100.1", 64501, 5)])
        _, w = run_analysis(a, b)
        self.assertIn("ecmp_or_nat", [x["code"] for x in w])


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
        d = tempfile.mkdtemp()
        put(os.path.join(d, "report.md"), f"x {cs.PH.format('summary')} y {cs.PH.format('causes')}")
        put(os.path.join(d, "share-summary.md"), cs.PH.format("share"))
        left = cs.fill(d, "@@ summary\nA is faster.\n@@ share\nRouter 192.0.2.1 <b>slow</b> [[x]]\n")
        self.assertEqual(left, ["report.md: causes"])
        self.assertIn("A is faster.", get(os.path.join(d, "report.md")))
        self.assertEqual(get(os.path.join(d, "share-summary.md")), "Router [address] slow [x]")
        shutil.rmtree(d)


class FakeFyisp(http.server.BaseHTTPRequestHandler):
    """api/status, compare.csv and survey.csv for one site; behaviour set per test via the class attrs."""
    token, site, mode, hits = "", "site-a", "ok", None
    fixture = None

    def log_message(self, *a):
        pass

    def do_GET(self):
        FakeFyisp.hits.append((self.server.site, self.path))
        if self.headers.get("Authorization") != "Bearer " + self.server.token:
            return self.reply(401, b'{"code":"unauthorized","error":"secret text from the server"}', "application/json")
        mode = self.server.mode
        if mode == "421":
            return self.reply(421, b'{"code":"unknown_host","error":"x"}', "application/json")
        if mode == "302":
            self.send_response(302)
            self.send_header("Location", "http://127.0.0.1:1/")
            self.end_headers()
            return
        if mode == "html502":
            return self.reply(502, b"<html>bad gateway</html>", "text/html")
        if mode == "busy" and self.server.busy:
            self.server.busy -= 1
            self.send_response(429)
            self.send_header("Retry-After", "1")
            self.end_headers()
            return
        p = self.path.split("?")[0]
        if p == "/api/status":
            st = {"site": self.server.site, "profile": "all", "version": "v0.18.2", "now": cs.iso(int(time.time()) // 3600) ,
                  "survey": {"state": "on"}, "caps": {"icmp": "udp"}}
            st["now"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
            return self.reply(200, json.dumps(st).encode(), "application/json")
        f = self.server.fixture
        if p == "/api/compare.csv":
            return self.reply(200, Fixture.gz(COMPARE_HEAD, f.compare), "text/csv", gz=True)
        if p == "/api/survey.csv":
            if mode == "survey413":
                return self.reply(413, b'{"code":"error","error":"too big"}', "application/json")
            return self.reply(200, Fixture.gz(SURVEY_HEAD, f.survey), "text/csv", gz=True)
        self.reply(404, b'{"code":"not_found"}', "application/json")

    def reply(self, code, body, ctype, gz=False):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        if gz:
            self.send_header("Content-Encoding", "gzip")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def serve(site, token, fixture, mode="ok"):
    srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), FakeFyisp)
    srv.site, srv.token, srv.fixture, srv.mode, srv.busy = site, token, fixture, mode, 2
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


class EndToEnd(unittest.TestCase):
    def setUp(self):
        self.tok = {"site-a": secrets.token_hex(20), "site-b": secrets.token_hex(20)}
        now = int(time.time()) // 3600
        hours = range(now - 6, now)
        fa = Fixture("site-a").conn("Slow", hours, 30).conn("Same", hours, 20).path("Slow", hours, [(4, "192.0.2.1", 64500, 30)])
        fb = Fixture("site-b").conn("Slow", hours, 95).conn("Same", hours, 21) \
            .path("Slow", hours, [(4, "198.51.100.1", 64501, 2), (6, "198.51.100.6", 64530, 95)])
        fb.survey[0]["rdns"] = "evil`](http://x)<img src=x>"
        self.fx = {"site-a": fa, "site-b": fb}
        self.dir = tempfile.mkdtemp()
        self.cwd = os.getcwd()
        os.chdir(self.dir)
        FakeFyisp.hits = []
        self.servers = []

    def tearDown(self):
        for s in self.servers:
            s.shutdown()
            s.server_close()
        os.chdir(self.cwd)
        shutil.rmtree(self.dir)

    def start(self, modes=("ok", "ok"), extra=""):
        lines = []
        for (n, m) in zip(("site-a", "site-b"), modes):
            s = serve(n, self.tok[n], self.fx[n], m)
            self.servers.append(s)
            lines.append(f"{n}=http://127.0.0.1:{s.server_address[1]}/#admin={self.tok[n]}")
        put("sites", "\n".join(lines) + "\n" + extra)

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
                    b = tf.extractfile(m).read()
                    files[m.name] = gzip.decompress(b) if m.name.endswith(".gz") else b
        return rc, err.getvalue(), raw, files

    def no_token(self, *blobs):
        for t in self.tok.values():
            for b in blobs:
                self.assertNotIn(t.encode() if isinstance(b, bytes) else t, b)

    def test_full_run(self):
        self.start()
        os.environ["HTTP_PROXY"] = os.environ["http_proxy"] = "http://127.0.0.1:9"
        try:
            rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"])
        finally:
            del os.environ["HTTP_PROXY"], os.environ["http_proxy"]
        self.assertEqual(rc, 0, err)
        self.assertIn("report.md", files)
        report = files["report.md"].decode()
        self.assertIn("latency added inside one network", report)
        self.assertIn("[[narrative:summary]]", report)
        self.assertNotIn("<img", report)
        share = files["share-summary.md"].decode()
        self.assertNotRegex(share, r"\d+\.\d+\.\d+\.\d+")
        f = json.loads(files["data/findings.json"])
        self.assertEqual(f["version"], "9.9.9")
        self.assertEqual(f["window"]["hours"], 6)
        self.no_token(err, raw, *files.values())

    def test_blocked_paths_never_leak(self):
        for modes, want in [(("ok", "421"), "only answers at its own address"), (("302", "ok"), "redirected"),
                            (("ok", "html502"), "tunnel is down")]:
            with self.subTest(modes=modes):
                for s in self.servers:
                    s.shutdown()
                    s.server_close()
                self.servers = []
                self.start(modes)
                rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"])
                self.assertEqual(rc, cs.BLOCKED)
                self.assertIn(want, err)
                self.assertNotIn("secret text", err)
                self.no_token(err, raw, *files.values())

    def test_wrong_token(self):
        self.start()
        good = self.tok["site-b"]
        put("sites", get("sites").replace(good, "x" * 40))
        rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"])
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
        rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"])
        self.assertEqual(rc, cs.BLOCKED)
        self.assertIn("site-b: connection refused", err)
        self.assertIn("retrying", err)
        self.no_token(err, raw, *files.values())

    def test_survey_413_halves_then_marks_partial(self):
        self.start(("survey413", "ok"))
        rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"])
        self.assertEqual(rc, 0, err)
        f = json.loads(files["data/findings.json"])
        self.assertEqual(f["data_quality"]["survey"], "partial")
        self.assertGreaterEqual(sum(1 for _, p in FakeFyisp.hits if p.startswith("/api/survey.csv")), 6)

    def test_out_of_time_keeps_files_and_resumes(self):
        self.start()
        rc, err, raw, files = self.run_main(["fetch", "--names", "site-a", "site-b", "--window", "6h"], deadline=4)
        self.assertEqual(rc, cs.OUT_OF_TIME)
        self.assertIn("data/manifest.json", files)
        os.makedirs("in/data")
        for n, b in files.items():
            if n.startswith("data/"):
                put(os.path.join("in", n), gzip.compress(b) if n.endswith(".gz") else b)
        before = len(FakeFyisp.hits)
        rc, err, raw, files = self.run_main(["fetch"])
        self.assertEqual(rc, 0, err)
        self.assertIn("report.md", files)
        self.assertGreater(len(FakeFyisp.hits), before)


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

"""Compare two fyisp sites from their HTTP API (api/status, api/compare.csv, api/survey.csv).

Runs inside the container that run.sh starts: inputs are in the working
directory, and the outputs leave as a tar on stdout. Progress and messages go
to stderr. Exit codes: 0 done (warnings in the report), 3 blocked, 8 out of time.
"""

import argparse
import csv
import gzip
import io
import ipaddress
import json
import math
import os
import re
import socket
import statistics
import sys
import tarfile
import threading
import time
import urllib.error
import urllib.request
import traceback
import zlib
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

OK, BLOCKED, OUT_OF_TIME = 0, 3, 8

# Thresholds (also in report.md's Method section and findings.json).
MIN_MS = 10.0  # a difference smaller than this is a tie
MIN_RATIO = 1.2  # and the slower median must be this much higher
MIN_LOSS = 0.02  # pooled loss difference (a fraction)
THIN_HOURS = 3
SURVEY_CHUNK_H = 6
MAX_BODY = 64 << 20  # the server's own cap; more is not a fyisp answer
MAX_CSV = 512 << 20  # decompressed bytes read from one file

SITE_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,31}$")
LINK_RE = re.compile(
    r"^(https?)://(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]{1,253})(?::([0-9]{1,5}))?/#admin=([A-Za-z0-9._~-]{32,256})$")
KNOWN_CODES = {"bad_request", "unauthorized", "forbidden", "not_found", "method_not_allowed",
               "unknown_host", "unavailable", "busy", "too_large", "error"}

COMPARE_COLS = ["site", "hour", "target", "kind", "provider", "city", "country", "geo", "tags",
                "n", "lost", "loss", "p50_ms", "complete", "dest_ips", "as_path"]
SURVEY_COLS = ["hour", "target", "dest_ip", "hops_via", "rounds", "fixed_flow", "ttl", "status",
               "ip", "asn", "owner", "rdns", "n", "min_ms"]

_print_lock = threading.Lock()


class Blocked(Exception):
    """A site can't be used; the message follows the template '<site>: <what>. <todo>.'"""


class OutOfTime(Exception):
    pass


def say(line):
    with _print_lock:
        print(line, file=sys.stderr, flush=True)


def clean(s, n=60):
    """Third-party text (router names, network owners) reduced to a safe charset."""
    s = re.sub(r"[^A-Za-z0-9 .,_-]", "", s or "")
    return re.sub(r"\s+", " ", s).strip()[:n]


def cell(s):
    """Undo fyisp's spreadsheet-safety quote (a leading ' before = + - @ tab CR)."""
    if len(s) > 1 and s[0] == "'" and s[1] in "=+-@\t\r":
        return s[1:]
    return s


def hour_of(s):
    return int(datetime.fromisoformat(s).timestamp()) // 3600


def iso(h):
    return datetime.fromtimestamp(h * 3600, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def fnum(s):
    return float(s) if s not in ("", None) else None


def median(xs):
    return statistics.median(xs) if xs else None


def pct(xs, q):
    xs = sorted(xs)
    if not xs:
        return None
    k = (len(xs) - 1) * q
    lo, hi = math.floor(k), math.ceil(k)
    return xs[lo] + (xs[hi] - xs[lo]) * (k - lo)


# ---------- sites ----------

def parse_sites(text, names):
    """name=link [http-ok] lines; errors name the line number, never the line."""
    found = {}
    for i, line in enumerate(text.splitlines(), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        name, sep, link = parts[0].partition("=")
        flags = parts[1:]
        if not sep or not SITE_RE.match(name) or any(f != "http-ok" for f in flags):
            raise Blocked(f"sites file line {i}: not 'name=link' or 'name=link http-ok'. Fix that line")
        if name not in names:
            continue
        m = LINK_RE.match(link)
        if not m:
            raise Blocked(f"{name}: its link in the sites file (line {i}) is not a fyisp sign-in link. "
                          "Save it again: http(s)://host:port/#admin=<token>")
        scheme, host, port, token = m.groups()
        port = port or ("443" if scheme == "https" else "80")
        if not 0 < int(port) < 65536:
            raise Blocked(f"{name}: the port in its link is not valid. Save the link again")
        found[name] = {"name": name, "scheme": scheme, "host": host.strip("[]").lower(), "port": port,
                       "token": token, "http_ok": "http-ok" in flags}
    for n in names:
        if n not in found:
            raise Blocked(f"{n}: not in the sites file. Add it with run.sh save-site {n}")
    hosts = Counter((s["host"], s["port"]) for s in found.values())
    if any(c > 1 for c in hosts.values()):
        raise Blocked("both sites have the same address. Give each site its own address or port")
    for s in found.values():
        check_transport(s)
    return [found[n] for n in names]


def check_transport(s):
    host = s["host"]
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        ip = None
    if ip is not None and ip in ipaddress.ip_network("169.254.0.0/16"):
        raise Blocked(f"{s['name']}: link-local addresses are refused. Use the server's own address")
    if s["scheme"] == "https":
        return
    loopback = host == "localhost" or (ip is not None and ip.is_loopback)
    if loopback:
        return
    if not s["http_ok"]:
        raise Blocked(f"{s['name']}: plain http to an address other than this machine sends the admin token "
                      "unencrypted. Use an SSH forward to 127.0.0.1 or the --tunnel link, or, on a network you "
                      "trust (VPN, Tailscale), add ' http-ok' after the link in the sites file")


def base_url(s):
    host = f"[{s['host']}]" if ":" in s["host"] else s["host"]
    return f"{s['scheme']}://{host}:{s['port']}/"


# ---------- HTTP ----------

class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), _NoRedirect())


class Deadline:
    def __init__(self, at):
        self.at = at

    def left(self):
        return self.at - time.time()

    def check(self, need=5):
        if self.left() < need:
            raise OutOfTime()


def http_get(s, path, dl, dest=None, tries=6):
    """GET base+path with the token in a header only. Returns (status, body bytes or None, headers).
    With dest, the body is streamed to that file (gzip kept as gzip, else compressed here)."""
    url = base_url(s) + path
    for attempt in range(tries):
        dl.check()
        req = urllib.request.Request(url, headers={"Accept-Encoding": "gzip", "User-Agent": "fyisp-compare-sites"})
        req.add_unredirected_header("Authorization", "Bearer " + s["token"])
        try:
            resp = _opener.open(req, timeout=max(5, min(dl.left(), 120)))
        except urllib.error.HTTPError as e:
            status, headers, body = e.code, e.headers, e.read(65536)
            if status == 429 and attempt + 1 < tries:
                wait = min(int(headers.get("Retry-After", "5") or 5), 10)
                dl.check(wait + 5)
                say(f"[fetch] {s['name']}: busy (another export runs), retrying in {wait} s")
                time.sleep(wait)
                continue
            return status, _decode(body, headers), headers
        except (urllib.error.URLError, socket.timeout, TimeoutError, ConnectionError, OSError) as e:
            reason = getattr(e, "reason", e)
            if isinstance(reason, (socket.timeout, TimeoutError)):
                return "timeout", None, {}
            if isinstance(reason, ConnectionRefusedError):
                return "refused", None, {}
            if isinstance(reason, socket.gaierror):
                return "dns", None, {}
            return "network", None, {}
        with resp:
            gz = resp.headers.get("Content-Encoding", "").lower() == "gzip"
            if dest is None:
                body = _read_capped(resp, dl)
                return resp.status, _decode(body, resp.headers), resp.headers
            tmp = dest + ".part"
            with open(tmp, "wb") as raw:
                out = raw if gz else gzip.GzipFile(fileobj=raw, mode="wb")
                n = 0
                while True:
                    dl.check(2)
                    chunk = resp.read(1 << 16)
                    if not chunk:
                        break
                    n += len(chunk)
                    if n > MAX_BODY:
                        raise Blocked(f"{s['name']}: an answer was larger than any fyisp export. "
                                      "Check that the link points at fyisp")
                    out.write(chunk)
                if out is not raw:
                    out.close()
            os.replace(tmp, dest)
            return resp.status, n, resp.headers
    return 429, None, {}


def _read_capped(resp, dl):
    buf = io.BytesIO()
    while True:
        dl.check(2)
        chunk = resp.read(1 << 16)
        if not chunk:
            return buf.getvalue()
        buf.write(chunk)
        if buf.tell() > MAX_BODY:
            raise OutOfTime()


def _decode(body, headers):
    if body and headers and headers.get("Content-Encoding", "").lower() == "gzip":
        try:
            d = zlib.decompressobj(16 + 15)
            body = d.decompress(body, 1 << 20)
        except Exception:
            return b""
    return body


def json_code(body):
    try:
        c = json.loads(body).get("code")
    except Exception:
        return None
    return c if c in KNOWN_CODES else "error"


def fail(s, status, body):
    """Turn a failed answer into a Blocked message: never the server's own text."""
    n = s["name"]
    if status == "refused":
        return Blocked(f"{n}: connection refused at the address in its link, from inside Docker. If you reach it "
                       "through an SSH forward or a tunnel, open it on the Docker host (in a devcontainer: on the "
                       "machine running Docker); on Docker Desktop, turn on host networking (4.34+)")
    if status == "timeout":
        return Blocked(f"{n}: no answer in time. Check that the server and any tunnel to it are up, then run again")
    if status == "dns":
        return Blocked(f"{n}: the host name in its link does not resolve. Check the link")
    if status == "network":
        return Blocked(f"{n}: the connection failed. Check the link and the network, then run again")
    code = json_code(body) if body else None
    if status == 401 or code == "unauthorized":
        return Blocked(f"{n}: the admin token was refused. Save the current sign-in link again (run.sh save-site {n})")
    if status == 421 or code == "unknown_host":
        return Blocked(f"{n}: fyisp only answers at its own address and port. Forward the same port it listens on "
                       "(e.g. 3001 to 127.0.0.1:3001), or run each server on its own port")
    if isinstance(status, int) and 300 <= status < 400:
        return Blocked(f"{n}: the address redirected elsewhere. Use the dashboard's own address")
    if status == 404 or code == "not_found":
        return Blocked(f"{n}: this server has no comparison API. Upgrade it to the current fyisp release")
    if isinstance(status, int) and status in (502, 503, 530) and code is None:
        return Blocked(f"{n}: the share link's tunnel is down or expired. Get the current link from the server's log")
    if status == 429:
        return Blocked(f"{n}: the server stayed busy with another export. Run again in a minute")
    if status == 524:
        return Blocked(f"{n}: the share link's edge timed out. Run again with a shorter window")
    return Blocked(f"{n}: unexpected answer ({status}{', ' + code if code else ''}). Check the link and the server")


def fetch_status(s, dl):
    status, body, _ = http_get(s, "api/status", dl)
    if status != 200:
        raise fail(s, status, body)
    try:
        st = json.loads(body)
    except Exception:
        raise Blocked(f"{s['name']}: api/status did not answer with fyisp's JSON. Check that the link points at fyisp")
    sv = st.get("survey") or {}
    return {"version": clean(str(st.get("version", "")), 32), "site": clean(str(st.get("site", "")), 32),
            "profile": clean(str(st.get("profile", "")), 80), "now": clean(str(st.get("now", "")), 40),
            "icmp": clean(str((st.get("caps") or {}).get("icmp", "")), 20),
            "survey": clean(str(sv.get("state", "off")), 20), "fetched": time.time()}


def fetch_site(s, win, data, man, dl):
    """Fetch one site's status, compare.csv and survey chunks into data/, recording each in man."""
    name = s["name"]
    entry = man["sites"].setdefault(name, {"files": {}, "survey_errors": []})
    entry["host"] = f"{s['host']}:{s['port']}"
    st = fetch_status(s, dl)
    entry["status"] = st
    say(f"[fetch] {name}: fyisp {st['version']}, profile {st['profile'] or '?'}, survey {st['survey']}")
    lo, hi = win
    if "compare" not in entry["files"]:
        f = f"{name}-compare.csv.gz"
        status, n, _ = http_get(s, "api/compare.csv?" + rng(lo, hi), dl, os.path.join(data, f))
        if status != 200:
            raise fail(s, status, n if isinstance(n, bytes) else None)
        entry["files"]["compare"] = f
        say(f"[fetch] {name}: compare.csv {n / 1e6:.1f} MB")
    if st["survey"] in ("off", ""):
        return
    chunks = entry["files"].setdefault("survey", {})
    todo = [(a, min(a + SURVEY_CHUNK_H, hi)) for a in range(lo, hi, SURVEY_CHUNK_H)]
    while todo:
        a, b = todo.pop(0)
        key = iso(a)
        if key in chunks:
            continue
        f = f"{name}-survey-{iso(a)[:13].replace('-', '')}.csv.gz"
        status, n, _ = http_get(s, "api/survey.csv?" + rng(a, b), dl, os.path.join(data, f))
        if status in (413, 524) and b - a > 1:
            mid = (a + b) // 2
            todo[:0] = [(a, mid), (mid, b)]
            continue
        if status != 200:
            entry["survey_errors"].append(f"{iso(a)}: {status}")
            say(f"[fetch] {name}: survey {iso(a)} failed ({status}); the report marks it")
            continue
        chunks[key] = {"file": f, "to": iso(b)}
        say(f"[fetch] {name}: survey {iso(a)[11:16]}+{b - a}h {n / 1e6:.1f} MB")


def rng(a, b):
    """Hours a <= h < b: the API includes every hour starting in [from, to], so to is the last second before b."""
    return f"from={iso(a)}&to={iso(b - 1)[:14]}59:59Z"


# ---------- parsing ----------

def open_csv(path):
    f = gzip.open(path, "rt", encoding="utf-8", newline="")
    count = [0]

    def lines():
        for line in f:
            count[0] += len(line)
            if count[0] > MAX_CSV:
                raise Blocked("a saved file is larger than any fyisp export. Fetch it again")
            yield line
    return f, csv.reader(lines())


def read_compare(path, site, win):
    """{(target, kind): {hour: row}} and {target: meta}, hours lo <= h < hi only."""
    lo, hi = win
    rows, meta = defaultdict(dict), {}
    f, r = open_csv(path)
    with f:
        head = next(r, [])
        missing = [c for c in COMPARE_COLS if c not in head]
        if missing:
            raise Blocked(f"{site}: its compare.csv lacks {', '.join(missing)}. Run the same fyisp release on both sites")
        ix = {c: head.index(c) for c in head}
        for rec in r:
            if len(rec) != len(head):
                continue
            h = hour_of(rec[ix["hour"]])
            if not lo <= h < hi or rec[ix["complete"]] != "1":
                continue
            t = cell(rec[ix["target"]])
            meta.setdefault(t, {k: cell(rec[ix[k]]) for k in ("provider", "city", "country", "geo", "tags")})
            rows[(t, rec[ix["kind"]])][h] = {
                "n": int(rec[ix["n"]] or 0), "lost": int(rec[ix["lost"]] or 0), "p50": fnum(rec[ix["p50_ms"]]),
                "dests": [d for d in rec[ix["dest_ips"]].split(";") if d],
                "as_path": tuple(a for a in rec[ix["as_path"]].split() if a)}
    return rows, meta


def read_survey(paths, site, win, want, owners, rdns, flags):
    """For the targets in want: {target: {hour: main path}}, a main path being {ttl: (ip, asn, min_ms)}
    plus its last router. Fills owners (asn -> name) and rdns (ip -> name) for every row."""
    lo, hi = win
    paths_by = defaultdict(lambda: defaultdict(dict))  # target -> hour -> pathkey -> path
    for p in paths:
        f, r = open_csv(p)
        with f:
            head = next(r, [])
            missing = [c for c in SURVEY_COLS if c not in head]
            if missing:
                raise Blocked(f"{site}: its survey.csv lacks {', '.join(missing)}. Run the same fyisp release on both sites")
            ix = {c: head.index(c) for c in head}
            for rec in r:
                if len(rec) != len(head):
                    continue
                asn, ip = rec[ix["asn"]], rec[ix["ip"]]
                if asn and asn not in owners:
                    owners[asn] = clean(cell(rec[ix["owner"]]))
                if ip and ip not in rdns:
                    rdns[ip] = clean(cell(rec[ix["rdns"]]), 80)
                t = cell(rec[ix["target"]])
                if t not in want:
                    continue
                h = hour_of(rec[ix["hour"]])
                if not lo <= h < hi:
                    continue
                key = (rec[ix["dest_ip"]], rec[ix["rounds"]], rec[ix["fixed_flow"]])
                path = paths_by[t][h].setdefault(key, {"rounds": int(rec[ix["rounds"]] or 0), "ttl": {},
                                                       "ecmp": rec[ix["fixed_flow"]] != rec[ix["rounds"]]
                                                       or rec[ix["hops_via"]] == "icmp"})
                if rec[ix["status"]] != "answered" or not ip:
                    continue
                ttl, n, mn = int(rec[ix["ttl"]]), int(rec[ix["n"]] or 0), fnum(rec[ix["min_ms"]])
                cur = path["ttl"].get(ttl)
                best = mn if cur is None or cur[3] is None or (mn is not None and mn < cur[3]) else cur[3]
                if cur is None or n > cur[2]:
                    path["ttl"][ttl] = (ip, asn, n, best)
                else:
                    path["ttl"][ttl] = (cur[0], cur[1], cur[2], best)
    out = defaultdict(dict)
    for t, hours in paths_by.items():
        for h, ps in hours.items():
            main = max(ps.values(), key=lambda p: p["rounds"])
            if main["ecmp"]:
                flags[site] += 1
            hops = [(ttl, ip, asn, mn) for ttl, (ip, asn, _, mn) in sorted(main["ttl"].items())]
            out[t][h] = hops
    return out


# ---------- analysis ----------

def pick_kind(ra, rb, target):
    for k in ("tcp", "https"):
        if (target, k) in ra and (target, k) in rb:
            return k
    return None


def compare_target(t, kind, a, b, ha, hb):
    """Verdict for one target from both sites' hourly rows (ha, hb: {hour: row})."""
    shared = sorted(set(ha) & set(hb))
    res = {"target": t, "kind": kind, "hours": len(shared), "faster": None, "slower": None, "thin": len(shared) < THIN_HOURS}
    if not shared:
        return res
    lat = [h for h in shared if ha[h]["n"] > 0 and hb[h]["n"] > 0 and ha[h]["p50"] is not None and hb[h]["p50"] is not None]
    pa = [ha[h]["p50"] for h in lat]
    pb = [hb[h]["p50"] for h in lat]
    diffs = [y - x for x, y in zip(pa, pb)]  # b - a
    def loss(rows):
        n = sum(rows[h]["n"] + rows[h]["lost"] for h in shared)
        return sum(rows[h]["lost"] for h in shared) / n if n else None
    la, lb = loss(ha), loss(hb)
    dead_a = sum(1 for h in shared if ha[h]["n"] == 0 and ha[h]["lost"] > 0)
    dead_b = sum(1 for h in shared if hb[h]["n"] == 0 and hb[h]["lost"] > 0)
    res.update({"p50": {a: median(pa), b: median(pb)}, "loss": {a: la, b: lb}, "delta_ms": None,
                "unreachable": None, "decided_by": None, "confidence": None, "note": None, "lat_hours": len(lat)})
    half = len(shared) / 2
    if (dead_a > half) != (dead_b > half):
        bad = a if dead_a > half else b
        res.update(unreachable=bad, slower=bad, faster=b if bad == a else a, decided_by="unreachable",
                   confidence=round(max(dead_a, dead_b) / len(shared), 2))
        return res
    lat_slower = None
    if diffs:
        md, ma, mb = median(diffs), median(pa), median(pb)
        res["delta_ms"] = round(abs(md), 1)
        if md >= MIN_MS and ma > 0 and mb / ma >= MIN_RATIO:
            lat_slower = b
        elif -md >= MIN_MS and mb > 0 and ma / mb >= MIN_RATIO:
            lat_slower = a
    loss_slower = None
    if la is not None and lb is not None:
        if lb - la >= MIN_LOSS:
            loss_slower = b
        elif la - lb >= MIN_LOSS:
            loss_slower = a
    slower = lat_slower or loss_slower
    if not slower:
        return res
    res.update(slower=slower, faster=b if slower == a else a, decided_by="latency" if lat_slower else "loss")
    if lat_slower and loss_slower and loss_slower != lat_slower:
        res["note"] = "loss_disagrees"
    if lat_slower:
        sign = 1 if slower == b else -1
        wins = sum(1 for x, y in zip(pa, pb) if sign * (y - x) >= MIN_MS and min(x, y) > 0 and max(x, y) / min(x, y) >= MIN_RATIO)
        res["confidence"] = round(wins / len(lat), 2)
    else:
        def hl(rows, h):
            n = rows[h]["n"] + rows[h]["lost"]
            return rows[h]["lost"] / n if n else 0.0
        sign = 1 if slower == b else -1
        wins = sum(1 for h in shared if sign * (hl(hb, h) - hl(ha, h)) >= MIN_LOSS)
        res["confidence"] = round(wins / len(shared), 2)
    return res


def mode(xs):
    xs = [x for x in xs if x]
    return Counter(xs).most_common(1)[0][0] if xs else None


def prefix(ip):
    a = ipaddress.ip_address(ip)
    return ipaddress.ip_network(f"{ip}/{24 if a.version == 4 else 48}", strict=False)


def jump(hops):
    """The hop where the lower envelope of per-TTL best RTTs rises most (robust to routers that
    deprioritise their own replies). hops: [(ttl, ip, asn, min_ms)]. Returns (hop, prev, step_ms)."""
    resp = [x for x in hops if x[1] and x[3] is not None]
    if len(resp) < 2:
        return None, None, 0.0
    floor, m = [], float("inf")
    for x in reversed(resp):
        m = min(m, x[3])
        floor.append(m)
    floor.reverse()
    best_i, best = None, 0.0
    for i in range(1, len(resp)):
        inc = floor[i] - floor[i - 1]
        if inc > best:
            best_i, best = i, inc
    if best_i is None:
        return None, None, 0.0
    return resp[best_i], resp[best_i - 1], best


def cause(res, ra, rb, sa, sb, a, b):
    """First matching cause for a slower target. ra/rb: hourly compare rows, sa/sb: survey paths."""
    S = res["slower"]
    F = res["faster"]
    rs, rf = (ra, rb) if S == a else (rb, ra)
    ss, sf = (sa, sb) if S == a else (sb, sa)
    ev = {}
    if res["decided_by"] == "unreachable":
        return "unreachable", ev
    ds = {d for r in rs.values() for d in r["dests"]}
    df = {d for r in rf.values() for d in r["dests"]}
    if ds and df:
        try:
            if not {prefix(d) for d in ds} & {prefix(d) for d in df}:
                return "geodns", {"slower_dests": len(ds), "faster_dests": len(df)}
        except ValueError:
            pass
    shared = sorted(set(rs) & set(rf))
    ps = mode([rs[h]["as_path"] for h in shared])
    pf = mode([rf[h]["as_path"] for h in shared])
    ev["slower_path"], ev["faster_path"] = list(ps or ()), list(pf or ())
    if res.get("geo") == "global" and ds & df and ss and sf:
        ls = mode([hops[-1][1] for hops in ss.values() if hops])
        lf = mode([hops[-1][1] for hops in sf.values() if hops])
        if ls and lf and ls != lf:
            ev.update(slower_last=ls, faster_last=lf)
            return "anycast", ev
    if ps and pf:
        meet = next((x for x in ps if x in pf), None)
        if meet is not None:
            after_s = len(ps) - ps.index(meet) - 1
            after_f = len(pf) - pf.index(meet) - 1
            if after_s > after_f:
                nxt = ps[ps.index(meet) + 1]
                ev.update(meet=meet, network=nxt, prev=meet, extra_networks=after_s - after_f)
                return "longer_path", ev
    if ss:
        delta = res["delta_ms"] or 0
        steps = []
        for h, hops in ss.items():
            hop, prev, step = jump(hops)
            if hop and step >= max(MIN_MS, 0.5 * delta):
                steps.append((hop[2], prev[2], hop[1], step))
        if steps and len(steps) >= len(ss) / 2:
            x = mode([s[0] for s in steps])
            sel = [s for s in steps if s[0] == x] or steps
            ev.update(network=x, prev=mode([s[1] for s in sel]), router=mode([s[2] for s in sel]),
                      step_ms=round(median([s[3] for s in sel]), 1))
            return "latency_inside", ev
    paths_s = {rs[h]["as_path"] for h in shared if rs[h]["as_path"]}
    lat = [h for h in shared if rs[h]["p50"] is not None and rf[h]["p50"] is not None]
    if len(paths_s) == 1 and len(lat) >= 6:
        sp = pct([rs[h]["p50"] for h in lat], .9) - pct([rs[h]["p50"] for h in lat], .1)
        fp = pct([rf[h]["p50"] for h in lat], .9) - pct([rf[h]["p50"] for h in lat], .1)
        need = max(MIN_MS, 0.5 * (res["delta_ms"] or 0))
        if sp >= need and fp < need:
            ev.update(slower_spread_ms=round(sp, 1), faster_spread_ms=round(fp, 1))
            return "congestion", ev
    if ss:
        return "same_path_unexplained", ev
    return "no_survey", ev


CAUSE_TEXT = {
    "unreachable": "unreachable from the slower site",
    "geodns": "GeoDNS: each site is sent to different servers",
    "anycast": "anycast: same address, a different (farther) site answers",
    "longer_path": "longer path: more networks after the paths meet",
    "latency_inside": "latency added inside one network",
    "congestion": "congestion: same path, latency varies by hour",
    "same_path_unexplained": "same path, unexplained",
    "no_survey": "no path survey data",
}


def analyze(names, win, data, man, warnings):
    a, b = names
    lo, hi = win
    rows, meta = {}, {}
    for n in names:
        f = man["sites"][n]["files"].get("compare")
        if not f:
            raise Blocked(f"{n}: no compare.csv was fetched. Run again")
        rows[n], m = read_compare(os.path.join(data, f), n, win)
        meta.update({k: v for k, v in m.items() if k not in meta})
    targets = sorted({t for (t, _) in rows[a]} & {t for (t, _) in rows[b]})
    results = []
    for t in targets:
        kind = pick_kind(rows[a], rows[b], t)
        if not kind:
            continue
        r = compare_target(t, kind, a, b, rows[a][(t, kind)], rows[b][(t, kind)])
        r.update({k: meta.get(t, {}).get(k, "") for k in ("provider", "city", "country", "geo", "tags")})
        results.append(r)
    slower = [r for r in results if r["slower"] and not r["thin"]]
    want = {r["target"] for r in slower}
    owners, rdns, flags = {}, {}, Counter()
    surveys = {}
    for n in names:
        chunks = man["sites"][n]["files"].get("survey") or {}
        surveys[n] = read_survey([os.path.join(data, c["file"]) for c in chunks.values()], n, win, want, owners, rdns, flags)
    for r in slower:
        t = r["target"]
        r["cause"], r["evidence"] = cause(r, rows[a][(t, r["kind"])], rows[b][(t, r["kind"])],
                                          surveys[a].get(t, {}), surveys[b].get(t, {}), a, b)
    for n in names:
        if want and not surveys[n] and man["sites"][n]["status"]["survey"] not in ("off", ""):
            warnings.append({"code": "no_survey_data", "site": n,
                             "detail": "the path survey has no rows in this window (it may have started later): causes can't be found"})
        if flags[n]:
            warnings.append({"code": "ecmp_or_nat", "site": n,
                             "detail": f"{flags[n]} target-hours where the survey's flow changed or ICMP-based hops were used; paths there are less certain"})
    thin = sum(1 for r in results if r["thin"])
    if thin:
        warnings.append({"code": "thin", "site": "", "detail": f"{thin} targets have under {THIN_HOURS} shared hours and are left out"})
    return results, owners, rdns, rows


def first_up(path):
    return path[0] if path else None


def groups(results, names, paths):
    a, b = names
    out = {}
    for key in ("geo", "country", "provider"):
        g = defaultdict(lambda: Counter())
        dl = defaultdict(list)
        for r in results:
            if r["thin"]:
                continue
            k = r.get(key) or "?"
            g[k]["targets"] += 1
            if r["faster"]:
                g[k][r["faster"] + "_faster"] += 1
                if r["delta_ms"] is not None:
                    dl[k].append(r["delta_ms"])
            else:
                g[k]["tie"] += 1
        out[key] = {k: {**v, "median_delta_ms": round(median(dl[k]), 1) if dl[k] else None} for k, v in g.items()}
    tags = defaultdict(Counter)
    for r in results:
        if r["thin"]:
            continue
        for tag in filter(None, (r.get("tags") or "").split(";")):
            tags[tag]["targets"] += 1
            tags[tag][(r["faster"] + "_faster") if r["faster"] else "tie"] += 1
    out["tags"] = {k: dict(v) for k, v in tags.items()}
    ups = {}
    for n in names:
        g = defaultdict(Counter)
        for r in results:
            if r["thin"]:
                continue
            up = first_up(paths[n].get(r["target"]))
            if not up:
                continue
            g[up]["targets"] += 1
            g[up][(r["faster"] + "_faster") if r["faster"] else "tie"] += 1
        ups[n] = {k: dict(v) for k, v in g.items()}
    out["first_upstream"] = ups
    return out


def common_paths(rows, names, results):
    """target -> that site's most common AS path, per site."""
    out = {}
    for n in names:
        out[n] = {}
        for r in results:
            hrs = rows[n].get((r["target"], r["kind"]), {})
            out[n][r["target"]] = mode([h["as_path"] for h in hrs.values()]) or ()
    return out


def clusters_and_fixes(results, paths):
    cl = defaultdict(list)
    for r in results:
        if not r.get("cause"):
            continue
        ev = r["evidence"]
        cl[(r["cause"], r["slower"], ev.get("network"), ev.get("prev"))].append(r)
    clusters = []
    for (c, s, x, p), rs in sorted(cl.items(), key=lambda kv: -len(kv[1])):
        clusters.append({"cause": c, "slower": s, "network": x, "prev": p, "targets": len(rs),
                         "router": mode([r["evidence"].get("router") for r in rs]),
                         "median_delta_ms": round(median([r["delta_ms"] for r in rs if r["delta_ms"] is not None]) or 0, 1),
                         "examples": [r["target"] for r in sorted(rs, key=lambda r: -(r["delta_ms"] or 0))[:5]]})
    fx = defaultdict(list)
    for r in results:
        if r.get("cause") not in ("longer_path", "latency_inside", "congestion", "same_path_unexplained"):
            continue
        us = first_up(paths[r["slower"]].get(r["target"]))
        uf = first_up(paths[r["faster"]].get(r["target"]))
        if us and uf and us != uf:
            fx[(r["slower"], us, r["faster"], uf)].append(r)
    fixes = []
    for (s, us, f, uf), rs in sorted(fx.items(), key=lambda kv: -len(kv[1])):
        dest = Counter(paths[f].get(r["target"], ())[-1] for r in rs if paths[f].get(r["target"]))
        fixes.append({"slower": s, "slower_via": us, "faster": f, "faster_via": uf, "targets": len(rs),
                      "median_delta_ms": round(median([r["delta_ms"] for r in rs if r["delta_ms"] is not None]) or 0, 1),
                      "dest_networks": [d for d, _ in dest.most_common(6)]})
    return clusters, fixes


# ---------- outputs ----------

def asn_label(asn, owners):
    if not asn:
        return "?"
    o = owners.get(str(asn), "")
    return f"AS{asn} {o}".strip()


def code(s):
    return "`" + str(s).replace("`", "") + "`"


def table(head, rows):
    out = ["| " + " | ".join(head) + " |", "|" + "---|" * len(head)]
    for r in rows:
        out.append("| " + " | ".join("" if v is None else str(v).replace("|", "/") for v in r) + " |")
    return "\n".join(out)


def ms(v):
    return "" if v is None else f"{v:.1f}"


def lossp(v):
    return "" if v is None else f"{v * 100:.1f}%"


PH = "[[narrative:{}]] _(written by the agent with run.sh fill)_"


def write_outputs(out, names, win, man, results, owners, rdns, warnings, version, partial):
    a, b = names
    lo, hi = win
    comp = [r for r in results if not r["thin"]]
    slower = [r for r in comp if r["slower"]]
    counts = Counter(r["faster"] for r in slower)
    causes = Counter(r["cause"] for r in slower)
    paths = man["_paths"]
    grp = groups(results, names, paths)
    clusters, fixes = clusters_and_fixes(slower, paths)
    window = f"{iso(lo)[:16].replace('T', ' ')}–{iso(hi)[11:16]} UTC, {hi - lo} hours"
    if hi - lo >= 24 or iso(lo)[:10] != iso(hi - 1)[:10]:
        window = f"{iso(lo)[:16].replace('T', ' ')} to {iso(hi)[:16].replace('T', ' ')} UTC, {hi - lo} hours"
    sites = {n: {"host": man["sites"][n]["host"], **{k: v for k, v in man["sites"][n]["status"].items() if k != "fetched"}}
             for n in names}
    med_extra = {n: round(median([r["delta_ms"] for r in slower if r["slower"] == n and r["delta_ms"] is not None]) or 0, 1)
                 for n in names}
    summary_facts = {
        "window": window, "targets_compared": len(comp),
        "faster": {n: counts.get(n, 0) for n in names},
        "ties": len(comp) - len(slower),
        "median_extra_ms_when_slower": med_extra,
        "causes": {CAUSE_TEXT[c]: k for c, k in causes.most_common()},
        "by_region": {k: v for k, v in sorted(grp["geo"].items())},
        "fixes": [{"slower": f["slower"], "slower_via": asn_label(f["slower_via"], owners),
                   "faster": f["faster"], "faster_via": asn_label(f["faster_via"], owners),
                   "targets": f["targets"], "median_delta_ms": f["median_delta_ms"],
                   "dest_networks": [asn_label(d, owners) for d in f["dest_networks"]]} for f in fixes[:8]],
        "data_quality": [w["detail"] for w in warnings],
    }
    findings = {"version": version, "window": {"from": iso(lo), "to": iso(hi), "hours": hi - lo},
                "sites": sites, "thresholds": {"min_ms": MIN_MS, "min_ratio": MIN_RATIO, "min_loss": MIN_LOSS,
                                               "thin_hours": THIN_HOURS},
                "targets": results, "groups": grp, "clusters": clusters, "fixes": fixes,
                "warnings": warnings, "data_quality": {"survey": "partial" if partial else
                                                       ("off" if all(not man["sites"][n]["files"].get("survey") for n in names) else "ok")},
                "summary_facts": summary_facts}
    with open(os.path.join(out, "data", "findings.json"), "w") as f:
        json.dump(findings, f, indent=1, default=list)

    L = []
    L.append(f"# Comparing {a} and {b}\n")
    L.append("> Internal: contains router addresses and host names. To share outside your team, send share-summary.md.\n")
    L.append(f"**Window:** {window}. " + "; ".join(
        f"**{n}:** fyisp {sites[n]['version']}, profile {code(sites[n]['profile'])}, survey {sites[n]['survey']}" for n in names) + ".\n")
    L.append("## Data quality\n")
    L.append("\n".join(f"- {w['site'] + ': ' if w['site'] else ''}{w['detail']}" for w in warnings) or "No problems found.")
    L.append("")
    L.append("## Summary\n")
    L.append(PH.format("summary") + "\n")
    L.append(table(["", a, b], [
        ["faster for", f"{counts.get(a, 0)} targets", f"{counts.get(b, 0)} targets"],
        ["median extra latency where slower", f"{med_extra[a]} ms", f"{med_extra[b]} ms"]]))
    L.append(f"\n{len(comp)} targets compared, {len(comp) - len(slower)} about the same.\n")
    L.append("## By region\n")
    L.append(table(["region", "targets", f"{a} faster", f"{b} faster", "same", "median gap ms"],
                   [[k, v.get("targets"), v.get(a + "_faster", 0), v.get(b + "_faster", 0), v.get("tie", 0), v.get("median_delta_ms")]
                    for k, v in sorted(grp["geo"].items(), key=lambda kv: -kv[1]["targets"])]))
    L.append("\n## First upstream network\n")
    L.append("Which network each site hands its traffic to first, and how its targets fare.\n")
    for n in names:
        rows = sorted(grp["first_upstream"][n].items(), key=lambda kv: -kv[1]["targets"])[:10]
        L.append(f"**{n}**\n")
        L.append(table(["upstream", "targets", f"{a} faster", f"{b} faster", "same"],
                       [[code(asn_label(k, owners)), v.get("targets"), v.get(a + "_faster", 0), v.get(b + "_faster", 0), v.get("tie", 0)]
                        for k, v in rows]) + "\n")
    L.append("## Causes\n")
    L.append(table(["cause", "targets"], [[CAUSE_TEXT[c], k] for c, k in causes.most_common()]))
    L.append("\n" + PH.format("causes") + "\n")
    L.append("## Clusters\n")
    L.append("Slower targets grouped by cause and the network where the time is added (and the one before it).\n")
    L.append(table(["cause", "slower", "network", "after", "example router", "targets", "median gap ms", "examples"],
                   [[CAUSE_TEXT[c["cause"]], c["slower"], code(asn_label(c["network"], owners)) if c["network"] else "",
                     code(asn_label(c["prev"], owners)) if c["prev"] else "",
                     code(f"{c['router']} {rdns.get(c['router'], '')}".strip()) if c["router"] else "",
                     c["targets"], c["median_delta_ms"], ", ".join(code(e) for e in c["examples"][:3])]
                    for c in clusters[:20]]))
    L.append("\n## Suggested routing changes\n")
    L.append("Where the slower site's first upstream differs from the faster site's for the same targets.\n")
    L.append(table(["slower", "its upstream", "faster", "its upstream", "targets", "median gap ms", "destination networks"],
                   [[f["slower"], code(asn_label(f["slower_via"], owners)), f["faster"], code(asn_label(f["faster_via"], owners)),
                     f["targets"], f["median_delta_ms"], ", ".join(code(asn_label(d, owners)) for d in f["dest_networks"][:4])]
                    for f in fixes[:12]]))
    L.append("\n" + PH.format("fixes") + "\n")
    L.append("## Slowest targets\n")
    top = sorted(slower, key=lambda r: -(r["delta_ms"] or 0))[:30]
    L.append(table(["target", "provider", "region", "slower", f"{a} p50", f"{b} p50", "gap ms", f"{a} loss", f"{b} loss",
                    "cause", "confidence"],
                   [[code(r["target"]), code(r["provider"]), r["geo"], r["slower"], ms(r["p50"][a]), ms(r["p50"][b]),
                     ms(r["delta_ms"]), lossp(r["loss"][a]), lossp(r["loss"][b]), CAUSE_TEXT[r["cause"]], r["confidence"]]
                    for r in top]))
    un = [r for r in slower if r["cause"] == "unreachable"]
    if un:
        L.append("\n## Unreachable from one site\n")
        L.append(table(["target", "provider", "unreachable from"], [[code(r["target"]), code(r["provider"]), r["unreachable"]] for r in un]))
    L.append("\n## Method\n")
    L.append(f"Hours {iso(lo)} ≤ hour < {iso(hi)} that both sites measured in full. Per target (TCP connects, else HTTPS): a site is "
             f"slower when the median of the hourly differences of the median connect time is at least {MIN_MS:.0f} ms and its median "
             f"is at least {MIN_RATIO}× the other's, or when its loss over those hours is at least {MIN_LOSS * 100:.0f} points higher "
             f"(latency wins when they disagree). Targets with fewer than {THIN_HOURS} shared hours are left out. Confidence is the "
             "share of hours the same rule picks the same site. Causes, first match: unreachable (no answer most hours), GeoDNS (no "
             "destination /24 in common), anycast (a global target, same address, different last router), longer path (more "
             "networks after the first network both paths share), latency added inside a network (the lower envelope of each hop's "
             f"best round trip rises by at least {MIN_MS:.0f} ms and half the gap at that hop, in most hours: 'at or after' that router), "
             "congestion (one path all window, hourly medians spread at least that much on the slower site only), then same path, "
             "unexplained. Router names and network owners come from the networks themselves.\n")
    write(os.path.join(out, "report.md"), "\n".join(L))

    S = [f"# {a} vs {b}: summary\n", "> OK to forward: no addresses or host names.\n", f"**Window:** {window}.\n",
         PH.format("share") + "\n",
         table(["", a, b], [["faster for", f"{counts.get(a, 0)} targets", f"{counts.get(b, 0)} targets"],
                            ["median extra latency where slower", f"{med_extra[a]} ms", f"{med_extra[b]} ms"]]),
         f"\n{len(comp)} targets compared, {len(comp) - len(slower)} about the same.\n", "## By region\n",
         table(["region", "targets", f"{a} faster", f"{b} faster", "same"],
               [[k, v.get("targets"), v.get(a + "_faster", 0), v.get(b + "_faster", 0), v.get("tie", 0)]
                for k, v in sorted(grp["geo"].items(), key=lambda kv: -kv[1]["targets"])])]
    if fixes:
        S += ["\n## Suggested routing changes\n",
              table(["slower", "its upstream", "faster", "its upstream", "targets", "median gap ms"],
                    [[f["slower"], asn_label(f["slower_via"], owners), f["faster"], asn_label(f["faster_via"], owners),
                      f["targets"], f["median_delta_ms"]] for f in fixes[:8]])]
    S += ["\n## Data quality\n", "; ".join(w["detail"] for w in warnings) or "No problems found."]
    write(os.path.join(out, "share-summary.md"), "\n".join(S) + "\n")
    return summary_facts


def write(path, text):
    with open(path, "w") as f:
        f.write(text)


def fill(out, text):
    """Replace [[narrative:NAME]] placeholders with the agent's sections ('@@ NAME' lines start one)."""
    secs, cur = {}, None
    for line in text.splitlines():
        m = re.match(r"^@@ ([a-z]+)\s*$", line)
        if m:
            cur = m.group(1)
            secs[cur] = []
        elif cur:
            secs[cur].append(line)
    left = []
    for fn in ("report.md", "share-summary.md"):
        p = os.path.join(out, fn)
        with open(p) as f:
            s = f.read()
        for name, lines in secs.items():
            body = "\n".join(lines).strip()
            body = re.sub(r"[^\S\n]+\n", "\n", re.sub(r"[\x00-\x08\x0b-\x1f\x7f]", "", body)).replace("[[", "[").replace("]]", "]")
            body = re.sub(r"<[^>]*>", "", body)[:4000]
            if fn == "share-summary.md":
                body = re.sub(r"\b\d{1,3}(?:\.\d{1,3}){3}\b|\b[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{0,4}){3,7}\b", "[address]", body)
            s = s.replace(PH.format(name), body)
        left += [f"{fn}: {m}" for m in re.findall(r"\[\[narrative:([a-z]+)\]\]", s)]
        write(p, s)
    return left


def emit(out, members):
    """The outputs leave as a tar on stdout, read by run.sh."""
    with tarfile.open(fileobj=sys.stdout.buffer, mode="w|") as tf:
        for m in members:
            p = os.path.join(out, m)
            if os.path.exists(p):
                tf.add(p, arcname=m, recursive=False)


# ---------- main ----------

def window_from(spec, now):
    end = int(now) // 3600
    m = re.fullmatch(r"([0-9]{1,2})h", spec)
    if m:
        n = int(m.group(1))
        if not 1 <= n <= 24:
            raise Blocked("window: 1h to 24h. Pick a shorter window")
        return end - n, end
    m = re.fullmatch(r"(\S+)\.\.(\S+)", spec)
    if not m:
        raise Blocked("window: '24h' or FROM..TO in UTC (2026-11-22T00:00Z..2026-11-22T12:00Z). Fix the window")
    try:
        lo, hi = (hour_of(x.replace("Z", ":00Z") if x.count(":") == 1 else x) for x in m.groups())
    except ValueError:
        raise Blocked("window: times must be whole UTC hours like 2026-11-22T06:00Z. Fix the window")
    hi = min(hi, end)
    if not 1 <= hi - lo <= 24:
        raise Blocked("window: 1 to 24 whole hours, ending before the current hour. Fix the window")
    return lo, hi


def run_fetch(args, out):
    data = os.path.join(out, "data")
    os.makedirs(data, exist_ok=True)
    dl = Deadline(args.deadline)
    man_path = os.path.join("in", "data", "manifest.json")
    if os.path.exists(man_path):
        with open(man_path) as f:
            man = json.load(f)
        for f in os.listdir(os.path.join("in", "data")):
            if f.endswith(".csv.gz"):
                os.replace(os.path.join("in", "data", f), os.path.join(data, f))
        names = man["names"]
        win = (hour_of(man["window"]["from"]), hour_of(man["window"]["to"]))
    else:
        names = args.names
        if len(names) != 2 or names[0] == names[1] or not all(SITE_RE.match(n) for n in names):
            raise Blocked("sites: give two different site names. Fix the command")
        win = window_from(args.window, time.time())
        man = {"names": names, "window": {"from": iso(win[0]), "to": iso(win[1])}, "version": args.version, "sites": {}}
    text = ""
    if os.path.exists("sites"):
        with open("sites") as f:
            text = f.read()
    sites = parse_sites(text, names)
    say(f"[fetch] {names[0]} and {names[1]}, {iso(win[0])} to {iso(win[1])} ({win[1] - win[0]} h)")
    errors, timed_out = {}, False
    with ThreadPoolExecutor(2) as ex:
        futs = {ex.submit(fetch_site, s, win, data, man, dl): s["name"] for s in sites}
        for fu, n in futs.items():
            try:
                fu.result()
            except OutOfTime:
                timed_out = True
            except Blocked as e:
                errors[n] = str(e)
    man_out = {k: v for k, v in man.items() if not k.startswith("_")}
    write(os.path.join(data, "manifest.json"), json.dumps(man_out, indent=1))
    keep = ["data/manifest.json"] + [f"data/{f}" for f in os.listdir(data) if f.endswith(".csv.gz")]
    if errors:
        for n in names:
            if n in errors:
                say(errors[n])
        emit(out, keep)
        return BLOCKED
    if timed_out:
        say("Out of time before every file arrived; what arrived is kept. Continue with: run.sh resume <report folder>")
        emit(out, keep)
        return OUT_OF_TIME
    return analyze_and_emit(names, win, data, man, out, args.version, keep)


def analyze_and_emit(names, win, data, man, out, version, keep):
    warnings = []
    st = {n: man["sites"][n]["status"] for n in names}
    a, b = names
    if st[a]["version"] != st[b]["version"]:
        warnings.append({"code": "version_mismatch", "site": "", "detail": f"the sites run different fyisp releases ({st[a]['version']}, {st[b]['version']}); upgrade both to the same one"})
    if st[a]["profile"] != st[b]["profile"]:
        warnings.append({"code": "profile_mismatch", "site": "", "detail": "the sites use different profiles; only targets both measure are compared"})
    for n in names:
        if st[n]["site"] and st[n]["site"] != n:
            warnings.append({"code": "site_name", "site": n, "detail": f"its server calls itself {st[n]['site']}"})
        if st[n]["survey"] != "on":
            warnings.append({"code": "survey_" + st[n]["survey"], "site": n,
                             "detail": "the path survey is off: causes can't be found (run it with --path-survey)" if st[n]["survey"] in ("off", "")
                             else f"the path survey is {st[n]['survey']}: paths may be missing for some hours"})
        try:
            skew = abs(datetime.fromisoformat(st[n]["now"]).timestamp() - st[n]["fetched"])
            if skew > 60:
                warnings.append({"code": "clock_skew", "site": n, "detail": f"its clock is {skew:.0f} s off; keep the clocks synchronized"})
        except (ValueError, KeyError):
            pass
        if man["sites"][n].get("survey_errors"):
            warnings.append({"code": "survey_partial", "site": n, "detail": f"{len(man['sites'][n]['survey_errors'])} survey chunks could not be fetched"})
    partial = any(man["sites"][n].get("survey_errors") for n in names)
    say("[analyze] comparing targets")
    results, owners, rdns, rows = analyze(names, win, data, man, warnings)
    man["_paths"] = common_paths(rows, names, results)
    facts = write_outputs(out, names, win, man, results, owners, rdns, warnings, version, partial)
    say(f"[write] {facts['targets_compared']} targets compared: {a} faster for {facts['faster'][a]}, "
        f"{b} faster for {facts['faster'][b]}, {facts['ties']} about the same")
    for w in warnings:
        say(f"[warn] {w['site'] + ': ' if w['site'] else ''}{w['detail']}")
    emit(out, keep + ["data/findings.json", "report.md", "share-summary.md"])
    return OK


def main(argv=None):
    p = argparse.ArgumentParser()
    p.add_argument("mode", choices=["fetch", "analyze", "fill"])
    p.add_argument("--names", nargs="*", default=[])
    p.add_argument("--window", default="24h")
    p.add_argument("--deadline", type=float, default=time.time() + 540)
    p.add_argument("--version", default="dev")
    args = p.parse_args(argv)
    out = "out"
    os.makedirs(os.path.join(out, "data"), exist_ok=True)
    try:
        if args.mode == "fetch":
            return run_fetch(args, out)
        if args.mode == "analyze":
            data = os.path.join("in", "data")
            with open(os.path.join(data, "manifest.json")) as f:
                man = json.load(f)
            names = man["names"]
            win = (hour_of(man["window"]["from"]), hour_of(man["window"]["to"]))
            return analyze_and_emit(names, win, data, man, out, args.version, [])
        for fn in ("report.md", "share-summary.md"):
            os.replace(os.path.join("in", fn), os.path.join(out, fn))
        with open(os.path.join("in", "narratives.txt")) as f:
            left = fill(out, f.read())
        emit(out, ["report.md", "share-summary.md"])
        if left:
            say("Sections still empty: " + ", ".join(left) + ". Write them and run fill again")
            return BLOCKED
        return OK
    except Blocked as e:
        say(str(e))
        return BLOCKED
    except OutOfTime:
        say("Out of time. Continue with: run.sh resume <report folder>")
        return OUT_OF_TIME
    except Exception as e:  # never a traceback: only our own messages reach the transcript
        say(f"compare-sites failed: {type(e).__name__} at line {traceback.extract_tb(e.__traceback__)[-1].lineno}. Report it with that line")
        return BLOCKED


if __name__ == "__main__":
    sys.exit(main())

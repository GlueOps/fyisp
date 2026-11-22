# Comparing sites

fyisp at two sites (two datacenters, two offices, two providers) tells you where each one is slower; with `--path-survey` it also keeps what you need to tell **why**. Back to the [README](../README.md).

## With an AI agent

The repository ships an agent skill, [`.claude/skills/compare-sites`](../.claude/skills/compare-sites/SKILL.md), that does the whole comparison: it fetches `api/status`, `api/compare.csv` and `api/survey.csv` from two sites, finds which site is slower to which targets and why (the causes [below](#telling-the-causes-apart)), and writes `report.md` (internal: it lists routers and their addresses) and `share-summary.md` (networks by number and owner only, to forward). Its script runs in a locked-down Docker container (no capabilities, read-only, as `nobody`), so the machine that runs it needs Docker and nothing else, and it reaches the sites only over their HTTP API with the admin token.

1. **Prepare the sites.** Each runs the same release with the README's [server command](../README.md#on-a-server-datacenter-or-office) (`--path-survey`, its own `--site`), for a day or more if you want time-of-day patterns. To open two sites from one machine, each needs its own local address or port (see the server section's note on *421 unknown host*).
2. **Save the sign-in links** in `~/.config/fyisp/sites`, one `name=<link>` per line, readable by you only. To keep the tokens out of the agent's session, write the file yourself, e.g. for a server reached over an SSH forward to port 3001:

   ```sh
   mkdir -p -m 700 ~/.config/fyisp
   (umask 077; printf 'site-b=http://127.0.0.1:3001/#admin=%s\n' "$(ssh site-b cat .fyisp-token)" >> ~/.config/fyisp/sites)
   ```

   A link may use plain `http://` only to this machine (an SSH forward), or, followed by ` http-ok`, to an address on a network you trust (a VPN, Tailscale); otherwise use the `--tunnel` link (`https://…`). You can also paste a link into the session: the skill saves it, and then it is in the session's transcript and logs (restarting fyisp with a new token signs everyone out).
3. **Run it** from your own directory. The reports go to `./fyisp-reports/`, which must not be committed: run.sh refuses a git repository that doesn't ignore it (this repository does). In Claude Code, open a clone of this repository, or copy the skill folder into your own project's `.claude/skills/` (or `~/.claude/skills/`), and type `/compare-sites site-a site-b 24h`, or a UTC window of up to 24 whole hours, `2026-11-22T00:00Z..2026-11-22T12:00Z` (twelve hours: the hour at the end is not included). A site not saved yet can come along as `site-b=<link>` after the window; the skill saves it, and the token is then in the session's transcript and logs. The skill starts only when you ask for it, and asks for nothing midway: if a link is missing it stops and says so.

The script can also run without an agent: `.claude/skills/compare-sites/scripts/run.sh compare site-a site-b 24h`, then `run.sh fill <folder>` with the narrative sections on stdin, or read the tables as they are.

**Where it runs.** Its container uses the Docker host's network: on Docker Desktop (macOS, Windows), turn on host networking (Settings → Resources → Network, Docker Desktop 4.34 or newer) to reach an SSH forward on your machine; in a devcontainer that talks to the host's Docker, open SSH forwards on the Docker host, or use the sites' `--tunnel` links. Each run fetches for at most 9 minutes; if that isn't enough, it says so and `run.sh resume <folder>` fetches the rest.

**When it stops**, each message names the site, what happened and what to do. Two need a word more: *the admin token was refused* usually means fyisp restarted without `FYISP_ADMIN_TOKEN` and made a new token, and *the share link's tunnel is down or expired* means the `--tunnel` name changed (fyisp prints the new one in its log).

**The report** starts with *Data quality*: different releases or profiles, a survey still on its first pass or off, a site clock more than a minute off the machine that ran the comparison, targets with too few hours, and paths the survey is less sure of (changing flows). Its causes compare the two paths where they cross the same networks, so a long stretch both share (an ocean crossing) doesn't count; *latency added in a network* names the network and the router after which the slower site's time grows. Its suggested routing changes say what kind of change each is: *peer* (the faster site reaches the destination's network directly), *prefer* (the slower site already has that upstream for other targets) or *new transit*.

## What to run

At every site, the same fyisp release with the same profile, and `--path-survey` on. The README's [server command](../README.md#on-a-server-datacenter-or-office) does that with Docker, `--profile all` and `--path-survey`, on a pinned release.

The path survey traces the TCP path to every TCP and HTTPS target of the profile every 15 minutes (to the target's own host and port, one TTL at a time) and keeps each target's paths hour by hour: which router answered each TTL, how often and how fast. It is separate from *Investigate* and the always-on traces and doesn't change what they measure. With `--profile all` it adds about 25 probes a second and, over the default 90 days, roughly 1 to 4 GB of disk. What routers and targets see is in [OPERATORS.md](OPERATORS.md). Both machines need the right time (NTP): the files are matched by UTC time.

`api/status` shows the survey's progress (`survey.state` is `first_pass` until every target has been tried once, about 15 minutes after the start). The survey also keeps the address each target's name resolved to (`--addrs`).

## What to export

Pick a UTC window and get the same window from every site, over HTTP or over SSH.

**Over HTTP**, with each site's address and admin token (an SSH tunnel per site, or another way to reach the dashboard): `api/status` first, to check every site runs the same `version` and `profile` with clocks in step (`now`), then `api/compare.csv`: a row per target, kind and hour with the connects' median and p95, the destination addresses and the networks past the provider ([API.md](API.md#get-apicomparecsv-apisurveycsv-and-apiaddrscsv)). Give each site a name with `--site` (the README's server command does), so every row says where it came from.

```bash
W="from=2026-12-01T13:00:00Z&to=2026-12-01T17:00:00Z"
for s in site-a:3000 site-b:3001; do
  n=${s%%:*} p=${s##*:}
  curl -fsS --compressed -H @<(printf 'Authorization: Bearer %s\n' "$(cat ~/.fyisp-token-$n)") \
    "http://127.0.0.1:$p/api/compare.csv?$W" > "$n-compare.csv"
done
```

Before the loop:
- **Tokens:** copy each server's admin token to your laptop once, readable by you only: `(umask 077; ssh site-a cat .fyisp-token > ~/.fyisp-token-site-a)`.
- **Tunnels:** one per site, each to its own port, e.g. `ssh -N -L 3001:127.0.0.1:3001 site-b` for a server started with `--listen=127.0.0.1:3001`: fyisp checks the port in the address you open, so each server needs its own.

The window takes RFC 3339 times (or `now-6h` and unix milliseconds), and every hour that *starts* in it is included in full: 13:00 to 17:00 gives five hours, the last one 17:00 to 18:00. `--compressed` makes a week's file about 14 times smaller on the way. `api/survey.csv` and `api/addrs.csv` are the same files as the exports below, for a closer look: with `--profile all` a day of `survey.csv` is about 60 MB, so ask for a day, or name the targets (`targets=`).

**Over SSH**, from your laptop (here `site-a` and `site-b`, running the server command):

```sh
W="--from 2026-12-01T13:00:00Z --to 2026-12-01T17:00:00Z"
for s in site-a site-b; do
  ssh "$s" docker exec fyisp /fyisp export $W --kind tcp,https > "$s-latency.csv"
  ssh "$s" docker exec fyisp /fyisp export $W --survey > "$s-survey.csv"
  ssh "$s" docker exec fyisp /fyisp export $W --addrs > "$s-addrs.csv"
done
```

The files land on your laptop, none on the servers. Don't add `-t` to `docker exec` (it turns line ends into CRLF), and don't use `-o` there: the container can't write to its own working directory. With systemd, the command is `sudo fyisp export --data-dir /var/lib/fyisp $W …`. The formats are in [API.md](API.md#fyisp-export). The files are named after their site because nothing in them says where they came from: fyisp never stores your public address.

## Telling the causes apart

For every target that is slower from one site in the window (its `rtt_ms` in the latency files), compare the two survey files, hour by hour:

| What the survey files show | Likely cause |
|---|---|
| Different `dest_ip` (and different addresses in `--addrs`) | GeoDNS: the name sends each site to a different place (each round looks the name up just before it traces) |
| Same `dest_ip`, a different router at the TTL before the destination, and a large gap in the destination's `min_ms` | Anycast: the same address answers from a different site |
| Same `dest_ip`, different networks (`asn`) along the way | Routing: each site's providers carry it differently. The `asn` right after `provider` is the network that took it on |
| Same path, but from one TTL on every later TTL's `min_ms` is higher, worst at busy hours | Congestion from that hop on |
| `reached` is 0 at one site only | Unreachable from that site |

Some things to keep in mind:

- **Use `min_ms`**, not one router's `mean_ms`: routers answer traceroutes at low priority, and a router's own answer can be slow while traffic through it is not. A real latency step carries on to every later TTL and to the destination.
- **`n` is small**: about 4 rounds an hour. Look at several hours. The routers every target goes through (yours, your provider's, often the first transit network's) get more survey rounds a second than fyisp's per-router cap allows; such a TTL takes the same router's answer to another target's round within the last minute, or is left out (`complete` < `rounds`).
- **A row's `asn`** is the network that announces the router's address. Links between two networks are often numbered from one side, so the handoff may show one TTL early or late; `rdns` names often say the network and city.
- **`fixed_flow` < `rounds`** means some rounds went out from changing ports (on Windows, `hops_via` is `icmp`; past a NAT or firewall that drops a flow's repeated SYNs, the rounds that recovered from fresh ports): with load balancing, those may mix routers of parallel paths. Compare sites that measure the same way. Behind NAT (carrier-grade NAT, a cloud NAT gateway) the outside port can change from one round to the next even when `fixed_flow` = `rounds`: each round is one flow, but two routers at one TTL within an hour may still be parallel paths.
- **`yours`, `provider` and `private` rows have no address** by design: they are your network, your provider's, and private routers past it. An empty `dest_ip` is a target in your own or your provider's network. Leave them out when comparing paths: each site's own network and provider differ by definition.

## A starting script

This Python script (standard library only) lists, for each target and hour where site A's TCP connect is at least 10 ms and 20% slower than site B's, which of the causes above the survey files point to.

```python
import csv, statistics, sys
from collections import defaultdict

def latency(path):  # (target, hour) -> median rtt_ms
    d = defaultdict(list)
    for r in csv.DictReader(open(path)):
        if r["rtt_ms"]:
            d[(r["target"], r["ts"][:13])].append(float(r["rtt_ms"]))
    return {k: statistics.median(v) for k, v in d.items()}

def survey(path):  # (target, hour) -> {dest, reached, hops: {ttl: (status, ip, asn, n, min_ms)}}
    d = {}
    for r in csv.DictReader(open(path)):
        s = d.setdefault((r["target"], r["hour"][:13]), {"dest": r["dest_ip"], "reached": 0, "hops": {}})
        s["reached"] = max(s["reached"], int(r["reached"]))
        if r["status"] != "silent":
            ttl = int(r["ttl"])
            best = s["hops"].get(ttl)
            if best is None or int(r["n"]) > best[3]:
                s["hops"][ttl] = (r["status"], r["ip"], r["asn"], int(r["n"]), float(r["min_ms"] or 0))
    return d

def as_path(s):  # the networks past each site's own network and provider
    out = []
    for ttl in sorted(s["hops"]):
        status, _, a = s["hops"][ttl][:3]
        if status == "answered" and a and (not out or out[-1] != a):
            out.append(a)
    return out

def cause(a, b):
    if a is None or b is None:
        return "no survey"
    if a["reached"] == 0 and b["reached"] > 0:
        return "unreachable from A"
    if a["dest"] != b["dest"]:
        return "GeoDNS (different addresses)"
    pa, pb = as_path(a), as_path(b)
    if pa != pb:
        return "routing: A " + ">".join(pa) + " vs B " + ">".join(pb)
    pen = lambda s: s["hops"].get(max(s["hops"]) - 1, (None, None))[1]
    if a["reached"] and b["reached"] and a["hops"] and b["hops"] and pen(a) != pen(b):
        return "anycast (different last router)"
    return "same path: congestion or the site's own network"

la, lb = latency(sys.argv[1]), latency(sys.argv[2])
sa, sb = survey(sys.argv[3]), survey(sys.argv[4])
w = csv.writer(sys.stdout)
w.writerow(["target", "hour", "a_ms", "b_ms", "extra_ms", "likely_cause"])
for k in sorted(la.keys() & lb.keys()):
    a, b = la[k], lb[k]
    if a - b >= 10 and a >= 1.2 * b:
        w.writerow([k[0], k[1] + ":00Z", round(a, 1), round(b, 1), round(a - b, 1), cause(sa.get(k), sb.get(k))])
```

Save it as `compare.py` and run it with the four files: `python3 compare.py site-a-latency.csv site-b-latency.csv site-a-survey.csv site-b-survey.csv`. It lists only the hours where the first site is slower: run it again with the sites swapped (`site-b-latency.csv site-a-latency.csv site-b-survey.csv site-a-survey.csv`) for the others.

# AGENTS.md

How to work on fyisp, for people and coding agents. User docs are in [README.md](README.md) and [docs/](docs/) (the API index is [docs/API.md](docs/API.md)), the rules for adding endpoints in [CONTRIBUTING.md](CONTRIBUTING.md), and security reports go to [SECURITY.md](SECURITY.md).

fyisp is one static Go binary (`./cmd/fyisp`). It probes services with TCP connects, STUN and ICMP, keeps up to 90 days in one SQLite file, and serves a dashboard and a JSON API on 127.0.0.1:3000.

## Repo map

| Path | What it is |
|---|---|
| `cmd/fyisp` | The binary: flags, wiring, subcommands (`profiles`, `export`, `paths`, `version`) |
| `cmd/fyisp-webdev` | Dev server: the real UI and API over fake probes. Never released |
| `internal/profile` | Target catalog (`catalog/*.yml`), named profiles, `default.yml`, Home tiles, `--config` |
| `internal/probe`, `netinfo`, `trace` | Probes, gateway and ISP-edge discovery, traceroutes, the path survey (`trace/survey.go`) |
| `internal/store` | SQLite store (hourly zstd blobs, summaries, incidents, routes, speed tests) |
| `internal/verdict`, `tiles`, `callq`, `baseline` | Whose fault it is, Home tiles, call quality, "normal" for each series |
| `internal/speedtest` | Pure-Go iperf3 client and scheduler. Servers are in `internal/profile/catalog/iperf3/` |
| `internal/web` | HTTP handler, JSON API, embedded UI (`static/`), goldens (`testdata/`) |
| `internal/metrics`, `export`, `asn` | `/metrics`, `fyisp export`, the built-in IP → AS table |
| `internal/storage` | Storage tests: targets and saved keys in `storage-targets.json` (atomic, owner-only), checks, campaigns (rounds in the speed-test queue) |
| `internal/s3test` | Storage tests: a SigV4 signer and its own HTTP/1.1 client, a round's phases, error classes; `s3fake` is a fake S3 for tests |
| `internal/tcpinfo` | TCP_INFO (Linux): RTT, retransmits, MSS, bytes acked |
| `internal/tunnel` | `--tunnel`: a Cloudflare quick tunnel run in-process (cloudflared linked in). Imports nothing from fyisp |
| `test/netns`, `test/ui` | Network-fault harness (Linux, sudo), Playwright specs and fixtures |
| `scripts/`, `packaging/`, `docs/` | Launchers, the systemd unit, user, API and operator docs |
| `.claude/skills/compare-sites` | Agent skill comparing two sites over the HTTP API: `SKILL.md`, `scripts/run.sh` (the only command it runs; fixes the Docker flags and image), `scripts/compare_sites.py` (stdlib Python, runs in that container) |

## Build and test: Docker only

Don't install Go or Node on the host. Everything runs through the [Dockerfile](Dockerfile).

| Task | Command |
|---|---|
| gofmt check, vet, `go test -race ./...` | `docker build --target test .` |
| UI tests (Playwright), with screenshots and report | `docker buildx build --target uitest-out --output type=local,dest=out --no-cache-filter uitest-run .` |
| Launcher tests | `docker build --target launchertest .` and `docker build --target launchertest-pwsh .` |
| systemd unit: `systemd-analyze verify`, and fails if `ProcSubset=` is set | `docker build --target servicetest .` |
| Runtime image smoke test | `docker build --target imagetest .` |
| compare-sites skill: its tests, and that `run.sh` pins `PYTHON_IMAGE` and the current release | `docker build --target skilltest .` |
| Storage engine against RustFS and Garage (`s3lab-full`: and MinIO, built from source) | `docker build --target s3lab .` |
| Release binaries and SHA256SUMS | `docker buildx build --target dist --output type=local,dest=dist .` |
| Runtime image | `docker build -t fyisp .` |
| `go mod tidy` | `docker buildx build --target modfiles --output type=local,dest=. .` |
| `gofmt -w` | `docker buildx build --target fmt --output type=local,dest=. .` |
| Regenerate the last release's database fixture (after tagging a release) | `docker buildx build --target dbfixture --output type=local,dest=. .` |
| Network-fault tests (**Linux, needs sudo**, ~20 min) | `test/netns/run.sh` (one scenario: `test/netns/run.sh -test.run 'NetworkFaults/a-baseline'`) |

The Dockerfile's stage comments list the rest (`uitest-report`, `uilock`, `asndb`). `out/` is not git-ignored, so delete it after a UI run.

**One Go test, or any go command:** use the `source` stage. It needs network access, because modules are not in the image.

```sh
docker build --target source -t fyisp-src .
docker run --rm fyisp-src go test ./internal/web -run TestHomeGolden -count=1
docker run --rm fyisp-src sh -c 'GOOS=windows go vet ./...'   # after touching *_windows.go
```

**Run a CLI command against your tree:** `docker build -t fyisp-dev . && docker run --rm fyisp-dev profiles aws`.

**Update goldens.** `docker run -v` doesn't work everywhere (see Pitfalls), so copy the files out:

```sh
docker run --name fyisp-gold fyisp-src go test ./internal/web -run 'TestHomeGolden|TestSpeedGolden' -update
docker cp fyisp-gold:/src/internal/web/testdata ./internal/web/ && docker rm fyisp-gold
git diff internal/web/testdata   # review the copy changes
```

**fyisp-webdev** serves the real UI over fake data, behind a fixed admin token. Open <http://localhost:3000/#admin=fyisp-webdev-not-a-secret> (as `localhost`, because any other Host name gets 421).

```sh
docker build --target webdev -t fyisp-webdev .
docker run --rm -p 3000:3000 fyisp-webdev /fyisp-webdev -listen 0.0.0.0:3000 -home blip
```

## Rules that tests enforce

- **Store schema:** `internal/store/sqlite.go` holds the full DDL, `SchemaVersion()` and `migrations`. Changes are additive only: new tables, columns and indexes; never drop, rename or retype anything, and keep reading the stored blob formats. A DDL change goes into `schema`, bumps `schemaVersion`, adds the step from the previous version to `migrations` (one transaction upgrades the file in place), and updates `tables` and `wantSchema` (`schema_test.go`). `internal/store/testdata/v0.19.0.db` is a database written by the last release (the `dbfixture` stage); `upgrade_test.go` checks that it opens with all of its rows, that every table, column and index in it survives with the same type, and that `migrations` cover every step from its schema. If a step is missing, fyisp refuses to start and leaves the file alone. A database from before v0.11.0 (schema older than 8) is replaced with an empty one, and a newer one is refused.
- **Names are identities.** Catalog target names, iperf3 server ids and region ids key the stored history. Never rename them.
- **Catalog:** the format is in [SCHEMA.md](internal/profile/catalog/SCHEMA.md). `docker build --target test .` runs all the catalog checks. Changing `default.yml` or a `tile:` tag changes the Home goldens.
- **iperf3 servers:** add the server to its provider file and to a region in `regions.yml`. Add a new metro to `metroZones` (`internal/speedtest/catalog.go`), and update the count in `TestBuiltinCatalog` and the table in `docs/IPERF3-TERMS.md`. List every new id in `internal/speedtest/testdata/shipped-servers.txt`. To remove a server, delete it and move its id to `removed:` in `regions.yml`: fyisp then drops its stored results at start, keeping only the test windows.
- **cloudflared:** `go.mod` pins it at a commit and repeats its `replace` directives (Go ignores them in dependencies). To bump it, change the version, copy its `replace` lines, and set `CloudflaredVersion` and `cloudflaredCommit` in `internal/tunnel/tunnel.go` (and the release in the `go.mod` comment); `TestCloudflaredVersionMatchesGoMod` checks both against the linked module and `TestReplaceDirectivesMatchCloudflared` the `replace` lines. Renovate is told to leave those three modules alone (`renovate.json`). A new cloudflared can raise the Go version (`GO_IMAGE` in the Dockerfile and `go-version` in ci.yml). Its live tests are the `livetest` stage and the manual "live tunnel" workflow.
- **compare-sites skill:** a change to `CompareHeader` or `export.SurveyHeader` also updates `.claude/skills/compare-sites/scripts/testdata/headers.txt` (`TestCompareSitesSkillHeaders`) and whatever the script reads. `run.sh`'s `IMAGE` equals the Dockerfile's `PYTHON_IMAGE`, and its `VERSION` the release (release-please bumps it via `extra-files`). Its tests start a fake fyisp and check that no output, message or tar carries the token.
- **Web UI:** plain ES modules, no build step, and a strict CSP. That means no inline scripts, styles or handlers, and no `innerHTML`. Build the DOM with `h()` from `core.js` and reuse its helpers (`fmtMs`, `fetchJSON`, `apiWrite`, ...). An API change also updates `docs/API.md` and `docs/*-API.md`.

## Conventions

- **Commits:** Conventional Commits, lowercase, e.g. `feat(web)!: ...`, `fix(ui): ...`, `docs(readme): ...`. `!` marks a breaking change. The body says why.
- **PRs:** keep them focused. They are squash-merged, so the PR title becomes the commit on `main` and must be a Conventional Commit (release-please loses commits behind merge commits). Open an issue before large changes.
- **CI** runs the Docker targets above, the UI tests when the UI changes, and native `go test` on Windows and macOS. The UI specs run four at a time against one shared fyisp-webdev. A spec that changes that server's state goes in `STATEFUL` in `test/ui/playwright.config.js`; those run one at a time afterwards, and not at all if an earlier spec failed (`--no-deps` runs them alone). A spec that needs its own data starts its own fyisp-webdev (`webdev()` in `fixtures.js`) on a port no other test uses. The netns suite runs nightly. Write timing tests with `testing/synctest`, not sleeps.
- **Releases:** `release-please.yaml` opens a release PR on `main` once a `feat`, `fix`, `perf`, `revert` or breaking commit has landed (other types ship with the next one and stay out of `CHANGELOG.md`). Before 1.0 a breaking change bumps the minor. Merging the PR creates the `v*` tag and a draft GitHub release; the tag runs `release.yml`, which publishes the draft once the binaries are uploaded. Releases are immutable: a published one can't be changed, and re-running the workflow for it leaves it alone. Don't push release tags by hand. release-please also bumps the README server command's `IMAGE=` tag (`extra-files`, the `x-release-please-start-version` / `x-release-please-end` block around it): keep only that line in the block, which rewrites every version-like string inside it (`127.0.0.1` too), and don't edit the tag by hand. After the release, set the new tag everywhere the fixture's release is written (the Dockerfile's header comment and the `dbfixture` stage's `RELEASE`; the comment and `Version` in `internal/store/testdata/genfixture/main.go`; `lastRelease` in `internal/store/upgrade_test.go`; the fixture's file name in the store schema rule above), regenerate the fixture with `dbfixture`, delete the old one and commit.

## Project policies

- **Roll forward,** with additive migrations from the last release's schema. No compat shims or deprecation periods. Remove dead code. fyisp makes no backups: users handle their own.
- **Docs describe current behaviour**, with no version history. Check every statement against the code.
- **No telemetry.** A new outbound connection must be documented in README "Privacy" and SECURITY.md.
- **Never store, log or serve the user's public IP address** (`cmd/fyisp/privacy_test.go`).
- **Be kind to operators.** Raising any probe frequency or volume needs an update to `docs/OPERATORS.md`, `IPERF3-TERMS.md` or `STUN-TERMS.md`.

## Pitfalls

- **Docker-outside-of-Docker** (devcontainers, some CI runners): `docker run -v $PWD:/src` mounts a path on the Docker host, not your checkout. Use `--output type=local` stages or `docker cp`.
- **Per-OS files** (`_windows.go`, `_darwin.go`, ...): `test` vets only `netinfo` for other OSes. Run `GOOS=windows go vet ./...` or rely on the native CI.
- **The Intel macOS runner stalls** for a second or more with the packages' tests side by side, so CI runs them one package at a time there. Tests over real sockets still need wide margins, and a closed loopback port can time out there instead of refusing.
- **Host allowlist:** the handler answers 421 to unknown Host headers, so tests must set `r.Host`.
- **SQLite pragmas:** modernc runs `_pragma` DSN values in lexicographic order, so persistent pragmas are set in code (`sqlite.go`), not in the DSN.

## Decisions

Settled choices; reviewers shouldn't re-raise them.

- **Comparing sites over HTTP uses the admin token** (2026-11-22): `api/compare.csv`, `api/survey.csv` and `api/addrs.csv` sit behind it like every route, with `--site` naming each deployment. No read-only or expiring token: the owner chose the least code. If one is ever added, it needs its own allowlist of GET routes, never `authorized()`: the CSRF token is in the public page.
- **Looking glass = Investigate, owner only** (2026-11-22): rounds 10 to 250 (`rounds=`, the dashboard's default 200) and a *Ping summary* report, for pasting into a datacenter ticket. No page or token for third parties (it would be the first route without the admin token, and only traces outward anyway), no TCP-only mode (NOCs expect ICMP). TCP takes one port per trace (`port=`, default 443): a port other than 443 is its own trace named `host:<host>:<port>` (its own series and slot), so 443's history never mixes with it; no ranges, so still no port scan (SECURITY.md).
- **`api/probes` "likely dropped"** is TCP probes of 1000 to 1400 ms: a SYN resent after a 1 s initial RTO. It is labelled as likely, not proven (nothing records the retransmit, and Windows may resend at 3 s, which is a timeout).
- **`api/probes` has no gaps.** `api/panel`'s per-bucket `gap` already says when fyisp wasn't measuring.
- **`api/probes` caps at the slowest 5000**, not the first, so a capped answer still shows the worst probes; `complete_above_ms` says what is complete.
- **"Probes over X ms"** (Details charts) shows only when one target is shown, and only for the panel's main kind. The threshold is typed, not dragged (dragging the plot zooms), starts at the verdict engine's spike rule (3× normal, at least +50 ms) and is kept per target and kind in localStorage, not the URL. The min–max band stays.
- **The target filter is a regular expression** (the owner's choice over an or-and-wildcard syntax), shared by the Overview and Charts in `oq`. It is capped at 100 characters and matched in a worker (`static/match.js`) that the page stops after 250 ms of matching, because the pattern can arrive in a link: never run it on the page itself, and never send it to the server.
- **The combined chart** (Charts, `#cc=1`) draws 2 to 8 of the filter's targets: the palette has 8 hues. It has no loss strip while several lines show (a sum over providers misleads), min–max bands only up to 4 targets (also when one of more is isolated), and the group charts' legend markup. The browser sends target names to `api/panel?targets=` (up to 16 there, for other callers), never the pattern.
- **Speed over time** (Details → Speed) is in Mbit/s, never "% of normal": a score is against the server's last 14 days, so a lasting slowdown drifts back to 100%. The pooled line is the 90th percentile of each point ("your faster tests"), not a median, which sits on a far server and misses a slowdown above its speed. It has its own range (24 hours to 90 days) beside the heatmap's span, draws at most 8 targets (the palette's hues), and keeps its range, zoom and targets out of the URL. With one target chosen every test is its own point while about 0.6 fit to a pixel (more is a band that hides the line); the row view's scatter over the days is gone.
- **Each round over time** (a storage campaign's results) is one chart with a line per target (show one alone, or pick several), not a panel each. It has three metrics, time to first byte (the default: every target has it), download and upload speed, and leaves out a schedule's hours between windows (a time fyisp wasn't running stays a gap). `part=timings` covers every round, not the cards' newest 50,000 attempts.
- **GlueOps org, Apache-2.0** (Copyright GlueOps, LLC), module `github.com/glueops/fyisp` (lowercase, like other GlueOps Go modules). Tags up to v0.14.0 keep their earlier module path and MIT license.
- **The path survey keeps hourly rows only** (`survey_hops`): no per-hop series, no stored TCP routes or route changes, no medians (about 4 rounds an hour). Paths over time come from the hourly rows. It never uses the other traces' shared hops or bumps the tracer's route-change generation, and takes a router's token only from a full bucket, so the other traces measure as before; a capped survey TTL may take the same router's answer to another survey round within a minute.
- **The survey's flow is fixed on Linux only.** macOS and BSD match TCP answers by source port, so a late answer would be taken for the next TTL's; there and on Windows (`hops_via=icmp`) rounds use changing ports and are not counted in `fixed_flow`. On Linux, when two probed TTLs of a round in a row are silent (TTLs the cap left out between them don't count), both are asked once more from fresh ports (each once its router's bucket is full again; twice a round, each time for two TTLs not asked yet), because some NATs and firewalls drop a flow's repeated SYNs; when one answers, the round goes on unfixed. One lost answer, or routers that answer neither way, keep the flow. Accepted: a lost answer at the end of a silent run (a router that never answers, then TTLs the cap left out) unfixes that round, and a recovered TTL may come from another per-flow path.
- **The survey skips UDP-only (STUN) and ICMP-only targets**, and its hops are classified when recorded (`classifyRoute`, shared with storage-test routes), not at export: an address of your network or provider is never stored.
- **The path survey is off by default** (the owner's choice, 2026-11-22: home users measure the basics; the README's server command adds `--path-survey`, with `--profile all`). `--path-survey` or `FYISP_PATH_SURVEY=1` turns it on.
- **`--addrs` is kept only with `--path-survey`**, never a LAN address or one in the provider's network; the survey blanks a destination it classifies as yours or your provider's.
- **`--addrs` is keyed by host, not target**: the runner resolves hosts, and `fyisp export` has no profile; the survey rows carry both.
- **compare-sites skill** (2026-11-22): HTTP only (no SSH), with the admin token from sign-in links in `~/.config/fyisp/sites`; the script always runs in Docker, through `run.sh`, which is the only command the skill pre-approves, and never with bind mounts (files go in and out as tar streams, because Docker-outside-of-Docker mounts the host's paths). It ships from a clone first; a plugin marketplace (`.claude-plugin/`) comes later, so add no plugin component folders or a `SKILL.md` at the repository root. Two sites, windows of up to 24 hours.
- **No upgrade notes for installs before v0.15.0** (old image name, old macOS launchd label): everyone moves to v0.15.0 or newer.

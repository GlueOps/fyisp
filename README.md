# fyisp

Is your internet good enough for calls and work right now, and when it isn't, is it your Wi-Fi, your internet provider or one app? fyisp answers that in plain words and keeps 90 days of evidence: when it went wrong, for how long, and whose fault it likely was. It runs on your own computer, NAS or Raspberry Pi, with no account and no telemetry.

![fyisp Home: the answer card, the connection strip from your router to the internet, 11 app tiles with their last 2 hours (Microsoft Teams' panel open, with its 24-hour chart), and the problems of the last 7 days](docs/img/home.png)

- **Calls, work apps and websites.** 11 tiles (Zoom, Microsoft Teams, Google Meet, Slack, Microsoft 365, Google Workspace, GitHub, Dropbox, OneDrive, YouTube and Netflix), each *Good*, *Slow* (*Choppy* for calls) or *Down*, with the last 2 hours in 5-minute bars.
- **Whose fault it is.** fyisp also checks your own router and your provider's first router, so it can say *your Wi-Fi or router*, *your internet provider*, *the wider internet* or *one app*.
- **Evidence for your provider.** Every problem of the last 7 days with its time, length and likely cause, on a page with no addresses on it, so a screenshot is safe to send. 90 days of charts and `fyisp export` for the raw numbers.
- **Private.** Your data stays on your machine. fyisp talks only to the services it checks, your DNS resolver and the routers on the way (for traceroutes), plus speed-test servers, storage endpoints or Cloudflare only if you turn those on; [Privacy](#privacy) lists everything.

fyisp is one small program. The default makes 28 checks for homes and remote workers and opens on the plain-words **Home** view. **Technical details** has every number: charts per service, traceroutes, call-quality scores, speed and storage tests, Prometheus metrics, and about 1,400 more endpoints (clouds, CDNs, gaming, regions of the world) [one flag away](docs/PROFILES.md).

## Quick start

For your home connection or a laptop, nothing to install: paste one command, and a launcher downloads fyisp, checks it against the release's published checksum, and runs it until you stop it. It checks the 28 everyday services above and nothing more. (For a server, a datacenter or an office, see [On a server](#on-a-server-datacenter-or-office) near the end.)

### Linux and macOS

Open **Terminal** (on macOS, or any terminal on Linux) and paste:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/glueops/fyisp/releases/latest/download/run.sh | sh
```

### Windows

Open **PowerShell** (not Command Prompt) and paste:

```powershell
& ([scriptblock]::Create((irm https://github.com/glueops/fyisp/releases/latest/download/run.ps1)))
```

On a NAS, a Raspberry Pi or a home server, use [Docker](#docker-nas-and-raspberry-pi). To read the launcher or check the download yourself first, see [Check before you run](#check-before-you-run).

> **On a work laptop**, read [Privacy](#privacy) first: your employer's network sees fyisp's name lookups, YouTube and Netflix included.

### What happens next

1. The launcher prints `fyisp-run: sha256 ok: …`, then fyisp prints its address and a private sign-in link:

   ```text
     Dashboard:  http://127.0.0.1:3000/

     To sign in, open this link once in each browser (keep it private):
     http://127.0.0.1:3000/#admin=…
   ```

   fyisp opens that link in your browser, signed in. If no browser opens (over SSH, or on a machine without a desktop), open the link yourself; from another machine, see [Opening it from another machine](#opening-it-from-another-machine).

   If port 3000 is busy (and you didn't pass `--listen`), fyisp takes the next free one, up to 3010: use the address it prints.
2. **Home** says *Checking your connection…*. After about a minute it has its first answer, and the tiles fill in as it runs.
3. For its first day fyisp is still learning what's usual for your connection, and Home flags only big problems until then.

Leave the window open: fyisp records only while it runs. **Stop it** with Ctrl-C, which saves first (closing the window also stops it, but can lose the last minute). **Update** by stopping it and running the same command again. Your history stays in fyisp's data directory, so the next run carries on. To remove fyisp, see [Where data lives, and uninstalling](#where-data-lives-and-uninstalling).

**Next:** to have it start by itself and keep a full record, see [Keep it running](#keep-it-running). Then read [What you see](#what-you-see-home-and-details).

### Options

Every option, with its default (and environment variable, where it has one), is in `fyisp --help` (`fyisp export --help` for exports). With the launcher nothing is installed, so ask the launcher, or Docker:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/glueops/fyisp/releases/latest/download/run.sh | sh -s -- --help
docker run --rm ghcr.io/glueops/fyisp --help
```

```powershell
& ([scriptblock]::Create((irm https://github.com/glueops/fyisp/releases/latest/download/run.ps1))) --help
```

Pass options after `sh -s --` on Linux and macOS, after `)))` in PowerShell (quote a value with a comma, which PowerShell would otherwise split), and after the image name in Docker:

```sh
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/glueops/fyisp/releases/latest/download/run.sh | sh -s -- --profile 'default,streaming'
```

```powershell
& ([scriptblock]::Create((irm https://github.com/glueops/fyisp/releases/latest/download/run.ps1))) --profile 'default,streaming'
```

The ones people use most:

| Option | What it does |
|---|---|
| `--profile NAMES` | What to measure, instead of the default. Combine profiles with commas to keep the default's checks, e.g. `default,streaming` or `default,clouds`; `all` is every target (about 1,400). `fyisp profiles` lists them ([What to measure](docs/PROFILES.md)). |
| `--speedtest` | Turns on scheduled speed tests, each of 15 regions once an hour. On a 100 Mbit/s line that is about 1.1 to 3 TB a month: leave it off on a metered or capped connection ([Speed tests](#speed-tests)). **Test speed now** on Home runs one test without it. |
| `--listen 127.0.0.1:8080` | Another port. `0.0.0.0:<port>` opens the dashboard to your network, behind the [admin token](#admin-token). |
| `--retention 30d` | Keeps less history, and so less disk (the default is 90 days). |
| `--admin-token TOKEN` | Your own sign-in token, kept across restarts (otherwise each start makes a new one); see [Admin token](#admin-token). |

### Check before you run

The launcher is pinned to the release it came from. It downloads the matching binary into a temporary directory, **refuses to run it unless its SHA-256 matches the release's `SHA256SUMS`**, also checks the GitHub build provenance attestation if the [GitHub CLI](https://cli.github.com/) is installed and logged in, and deletes the binary when fyisp exits. To read it before running it:

```sh
curl --proto '=https' --tlsv1.2 -fsSLO https://github.com/glueops/fyisp/releases/latest/download/run.sh
less run.sh
sh run.sh
```

Or download the binary yourself: get `fyisp-<os>-<arch>` (`linux-amd64`, `linux-arm64`, `linux-armv7`, `darwin-amd64`, `darwin-arm64`, `windows-amd64.exe`, `windows-arm64.exe`) and `SHA256SUMS` from the [latest release](https://github.com/glueops/fyisp/releases/latest) into one folder, keeping their names, then verify and run it. On Linux (on macOS, the first line is `shasum -a 256 -c SHA256SUMS --ignore-missing`; use your file's name, and `gh attestation verify` is optional):

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify fyisp-linux-amd64 --repo glueops/fyisp
chmod +x fyisp-linux-amd64 && ./fyisp-linux-amd64
```

On Windows, this runs it only if the checksum matches:

```powershell
$want = (Select-String -Path .\SHA256SUMS -Pattern 'fyisp-windows-amd64\.exe$').Line.Split(' ')[0]
if ((Get-FileHash .\fyisp-windows-amd64.exe).Hash -eq $want) { .\fyisp-windows-amd64.exe } else { 'checksum mismatch: do not run it' }
```

<details>
<summary>"Windows protected your PC" and macOS Gatekeeper</summary>

The binaries are not code-signed. Windows SmartScreen and macOS Gatekeeper check only files that carry the browser's "downloaded from the internet" mark. The launchers download without it, so they show no warning.

- **Windows, browser download:** SmartScreen says "Windows protected your PC". Choose **More info**, then **Run anyway**, or, after verifying the checksum, remove the mark once with `Unblock-File .\fyisp-windows-amd64.exe`. Double-clicking the `.exe` opens a console window with the log and the dashboard address; closing the window stops fyisp. To pass options, run it from a terminal or put them in a shortcut's **Target**.
- **macOS, browser download:** Gatekeeper blocks it. After verifying it, run `xattr -d com.apple.quarantine fyisp-darwin-arm64`.

</details>

### If something's off

- **The page asks you to sign in.** Open the newest sign-in link fyisp printed. Each start makes a new one, which signs every browser out; to keep one, pass your own [admin token](#admin-token).
- **`fyisp is already running`.** Another fyisp uses the same data directory: use that one (its sign-in link is in its window or log), or stop it first.
- **`ICMP probes disabled` in the log.** fyisp then can't tell your router from your provider and runs no traceroutes; see [ICMP permissions](#icmp-permissions).
- **`is /tmp mounted noexec?`** Run the launcher with `TMPDIR` set to a directory that allows running programs, e.g. `mkdir -p ~/tmp && curl … | TMPDIR=$HOME/tmp sh`.
- **`421 unknown host`.** You opened the dashboard by a name: start fyisp with `--allow-host <that name>`.

### Opening it from another machine

fyisp listens on `127.0.0.1` (this machine only) by default. To open it from your laptop or phone, either start it with `--listen 0.0.0.0:3000` and replace `127.0.0.1` in the sign-in link with the machine's address (e.g. `http://192.168.1.10:3000/#admin=…`), or keep it local and forward the port over SSH (`ssh -L 3000:127.0.0.1:3000 <host>`, then open the link on your laptop). For access from outside your home, see [Remote access](#remote-access).

## What you see: Home and Details

**Home** opens first. It answers one question, *is my internet OK for calls and work, and which apps work?*, in plain words. Numbers stay in the tile panels.

- **The answer card**: one sentence, e.g. *Everything you use is working normally.*, *Your internet provider is likely having trouble.*, *Your internet is fine. Zoom isn't connecting.* or, for 30 minutes after a drop, *Working again. Your internet dropped for about 40 seconds at 10:14 am.* The line under it gives the *likely cause* and what it means for you (*Teams and Google Meet are working if you need to switch*). Below that: how many problems the last 24 hours had, and when fyisp last checked. The card's color follows your connection, not the worst app: Zoom down on Zoom's side is a green card. The browser tab's title says the same in a few words (*✓ All good*, *✕ Provider problem*).
- **Your connection**: *Your home router → Your internet provider → The internet*, each with an icon (✓, ! or ✕) and a short line: *Working normally*, *Losing some data since 10:12 am*, *Dropped for 40 s at 10:14 am, fine now*. The first box says *Your Wi-Fi and router* only when fyisp can tell it is looking at the Wi-Fi of the machine you're reading on; otherwise *Your home router* (see [Wi-Fi or router?](docs/VERDICTS.md#wi-fi-or-router)).
- **11 app tiles**: Zoom, Microsoft Teams, Google Meet and Slack (calls); Microsoft 365, Google Workspace, GitHub, Dropbox and OneDrive (apps); YouTube and Netflix (streaming). Each says *Good*, *Slow* (*Choppy* for calls) or *Down*, with *since 10:12 am* when it isn't Good, or *Dropped 40 s at 10:14 am* for 30 minutes after a drop. Under it, 24 bars show the last 2 hours in 5-minute steps: green, yellow (slow for 2 minutes or more), red (down, or a drop of 30 seconds or more) or grey (fyisp wasn't running). A tile your profile doesn't cover says *Not checked* and how to turn it on.
- **Click a tile** for its panel: why it has that word, its numbers against their limits (delay, jitter and loss for calls; the usual time and the *Slow above* limit for the others), and a 24-hour chart with the usual line, the problems and the time fyisp wasn't running. Escape or *Close* returns to the tile. *How fyisp decides Good, Slow, Choppy and Down*, under the tiles, explains the rules (see [How fyisp decides](docs/VERDICTS.md)).
- **Speed**: your latest result in Mbps against your usual speed (once there are a few tests), and **Test speed now**, which runs one test against fyisp's built-in servers. Scheduled tests are off unless you turn on [speed tests](#speed-tests).
- **Problems in the last 7 days**: a summary by cause (*5 problems: 1 likely from your internet provider, 1 from your home router, 3 from the apps themselves*), then each problem with its time, what happened, its *likely cause* and one line of evidence (*Your router kept answering, but nothing past it did.*). A problem during one of fyisp's own speed tests says so.
- **The footer**: *Watching your connection since …* and **Technical details →**.

<img src="docs/img/home-phone.png" width="260" align="right" alt="fyisp Home on a phone, in the dark theme, during a problem at the internet provider: a red card, the provider marked on the connection strip, call tiles Choppy and the other apps Slow or Down since 8:59 pm">

On a phone the strip stacks and the tiles take 2 columns (right: a problem at the internet provider, in the dark theme).

For its first day fyisp is still learning what's usual for your connection: Home says so and flags only big problems until then. If fyisp hasn't checked for 2 minutes or more (the computer it runs on is asleep, or fyisp stopped), the card turns grey, says since when, and the tiles are dimmed.

**Technical details** (*Technical details →* on Home; the *Home | Details* switch at the top goes back) is the full instrument panel: the verdict and outage log, the [Calls view](docs/CALL-QUALITY.md) with scores per app and *What happened at …?*, the [Overview](docs/PROFILES.md#the-overview-large-profiles), charts for every service (filter them with a pattern such as `ap-south-2|us-east`, isolate one to count and mark each probe slower than a threshold you set), traceroutes (*Investigate*), speed tests and CSV export.

Home's 11 tiles and what each one checks, the 28 default targets and the profiles that add about 1,400 more are in [What to measure](docs/PROFILES.md); how a tile becomes *Slow*, *Choppy* or *Down*, and how fyisp decides whose fault a problem is, in [How fyisp decides](docs/VERDICTS.md).

### Investigate (traceroutes)

*Investigate* follows one target's path hop by hop, like `mtr`. The *ICMP | TCP | UDP* picker chooses the probes: ICMP echo (like `mtr`), TCP SYNs to port 443, or the port you type in the box next to *TCP* (like `mtr -T -P 443`: the same path and treatment as that traffic, useful when routers filter or deprioritise ICMP), or UDP to port 33434 (like `mtr -u`). A trace to another TCP port is a trace of the target's host to that port, with its own numbers and URL (`#t=host:example.com:22`). None needs admin rights. Each probe type is its own trace, with its own numbers; always-on traces use ICMP, and route changes are recorded for ICMP traces only. The table reads like `mtr -rwz`, and each hop's first row matches its line in the copied mtr text (more addresses at one hop are rows of their own, with their numbers; mtr lists them under it): *Host* as mtr -z shows it (`AS64500  name (address)`, `???` for a router that never answers), *Loss%*, *Snt* (probes sent), *Last*, *Avg*, *Best*, *Wrst* and *StDev* (round trips in ms). A last *Note* column adds what mtr doesn't say: where real loss starts, a router that only rate-limits its replies, a router that never answers (harmless), your ISP's hops, and the network's name. *More columns* adds *P95*, *Javg* (average jitter), *Jmax* (worst jitter) and a bar from best to worst with a tick at the average, and the browser remembers the choice. Over ranges longer than 48 hours fyisp has only hourly summaries, so *Last*, *StDev* and *Jmax* are blank.

- **Loss**: the first hop where real loss starts (it continues to the destination) is marked *◀ loss starts here*. A middle hop that drops replies while the destination doesn't is marked *rate-limited, not real*. The line above the table says the same in words. See [Hop loss flags](docs/TRACE-API.md#hop-loss-flags).
- **Range**: while a trace runs, and for an hour after it finishes, the numbers cover that trace (*This trace* in the range buttons). Pick another range to see more; *This trace* goes back.
- **Rounds**: how many rounds the next on-demand trace runs, like `mtr -c`: 10 (about 25 s), 100 (about 4 minutes), 200 (about 8 minutes, the default) or 250 (about 10 minutes). The browser remembers the choice; a running trace keeps its own.
- **Copy mtr text** (above the table) copies it as `mtr -rwz` text in one click, and says so if the trace is still running. **Copy report** has the other formats: `mtr -j` JSON, `mtr -C` CSV, *Ping summary* (`ping`'s statistics for the destination: packets sent and received, loss, min/avg/max/mdev) and *Traceroute* (`traceroute`'s shape, one line per hop with three times: the best, average and worst over the rounds, not three single probes; `* * *` for a router that never answers), for a support ticket or a forum post. The source host is always `fyisp`, never your machine's name or address. The text ends with how fyisp measured: the probe type, the round interval, and at most 1 probe a second per router; for TCP and UDP the JSON also has `type`, `port` and `probe`.
- **Any public host**: type a name or an IPv4 address in the host box to trace it with the picked probe type, like `mtr <host>`. It must resolve to public addresses only: fyisp refuses names and addresses on your LAN (private, loopback, link-local, carrier-grade NAT, multicast and the like) and says why under the box, and stops a trace whose name later resolves to one. The URL (`#t=host:example.com`) reopens it. The same limits apply as for targets; see [Hosts outside the profile](docs/TRACE-API.md#hosts-outside-the-profile).
- **Run it yourself**: the same trace as an `mtr` command for the target (`mtr -rwzbc 100 <host>`, with `-T -P <port>` or `-u -P 33434` for TCP and UDP), to compare from Linux or macOS (it may need `sudo`), and `pathping -n <host>` (ICMP only) for Windows.

**When your datacenter or provider asks for an MTR or a ping**, open *Investigate* (on a server, over the [SSH tunnel](#on-a-server-datacenter-or-office)), type the host or address they name, pick ICMP (what most NOCs expect) or TCP (if ICMP is filtered on the way; 443, or the port they name) and the rounds they ask for (the default 200 covers most requests), and click *Trace host*. When it says *Finished*, click *Copy mtr text* (or *Copy report* → *Ping summary* or *Traceroute*, whichever they asked for), and paste it into the ticket. The report says `fyisp` instead of your address: add your server's address yourself. A trace from your side to theirs is all fyisp can run; the reverse path (theirs to yours) comes from their side.

**Platform notes.** Tracing, in any mode, needs fyisp's ICMP socket to start (see [ICMP permissions](#icmp-permissions)); without it no trace runs, and Investigate shows every probe type as unavailable. The TCP and UDP probes themselves need no ping socket: Linux reads the routers' replies to them from the socket's error queue, and macOS gets them on its unprivileged ICMP socket. On **Windows** the default firewall drops the routers' replies to TCP and UDP probes of a standard user, so a TCP trace sends an ICMP echo request and a TCP SYN to its port with the same TTL at every hop and names the routers from the ICMP replies (the view says so), and UDP traces aren't offered. Traces are IPv4 only.

Each round stops after 5 silent hops in a row past the last one that answered (like `mtr --max-unknown`), or past the destination once fyisp knows how far it is.

## Keep it running

fyisp can only tell you about time when it was running. Started from a terminal or the launcher, it stops when you close that window or log out, and Home shows that time as grey bars, and as hatched bands in a tile's chart: *fyisp wasn't running*. To have it start by itself, install the binary once and register it with your system, as below. It then runs in the background whenever you're logged in (or, on Linux and Docker, whenever the machine is on) and you open <http://127.0.0.1:3000> and sign in (see [Admin token](#admin-token)).

A laptop that sleeps still leaves gaps; that's expected and never counted as a problem. For a complete record, run fyisp on something that is always on and on the same network: a NAS, a Raspberry Pi or a home server, with [Docker](#docker-nas-and-raspberry-pi) or [systemd](#linux-systemd).

### macOS (LaunchAgent)

In Terminal (Apple silicon; on an Intel Mac use `fyisp-darwin-amd64`):

```sh
mkdir -p ~/.local/bin && cd ~/.local/bin
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/fyisp-darwin-arm64
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing && mv fyisp-darwin-arm64 fyisp && chmod +x fyisp

tok=$(openssl rand -hex 16)
cat > ~/Library/LaunchAgents/io.github.glueops.fyisp.plist <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.github.glueops.fyisp</string>
  <key>ProgramArguments</key><array><string>$HOME/.local/bin/fyisp</string><string>--open-browser=false</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>EnvironmentVariables</key><dict><key>FYISP_ADMIN_TOKEN</key><string>$tok</string></dict>
  <key>StandardOutPath</key><string>$HOME/Library/Logs/fyisp.log</string>
  <key>StandardErrorPath</key><string>$HOME/Library/Logs/fyisp.log</string>
</dict></plist>
PLIST
chmod 600 ~/Library/LaunchAgents/io.github.glueops.fyisp.plist
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/io.github.glueops.fyisp.plist
echo "Sign in once per browser: http://127.0.0.1:3000/#admin=$tok"
```

fyisp now starts whenever you log in, with the admin token the last line printed (it is also in the plist). Its log is `~/Library/Logs/fyisp.log`. To stop it for good: `launchctl bootout gui/$(id -u)/io.github.glueops.fyisp && rm ~/Library/LaunchAgents/io.github.glueops.fyisp.plist`. Downloaded with `curl`, the binary carries no quarantine mark, so Gatekeeper doesn't block it.

### Windows (Task Scheduler)

In PowerShell (on an ARM PC use `fyisp-windows-arm64.exe`):

```powershell
$dir = "$env:LOCALAPPDATA\Programs\fyisp"; New-Item -ItemType Directory -Force $dir | Out-Null
$url = "https://github.com/glueops/fyisp/releases/latest/download"
Invoke-WebRequest "$url/fyisp-windows-amd64.exe" -OutFile "$dir\fyisp.exe"
Invoke-WebRequest "$url/SHA256SUMS" -OutFile "$dir\SHA256SUMS"
$want = (Select-String -Path "$dir\SHA256SUMS" -Pattern 'fyisp-windows-amd64\.exe$').Line.Split(' ')[0]
if ((Get-FileHash "$dir\fyisp.exe").Hash -ne $want) { Remove-Item "$dir\fyisp.exe"; throw "checksum mismatch" }

$b = [byte[]]::new(16); [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
$tok = -join ($b | ForEach-Object { $_.ToString('x2') })
$action  = New-ScheduledTaskAction -Execute "powershell.exe" -Argument "-NoProfile -WindowStyle Hidden -Command & '$dir\fyisp.exe' --open-browser=false --admin-token $tok"
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName fyisp -Action $action -Trigger $trigger -Settings $settings
Start-ScheduledTask -TaskName fyisp
"Sign in once per browser: http://127.0.0.1:3000/#admin=$tok"
```

fyisp now starts, hidden, whenever you log in, with the admin token the last line printed. The hidden task has no log, so `--tunnel`, whose link is printed only to the log, needs fyisp run from a terminal instead. To stop it for good: `Unregister-ScheduledTask -TaskName fyisp -Confirm:$false`, then end `fyisp.exe` in Task Manager.

### Linux (systemd)

On an ARM machine, use `fyisp-linux-arm64` (or `fyisp-linux-armv7` on a 32-bit Raspberry Pi OS) in the first and fourth lines:

```sh
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/fyisp-linux-amd64
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
sudo install -m 0755 fyisp-linux-amd64 /usr/local/bin/fyisp
sudo curl --proto '=https' --tlsv1.2 -fsSL -o /etc/systemd/system/fyisp.service \
  https://raw.githubusercontent.com/glueops/fyisp/main/packaging/fyisp.service
sudo systemctl daemon-reload && sudo systemctl enable --now fyisp
```

fyisp now runs whenever the machine is on, from boot. [`packaging/fyisp.service`](packaging/fyisp.service) runs it as a transient unprivileged user (`DynamicUser`) with a read-only system, no access to home directories and a system-call filter; data lives in `/var/lib/fyisp`. Options go in a drop-in (`sudo systemctl edit fyisp`), e.g. `Environment=FYISP_PROFILE=default,streaming`; the comments in the unit show how to pass other flags (e.g. `--speedtest`), keep one admin token across restarts, or grant `CAP_NET_RAW`. With a generated token, the sign-in link of the current run is in the journal: `sudo journalctl -u fyisp | grep -o 'http://[^ "]*#admin=[0-9a-f]*' | tail -1`; with your own, it is `http://127.0.0.1:3000/#admin=<token>`.

### Docker, NAS and Raspberry Pi

On Linux, run the container on the host's network, so that fyisp sees your real home router, and with your own admin token (the first line makes it, readable by you only), so that a restart doesn't sign you out:

```sh
(umask 077; openssl rand -hex 16 > fyisp-token)
FYISP_ADMIN_TOKEN="$(cat fyisp-token)" docker run -d --name fyisp --restart unless-stopped --network host -v fyisp:/data \
  -e FYISP_ADMIN_TOKEN ghcr.io/glueops/fyisp
echo "http://127.0.0.1:3000/#admin=$(cat fyisp-token)"
```

Open the link the last command prints.

- **From another machine:** replace `127.0.0.1` with the Docker host's address, e.g. `http://192.168.1.10:3000/#admin=…`. The image listens on all addresses (`FYISP_LISTEN=0.0.0.0:3000`), so the dashboard is reachable from your network, behind the token.
- **This machine only:** add `--listen=127.0.0.1:3000` after the image name.
- **By name:** to open it as `http://nas.local:3000` or a Tailscale MagicDNS name, add `--allow-host nas.local` after the image name. Otherwise fyisp answers *421 unknown host* (a guard against DNS rebinding).
- **Doesn't load:** `docker logs fyisp` says why.
- **Raspberry Pi:** a Pi 2 or newer (the image is published for `linux/arm/v7` and `linux/arm64`); the Pi 1 and the first Pi Zero (armv6) aren't supported.
- **Synology and other NAS:** if `docker` needs `sudo` there, put `sudo` first and the token after it, so `sudo` doesn't drop it: `sudo FYISP_ADMIN_TOKEN="$(cat fyisp-token)" docker run …`, and `sudo` before the other `docker` commands. A NAS screen (Synology Container Manager, QNAP Container Station, Portainer, Unraid) can take a Compose project instead; put your own token in it (32 or more characters, e.g. from `openssl rand -hex 16`), or leave out the whole `environment:` block (both lines) and read the sign-in link from the container's log at each start:

  ```yaml
  services:
    fyisp:
      image: ghcr.io/glueops/fyisp
      container_name: fyisp
      restart: unless-stopped
      network_mode: host
      environment:
        FYISP_ADMIN_TOKEN: "replace-with-your-own-token"
      volumes: [fyisp:/data]
  volumes:
    fyisp:
      name: fyisp
  ```

**Stop:** `docker stop fyisp`. **Update:** see [Upgrading](#upgrading). **Remove:** `docker rm -f fyisp && docker volume rm fyisp`.

The command's `--restart unless-stopped` brings the container back after a reboot, as long as Docker itself starts at boot (the default on Linux; on Docker Desktop, turn on *Start Docker Desktop when you sign in*).

<details>
<summary>Docker's default network, Docker Desktop, and the image</summary>

**Bridge network.** With Docker's default bridge network (`-p 3000:3000` instead of `--network host`), the container's gateway is Docker's bridge (`172.17.0.1`), not your home router. fyisp then measures the bridge as your router (always fine), and a Wi-Fi or router problem shows up one step too far out, as your provider or the wider internet. The charts are still right. To use it anyway, publish on this machine only with `-p 127.0.0.1:3000:3000`.

**Docker Desktop (macOS, Windows)** runs containers inside a Linux VM: the gateway is the VM's virtual network and `--network host` means the VM's network, not your Mac's or PC's. The router and provider verdicts aren't about your real network there, so on macOS and Windows use the launcher instead.

**The image** is published for `linux/amd64`, `linux/arm64` and `linux/arm/v7`. It runs as an unprivileged user (65532) from a `scratch` image, keeps its data in the `/data` volume, and sets `FYISP_DATA_DIR=/data` and `FYISP_LISTEN=0.0.0.0:3000` (`--data-dir` and `--listen` after the image name override them). With a bridge network, ICMP works without extra capabilities on Docker 20.10+. With `--network host`, the host's `net.ipv4.ping_group_range` applies: if the log says ICMP is unavailable, see [ICMP permissions](#icmp-permissions).

</details>

## Call quality

fyisp measures the UDP path that call audio and video take with tiny STUN requests to Google's, Cloudflare's, Twilio's and Microsoft Teams' servers, and scores each minute as a mean opinion score (MOS). The Calls view under Technical details answers *is my connection good for calls right now?* and *why did my call at 14:14 go bad?* Details: [Call quality](docs/CALL-QUALITY.md).

## Speed tests

Speed tests are **off by default**. `--speedtest` (or a mode picked in Details → Speed) tests your download and upload speed against public iperf3 servers in 15 regions (9 of them in the US), each once an hour, so you can see whether your connection slows down at certain times of day, and whose network the slowdown is in. A test fills your line while it runs: on a 100 Mbit/s line that is about 1.1 to 3 TB a month, so leave speed tests off on metered or capped connections. Modes, data use, the servers and how they're treated: [Speed tests](docs/SPEED-TESTS.md).

## Storage tests

In Details → Storage, fyisp checks the S3-compatible endpoints you add (any provider, or a server on your network): whether it can connect to each, how long the steps take, and what to fix when one refuses. A campaign tests them over time, on demand or inside a weekly window, and stores every attempt. Nothing is tested until you ask. Details: [Storage tests](docs/STORAGE-TESTS.md).

## What to measure (profiles)

The default measures 28 targets for calls, work apps and websites. `--profile` picks others instead from about 1,400 more (clouds, CDNs, gaming, regions of the world; `--profile default,clouds` keeps the default too), and `--config` loads your own file that adds, removes or changes targets. `fyisp profiles` lists the profiles. Details: [What to measure](docs/PROFILES.md).

## Privacy

fyisp has no account and no telemetry, and its data stays on your machine. What others can see:

- **The services it checks** see a TCP connection from your public IP address every 15 seconds (STUN servers: a few tiny packets every 5 seconds). No data is sent, nobody logs in, no cookies. The STUN servers include **Microsoft's Teams relay** (`worldaz.tr.teams.microsoft.com`, UDP 3478): a burst of 5 tiny STUN requests every 5 seconds, so Microsoft sees your address around the clock, as Google, Cloudflare and Twilio already did.
- **Your DNS resolver** sees fyisp look up each service's name about once a minute, all day. That resolver is usually your provider's, a Pi-hole, or **on a work laptop, your employer's**, whose security tools may log every lookup. The default has one target for each of Home's app tiles, including **GitHub, Dropbox, OneDrive, YouTube and Netflix**: lookups for `youtube.com` or `netflix.com` every minute from a work laptop can look like you stream all day, and the Teams relay's name is looked up every minute too. Social sites (part of `--profile common`) and the rest of streaming (`--profile streaming`) stay opt-in.
- **To leave targets out**, start fyisp with `--config mine.yml`, a file that extends the default and removes them by name; fyisp then neither checks nor looks them up (the format is in [What to measure](docs/PROFILES.md#the---config-file)). In Docker, mount the file and pass `--config` after the image name (e.g. `-v "$PWD/mine.yml:/mine.yml:ro" ghcr.io/glueops/fyisp --config /mine.yml`).

  ```yaml
  # mine.yml
  name: default-no-streaming
  extends: [default]
  remove: [YouTube, Netflix]      # also possible: dev-GitHub, Dropbox, OneDrive, Teams-Relay
  ```
- **On a work laptop**, keep the default (or the default without YouTube and Netflix, as above) and run any extra profiles on a home machine instead: a NAS, a Raspberry Pi or a family computer on the same network sees the same connection. If your employer manages the laptop, check its policy before running monitoring tools on it. `fyisp profiles default` lists the names fyisp looks up (Google Meet's TCP check goes to `meet.google.com`).
- **Hosts traced on request.** *Investigate* traces any public host someone signed in to your dashboard types in (within its limits: 4 at once, at most about 10 minutes each). That host and the routers on the way see probes from your public IP address, and your DNS resolver sees the name looked up, about once a minute while the trace runs. Its hop numbers age out with the rest of the data, but the host's name stays in the database's index of series. No other service is involved.
- **Wi-Fi or router.** To word Home's first box, fyisp reads whether its own connection is Wi-Fi (no network name, nothing about other devices) and whether the page is opened on the same machine, which is never stored, logged or returned (see [Wi-Fi or router?](docs/VERDICTS.md#wi-fi-or-router)). The connection type itself is in fyisp's log and in speed-test results.
- **`--tunnel`** (off by default) connects to Cloudflare to publish the dashboard at a random `https://….trycloudflare.com` address (see [Remote access](#remote-access)). Cloudflare sees your public IP address and your machine's local address, and it decrypts the dashboard's traffic at its edge, the admin token included. Your DNS resolver sees the lookups of `api.trycloudflare.com` and Cloudflare's `argotunnel.com` names (or fyisp asks 1.1.1.1 over TLS), and the request for a tunnel goes through `HTTPS_PROXY` when that is set. The dashboard there needs the admin token, as everywhere. [SECURITY.md](SECURITY.md#what-leaves-your-machine) lists everything the tunnel sends.
- **Storage tests** connect only to the S3-compatible endpoints you save as targets, over IPv4, and only when you ask: a check is one request, and a campaign sends rounds every few minutes (fresh connections, and with keys a download and an upload to your bucket under `fyisp-test/`) while it runs, and traceroutes the provider's address (as *Investigate* does) after the first round and after a connection failure. The endpoint sees your public IP address and your requests; your DNS resolver sees its name looked up for every fresh connection. Targets are kept in `storage-targets.json` in the data directory, readable by fyisp's user only, never in the database. Keys you enter are never shown again, logged or exported: a campaign holds them in memory until it ends, or a weekly schedule saves them in that file until it ends. Through `--tunnel` they pass through Cloudflare, so use keys scoped to a test bucket. [SECURITY.md](SECURITY.md#what-leaves-your-machine) has the details.
- **The path survey** (`--path-survey`, off by default; the server command turns it on) traces the TCP path to every target every 15 minutes, so every router on the way to them sees probes from your public IP address, as with *Investigate*. It keeps no address of your own network or your provider's (only "yours" and "provider", and the provider's network number), and no private address past it. Turning it off stops new rows; what it kept stays until `--retention` ages it out.
- **A screenshot or export you send** shows what it says. Home and *What happened at …?* show no addresses; the Details charts and traceroutes include your router's and your provider's addresses. `fyisp export` lists your targets; its trace rows give the hop number, not the address; `--survey` gives the routers past your provider with their addresses, and `--addrs` the addresses your targets' names resolved to.

## ICMP permissions

fyisp never asks for admin rights. Only the Network path, ICMP traceroutes (Investigate, and the always-on ones of `trace: true` targets), speed tests' latency under load and your own ICMP targets use ICMP (the services get TCP connects, which need no permission). On Linux it uses unprivileged ping sockets, which the kernel allows for the groups in `net.ipv4.ping_group_range` (most distributions allow everyone). If fyisp logs `ICMP unavailable`, allow your group:

```sh
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

That lasts until a reboot; to keep it, also run `echo 'net.ipv4.ping_group_range = 0 2147483647' | sudo tee /etc/sysctl.d/99-fyisp-ping.conf`. fyisp checks for ICMP when it starts, so restart it afterwards (`docker restart fyisp` under Docker). Or carry on without ICMP: the services and the anycast resolvers are still measured over TCP, but the gateway and the ISP edge are not, so the verdict can no longer tell `lan` from `isp`. macOS and Windows need nothing.

## Remote access

To show someone what happened, send a screenshot of Home; for raw data, `fyisp export` (review it first: it lists the targets you measure).

**Over HTTPS from anywhere** (your phone, or your IT team watching the dashboard): start fyisp with `--tunnel` (or set `FYISP_TUNNEL=1`). It opens a Cloudflare quick tunnel from inside fyisp, with nothing to install and no Cloudflare account, and prints the link once it is up:

```sh
fyisp --tunnel
```

```text
  Dashboard over HTTPS:  https://calm-river-words-1234.trycloudflare.com/
```

Under Docker, add `--tunnel` after the image name and read the link from the log: `docker logs fyisp 2>&1 | grep trycloudflare`. The name is random and lives as long as fyisp keeps the tunnel: if the tunnel stays down for 5 minutes, fyisp opens a new one and prints its new link, and the link stops working when fyisp stops. fyisp uses QUIC and switches to HTTP/2 by itself when a network blocks UDP. With a generated admin token, fyisp also prints the private sign-in link for that address (to the terminal; under Docker, systemd or launchd that is the log); with your own, open `https://<name>.trycloudflare.com/#admin=<token>` or paste the token into the sign-in box. Quick tunnels are Cloudflare's free service for testing and demos, without an uptime guarantee.

For a name of your own, run a tunnel or a reverse proxy yourself, for example a [cloudflared named tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) (`cloudflared tunnel run`) or `tailscale serve 3000`, and add its host name, the name the tunnel or proxy uses, with `--allow-host`:

```sh
fyisp --allow-host fyisp.example.com
```

- The address shows the sign-in box until a browser has the [admin token](#admin-token).
- Anyone you give the token to sees the **whole dashboard as you do**, every address and host name, and can run speed tests and traces (*Investigate*), including of a host they run, which shows them your public IP address. For people outside your household, send a screenshot of Home instead.

## Admin token

Everything fyisp serves except the page itself needs the admin token: the dashboard, its API and `/metrics`, on this machine as much as from your network or through a tunnel. Pass your own with `--admin-token` or `FYISP_ADMIN_TOKEN` (at least 32 letters, digits or `- . _ ~`, e.g. from `openssl rand -hex 16`). Without one, fyisp makes a new random token at every start and logs an "open this link" URL with it, like Jupyter; started from a terminal, it opens that link in your browser.

- Open the link once per browser. The page swaps the token for an HttpOnly cookie and removes it from the address bar. The cookie holds a value derived from the token, not the token, but it signs a browser in by itself until the token changes, and browsers send it to every port of that host name: other web apps on the same host name see it and could use it to open this dashboard. Don't run fyisp next to web apps you don't trust under the same host name. The token is in the part after `#`, which browsers never send to a server, so it doesn't end up in proxy logs or `Referer` headers.
- Opening the dashboard from another machine: see [Opening it from another machine](#opening-it-from-another-machine). A browser without the cookie asks for the token instead.
- To sign every browser out, restart fyisp with a new token. A generated token is new at every start, so a restart always signs everyone out: for a service that restarts (Docker, systemd, a NAS), pass your own.
- Under systemd, keep the token out of the unit, which any user can read: put `FYISP_ADMIN_TOKEN=<token>` in a file only root can read and load it with `EnvironmentFile=` (see the comments in [`packaging/fyisp.service`](packaging/fyisp.service)). On Linux and macOS, `--admin-token` on the command line is visible to other users of the machine (`ps`), and `FYISP_ADMIN_TOKEN` is not; fyisp never prints either one, `-h` included.
- Scripts and Prometheus send it as `Authorization: Bearer <token>` (or `X-FYISP-Admin: <token>`); see [Prometheus metrics](#prometheus-metrics).
- The dashboard is plain HTTP: someone who can watch your network traffic could copy the token or the cookie, as with any HTTP login. Keep it on a network you trust, use `-p 127.0.0.1:3000:3000`, or reach it through `--tunnel` (HTTPS).

*Investigate* traces a profile target or any public host (never an address on your LAN), for at most 250 rounds and 4 at once.

## Upgrading

fyisp does not update itself. The latest release is on the [releases page](https://github.com/glueops/fyisp/releases/latest); to see which one runs: `docker exec fyisp /fyisp version` (Docker), `fyisp version` (Linux, `~/.local/bin/fyisp version` on macOS, `& "$env:LOCALAPPDATA\Programs\fyisp\fyisp.exe" version` on Windows), or `version` in `api/status`. Each way below keeps your data, and your admin token where you set your own (the Docker commands, the macOS and Windows tasks, systemd with `/etc/fyisp.env`); otherwise a restart makes a new one.

**Launcher (`run.sh`, `run.ps1`).** Stop fyisp (Ctrl-C) and run the same command again: it always fetches the latest release. It makes a new admin token unless you pass your own with `--admin-token`.

**Docker at home.** From the directory that holds `fyisp-token`: pull the latest image and, only once that worked, replace the container (the volume keeps your data):

```sh
tok=$(cat fyisp-token) && docker pull ghcr.io/glueops/fyisp && docker rm -f fyisp && \
FYISP_ADMIN_TOKEN="$tok" docker run -d --name fyisp --restart unless-stopped --network host -v fyisp:/data \
  -e FYISP_ADMIN_TOKEN ghcr.io/glueops/fyisp
```

Add any options you started it with after the image name, as before.

**Docker on a server** (the [server command](#on-a-server-datacenter-or-office)). Pick the latest release's tag, pull it, then replace the container:

```sh
IMAGE=ghcr.io/glueops/fyisp:$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/glueops/fyisp/releases/latest | sed 's#.*/##')
tok=$(cat ~/.fyisp-token) && echo "$IMAGE" && docker pull "$IMAGE" && docker rm -f fyisp && \
FYISP_ADMIN_TOKEN="$tok" docker run -d --name fyisp --restart unless-stopped --network host -v fyisp:/data \
  --log-driver local --log-opt max-size=10m --log-opt max-file=3 \
  -e FYISP_ADMIN_TOKEN "$IMAGE" --listen=127.0.0.1:3000 --profile all --path-survey --site site-a
```

Keep the options you started it with (this server's `--site`, a second server's `--listen=127.0.0.1:3001`, `--speedtest`, `--retention`; on the bridge network, `-p 127.0.0.1:3000:3000` instead of `--network host` and `--listen`). To compare sites, upgrade every site to the same tag, the same day.

**Linux (systemd).** Download and check the new binary (on ARM, `fyisp-linux-arm64` or `-armv7` in both places), replace the old one and restart:

```sh
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/fyisp-linux-amd64
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing && sudo install -m 0755 fyisp-linux-amd64 /usr/local/bin/fyisp && sudo systemctl restart fyisp
```

**macOS (LaunchAgent).** The same download as in [Keep it running](#macos-launchagent) (on an Intel Mac, `fyisp-darwin-amd64` in both places), without writing a new plist (which would make a new token), then a restart:

```sh
cd ~/.local/bin
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/fyisp-darwin-arm64
curl -fsSLO https://github.com/glueops/fyisp/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing && mv fyisp-darwin-arm64 fyisp && chmod +x fyisp && \
launchctl kickstart -k gui/$(id -u)/io.github.glueops.fyisp
```

**Windows (Task Scheduler).** Download and check the new binary next to the old one, stop fyisp (Windows locks a running `fyisp.exe`), swap them and start it again:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\fyisp"
$url = "https://github.com/glueops/fyisp/releases/latest/download"
Invoke-WebRequest "$url/fyisp-windows-amd64.exe" -OutFile "$dir\fyisp.new.exe"
Invoke-WebRequest "$url/SHA256SUMS" -OutFile "$dir\SHA256SUMS"
$want = (Select-String -Path "$dir\SHA256SUMS" -Pattern 'fyisp-windows-amd64\.exe$').Line.Split(' ')[0]
if ((Get-FileHash "$dir\fyisp.new.exe").Hash -ne $want) { Remove-Item "$dir\fyisp.new.exe"; throw "checksum mismatch" }
Stop-ScheduledTask -TaskName fyisp; Stop-Process -Name fyisp -ErrorAction SilentlyContinue; Start-Sleep 2
Move-Item -Force "$dir\fyisp.new.exe" "$dir\fyisp.exe"
Start-ScheduledTask -TaskName fyisp
```

(On an ARM PC, use `fyisp-windows-arm64.exe` in both places.)

Upgrading keeps your history. When a release changes the database format, its first start upgrades the database in place and logs `store: database upgraded from schema vN to vM`. A database from before v0.11.0 is the exception: it is replaced with an empty one, and fyisp logs `store: database from an older fyisp (schema vN) replaced with a new empty one`. Going back to an older release never wipes data: fyisp refuses a database from a newer version and leaves it untouched. fyisp makes no backups of its database; to keep one, stop fyisp and copy `fyisp.db`, and `storage-targets.json` if you saved storage targets (`fyisp paths` shows where they are).

## Prometheus metrics

The dashboard serves `/metrics`: network_exporter-compatible `ping_*` series for the Network path, and fyisp's own series for TCP, UDP, call quality, the verdict, traceroutes and speed tests. Every series is listed in [API.md](docs/API.md#metrics), hop addresses included. It needs the [admin token](#admin-token), as a bearer token:

```yaml
scrape_configs:
  - job_name: fyisp
    authorization:
      credentials_file: /etc/prometheus/fyisp-token   # the token alone, readable by Prometheus
    static_configs:
      - targets: ['127.0.0.1:3000']
```

Scraping from another machine by host name also needs that name in `--allow-host`.

## Where data lives, and uninstalling

`fyisp paths` prints every location fyisp uses. By default:

| Platform | Data directory |
|---|---|
| Linux | `$XDG_STATE_HOME/fyisp` (usually `~/.local/state/fyisp`) |
| macOS | `~/Library/Application Support/fyisp` |
| Windows | `%LOCALAPPDATA%\fyisp` |
| systemd unit | `/var/lib/fyisp` (`$STATE_DIRECTORY`) |
| Docker | the `/data` volume |

It holds the database (`fyisp.db`), its lock file and, once you save storage targets, `storage-targets.json` (readable by fyisp's user only). Change it with `--data-dir`, or use `--ephemeral` for a temporary directory that is deleted on exit. Samples older than 90 days are deleted automatically (`--retention`).

To uninstall, stop fyisp, then delete its data directory and the binary. The launchers leave no binary behind; only the data directory remains.

- **Linux:** `rm -rf ~/.local/state/fyisp` (or the directory `fyisp paths` printed).
- **macOS:** `rm -rf ~/Library/Application\ Support/fyisp`.
- **Windows:** `Remove-Item -Recurse "$env:LOCALAPPDATA\fyisp"`.
- **Docker:** `docker rm -f fyisp && docker volume rm fyisp`.
- **systemd:** `sudo systemctl disable --now fyisp && sudo rm /etc/systemd/system/fyisp.service /usr/local/bin/fyisp && sudo rm -rf /etc/systemd/system/fyisp.service.d /var/lib/private/fyisp /etc/fyisp.env`.

## On a server (datacenter or office)

To measure a server's connection, or to [compare sites](docs/COMPARING-SITES.md) (two datacenters, two offices, two providers), run fyisp on each Linux server with Docker, with everything on that a server needs: every target of the catalog (`--profile all`, about 1,400) and the path survey (`--path-survey`: a TCP traceroute to every target every 15 minutes, kept hour by hour with the address each target resolved to, to tell GeoDNS, anycast, routing and congestion apart). Give each server its own `--site` name (here `site-a`), which every comparison row carries. The first block picks the release; in the second, the first line makes your admin token, readable by you only:

<!-- x-release-please-start-version -->
```sh
IMAGE=ghcr.io/glueops/fyisp:v0.19.0
```
<!-- x-release-please-end -->

```sh
(umask 077; openssl rand -hex 16 > ~/.fyisp-token)
FYISP_ADMIN_TOKEN="$(cat ~/.fyisp-token)" docker run -d --name fyisp --restart unless-stopped --network host -v fyisp:/data \
  --log-driver local --log-opt max-size=10m --log-opt max-file=3 \
  -e FYISP_ADMIN_TOKEN "$IMAGE" --listen=127.0.0.1:3000 --profile all --path-survey --site site-a
```

- **Open the dashboard** from your laptop over SSH: `ssh -L 3000:127.0.0.1:3000 <server>`, then `http://127.0.0.1:3000/#admin=<token>` (the token is in `~/.fyisp-token` on the server). `--listen=127.0.0.1:3000` keeps the dashboard off the server's public address: without it the image listens on every address, over plain HTTP. fyisp checks the port in the address you open, so forwarding another local port to 3000 gets *421 unknown host*: to open two servers at once, run the second with `--listen=127.0.0.1:3001` (recreate its container if it already runs) and forward `3001:127.0.0.1:3001`.
- **ICMP:** some server installs set `net.ipv4.ping_group_range` to `1 0`, which leaves fyisp, and so the path survey, without ICMP under `--network host` (`api/status` shows the survey `unavailable`). Allow it for good as in [ICMP permissions](#icmp-permissions), or use Docker's [bridge network](#docker-nas-and-raspberry-pi): `-p 127.0.0.1:3000:3000` in place of both `--network host` and `--listen=127.0.0.1:3000`.
- **Check it:** `docker logs fyisp` shows a `path survey` line with the number of targets (about 1,400), and `curl -s -H @<(printf 'Authorization: Bearer %s\n' "$(cat ~/.fyisp-token)") http://127.0.0.1:3000/api/status` (the token stays out of the process list) shows `"survey"` with `"state": "first_pass"`, then `"on"` after about 15 minutes. `unavailable` means no ICMP socket: see [ICMP permissions](#icmp-permissions).
- **What it sends:** a TCP connect to each of about 1,400 targets every 15 seconds, and the [path survey](docs/COMPARING-SITES.md)'s TCP traceroutes, about 25 probes a second. Networks that watch for scans may send your hosting provider an abuse report; [OPERATORS.md](docs/OPERATORS.md) says what they see.
- **Disk:** 90 days of history (the default), the path survey's 1 to 4 GB of it. Check the volume after the first days (`docker system df -v`); `--retention 30d` keeps less.
- **Every site the same:** run the same release (the `IMAGE` tag) and keep the clock synchronized (`timedatectl` says `System clock synchronized: yes`). To upgrade, see [Upgrading](#upgrading): it picks the latest tag, replaces the container and keeps the token and data; upgrade all sites together.
- **Speed tests stay off:** they fill the line (terabytes a month on a fast one) and use other people's iperf3 servers; add `--speedtest` only after reading [Speed tests](#speed-tests).
- **Comparing sites:** export the same time window from each with `fyisp export` and run the script in [Comparing sites](docs/COMPARING-SITES.md), which reads those files. Over HTTP with the admin token, `api/compare.csv` and `api/survey.csv` give each target's hourly median, addresses and path ([API.md](docs/API.md#get-apicomparecsv-apisurveycsv-and-apiaddrscsv)).

<details>
<summary>Without Docker: systemd</summary>

Install fyisp as in [Linux (systemd)](#linux-systemd), then give it a token, the catalog and the path survey with a drop-in:

```sh
sudo sh -c 'umask 077; echo FYISP_ADMIN_TOKEN=$(openssl rand -hex 16) > /etc/fyisp.env'
sudo mkdir -p /etc/systemd/system/fyisp.service.d
printf '%s\n' '[Service]' 'EnvironmentFile=/etc/fyisp.env' 'Environment=FYISP_PROFILE=all' 'Environment=FYISP_PATH_SURVEY=1' 'Environment=FYISP_SITE=site-a' \
  | sudo tee /etc/systemd/system/fyisp.service.d/server.conf >/dev/null
sudo systemctl daemon-reload && sudo systemctl restart fyisp
```

The unit already listens on `127.0.0.1:3000` only. The token is `sudo sed -n 's/^FYISP_ADMIN_TOKEN=//p' /etc/fyisp.env`. Export with `sudo fyisp export --data-dir /var/lib/fyisp …`.

</details>

## Building from source

Everything builds in Docker; no Go toolchain is needed on your machine.

| What | Command |
|---|---|
| gofmt check, go vet, `go test -race` | `docker build --target test .` |
| the 7 release binaries and SHA256SUMS | `docker buildx build --target dist --output type=local,dest=dist .` |
| the runtime image | `docker build -t fyisp .` |

[AGENTS.md](AGENTS.md) lists every other target (UI tests, launcher and image tests, regenerating goldens and fixtures) and how the code is laid out.

Hop owners (AS number and name) come from a table built into the binary, [`internal/asn/data/ip2asn-v4.bin`](internal/asn/data), generated from the [iptoasn.com](https://iptoasn.com/) IPv4 data; fyisp never looks them up over the network. The `asndb` target downloads the latest file and regenerates the table; releases do this automatically and fall back to the committed table if iptoasn.com is unreachable.

Add `--build-arg VERSION=v0.1.0` to stamp a version. Releases are built by [`.github/workflows/release.yml`](.github/workflows/release.yml) from a `v*` tag using the same targets, and carry [build provenance attestations](https://docs.github.com/actions/security-for-github-actions/using-artifact-attestations).

### Network-fault tests (Linux, sudo)

`test/netns/run.sh` builds fyisp in Docker, then (with sudo) builds throwaway network namespaces (client ↔ gateway ↔ ISP router ↔ internet), runs fyisp in the client namespace as uid 65532 without capabilities, and injects real faults: dropped and rejected packets, a withdrawn route, loss, delay, DNS and TLS failures, blocked UDP and restarts. It checks that the verdict blames the right layer, and that Home answers correctly in 17 scenarios (TestHome10). A full run takes about 20 minutes; it runs nightly in CI ([`netns.yml`](.github/workflows/netns.yml)), not per pull request. The commands are in [AGENTS.md](AGENTS.md#build-and-test-docker-only).

## More docs

- [What to measure](docs/PROFILES.md): Home's tiles, the profiles, the Overview and the `--config` format
- [How fyisp decides](docs/VERDICTS.md): Good, Slow, Choppy and Down, the verdicts, and Wi-Fi or router
- [Call quality](docs/CALL-QUALITY.md), [Speed tests](docs/SPEED-TESTS.md) and [Storage tests](docs/STORAGE-TESTS.md)
- [Comparing sites](docs/COMPARING-SITES.md): the path survey, telling GeoDNS, anycast, routing and congestion apart, and the compare-sites agent skill that does it for two sites
- [API](docs/API.md): every route, the `/metrics` series and the `fyisp export` columns
- [STUN servers' terms](docs/STUN-TERMS.md), [iperf3 servers' terms](docs/IPERF3-TERMS.md) and [a note for iperf3 operators](docs/OPERATORS.md)
- [SECURITY.md](SECURITY.md), [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md)

## License

Apache-2.0, see [LICENSE](LICENSE). fyisp includes third-party code under its own licenses, see [NOTICE](NOTICE). IP-to-ASN data from [iptoasn.com](https://iptoasn.com/) (PDDL).

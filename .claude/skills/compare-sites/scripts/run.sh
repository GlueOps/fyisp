#!/usr/bin/env bash
# compare-sites runner. Every docker flag and the image are fixed here, so an
# agent can be allowed to run this script and nothing else.
#
#   run.sh save-site NAME             reads one sign-in link (and optional " http-ok") from stdin
#   run.sh compare A B [24h|FROM..TO] fetches both sites and writes a report folder
#   run.sh resume DIR                 fetches what an earlier run didn't get, then analyzes
#   run.sh analyze DIR                analyzes the saved files again (no network)
#   run.sh fill DIR                   reads the narratives ("@@ name" sections) from stdin
#
# Exit codes: 0 done, 3 blocked (the messages say why), 8 out of time (resume).
set -euo pipefail

IMAGE=python:3.12.16-slim-trixie@sha256:5e2bdb1b0d542d5039cffb8181c65930093949194a65c365dfb1b0fe13a4fa5b
VERSION=0.18.4 # x-release-please-version
BUDGET=540 # seconds; the agent's command limit is 600
HERE=$(cd "$(dirname "$0")" && pwd)
CONF=${XDG_CONFIG_HOME:-$HOME/.config}/fyisp
SITES=$CONF/sites
BASE=fyisp-reports
NAME_RE='^[a-z0-9][a-z0-9-]{0,31}$'
LINK_RE='^https?://(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]{1,253})(:[0-9]{1,5})?/#admin=[A-Za-z0-9._~-]{32,255}$'
WIN_RE='^([1-9]|1[0-9]|2[0-4])h$|^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00(:00)?Z\.\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00(:00)?Z$'
DIR_RE='^fyisp-reports/[a-z0-9-]+-vs-[a-z0-9-]+-[0-9]{8}T[0-9]{4}Z/?$'
export COPYFILE_DISABLE=1 # macOS tar: no ._ files
STAGE='' NAME=''
trap 'if [[ -n $NAME ]]; then docker rm -f "$NAME" >/dev/null 2>&1 || true; fi; if [[ -n $STAGE ]]; then rm -rf "$STAGE"; fi' EXIT
trap 'exit 130' INT TERM

die() { printf '%s\n' "$*" >&2; exit 3; }

save_site() {
  local name=$1 line link flags tmp
  [[ $name =~ $NAME_RE ]] || die "save-site: the name must be lowercase letters, digits and -, up to 32."
  IFS= read -r line || true
  read -r link flags <<<"$line"
  [[ ${link:-} =~ $LINK_RE ]] || die "$name: that is not a fyisp sign-in link (http(s)://host:port/#admin=<token>). Copy it again."
  [[ -z ${flags:-} || $flags == http-ok ]] || die "$name: only ' http-ok' may follow the link."
  mkdir -p "$CONF" && chmod 700 "$CONF"
  tmp=$(mktemp "$CONF/.sites.XXXXXX")
  chmod 600 "$tmp"
  if [[ -f $SITES ]]; then grep -v "^$name=" "$SITES" >"$tmp" || true; fi
  printf '%s=%s%s\n' "$name" "$link" "${flags:+ $flags}" >>"$tmp"
  mv "$tmp" "$SITES"
  echo "$name: saved in $SITES."
}

check_sites_file() {
  [[ -f $SITES ]] || die "No sites file yet ($SITES), so no site is in the sites file. Add each with: run.sh save-site NAME (see docs/COMPARING-SITES.md)."
  # shellcheck disable=SC2012 # one known file; ls -l is the portable way to read its mode
  [[ $(ls -l "$SITES" | cut -c5-10) == ------ ]] || die "$SITES can be read by other users. Run: chmod 600 $SITES"
}

check_dir() {
  [[ $1 =~ $DIR_RE && -f $1/data/manifest.json ]] || die "$1: not a report folder from this script (fyisp-reports/A-vs-B-DATE)."
}

# The report folder must not end up in a git commit (it holds router addresses).
check_git() {
  command -v git >/dev/null || return 0
  git rev-parse --is-inside-work-tree >/dev/null 2>&1 || return 0
  git check-ignore -q "$BASE/x" ||
    die "This directory is in a git repository that doesn't ignore $BASE/. Add '$BASE/' to its .gitignore, or run from another directory."
}

ensure_image() {
  command -v docker >/dev/null || die "Docker is needed: install Docker, or run this where docker works."
  docker image inspect "$IMAGE" >/dev/null 2>&1 && return
  echo "[setup] pulling $IMAGE" >&2
  docker pull -q "$IMAGE" >/dev/null || die "Docker could not pull $IMAGE. Check that Docker runs and can reach Docker Hub."
}

# stage OUT [data]: a private folder with the script (and OUT's saved data) to send in.
stage() {
  STAGE=$(mktemp -d)
  chmod 700 "$STAGE"
  cp "$HERE/compare_sites.py" "$STAGE/"
  if [[ ${2:-} == data && -f $1/data/manifest.json ]]; then
    mkdir -p "$STAGE/in/data"
    cp "$1/data/manifest.json" "$STAGE/in/data/"
    if compgen -G "$1/data/*.csv.gz" >/dev/null; then cp "$1"/data/*.csv.gz "$STAGE/in/data/"; fi
  fi
}

# run_in OUT NETWORK ARGS...: STAGE goes in as a tar, the outputs come back as a
# tar into OUT; exits with the script's own code.
run_in() {
  local out=$1 net=$2 rcs
  shift 2
  NAME=fyisp-cmp-$$-$RANDOM
  set +e
  tar -C "$STAGE" -cf - . | docker run -i --rm --init --log-driver none --name "$NAME" --network "$net" \
    --user 65534:65534 --read-only --tmpfs /w:uid=65534,gid=65534,mode=0700,size=1g -e TMPDIR=/w \
    --cap-drop ALL --security-opt no-new-privileges --memory 2g --pids-limit 64 "$IMAGE" \
    sh -c 'cd /w && tar --warning=no-unknown-keyword -xf - && exec python3 -I compare_sites.py "$@"' sh "$@" |
    (umask 077 && tar -xf - -C "$out" --no-same-owner 2>/dev/null)
  rcs=("${PIPESTATUS[@]}")
  set -e
  NAME=
  case ${rcs[1]} in
    125) die "Docker could not start the container (is the Docker daemon running?)." ;;
    126|127) die "Docker could not run the script in $IMAGE." ;;
  esac
  [[ ${rcs[2]} == 0 || ${rcs[1]} != 0 ]] || die "The results could not be unpacked into $out."
  case ${rcs[1]} in
    0) echo "Report: $out/report.md (internal: router addresses)" >&2
       echo "Summary to forward: $out/share-summary.md" >&2 ;;
    8) echo "Continue with: $0 resume $out" >&2 ;;
  esac
  exit "${rcs[1]}"
}

fetch() { # fetch OUT SEL ARGS...: SEL is the two names, a|b
  local out=$1 sel=$2
  shift 2
  stage "$out" data
  (umask 077 && { grep -E "^($sel)=" "$SITES" || true; } >"$STAGE/sites")
  run_in "$out" host fetch --deadline "$(($(date +%s) + BUDGET))" --version "$VERSION" "$@"
}

cmd=${1:-}
shift || true
case $cmd in
  save-site)
    [[ $# == 1 ]] || die "usage: run.sh save-site NAME  (the link on stdin)"
    save_site "$1" ;;
  compare)
    [[ $# == 2 || $# == 3 ]] || die "usage: run.sh compare SITE-A SITE-B [24h|FROM..TO]"
    a=$1 b=$2 w=${3:-24h}
    [[ $a =~ $NAME_RE && $b =~ $NAME_RE && $a != "$b" ]] || die "Give two different site names (lowercase letters, digits, -)."
    [[ $w =~ $WIN_RE ]] || die "Window: 1h to 24h, or FROM..TO in whole UTC hours (2026-11-22T00:00Z..2026-11-22T12:00Z)."
    check_sites_file
    check_git
    ensure_image
    out=$BASE/$a-vs-$b-$(date -u +%Y%m%dT%H%MZ)
    mkdir -p "$BASE"
    mkdir "$out" || die "$out already exists; wait a minute or resume it."
    chmod 700 "$out"
    fetch "$out" "$a|$b" --names "$a" "$b" --window "$w" ;;
  resume)
    [[ $# == 1 ]] || die "usage: run.sh resume DIR"
    check_dir "$1"
    sel=$(tr -d ' \n' <"$1/data/manifest.json" | grep -oE '"names":\["[a-z0-9-]+","[a-z0-9-]+"\]' | grep -oE '[a-z0-9-]+' | grep -vx names | paste -sd'|' - || true)
    [[ $sel =~ ^[a-z0-9-]+\|[a-z0-9-]+$ ]] || die "$1: its manifest names no two sites."
    check_sites_file
    ensure_image
    fetch "${1%/}" "$sel" ;;
  analyze)
    [[ $# == 1 ]] || die "usage: run.sh analyze DIR"
    check_dir "$1"
    ensure_image
    stage "${1%/}" data
    run_in "${1%/}" none analyze --deadline 0 --version "$VERSION" ;;
  fill)
    [[ $# == 1 ]] || die "usage: run.sh fill DIR  (the narratives on stdin)"
    check_dir "$1"
    out=${1%/}
    [[ -f $out/report.md ]] || die "$out has no report yet; run analyze first."
    ensure_image
    stage "$out"
    mkdir -p "$STAGE/in"
    cp "$out/report.md" "$out/share-summary.md" "$STAGE/in/"
    head -c 65536 >"$STAGE/in/narratives.txt"
    run_in "$out" none fill --deadline 0 --version "$VERSION" ;;
  *) die "usage: run.sh save-site NAME | compare A B [24h|FROM..TO] | resume DIR | analyze DIR | fill DIR" ;;
esac

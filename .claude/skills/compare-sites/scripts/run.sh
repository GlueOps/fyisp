#!/usr/bin/env bash
# compare-sites runner. Every docker flag and the image are fixed here, so an
# agent can be allowed to run this script and nothing else.
#
#   run.sh save-site NAME            reads one sign-in link (and optional " http-ok") from stdin
#   run.sh compare A B [24h|FROM..TO] fetches both sites and writes a report folder
#   run.sh resume DIR                fetches what an earlier run didn't get, then analyzes
#   run.sh analyze DIR               analyzes the saved files again (no network)
#   run.sh fill DIR                  reads the narratives ("@@ name" sections) from stdin
#
# Exit codes: 0 done, 3 blocked (the messages say why), 8 out of time (resume).
set -euo pipefail

IMAGE=python:3.12.16-slim-trixie@sha256:5e2bdb1b0d542d5039cffb8181c65930093949194a65c365dfb1b0fe13a4fa5b
VERSION=0.18.2 # x-release-please-version
BUDGET=540 # seconds; the agent's command limit is 600
HERE=$(cd "$(dirname "$0")" && pwd)
CONF=${XDG_CONFIG_HOME:-$HOME/.config}/fyisp
SITES=$CONF/sites
BASE=fyisp-reports
NAME_RE='^[a-z0-9][a-z0-9-]{0,31}$'
DIR_RE='^fyisp-reports/[a-z0-9-]+-vs-[a-z0-9-]+-[0-9]{8}T[0-9]{4}Z/?$'
export COPYFILE_DISABLE=1 # macOS tar: no ._ files

die() { printf '%s\n' "$*" >&2; exit 3; }

save_site() {
  local name=$1 line link flags
  [[ $name =~ $NAME_RE ]] || die "save-site: the name must be lowercase letters, digits and -, up to 32."
  IFS= read -r line || true
  read -r link flags <<<"$line"
  [[ ${link:-} =~ ^https?://(\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9.-]{1,253})(:[0-9]{1,5})?/#admin=[A-Za-z0-9._~-]{32,256}$ ]] ||
    die "$name: that is not a fyisp sign-in link (http(s)://host:port/#admin=<token>). Copy it again."
  [[ -z ${flags:-} || $flags == http-ok ]] || die "$name: only ' http-ok' may follow the link."
  mkdir -p -m 700 "$CONF" && chmod 700 "$CONF"
  local tmp
  tmp=$(mktemp "$CONF/.sites.XXXXXX")
  chmod 600 "$tmp"
  if [[ -f $SITES ]]; then grep -v "^$name=" "$SITES" >"$tmp" || true; fi
  printf '%s=%s%s\n' "$name" "$link" "${flags:+ $flags}" >>"$tmp"
  mv "$tmp" "$SITES"
  echo "$name: saved in $SITES."
}

check_sites_file() {
  [[ -f $SITES ]] || die "No sites file yet ($SITES). Add each site with: run.sh save-site NAME, or see docs/COMPARING-SITES.md."
  local mode
  mode=$(ls -l "$SITES" | cut -c5-10)
  [[ $mode == ------ ]] || die "$SITES can be read by other users. Run: chmod 600 $SITES"
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

# run_container NETWORK OUTDIR STAGEDIR ARGS...: the stage dir goes in as a tar,
# the outputs come back as a tar; returns the script's own exit code.
run_container() {
  local net=$1 out=$2 stage=$3
  shift 3
  local name rcs
  name=fyisp-cmp-$$-$RANDOM
  trap 'docker rm -f "'"$name"'" >/dev/null 2>&1 || true' EXIT INT TERM
  set +e
  tar -C "$stage" -cf - . | docker run -i --rm --init --log-driver none --name "$name" --network "$net" \
    --user 65534:65534 --read-only --tmpfs /w:uid=65534,gid=65534,mode=0700,size=1g -e TMPDIR=/w \
    --cap-drop ALL --security-opt no-new-privileges "$IMAGE" \
    sh -c 'cd /w && tar -xf - && exec python3 -I compare_sites.py "$@"' sh "$@" |
    (umask 077 && tar -xf - -C "$out" --no-same-owner 2>/dev/null)
  rcs=("${PIPESTATUS[@]}")
  set -e
  rm -rf "$stage"
  case ${rcs[1]} in
    125) die "Docker could not start the container (is the Docker daemon running?)." ;;
    126|127) die "Docker could not run the script in $IMAGE." ;;
  esac
  [[ ${rcs[2]} == 0 || ${rcs[1]} != 0 ]] || die "The results could not be unpacked into $out."
  return "${rcs[1]}"
}

ensure_image() {
  command -v docker >/dev/null || die "Docker is needed: install Docker, or run this where docker works."
  docker image inspect "$IMAGE" >/dev/null 2>&1 && return
  echo "[setup] pulling $IMAGE" >&2
  docker pull -q "$IMAGE" >/dev/null || die "Docker could not pull $IMAGE. Check that Docker runs and can reach Docker Hub."
}

stage() {
  local s
  s=$(mktemp -d)
  chmod 700 "$s"
  cp "$HERE/compare_sites.py" "$s/"
  echo "$s"
}

fetch() { # fetch OUT [names...] -- mode args
  local out=$1
  shift
  check_sites_file
  ensure_image
  local deadline=$(($(date +%s) + BUDGET)) s
  s=$(stage)
  (umask 077 && grep -E "^($SEL)=" "$SITES" >"$s/sites" || true)
  if [[ -f $out/data/manifest.json ]]; then
    mkdir -p "$s/in/data"
    cp "$out/data/manifest.json" "$out"/data/*.csv.gz "$s/in/data/" 2>/dev/null || true
  fi
  local rc=0
  run_container host "$out" "$s" fetch --deadline "$deadline" --version "$VERSION" "$@" || rc=$?
  [[ $rc != 8 ]] || echo "Continue with: $0 resume $out" >&2
  [[ $rc != 0 ]] || report_paths "$out"
  return $rc
}

report_paths() {
  echo "Report: $1/report.md (internal: router addresses)" >&2
  echo "Summary to forward: $1/share-summary.md" >&2
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
    [[ $w =~ ^([1-9]|1[0-9]|2[0-4])h$ || $w =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00(:00)?Z\.\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:00(:00)?Z$ ]] ||
      die "Window: 1h to 24h, or FROM..TO in whole UTC hours (2026-11-22T00:00Z..2026-11-22T12:00Z)."
    check_git
    out=$BASE/$a-vs-$b-$(date -u +%Y%m%dT%H%MZ)
    mkdir -p "$BASE"
    mkdir "$out" || die "$out already exists; wait a minute or resume it."
    chmod 700 "$out"
    SEL="$a|$b"
    fetch "$out" --names "$a" "$b" --window "$w" ;;
  resume)
    [[ $# == 1 ]] || die "usage: run.sh resume DIR"
    check_dir "$1"
    SEL=$(tr -d ' \n' <"$1/data/manifest.json" | grep -oE '"names":\["[a-z0-9-]+","[a-z0-9-]+"\]' | grep -oE '[a-z0-9-]+' | grep -vx names | paste -sd'|' -)
    [[ $SEL =~ ^[a-z0-9-]+\|[a-z0-9-]+$ ]] || die "$1: its manifest names no two sites."
    fetch "${1%/}" ;;
  analyze)
    [[ $# == 1 ]] || die "usage: run.sh analyze DIR"
    check_dir "$1"
    ensure_image
    out=${1%/} s=$(stage)
    mkdir -p "$s/in/data"
    cp "$out/data/manifest.json" "$out"/data/*.csv.gz "$s/in/data/"
    rc=0
    run_container none "$out" "$s" analyze --version "$VERSION" || rc=$?
    [[ $rc != 0 ]] || report_paths "$out"
    exit $rc ;;
  fill)
    [[ $# == 1 ]] || die "usage: run.sh fill DIR  (the narratives on stdin)"
    check_dir "$1"
    out=${1%/}
    [[ -f $out/report.md ]] || die "$out has no report yet; run analyze first."
    ensure_image
    s=$(stage)
    mkdir -p "$s/in"
    cp "$out/report.md" "$out/share-summary.md" "$s/in/"
    head -c 65536 >"$s/in/narratives.txt"
    rc=0
    run_container none "$out" "$s" fill || rc=$?
    [[ $rc != 0 ]] || report_paths "$out"
    exit $rc ;;
  *) die "usage: run.sh save-site NAME | compare A B [24h|FROM..TO] | resume DIR | analyze DIR | fill DIR" ;;
esac

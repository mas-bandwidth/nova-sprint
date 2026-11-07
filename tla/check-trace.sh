#!/usr/bin/env bash
# Check one recorded trace against its model (or all Layer 4 traces when called with 0 args):
#   bash tla/check-trace.sh <model> <trace dir>
#   bash tla/check-trace.sh
set -u

VERSION=v1.8.0
URL="https://github.com/tlaplus/tlaplus/releases/download/$VERSION/tla2tools.jar"
SHA256=7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
CACHE="${TLA_CACHE:-$HOME/.cache/nova-sprint}"
JAR="$CACHE/tla2tools-$VERSION.jar"

if [ $# -eq 0 ]; then
    OUT="$(mktemp -d)"
    trap 'rm -rf "$OUT"' EXIT
    export NOVA_TRACE_DIR="$OUT"
    (
        cd "$ROOT" || exit 1
        go test -count=1 ./edge/bus -run "^TestPushTraceRefinementWalk$" >/dev/null || exit 1
        go test -count=1 ./edge/git -run "^TestLandEdgeTraceRefinementWalk$" >/dev/null || exit 1
        go test -count=1 ./edge/worker -run "^TestRunnerTraceRefinementWalk$" >/dev/null || exit 1
    ) || exit 1
    status=0
    for d in "$OUT"/*/; do
        name="$(basename "$d")"
        case "$name" in
            push-*)     model=Push ;;
            landedge-*) model=LandEdge ;;
            runner-*)   model=Runner ;;
            *)          continue ;;
        esac
        cp "$HERE/${model}Trace.cfg" "$d/${model}Trace.cfg"
        if out="$(bash "$HERE/check-trace.sh" "$model" "$d" 2>&1)"; then
            echo "ok   $name: $out"
        else
            echo "FAIL $name: $out" >&2
            status=1
        fi
    done
    exit "$status"
fi

[ $# -eq 2 ] || { echo "usage: check-trace.sh [<Model> <trace dir>]" >&2; exit 2; }
MODEL="$1"
TRACE="$(cd "$2" && pwd)" || exit 2
CFG="$TRACE/${MODEL}Trace.cfg"
LOGMOD="$TRACE/${MODEL}TraceLog.tla"
[ -f "$CFG" ] && [ -f "$LOGMOD" ] || { echo "no $CFG or $LOGMOD" >&2; exit 2; }

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d" " -f1
    else shasum -a 256 "$1" | cut -d" " -f1; fi
}
JAVA="$HOME/sdk/bin/java"
[ -x "$JAVA" ] || JAVA="$(command -v java || true)"
[ -n "$JAVA" ] || { echo "no java on this host" >&2; exit 2; }
mkdir -p "$CACHE" || exit 2
if [ ! -f "$JAR" ] || [ "$(sha256_of "$JAR")" != "$SHA256" ]; then
    curl -fsSL -o "$JAR.part" "$URL" && mv "$JAR.part" "$JAR" || { echo "download of $URL failed" >&2; exit 2; }
fi
[ "$(sha256_of "$JAR")" = "$SHA256" ] || { echo "tla2tools.jar sha256 mismatch" >&2; rm -f "$JAR"; exit 2; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$HERE" || exit 2
"$JAVA" -XX:+UseParallelGC -Djava.io.tmpdir="$WORK" -DTLA-Library="$TRACE" -cp "$JAR" tlc2.TLC     -noGenerateSpecTE -workers 1 -metadir "$WORK/states" -config "$CFG" "${MODEL}Trace.tla" > "$WORK/tlc.log" 2>&1
code=$?

start="$(grep -n '^TraceLog == <<$' "$LOGMOD" | cut -d: -f1)"
n="$(awk -v s="$start" 'NR > s && /^  \[i \|-> /' "$LOGMOD" | wc -l | tr -d ' ')"
want="$(sed -n 's/^\\\* expect: //p' "$CFG")"
record() { sed -n "$((start + $1))p" "$LOGMOD" | sed 's/^  //; s/,$//'; }

if [ "$code" -eq 0 ]; then
    echo "TRACE OK steps=$n"
    [ "$want" = matched ] && exit 0
    echo "UNEXPECTED: wanted $want" >&2; exit 1
fi
if [ "$code" -ne 13 ] || ! grep -q "Temporal property TraceMatched was violated" "$WORK/tlc.log"; then
    echo "TLC exit $code: not a trace verdict (log tail below)"
    tail -n 30 "$WORK/tlc.log"
    exit 2
fi
lastl="$(grep -E '^/\\ l = [0-9]+$' "$WORK/tlc.log" | tail -n 1 | awk '{print $4}')"
k=$((lastl + 1))
act="$(record "$k" | grep -o 'action |-> "[A-Za-z]*"' | head -n 1 | sed 's/.*"\(.*\)"//')"
echo "TRACE REFUSED at $k of $n: $act ($(record "$k" | cut -c1-160)...)"
[ "$want" = "forbidden at $k" ] && exit 0
echo "UNEXPECTED: wanted $want" >&2
exit 1

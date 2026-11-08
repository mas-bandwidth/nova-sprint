#!/usr/bin/env bash
# Check one recorded trace against its model:
#   bash tla/check-trace.sh <model> <trace dir>
# <model> is a model with a <model>Trace.tla (Store, CardMachine, Steps, Policy,
# Push, LandEdge, Runner, ...); <trace dir> holds <model>TraceLog.tla and
# <model>Trace.cfg (tla/make-traces.sh writes them under tla/traces/).
#
# Runs TLC on tla/<model>Trace.tla with the trace dir on the TLA-Library
# path and prints one line:
#   TRACE OK steps=<n>                          every record matched
#   TRACE REFUSED at <k> of <n>: <action> ...   record k is the first step
#                                               the model refuses
# and, for a refusal, the model's state after record k - 1 (what the model
# holds) and record k (what the code did). The cfg's "expect:" line says what
# the trace should do ("matched", or "forbidden at K" for a breaker);
# the exit code is 0 when the outcome is the expected one, 1 when it is not,
# 2 when TLC ended any other way (a parse error, a crash).
# Run it on a bench host (hetzner2), never on the working machine.
# TLA_CACHE is where the pinned tla2tools.jar is kept (default ~/.cache/nova-sprint).
set -u

VERSION=v1.8.0
# This repository's copy of the v1.8.0 jar (release tla-tools-1.8.0): upstream
# replaced its v1.8.0 asset with a different binary, so the pin is kept here.
URL="https://github.com/mas-bandwidth/nova-sprint/releases/download/tla-tools-1.8.0/tla2tools.jar"
SHA256=7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04

[ $# -eq 2 ] || { echo "usage: check-trace.sh <Store|CardMachine> <trace dir>" >&2; exit 2; }
MODEL="$1"
TRACE="$(cd "$2" && pwd)" || exit 2
HERE="$(cd "$(dirname "$0")" && pwd)"
CACHE="${TLA_CACHE:-$HOME/.cache/nova-sprint}"
JAR="$CACHE/tla2tools-$VERSION.jar"
CFG="$TRACE/${MODEL}Trace.cfg"
# A walk that writes no cfg (the layer 4 walks) is checked with its model's own.
[ -f "$CFG" ] || [ ! -f "$HERE/${MODEL}Trace.cfg" ] || cp "$HERE/${MODEL}Trace.cfg" "$CFG" || exit 2
LOGMOD="$TRACE/${MODEL}TraceLog.tla"
[ -f "$CFG" ] && [ -f "$LOGMOD" ] || { echo "no $CFG or $LOGMOD" >&2; exit 2; }

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
JAVA="$HOME/sdk/bin/java"
[ -x "$JAVA" ] || JAVA="$(command -v java || true)"
[ -n "$JAVA" ] || { echo "no java on this host" >&2; exit 2; }
mkdir -p "$CACHE" || exit 2
if [ ! -f "$JAR" ] || [ "$(sha256_of "$JAR")" != "$SHA256" ]; then
    curl -fsSL -o "$JAR.part.$$" "$URL" && mv -f "$JAR.part.$$" "$JAR" || { echo "download of $URL failed" >&2; exit 2; }
fi
[ "$(sha256_of "$JAR")" = "$SHA256" ] || { echo "tla2tools.jar sha256 mismatch" >&2; rm -f "$JAR"; exit 2; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$HERE" || exit 2
"$JAVA" -XX:+UseParallelGC -Djava.io.tmpdir="$WORK" -DTLA-Library="$TRACE" -cp "$JAR" tlc2.TLC \
    -noGenerateSpecTE -workers 1 -metadir "$WORK/states" -config "$CFG" "${MODEL}Trace.tla" > "$WORK/tlc.log" 2>&1
code=$?
# Optional retained raw evidence; verdict semantics are unchanged.
if [ -n "${TRACE_LOG:-}" ]; then
    cp "$WORK/tlc.log" "$TRACE_LOG" || exit 2
fi

# The records are one per line after "TraceLog == <<"; record k is line start + k.
start="$(grep -n '^TraceLog == <<$' "$LOGMOD" | cut -d: -f1)"
n="$(awk -v s="$start" 'NR > s && /^  \[i \|-> /' "$LOGMOD" | wc -l | tr -d ' ')"
[ "$n" -gt 0 ] || { echo "no recognized trace records" >&2; exit 2; }
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
act="$(record "$k" | grep -o 'action |-> "[A-Za-z]*"' | head -n 1 | sed 's/.*"\(.*\)"/\1/')"
echo "TRACE REFUSED at $k of $n: $act ($(record "$k" | cut -c1-160)...)"
echo "-- the model after record $lastl (TLC's last state):"
last="$(grep -n '^State [0-9]*: <' "$WORK/tlc.log" | tail -n 1 | cut -d: -f1)"
sed -n "$last,\$p" "$WORK/tlc.log" | sed -n '2,/^$/p' \
    | awk '/^\/\\ [a-z]+ = / { skip = ($2 ~ /^(events|ledger|receipt|last|applied|lost|phase)$/) } !skip' | cut -c1-2000
echo "-- record $k (the code's step and state):"
record "$k"
[ "$want" = "forbidden at $k" ] && exit 0
echo "UNEXPECTED: wanted $want" >&2
exit 1

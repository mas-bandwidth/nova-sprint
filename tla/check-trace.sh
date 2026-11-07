#!/usr/bin/env bash
# Trace validation of the steps (the lock gate since 2026-11-07):
#   bash tla/check-trace.sh [traces/<fixture>...]
# Runs TLC on StepsTrace.tla over each fixture directory (default: every
# tla/traces/steps-*/), a StepsTraceLog.tla the Go walk wrote
# (go test -run TestTraceFixtures ./steps/ with NOVA_TRACES=tla/traces) or
# make-steps-traces.py generated, with its StepsTrace.cfg whose "expect:"
# line says "matched" or "forbidden at K". A matched trace is a behaviour
# of the model (TLC exit 0). A refused one prints the first record the model
# refuses: its index, action and arguments, so the Go state that record
# carries is what the model forbids. Exits non-zero on any fixture that does
# not end as its cfg expects. Run it on a bench host, never the working
# machine; it fetches the pinned jar as ci-tlc.sh does.
set -u

VERSION=v1.8.0
URL="https://github.com/tlaplus/tlaplus/releases/download/$VERSION/tla2tools.jar"
SHA256=7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04
HERE="$(cd "$(dirname "$0")" && pwd)"
CACHE="${TLA_CACHE:-$HOME/.cache/nova-sprint}"
JAR="$CACHE/tla2tools-$VERSION.jar"

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}
JAVA="$([ -x "$HOME/sdk/bin/java" ] && echo "$HOME/sdk/bin/java" || command -v java || true)"
[ -n "$JAVA" ] || { echo "no java on this host" >&2; exit 1; }
mkdir -p "$CACHE" || exit 1
if [ ! -f "$JAR" ] || [ "$(sha256_of "$JAR")" != "$SHA256" ]; then
    curl -fsSL -o "$JAR.part" "$URL" || { echo "download of $URL failed" >&2; exit 1; }
    mv "$JAR.part" "$JAR" || exit 1
fi
[ "$(sha256_of "$JAR")" = "$SHA256" ] || { echo "tla2tools.jar sha256 mismatch" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

if [ $# -gt 0 ]; then
    fixtures=("$@")
else
    fixtures=("$HERE"/traces/steps-*/)
fi

# record K: the K-th record's index, action and arguments, from the log module.
record() {
    awk -v k="$2" '/^  \[i \|-> /{n++; if (n == k) {print; exit}}' "$1/StepsTraceLog.tla" \
        | sed -E 's/, (w|ps|cs|up|sh|tko|inbox|inflight|js) \|->.*$//' | cut -c1-300
}

status=0
for d in "${fixtures[@]}"; do
    d="${d%/}"
    case "$d" in /*) ;; *) d="$HERE/../$d" ;; esac
    fx="$(basename "$d")"
    want="$(sed -n 's/^\\\* expect: //p' "$d/StepsTrace.cfg")"
    mkdir -p "$WORK/tmp/$fx"
    (cd "$HERE" && "$JAVA" -XX:+UseParallelGC -Djava.io.tmpdir="$WORK/tmp/$fx" -DTLA-Library="$d" -cp "$JAR" tlc2.TLC \
        -noGenerateSpecTE -workers 2 -metadir "$WORK/states/$fx" -config "$d/StepsTrace.cfg" StepsTrace.tla > "$WORK/$fx.log" 2>&1)
    code=$?
    lastl="$(grep -E '^/\\ l = [0-9]+$' "$WORK/$fx.log" | tail -n 1 | awk '{print $4}')"
    n="$(grep -c '^  \[i |-> ' "$d/StepsTraceLog.tla")"
    if [ "$want" = matched ] && [ "$code" -eq 0 ]; then
        echo "ok   $fx: matched whole, $n records"
    elif [ "${want% *}" = "forbidden at" ] && [ "$code" -eq 13 ] && [ "$lastl" = "$(( ${want##* } - 1 ))" ] \
        && ! grep -q "Invariant .* is violated" "$WORK/$fx.log"; then
        echo "ok   $fx: record ${want##* } forbidden, as expected"
    elif [ "$code" -eq 13 ] && [ -n "$lastl" ]; then
        echo "FAIL $fx: the model refuses record $((lastl + 1)) of $n (wanted $want):"
        echo "     $(record "$d" $((lastl + 1)))"
        status=1
    else
        echo "FAIL $fx: TLC exit $code (wanted $want); log tail:"
        tail -n 20 "$WORK/$fx.log" | sed 's/^/     /'
        status=1
    fi
done
exit $status

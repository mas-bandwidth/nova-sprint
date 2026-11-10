#!/usr/bin/env bash
# Run the walks on the fixed seed set and write their traces under
# tla/traces/ (tla/README.md, "Trace validation"):
#   bash tla/make-traces.sh          write the traces
#   bash tla/make-traces.sh check    write them, then check every trace under
#                                    tla/traces/ with check-trace.sh, all in
#                                    parallel; exit 1 if any is not as expected
#
# The walks and their seeds (the seeds are in the tests):
#   store-fake-<seed>   modules/store/trace_test.go TestTraceWalk, the Fake, with transport faults
#   store-pg-<seed>     TestFunctionalTraceWalk, PostgreSQL (needs initdb; skipped,
#                       and said so, when it is missing, unless NOVA_TRACE_PG=1,
#                       which makes a missing initdb a failure)
#   card-walk-<seed>    modules/card/trace_test.go TestTraceColdWalk, the card walk on the Fake
#   policy-walk-<seed>  modules/policy/trace_test.go TestTraceWalk, the policy walk on the Fake
#   steps-walk-<scenario>-<seed>  modules/steps/trace_test.go TestTraceFixtures, the steps walk of a
#                       Steps.tla scenario on the Fake; steps-ok-*, steps-bad-* are the
#                       model's own fixtures (make-steps-traces.py), checked too
#   store-breaker, card-breaker, policy-breaker   the divergence breakers (committed fixtures,
#                       rewritten here; each must be refused at its last record)
# Run it on a bench host (hetzner2 for the TLC half), never on the working machine.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(dirname "$HERE")"
OUT="$HERE/traces"
cd "$ROOT" || exit 1

find "$OUT" -mindepth 1 -maxdepth 1 -type d \( -name 'store-fake-*' -o -name 'store-pg-*' -o -name 'card-walk-*' -o -name 'policy-walk-*' -o -name 'steps-walk-*' -o -name 'landedge-walk-*' -o -name 'runner-walk-*' \) -exec rm -rf {} +
export NOVA_TRACE_DIR="$OUT"
# The walks write disjoint trace dirs, so they run in parallel; each one's
# exit code is kept and any failure fails the script after all have ended.
pids=()
go test -count=1 -run 'TestTraceWalk$|TestTraceRefusesDivergence|TestTraceCursorZero' ./modules/store/ & pids+=($!)
go test -count=1 -run 'TestTraceColdWalk$|TestTraceRefusesDivergence' ./modules/card/ & pids+=($!)
go test -count=1 -run 'TestTraceWalk$|TestTraceRefusesDivergence' ./modules/policy/ & pids+=($!)
go test -count=1 -run 'TestTraceFixtures$' ./modules/steps/ & pids+=($!)
go test -count=1 -run 'TestLandEdgeTraceRefinementWalk$' ./modules/edge/git & pids+=($!)
go test -count=1 -run 'TestRunnerTraceRefinementWalk$' ./modules/edge/worker & pids+=($!)
if command -v initdb >/dev/null || ls /usr/lib/postgresql/*/bin/initdb >/dev/null 2>&1 || ls /opt/homebrew/opt/postgresql@18/bin/initdb >/dev/null 2>&1; then
    go test -count=1 -run 'TestFunctionalTraceWalk$' ./modules/store/ & pids+=($!)
elif [ "${NOVA_TRACE_PG:-0}" = 1 ]; then
    echo "make-traces: NOVA_TRACE_PG=1 and no initdb: the PostgreSQL walk cannot run" >&2
    exit 1
else
    echo "make-traces: no initdb, the PostgreSQL walk (store-pg-*) is not run"
fi
walks=0
for p in "${pids[@]}"; do wait "$p" || walks=1; done
[ "$walks" = 0 ] || { echo "make-traces: a walk failed (its go test output is above)" >&2; exit 1; }
ls "$OUT"
[ "${1:-}" = check ] || exit 0

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
for d in "$OUT"/*/; do
    name="$(basename "$d")"
    case "$name" in store-*) model=Store ;; card-*) model=CardMachine ;; policy-*) model=Policy ;; steps-*) model=Steps ;; landedge-*) model=LandEdge ;; runner-*) model=Runner ;; *) continue ;; esac
    ( bash "$HERE/check-trace.sh" "$model" "$d" > "$WORK/$name.out" 2>&1; echo $? > "$WORK/$name.code" ) &
done
wait
status=0
for f in "$WORK"/*.code; do
    name="$(basename "$f" .code)"
    if [ "$(cat "$f")" = 0 ]; then
        echo "ok   $name: $(head -n 1 "$WORK/$name.out")"
    else
        status=1
        echo "FAIL $name:"
        cat "$WORK/$name.out"
    fi
done
exit $status

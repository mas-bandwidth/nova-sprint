#!/bin/sh
# Run TLC on Store.tla, every reversed witness and every reach check, in
# parallel, one log each under logs/. Run from this directory on a bench host,
# never on the working machine:  sh run-tlc.sh /path/to/tla2tools.jar [java]
# Store gets 4 workers; every other run gets 1 (they stop at the first
# violation). ci-tlc.sh is the CI form that also judges each outcome.
set -u
JAR="$1"
JAVA="${2:-java}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ONE="$HERE/run-one.sh"

sh "$ONE" "$JAR" "$JAVA" . Store 4 &
for f in "$HERE"/witnesses/*.tla; do
    case "$f" in *_TTrace_*) continue ;; esac   # TLC trace output, not a witness
    sh "$ONE" "$JAR" "$JAVA" witnesses "$(basename "$f" .tla)" 1 &
done
for f in "$HERE"/reach/*.cfg; do
    r="$(basename "$f" .cfg)"
    sh "$ONE" "$JAR" "$JAVA" . Store 1 "reach/$r.cfg" "reach-$r" &
done
wait
echo all-done > "$HERE/logs/DONE"

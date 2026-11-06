#!/bin/sh
# Print the event list of every violated run in logs/ (show-trace.sh per log),
# for checking the counterexamples by hand.
HERE="$(cd "$(dirname "$0")" && pwd)"
for f in "$HERE"/logs/*.log; do
    n="$(basename "$f" .log)"
    grep -q "is violated" "$f" || continue
    echo "#### $n"
    sh "$HERE/show-trace.sh" "$n" | grep -v '^State'
done

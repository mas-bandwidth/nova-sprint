#!/bin/sh
# Run TLC on Store.tla and the four reversed witnesses, in parallel, one log
# each under logs/. Run from this directory on a bench host, never on the
# working machine:  sh run-tlc.sh /path/to/tla2tools.jar [java]
# Store gets 4 workers; each witness gets 1 (they fail within seconds).
set -u
JAR="$1"
JAVA="${2:-java}"
HERE="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$HERE/logs" "$HERE/states"

run() { # dir module workers
    ( cd "$HERE/$1" && "$JAVA" -XX:+UseParallelGC -cp "$JAR" tlc2.TLC \
        -workers "$3" -metadir "$HERE/states/$2" -config "$2.cfg" "$2.tla" \
        > "$HERE/logs/$2.log" 2>&1
      echo "exit $?" >> "$HERE/logs/$2.log" ) &
}

run . Store 4
run witnesses StoreNoEvent 1
run witnesses StoreReplayApplies 1
run witnesses StoreStaleCheck 1
run witnesses StoreTwoHolders 1

reach() { # name: Store.tla against reach/<name>.cfg, expected to be violated
    ( cd "$HERE" && "$JAVA" -XX:+UseParallelGC -cp "$JAR" tlc2.TLC \
        -workers 1 -metadir "$HERE/states/reach-$1" -config "reach/$1.cfg" Store.tla \
        > "$HERE/logs/reach-$1.log" 2>&1
      echo "exit $?" >> "$HERE/logs/reach-$1.log" ) &
}

reach NeverRefused
reach NeverTaken
reach NeverRecovered
wait
echo all-done > "$HERE/logs/DONE"

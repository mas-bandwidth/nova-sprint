#!/bin/sh
# Run TLC on Store.tla and CardMachine.tla, every reversed witness and every
# reach check, in parallel, one log each under logs/. Run from this directory
# on a bench host, never on the working machine:
#   sh run-tlc.sh /path/to/tla2tools.jar [java]
# Store gets 4 workers, CardMachine 8; every other run gets 1 (they stop at
# the first violation). A reach cfg named in CARD_REACH checks CardMachine;
# every other one checks Store. ci-tlc.sh is the CI form that also judges
# each outcome.
set -u
JAR="$1"
JAVA="${2:-java}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ONE="$HERE/run-one.sh"
CARD_REACH="NeverLanded NeverFixed NeverRed NeverNeedFailed NeverBlocked"

sh "$ONE" "$JAR" "$JAVA" . Store 4 &
sh "$ONE" "$JAR" "$JAVA" . CardMachine 8 &
for f in "$HERE"/witnesses/*.tla; do
    case "$f" in *_TTrace_*) continue ;; esac   # TLC trace output, not a witness
    sh "$ONE" "$JAR" "$JAVA" witnesses "$(basename "$f" .tla)" 1 &
done
for f in "$HERE"/reach/*.cfg; do
    r="$(basename "$f" .cfg)"
    case " $CARD_REACH " in
        *" $r "*) mod=CardMachine ;;
        *) mod=Store ;;
    esac
    sh "$ONE" "$JAR" "$JAVA" . "$mod" 1 "reach/$r.cfg" "reach-$r" &
done
wait
echo all-done > "$HERE/logs/DONE"

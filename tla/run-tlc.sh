#!/bin/sh
# Run TLC on Store.tla and CardMachine.tla, the reversed witnesses and the
# reach checks, in parallel, one log each under logs/. Run from this directory
# on a bench host, never on the working machine:
#   sh run-tlc.sh /path/to/tla2tools.jar [java]
# Store gets 4 workers, CardMachine 8; every other run gets 1 (they stop at
# the first violation).
set -u
JAR="$1"
JAVA="${2:-java}"
HERE="$(cd "$(dirname "$0")" && pwd)"
ONE="$HERE/run-one.sh"

sh "$ONE" "$JAR" "$JAVA" . Store 4 &
sh "$ONE" "$JAR" "$JAVA" . CardMachine 8 &
for m in StoreNoEvent StoreReplayApplies StoreStaleCheck StoreTwoHolders \
         StoreEpochUnfenced StoreWidthStale \
         CardLandedMoves CardAuthorReads CardRedAdvances CardNeedFailedIgnored \
         CardReleaseBumpsAttempt CardNonHolderFinishes; do
    sh "$ONE" "$JAR" "$JAVA" witnesses "$m" 1 &
done
for r in NeverTaken NeverRecovered NeverStaleEpochRefused NeverNeedsReady \
         NeverNewEpochOp NeverWidthFull; do
    sh "$ONE" "$JAR" "$JAVA" . Store 1 "reach/$r.cfg" "reach-$r" &
done
for r in NeverLanded NeverFixed NeverRed NeverNeedFailed NeverBlocked; do
    sh "$ONE" "$JAR" "$JAVA" . CardMachine 1 "reach/$r.cfg" "reach-$r" &
done
wait
echo all-done > "$HERE/logs/DONE"

#!/bin/sh
# Print the result lines of each TLC log written by run-tlc.sh.
HERE="$(cd "$(dirname "$0")" && pwd)"
for m in Store StoreNoEvent StoreReplayApplies StoreStaleCheck StoreTwoHolders \
         StoreEpochUnfenced StoreWidthStale \
         reach-NeverTaken reach-NeverRecovered reach-NeverStaleEpochRefused \
         reach-NeverNeedsReady reach-NeverNewEpochOp reach-NeverWidthFull \
         CardMachine CardLandedMoves CardAuthorReads CardRedAdvances \
         CardNeedFailedIgnored CardReleaseBumpsAttempt CardNonHolderFinishes \
         reach-NeverLanded reach-NeverFixed reach-NeverRed reach-NeverNeedFailed \
         reach-NeverBlocked; do
    echo "== $m"
    grep -e 'is violated' -e 'No error has been found' -e 'states generated,' \
         -e 'depth of the complete' -e 'Finished in' -e '^exit' -e '^Error: [A-Z]' \
         "$HERE/logs/$m.log"
done

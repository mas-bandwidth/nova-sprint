#!/bin/sh
# Print the result lines of each TLC log written by run-tlc.sh.
HERE="$(cd "$(dirname "$0")" && pwd)"
for m in Store StoreNoEvent StoreReplayApplies StoreStaleCheck StoreTwoHolders \
         reach-NeverRefused reach-NeverTaken reach-NeverRecovered; do
    echo "== $m"
    grep -e 'is violated' -e 'No error has been found' -e 'states generated,' \
         -e 'depth of the complete' -e 'Finished in' -e '^exit' -e '^Error: [A-Z]' \
         "$HERE/logs/$m.log"
done

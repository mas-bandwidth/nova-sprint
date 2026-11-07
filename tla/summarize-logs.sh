#!/bin/sh
# Print the result lines of each TLC log written by run-tlc.sh.
HERE="$(cd "$(dirname "$0")" && pwd)"
names="Store StoreRecords CardMachine CardDeps CardPreempt CardReads CardMerge"
for f in "$HERE"/witnesses/*.tla; do
    case "$f" in *_TTrace_*) continue ;; esac
    names="$names $(basename "$f" .tla)"
done
for f in "$HERE"/reach/*.cfg; do names="$names reach-$(basename "$f" .cfg)"; done
for m in $names; do
    echo "== $m"
    grep -e 'is violated' -e 'No error has been found' -e 'states generated,' \
         -e 'depth of the complete' -e 'Finished in' -e '^exit' -e '^Error: [A-Z]' \
         "$HERE/logs/$m.log"
done

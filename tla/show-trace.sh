#!/bin/sh
# Print the step headers and op ids of the counterexample in logs/<Module>.log.
HERE="$(cd "$(dirname "$0")" && pwd)"
grep -n -e '^State ' -e 'id |-> "o' -e 'accepted |->' -e '^/\\ lost' "$HERE/logs/$1.log"

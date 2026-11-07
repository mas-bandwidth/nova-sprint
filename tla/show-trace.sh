#!/bin/sh
# Print the violated invariant, the step actions, and the event log (accepted
# flag and op id, in order) of the last state of the counterexample in
# logs/<log>.log.
HERE="$(cd "$(dirname "$0")" && pwd)"
LOG="$HERE/logs/$1.log"
grep -e 'is violated' "$LOG"
grep -e '^State ' "$LOG"
last=$(grep -n '^State ' "$LOG" | tail -n 1 | cut -d: -f1)
sed -n "$last,\$p" "$LOG" | sed -n '/^\/\\ events/,/^\/\\ [a-df-z]/p' \
    | grep -o -e 'accepted |-> [A-Z]*' -e 'id |-> "[a-z0-9]*"' | paste - -

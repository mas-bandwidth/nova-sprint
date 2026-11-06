#!/bin/sh
# Print the store variables (epoch, cards, rows) of the last state of a
# counterexample in logs/<name>.log, for checking a trace by hand.
HERE="$(cd "$(dirname "$0")" && pwd)"
LOG="$HERE/logs/$1.log"
start="$(grep -n '^State ' "$LOG" | tail -n 1 | cut -d: -f1)"
[ -n "$start" ] || { echo "no trace in $LOG"; exit 1; }
tail -n +"$start" "$LOG" | awk '/^State /{print} /^\/\\ /{show = ($2 == "epoch" || $2 == "cards" || $2 == "rows")} show{print}'

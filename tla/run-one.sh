#!/bin/sh
# Run TLC on one module in the foreground, log to logs/<log>.log.
#   sh run-one.sh JAR JAVA DIR MODULE WORKERS [CFG] [LOG]
# DIR is . or witnesses; CFG defaults to MODULE.cfg; LOG defaults to MODULE.
set -u
JAR="$1"; JAVA="$2"; DIR="$3"; MOD="$4"; W="$5"
CFG="${6:-$MOD.cfg}"
LOG="${7:-$MOD}"
HERE="$(cd "$(dirname "$0")" && pwd)"
# Each run gets its own java.io.tmpdir: TLC unpacks the standard modules
# there, and parallel runs sharing /tmp race on it (a parser NPE).
TMP="$HERE/states/$LOG.tmp"
mkdir -p "$HERE/logs" "$HERE/states" "$TMP"
cd "$HERE/$DIR" || exit 1
"$JAVA" -XX:+UseParallelGC -Djava.io.tmpdir="$TMP" -cp "$JAR" tlc2.TLC -noGenerateSpecTE -workers "$W" \
    -metadir "$HERE/states/$LOG" -config "$CFG" "$MOD.tla" > "$HERE/logs/$LOG.log" 2>&1
echo "exit $?" >> "$HERE/logs/$LOG.log"

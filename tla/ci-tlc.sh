#!/usr/bin/env bash
# CI run of the store model (layer 1) and the card machine (layer 2):
# bash tla/ci-tlc.sh
#
# Runs TLC once per module, all in parallel, each in its own invocation:
#   Store.tla with Store.cfg and with StoreRecords.cfg (the two scenarios)
#                                      must pass            (TLC exit 0)
#   CardMachine.tla with CardMachine.cfg, CardDeps.cfg, CardPreempt.cfg, CardReads.cfg
#     (its four scenarios)             must pass            (TLC exit 0)
#   witnesses/*.tla with their .cfg    must report a violation (TLC exit 12)
#   reach/*.cfg                        must report a violation (TLC exit 12):
#     reach/Card*.cfg on CardMachine.tla, every other one on Store.tla
# Exits non-zero if a model fails, if any witness or reach run passes, or if any
# run ends any other way (a parse error, a crash, a timeout is not a violation).
# Run it on a bench host, never on the working machine.
#
# DRY_RUN=1 prints the commands and fetches and runs nothing.
# TLA_CACHE is where the pinned tla2tools.jar is kept (default ~/.cache/nova-sprint).
set -u

VERSION=v1.8.0
URL="https://github.com/tlaplus/tlaplus/releases/download/$VERSION/tla2tools.jar"
# The sha256 GitHub publishes for that release asset (releases API, "digest").
SHA256=7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04

HERE="$(cd "$(dirname "$0")" && pwd)"
CACHE="${TLA_CACHE:-$HOME/.cache/nova-sprint}"
JAR="$CACHE/tla2tools-$VERSION.jar"
DRY="${DRY_RUN:-0}"

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1
    else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

find_java() {
    if [ -x "$HOME/sdk/bin/java" ]; then echo "$HOME/sdk/bin/java"; return; fi
    command -v java || true
}

JAVA="$(find_java)"
if [ "$DRY" = 1 ]; then
    echo "dry run: would use jar $JAR (sha256 $SHA256) from $URL"
    echo "dry run: java = ${JAVA:-none found}"
else
    [ -n "$JAVA" ] || { echo "no java on this runner" >&2; exit 1; }
    mkdir -p "$CACHE" || exit 1
    if [ ! -f "$JAR" ] || [ "$(sha256_of "$JAR")" != "$SHA256" ]; then
        curl -fsSL -o "$JAR.part" "$URL" || { echo "download of $URL failed" >&2; exit 1; }
        mv "$JAR.part" "$JAR" || exit 1
    fi
    got="$(sha256_of "$JAR")"
    if [ "$got" != "$SHA256" ]; then
        echo "tla2tools.jar sha256 is $got, want $SHA256" >&2
        rm -f "$JAR"
        exit 1
    fi
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# run NAME EXPECT DIR CONFIG MODULE WORKERS: one TLC invocation.
# EXPECT is pass or violate. Writes $WORK/NAME.log and $WORK/NAME.result.
run() {
    name="$1"; expect="$2"; dir="$3"; cfg="$4"; mod="$5"; workers="$6"
    echo "$name" >> "$WORK/expected"
    if [ "$DRY" = 1 ]; then
        echo "dry run: (cd tla/$dir && java -XX:+UseParallelGC -cp $JAR tlc2.TLC -workers $workers -metadir \$WORK/states/$name -config $cfg $mod)  expect $expect"
        return
    fi
    (
        cd "$HERE/$dir" || exit 1
        # Own java.io.tmpdir per run: TLC unpacks the standard modules there,
        # and parallel runs sharing /tmp race on it (a parser NPE, exit 150).
        mkdir -p "$WORK/tmp/$name" || exit 1
        # -noGenerateSpecTE: no *_TTrace_*.tla trace modules beside the spec,
        # which the witnesses/*.tla glob would otherwise run on the next pass.
        "$JAVA" -XX:+UseParallelGC -Djava.io.tmpdir="$WORK/tmp/$name" -cp "$JAR" tlc2.TLC \
            -noGenerateSpecTE -workers "$workers" \
            -metadir "$WORK/states/$name" -config "$cfg" "$mod" > "$WORK/$name.log" 2>&1
        code=$?
        # The invariant a violate run must break is the first one in its cfg;
        # a violation of any other invariant is a FAIL (the fault is not the
        # one the witness names, or the model broke elsewhere first).
        target="$(grep -m 1 '^INVARIANT' "$cfg" | awk '{print $2}')"
        if [ "$expect" = pass ] && [ "$code" -eq 0 ]; then
            echo "ok   $name: no error found" > "$WORK/$name.result"
        elif [ "$expect" = violate ] && [ "$code" -eq 12 ] \
            && grep -q "Invariant $target is violated" "$WORK/$name.log"; then
            echo "ok   $name: $target violated, as required" > "$WORK/$name.result"
        else
            echo "FAIL $name: TLC exit $code, wanted $expect (log below)" > "$WORK/$name.result"
            echo 1 > "$WORK/$name.failed"
        fi
    ) &
}

run Store pass . Store.cfg Store.tla 4
run StoreRecords pass . StoreRecords.cfg Store.tla 4
run CardMachine pass . CardMachine.cfg CardMachine.tla 4
run CardDeps pass . CardDeps.cfg CardMachine.tla 4
run CardPreempt pass . CardPreempt.cfg CardMachine.tla 4
run CardReads pass . CardReads.cfg CardMachine.tla 4
run Push pass . Push.cfg Push.tla 2
run LandEdge pass . LandEdge.cfg LandEdge.tla 2
run Runner pass . Runner.cfg Runner.tla 2

for f in "$HERE"/witnesses/*.tla; do
    m="$(basename "$f" .tla)"
    case "$m" in *_TTrace_*) continue ;; esac   # TLC trace output, not a witness
    run "witness-$m" violate witnesses "$m.cfg" "$m.tla" 1
done

for f in "$HERE"/reach/*.cfg; do
    m="$(basename "$f" .cfg)"
    case "$m" in
        Card*) mod=CardMachine.tla ;;   # the card machine's reach checks
        *) mod=Store.tla ;;
    esac
    run "reach-$m" violate . "reach/$m.cfg" "$mod" 1
done

wait
[ "$DRY" = 1 ] && exit 0

status=0
# A run that ended before writing its result (a failed cd or mkdir) is a
# failure too, never a silent skip.
while read -r name; do
    if [ ! -f "$WORK/$name.result" ]; then
        echo "FAIL $name: no result written" > "$WORK/$name.result"
        echo 1 > "$WORK/$name.failed"
        touch "$WORK/$name.log"
    fi
done < "$WORK/expected"
for r in "$WORK"/*.result; do
    cat "$r"
    if [ -f "${r%.result}.failed" ]; then
        status=1
        echo "---- ${r%.result}.log (tail)"
        tail -n 40 "${r%.result}.log"
    fi
done
exit $status

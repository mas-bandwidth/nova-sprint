#!/usr/bin/env bash
# gen-witnesses.sh DIR: writes the reversed witnesses into DIR.
#
# Each witness is its model with one fault; the fault lives in the make-*witnesses.py
# generators (exact text replacements that must match, so a model change a fault no
# longer fits stops the generator). This copies the models, configs and generators
# into DIR and runs every generator there, leaving DIR/witnesses/*.tla,
# DIR/witnesses/layer6/*.tla, their configs, the reach configs and the passing runs'
# configs. None of those is committed (tla/.gitignore); the reach checks without a
# generator (Runner) are.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
DIR="$1"
mkdir -p "$DIR/witnesses/layer6" "$DIR/reach"
cp "$HERE"/*.tla "$HERE"/*.cfg "$HERE"/make-*witnesses.py "$DIR"/
cp "$HERE"/reach/*.cfg "$DIR/reach"/
for g in "$DIR"/make-*witnesses.py; do
    python3 -I "$g" > /dev/null
done

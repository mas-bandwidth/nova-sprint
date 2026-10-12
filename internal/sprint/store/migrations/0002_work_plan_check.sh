#!/usr/bin/env sh
# Run on a STOPPED store before installing this build; never as part of a gate.
# Existing cards retain their columns. The locked-shape check prints these same commands.
set -eu
nova-table col add work plan --first
nova-table col add work check --after plan

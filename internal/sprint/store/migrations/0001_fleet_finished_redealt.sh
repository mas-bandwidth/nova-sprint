#!/bin/bash
# 0001_fleet_finished_redealt.sh
# Adds finished (hidden) and redealt (shown) columns to the fleet table.
# Run on a STOPPED machine before install.
#
# The owner, 2026-10-04: "trust but VERIFY"; "I want to trust the ok%";
# "Are they actually doing the work that is shown in the friend table? Really?"
# "i don't want dollar amounts for friends. token counts are fine."

set -eu
cd "$(dirname "$0")/.."

# finished (hidden): a worker's finish, ok or failed, moves its work card here
# before its readers' verdicts move it to ok or failed.
# redealt (shown): a friend's card past its deadline unfinished, never a failure.
nova-table col add fleet finished --after withdrawn
nova-table col add fleet redealt --after okpct
nova-table set fleet --hide finished

#!/bin/bash
# 0001_fleet_finished_redealt.sh
# Adds finished (hidden), redealt (shown) and the end-class columns (provider,
# coordination, environment, harness, no_result; all hidden) to the fleet table,
# and rewrites ok% to count only the work the machine did (ok over ok + failed
# work + no result). Run on a STOPPED machine before install.
#
# The owner, 2026-10-04: "trust but VERIFY"; "I want to trust the ok%";
# "Are they actually doing the work that is shown in the friend table? Really?"

set -eu
cd "$(dirname "$0")/.."

# The end-class columns, each hidden, hold a member's work cards whose take ended
# not-its-fault (provider, coordination, environment, harness) or with no result.
nova-table col add fleet provider --after okpct
nova-table col add fleet coordination --after provider
nova-table col add fleet environment --after coordination
nova-table col add fleet harness --after environment
nova-table col add fleet no_result --after harness

# ok% counts only work ends: ok over ok + failed work + no result. no_result must
# exist before the formula reads it (the column add above), so the formula is
# dropped and re-added now.
nova-table col del fleet okpct
nova-table col add fleet okpct:pct(ok/ok+failed+no_result):pooled:ok% --after done

# redealt (shown): a friend's card past its deadline unfinished, never a failure.
nova-table col add fleet redealt --after no_result

# finished (hidden): a worker's finish, ok or failed, moves its work card here
# before its readers' verdicts move it to ok or failed.
nova-table col add fleet finished --after withdrawn
nova-table set fleet --hide finished
nova-table set fleet --hide provider
nova-table set fleet --hide coordination
nova-table set fleet --hide environment
nova-table set fleet --hide harness
nova-table set fleet --hide no_result

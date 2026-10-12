#!/bin/bash
# 0002_fleet_end_class_columns.sh
# Adds the end-class columns (provider, coordination, environment, harness,
# no_result; all hidden) to the fleet table, and rewrites ok% to count only the
# work the machine did (ok over ok + failed work + no result). Run on a STOPPED
# machine before install, after 0001_fleet_finished_redealt.sh.
#
# The owner, 2026-10-12: "have you fixed all the silly shit where the
# machines/models got blamed for coordination mistakes?" / "I want to see
# accurate ok%".

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

nova-table set fleet --hide provider
nova-table set fleet --hide coordination
nova-table set fleet --hide environment
nova-table set fleet --hide harness
nova-table set fleet --hide no_result

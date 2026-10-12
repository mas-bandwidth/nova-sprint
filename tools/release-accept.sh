#!/bin/sh
# release-accept: the release gate of a nova-sprint point release (docs/TESTS.md, "Release
# acceptance"). Each check a release passed by watching the live server is a test here, every
# one on a store of its own, run in parallel, the whole set under one minute on a bench.
#
#   make release-accept
#
# It needs redis-server on PATH (NOVA_CI=1: a missing one fails, never skips). Every name
# below must run and pass: a test renamed or gone fails the gate by the count, so the set
# never shrinks in silence. The name list is the gate; add a check by adding its test here.
set -eu

GO=${GO:-go}
P=${RELEASE_ACCEPT_P:-8}

# the tests, by the check each pins
NAMES='
TestConsumersOutWorkingCardWithNoAttempt
TestConsumersOutReviewCardWhoseReadsAreAllForOtherCards
TestConsumersOutReviewCardWithNoReadAndNoJudgment
TestConsumersOutDoesNotFlagCardsWhoseConsumersAreOut
TestEveryProducerCard
'
PKGS='./cmd/nova-sprint/ ./internal/sprint/'

want=$(printf '%s\n' $NAMES | sed '/^$/d' | sort -u | wc -l | tr -d ' ')
run="^($(printf '%s\n' $NAMES | sed '/^$/d' | paste -sd '|' -))\$"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ -z "${NOVA_SPRINT_BIN:-}" ]; then
	NOVA_SPRINT_BIN=$work/nova-sprint
	$GO build -o "$NOVA_SPRINT_BIN" ./cmd/nova-sprint
fi
export NOVA_SPRINT_BIN NOVA_CI=1
# auto maintenance off for every git the tests start (Makefile, NO_AUTO_MAINTENANCE)
export GIT_CONFIG_PARAMETERS="'maintenance.auto'='false'"

began=$(date +%s)
status=0
# shellcheck disable=SC2086 # PKGS is a list of packages
$GO test -tags functional -count=1 -p "$P" -parallel "$P" -timeout 2m -v -run "$run" $PKGS >"$work/out" 2>&1 || status=$?
wall=$(($(date +%s) - began))

grep -E '^(FAIL|ok  )' "$work/out" || true
passed=$(grep -E '^--- PASS: Test' "$work/out" | awk '{print $3}' | sort -u | wc -l | tr -d ' ')
failed=$(grep -cE '^--- FAIL: Test' "$work/out" || true)
missing=$(printf '%s\n' $NAMES | sed '/^$/d' | sort -u | while read -r n; do grep -qE "^--- (PASS|FAIL): $n " "$work/out" || echo "$n"; done)

if [ "$status" -ne 0 ] || [ "$passed" -ne "$want" ]; then
	[ -n "$missing" ] && printf 'RELEASE ACCEPT MISSING %s\n' $missing
	awk '/^[[:space:]]*--- FAIL/ {f=1; print; next} f && /^[[:space:]]+[^=]/ {print; next} {f=0}' "$work/out" | head -200
	grep -E '^panic:' -A20 "$work/out" | head -40 || true
	echo "RELEASE ACCEPT FAILED passed=$passed of $want failed=$failed wall=${wall}s bin=$NOVA_SPRINT_BIN"
	exit 1
fi
echo "RELEASE ACCEPT OK passed=$passed of $want wall=${wall}s bin=$NOVA_SPRINT_BIN"

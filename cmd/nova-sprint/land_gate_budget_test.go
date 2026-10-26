package main

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/bench"
	"github.com/stretchr/testify/assert"
)

// 2026-10-17: one tree-gate run sat 14 minutes on the bench vision and the
// landing waited on it. A gate run past its budget is the bench's fault: the
// ring steps to the next bench and the stage line names the budget.
func TestAGateRunPastItsBudgetStepsToTheNextBench(t *testing.T) {
	t.Parallel()
	f := newBenchFullRig(t)
	ring := benchRing("s1", f.hosts)
	slow, next := ring[0], ring[1]
	f.answer = func(host string) (string, int, error) {
		if host == slow {
			return "GATE RUN: go test ./...\n", bench.NoAnswer, context.DeadlineExceeded
		}
		return "", 0, nil
	}
	runs := gateCmds(gateRuns(false, nil, nil))
	l := &lander{a: f.r.a, st: f.l.st, gateKey: "s1"}
	why, ran := l.benchGate(t.Context(), f.hosts, f.r.clone, runs, false)
	assert.Empty(t, why)
	assert.True(t, ran, "the gate ran on the next bench")
	assert.Equal(t, []string{slow, next}, f.asked, "the slow bench first (the ring's order), then the next")
	assert.Contains(t, f.say(l), slow, "the slow bench is named: %s", f.say(l))
	assert.Contains(t, f.say(l), "no answer within", "the budget is named: %s", f.say(l))
	f.r.clean()
}

// 2026-10-18: the coordinator's ruling is to keep the v1.6.3 bound until gate-run
// walls are measured per repository from the land log; the 2026-10-17 vision run
// is one sample, not a measurement.
func TestTheGateRunBudgetIsFifteenMinutesUntilMeasured(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 15*time.Minute, landGateRunBudget, "the v1.6.3 bound, kept until the gate runs are measured")
}

// A run that returned green as the budget's deadline fired is the run's answer: the
// gate is green on that bench and the ring does not step on. gateRunBudgetOverride is the
// test's seam: one nanosecond, so the run's context is past its deadline before the
// answer returns; no sleep, no real time. It sets a package variable, so it does not
// run parallel.
func TestAGateRunGreenAtItsDeadlineIsKept(t *testing.T) {
	old := gateRunBudgetOverride
	gateRunBudgetOverride = time.Nanosecond
	defer func() { gateRunBudgetOverride = old }()
	f := newBenchFullRig(t)
	first := benchRing("s1", f.hosts)[0]
	f.answer = func(string) (string, int, error) { return "", 0, nil }
	runs := gateCmds(gateRuns(false, nil, nil))
	l := &lander{a: f.r.a, st: f.l.st, gateKey: "s1"}
	why, ran := l.benchGate(t.Context(), f.hosts, f.r.clone, runs, false)
	assert.Empty(t, why)
	assert.True(t, ran, "the gate is green on the first bench")
	assert.Equal(t, []string{first}, f.asked, "the ring did not step on: %s", f.asked)
	f.r.clean()
}

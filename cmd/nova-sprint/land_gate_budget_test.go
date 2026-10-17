package main

import (
	"context"
	"testing"

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

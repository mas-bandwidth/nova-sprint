package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// workerVerbCase is one worker verb's shortest valid line.
type workerVerbCase struct {
	// line is the shortest valid line that names its worker the verb's own
	// way, run with no NOVA_SPRINT_ACTOR and no --actor.
	line string
	// actor is the worker the line names: the record it writes must name it.
	actor string
	// setup prepares the state the line needs on a fresh app (init is done).
	setup func(*testApp)
	// logVerb is the verb's own line in the log, read back to hold that the
	// record names the worker; "" when the verb writes no card move to read.
	logVerb string
}

// Every worker's verb is its worker's (coordinator.go, the classes): the member
// or reader it names is the actor, whatever --actor or NOVA_SPRINT_ACTOR say,
// and no --actor is needed (coordinator.go:25-26, orActor's doc at
// coordinator.go:60-62). A worker verb that stops naming its worker before the
// store is opened refuses with "--actor <name> is required" (needsActor,
// coordinator.go:68-77); this test holds each one runs without one.
func TestEveryWorkerVerbRunsAsItsWorkerWithNoActor(t *testing.T) {
	t.Parallel()
	cases := map[string]workerVerbCase{
		"take":        {line: "take --as m1 --limit 1", actor: "m1", setup: readyForM1, logVerb: "take"},
		"finish":      {line: "finish --as m1 s1-1.w1@1", actor: "m1", setup: heldByM1, logVerb: "finish"},
		"progress":    {line: "progress --as m1 --epoch 0 s1-1.w1", actor: "m1", setup: heldByM1, logVerb: "progress"},
		"read":        {line: "read --as reader-a --begin", actor: "reader-a", setup: readAskedOfReaderA, logVerb: "read"},
		"queue":       {line: "queue --as m1", actor: "m1", setup: readyForM1},
		"stop-return": {line: "stop-return --as m1 --reason r s1-1.w1@1", actor: "m1", setup: heldByM1},
		"fleet beat":  {line: "fleet beat m1", actor: "m1"},
		"friend beat": {line: "friend beat friend-a", actor: "friend-a"},
		"lane take":   {line: "lane take go --machine bench --as m1", actor: "m1"},
		"lane give":   {line: "lane give go --machine bench --as m1", actor: "m1"},
		// remind is any actor's (coordinator.go:47): its --for defaults to the
		// caller's --actor (remind.go:82), so it is run with --actor <member> and
		// without one is refused by the actor line; checked in its own test below.
		"remind": {line: "remind --actor m1 --in 30m --note n", actor: "m1"},
		// the rate limiter's verbs name their worker through --as like the rest.
		"budget take":    {line: "budget take openrouter/gpt --as m1 --holder h1", actor: "m1"},
		"budget renew":   {line: "budget renew openrouter/gpt --as m1 --holder h1", actor: "m1"},
		"budget release": {line: "budget release openrouter/gpt --as m1 --holder h1", actor: "m1"},
	}
	for verb, class := range verbClasses {
		if class != classWorker {
			continue
		}
		k, ok := cases[verb]
		if !assert.True(t, ok, "add a case for %s", verb) {
			continue
		}
		verb, k := verb, k
		t.Run(verb, func(t *testing.T) {
			t.Parallel()
			ta := newWorkerApp(t)
			if k.setup != nil {
				k.setup(ta)
			}
			ta.a.getenv = func(name string) string {
				return map[string]string{"NOVA_SPRINT_REDIS": "mem:0"}[name]
			}
			code, out, errs := ta.do(k.line)
			assert.NotContains(t, out+errs, "--actor <name> is required", "%s with no actor: exit %d %q %q", k.line, code, out, errs)
			if k.logVerb != "" {
				assertWorkerRecorded(t, ta, k.logVerb, k.actor)
			}
		})
	}
}

// A worker verb that names no worker and no --actor is refused and writes
// nothing: take wants --as, and with no --as it is refused before the store is
// opened, as the coordinator verbs are in TestTheActorHasNoDefault.
func TestAWorkerVerbThatNamesNoWorkerIsRefused(t *testing.T) {
	t.Parallel()
	ta := newWorkerApp(t)
	ta.deal(1)
	ta.a.getenv = func(name string) string { return map[string]string{"NOVA_SPRINT_REDIS": "mem:0"}[name] }
	before := ta.applies()
	code, out, errs := ta.do("take")
	require.Equal(t, 2, code, "take with no --as and no --actor: exit %d %q %q", code, out, errs)
	require.True(t, strings.Contains(errs, "--actor <name> is required") || strings.Contains(errs, "wants --as <member>"),
		"take with no worker is refused by the actor line or its own usage line: %q", errs)
	require.Equal(t, before, ta.applies(), "a refused take wrote")
}

// remind is any actor's, and its worker run names its worker through --actor;
// without one it is refused by the actor line and writes nothing.
func TestRemindWithoutAnActorIsRefused(t *testing.T) {
	t.Parallel()
	ta := newWorkerApp(t)
	ta.a.getenv = func(name string) string { return map[string]string{"NOVA_SPRINT_REDIS": "mem:0"}[name] }
	before := ta.applies()
	code, out, errs := ta.do("remind --in 30m --note n")
	require.Equal(t, 2, code, "remind with no actor: exit %d %q %q", code, out, errs)
	require.Contains(t, errs, "--actor <name> is required (or NOVA_SPRINT_ACTOR)", "remind with no actor: %q", errs)
	require.Equal(t, before, ta.applies(), "a refused remind wrote")
	require.Contains(t, ta.ok("remind --actor m1 --in 30m --note n"), "REMIND OK", "remind as the member")
}

// newWorkerApp is a testApp with the sprint's tables, its coordinator still the
// caller (NOVA_SPRINT_ACTOR): init and every setup line run as the coordinator,
// then the caller takes that variable away before the worker verb runs.
func newWorkerApp(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --members m1,m2 --readers reader-a,reader-b")
	ta.ok("set --read-cards off")
	return ta
}

// readyForM1 is a card added and dealt, ready for m1's take or queue.
func readyForM1(ta *testApp) {
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
}

// heldByM1 is a card added, dealt and taken by m1: the state finish, progress
// and stop-return need.
func heldByM1(ta *testApp) {
	readyForM1(ta)
	ta.ok("take --as m1 --limit 1")
}

// readAskedOfReaderA is a card taken and finished and asked of the readers, so
// read --begin has a read card to move.
func readAskedOfReaderA(ta *testApp) {
	ta.ok("add --stream s1 --count 1 --one")
	ta.ok("start")
	ta.deal(1)
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1")
	ta.ok("tick")
}

// assertWorkerRecorded holds that the log holds a <verb> line whose actor is
// the worker the verb named, read through log --json as the package's tests do.
func assertWorkerRecorded(t *testing.T, ta *testApp, verb, actor string) {
	t.Helper()
	var log struct {
		Lines []sprint.Line `json:"lines"`
	}
	ta.json("log", &log)
	found := false
	for _, l := range log.Lines {
		if l.Verb == verb && l.Actor == actor {
			found = true
		}
	}
	assert.True(t, found, "the log holds no %s line for %s (the worker the verb named)", verb, actor)
}

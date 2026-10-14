package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// --as and --actor name the same actor on a coordinator verb (RATING 2026-10-14:
// coordinator verbs require --actor and refuse --as; worker verbs require --as).
// A coordinator read verb run with --as records that actor. take --as still
// names the member, not the actor.
func TestAsAndActorNameTheSameActor(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --members m1 --as boss")
	ta.ok("add --stream s1 --count 1 --one --as boss")
	code, out, errs := ta.do("log --as boss")
	require.Equal(t, 0, code, "a coordinator read verb with --as: %s%s", out, errs)
	var log struct {
		Lines []sprint.Line `json:"lines"`
	}
	ta.json("log --as boss", &log)
	var addActor string
	for _, l := range log.Lines {
		if l.Verb == "add" {
			addActor = l.Actor
		}
	}
	assert.Equal(t, "boss", addActor, "log --as boss records actor boss")

	ta.ok("start --as boss")
	ta.ok("tick")
	ta.ok("take --as m1")
	ta.json("log", &log)
	var takeActor string
	for _, l := range log.Lines {
		if l.Verb == "take" {
			takeActor = l.Actor
		}
	}
	assert.Equal(t, "m1", takeActor, "take --as m1 still means member m1")

	_, help, herr := ta.do("log -h")
	assert.Empty(t, herr)
	assert.Contains(t, help, "--actor (alias --as)")
	_, takeHelp, _ := ta.do("take -h")
	assert.NotContains(t, takeHelp, "--actor (alias --as)", "take --as names the member, not the actor")
	assert.False(t, strings.Contains(takeHelp, "alias --as"), "take -h does not alias --as to --actor")
}

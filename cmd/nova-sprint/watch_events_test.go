package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestWatchEventsPrintsOneLinePerKindAndFoldsARepeat(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	ta.a.setWatchLines(func(context.Context, *store.Store) ([]sprint.Line, error) {
		return []sprint.Line{
			{Verb: "ask"},
			{Verb: "ask"},
			{Verb: "read"},
			{Verb: "finish"},
			{Verb: "merge", To: "s1:merging"},
			{Verb: "land"},
			{Note: &sprint.Note{Kind: sprint.Judgment, Type: sprint.NConflict}},
			{Verb: "drain", Cause: "refused: the queue is held"},
			{Note: &sprint.Note{Kind: sprint.Judgment, Type: sprint.NWorkFailed}},
		}, nil
	})
	out := ta.ok("watch --events")
	for _, kind := range sprint.EventKinds {
		assert.Contains(t, out, "EVENT "+kind+" count=")
	}
	assert.Contains(t, out, "EVENT read-asked count=2")
	assert.Contains(t, out, "EVENT blocked-merge count=1")
	assert.Contains(t, out, "EVENT pushed-judgment count=1")
	assert.Equal(t, len(sprint.EventKinds), strings.Count(out, "\n"))
	assert.NotContains(t, out, "WAKE")

	code, _, errs := ta.do("watch")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--wake")
	assert.Contains(t, errs, "run: nova-sprint watch")

	code, _, errs = ta.do("watch --wake --events")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--wake")

	up := ta.ok("check --bring-up")
	assert.Contains(t, up, "BRING-UP event-watch running")
}

func TestWatchEventsReadsTheStoreLogWhenNoLinesAreGiven(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members m1")
	out := ta.ok("watch --events")
	assert.NotContains(t, out, "WAKE")
	up := ta.ok("check --bring-up")
	assert.Contains(t, up, "BRING-UP event-watch running")
	require.NotContains(t, up, "launchctl")
}

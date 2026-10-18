package main

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/decide"
)

// reviewRig is a sprint with s1-1 in review at a head, and a decide lane over a fresh record
// whose clock is the test's.
func reviewRig(t *testing.T) (*testApp, *decideLane) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + proBriefFile(t))
	ta.deal(1)
	ta.ok("take --as m1")
	ta.ok("finish --as m1 s1-1.w1@1 --head 0123456789abcdef0123456789abcdef01234567")
	require.Equal(t, sprint.Review, ta.primary("s1-1").Col)
	lane := newDecideLane(t.TempDir(), &fakeShadowReadBackend{verdict: decide.Bounce}, ta.a.now, GradeWait)
	ta.a.decide = lane
	return ta, lane
}

// A shadow read whose diff cannot be taken (git cat-file exit status 128, 2026-10-17) is
// asked again after a minute, then after two, four, ... up to thirty; never every round.
// Before, it was asked again every DecideEvery for hours, six git processes a second.
// A new head on the card is a new ask, asked at once.
func TestAFailedShadowReadBacksOffUntilItsHeadChanges(t *testing.T) {
	t.Parallel()
	ta, lane := reviewRig(t)
	diffs := 0
	lane.diffOf = func(context.Context, *sprint.Card) (string, error) {
		diffs++
		return "", errors.New("exit status 128")
	}
	ctx := context.Background()
	s, err := ta.a.decideSnapshot(ctx, "mem:0")
	require.NoError(t, err)
	round := func() []string {
		_, problems := lane.shadowReadRound(ctx, s)
		return problems
	}
	require.Len(t, round(), 1)
	require.Equal(t, 1, diffs, "the first round asks")
	for range 11 {
		ta.step(DecideEvery)
		require.Empty(t, round(), "a backed-off read says nothing")
	}
	require.Equal(t, 1, diffs, "no ask inside the first minute")
	ta.step(readRetryFirst - 11*DecideEvery)
	require.Len(t, round(), 1)
	require.Equal(t, 2, diffs, "asked again at one minute")
	ta.step(2*readRetryFirst - time.Second)
	round()
	require.Equal(t, 2, diffs, "the second wait is two minutes")
	ta.step(time.Second)
	round()
	require.Equal(t, 3, diffs)
	for range 10 {
		ta.step(readRetryCap)
		round()
	}
	require.Equal(t, 13, diffs, "the wait stops doubling at the cap")
	require.Equal(t, readRetryCap, lane.readFail["s1-1@0123456789abcdef0123456789abcdef01234567"].wait)

	// a new head is a new ask, at once, and the old head's wait is forgotten
	c := s.Work.Placed("s1-1")
	c.Fields["head"] = "fedcba9876543210fedcba9876543210fedcba98"
	require.Len(t, round(), 1)
	require.Equal(t, 14, diffs, "a new head is asked at once")
	require.Len(t, lane.readFail, 1)
	_, old := lane.readFail["s1-1@0123456789abcdef0123456789abcdef01234567"]
	assert.False(t, old, "the old head is forgotten")

	// and a read that goes through clears its wait
	lane.diffOf = func(context.Context, *sprint.Card) (string, error) { diffs++; return "+new\n", nil }
	ta.step(readRetryFirst)
	n, problems := lane.shadowReadRound(ctx, s)
	require.Empty(t, problems)
	assert.Equal(t, 1, n)
	assert.Empty(t, lane.readFail)
}

// The record's open decisions are decoded again only when its file changed: a file of the
// same size and time is not read (the proof: garbage of the same size and time is not
// noticed), and an append is read at once.
func TestTheLaneReadsARecordOnlyWhenItsFileChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := func() time.Time { return time.Unix(1000, 0).UTC() }
	lane := newDecideLane(dir, nil, now, GradeWait)
	path := lane.record(decide.JudgmentAnswerName)
	d := func(id string) decide.Decision {
		return decide.Decision{ID: id, Decision: decide.JudgmentAnswerName, At: now().Format(time.RFC3339), Inputs: map[string]string{"card": "s1-1"}}
	}
	require.NoError(t, lane.recordAnswer(d("j1")))
	open, err := lane.openOf(path)
	require.NoError(t, err)
	require.Len(t, open, 1)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	garbage := bytes.Repeat([]byte("x"), int(fi.Size()))
	require.NoError(t, os.WriteFile(path, garbage, 0o644))
	require.NoError(t, os.Chtimes(path, fi.ModTime(), fi.ModTime()))
	open, err = lane.openOf(path)
	require.NoError(t, err, "an unchanged size and time is not read again")
	require.Len(t, open, 1)

	require.NoError(t, os.WriteFile(path, nil, 0o644))
	require.NoError(t, lane.recordAnswer(d("j2")))
	open, err = lane.openOf(path)
	require.NoError(t, err)
	require.Len(t, open, 1, "the changed file is read again")
	assert.Equal(t, "j2", open[0].ID)

	_, err = lane.openOf(filepath.Join(dir, "absent.jsonl"))
	require.NoError(t, err, "an absent record is no decisions")
}

// A round over a store whose write generation stands still, with nothing queued and nothing
// failed, reads the store's fence and nothing else: no line, no work-table load. A queued
// decision, a write to the store, or decideQuietMax brings the full round back.
func TestAQuietDecideRoundReadsOnlyTheFence(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 s1-1 --one --brief-file " + proBriefFile(t))
	ta.a.decide = newDecideLane(t.TempDir(), fixedGrade(), ta.a.now, GradeWait)
	ctx := context.Background()
	var out bytes.Buffer
	ta.a.decideRound(ctx, "mem:0", &out)
	require.Contains(t, out.String(), "graded=1")
	ta.a.decideRound(ctx, "mem:0", &out) // the grade's write moved the generation: one full round
	calls := func() map[string]int { return maps.Clone(ta.m.Calls) }
	before := calls()
	ta.step(DecideEvery)
	out.Reset()
	ta.a.decideRound(ctx, "mem:0", &out)
	assert.Empty(t, out.String())
	after := calls()
	diff := map[string]int{}
	for k, v := range after {
		if v != before[k] {
			diff[k] = v - before[k]
		}
	}
	assert.Equal(t, map[string]int{"fence": 1}, diff, "a quiet round reads the fence alone")

	// full says a round read more of the store than its fence
	full := func() bool {
		b := calls()
		ta.a.decideRound(ctx, "mem:0", &out)
		for k, v := range calls() {
			if k != "fence" && v != b[k] {
				return true
			}
		}
		return false
	}
	ta.a.decide.put(decide.Decision{ID: "s1-1@attempt.x", Decision: decide.AttemptName, At: ta.a.now().Format(time.RFC3339), Inputs: map[string]string{"card": "s1-1"}})
	assert.True(t, full(), "a queued decision is a full round")
	full() // its own record write is read once
	assert.False(t, full(), "and then quiet again")
	ta.ok("add --stream s1 s1-9 --one --brief-file " + proBriefFile(t))
	assert.True(t, full(), "a write to the store is a full round")
	full()
	assert.False(t, full())
	ta.step(decideQuietMax)
	assert.True(t, full(), "decideQuietMax brings a full round back")
}

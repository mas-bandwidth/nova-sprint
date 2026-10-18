package main

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

const friendCLIEnvFault = "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"

func seedFriendControlForEnvFault(t *testing.T, ta *testApp, row string) *store.Store {
	t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	_, err = st.Run(context.Background(), store.Step{
		Verb: "fixture friend control",
		Load: []string{sprint.Fleet},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			var p sprint.Plan
			if !s.Fleet.HasRow(row) {
				p.Rows = append(p.Rows, sprint.RowAdd{Table: sprint.Fleet, Row: row})
			}
			id := sprint.CtlID(row)
			e := ntable.BatchMemberEntry{
				ID:     id,
				Expect: &ntable.MemberExpect{Absent: true},
				Create: &ntable.MemberCreateOp{Row: row, Col: sprint.Ctl},
				Set:    map[string]string{"kind": "member", "status": sprint.Up, sprint.FieldWidth: "2"},
			}
			p.Units = append(p.Units, sprint.Unit{Key: id, Changes: []sprint.Change{{Table: sprint.Fleet, Entry: e}}})
			return p
		},
	})
	require.NoError(t, err)
	return st
}

func TestFleetUpFriendRowCLIOnlyClearsItsEnvironmentFault(t *testing.T) {
	t.Parallel()
	ta, _ := friendCardApp(t, "only friend amy", "amy")
	row := sprint.FriendRow("amy")
	st := seedFriendControlForEnvFault(t, ta, row)
	ta.ok("tick")
	ta.startFriend("amy", 1)

	workCards, err := st.ReadCells(context.Background(), sprint.Fleet, row, sprint.Working)
	require.NoError(t, err)
	require.Len(t, workCards, 1)
	workID := workCards[0].ID

	res, err := st.Run(context.Background(), store.FinishStep(sprint.FinishReq{
		As: row, Sel: sprint.Sel{IDs: []string{workID}}, Gens: map[string]int{workID: workCards[0].Int("gen")},
		Failed: true, Report: friendCLIEnvFault,
	}))
	require.NoError(t, err)
	require.Empty(t, res.Refused)

	beforeCards, err := st.ReadCells(context.Background(), sprint.Fleet, row, sprint.Ctl)
	require.NoError(t, err)
	require.Len(t, beforeCards, 1)
	before := maps.Clone(beforeCards[0].Fields)
	require.Equal(t, sprint.EnvPush, before[sprint.FieldEnvFault])
	require.Equal(t, sprint.Held, before["status"])
	beforeBeats, err := st.Beats(context.Background(), []string{row})
	require.NoError(t, err)

	// The CLI's own command prelude must not synthesize friend liveness.
	ta.live, ta.noServer = nil, true
	ta.ok("fleet up " + row + " --probe passed")

	afterStore, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	afterCards, err := afterStore.ReadCells(context.Background(), sprint.Fleet, row, sprint.Ctl)
	require.NoError(t, err)
	require.Len(t, afterCards, 1)
	want := maps.Clone(before)
	for _, field := range []string{
		sprint.FieldEnvFault, sprint.FieldEnvFaultReason, sprint.FieldEnvFaultAt,
		"held", sprint.FieldHeldBy, sprint.FieldHeldReason, sprint.FieldHeldFinish,
	} {
		delete(want, field)
	}
	assert.Equal(t, want, afterCards[0].Fields, "only the environment fault and its own hold clear")
	assert.Equal(t, before["status"], afterCards[0].F("status"), "friend status remains hook-owned")
	assert.Equal(t, before[sprint.FieldWidth], afterCards[0].F(sprint.FieldWidth), "friend width remains hook-owned")
	open, err := afterStore.B.OpenNotes(context.Background())
	require.NoError(t, err)
	for _, note := range open {
		assert.False(t, note.Note.Type == sprint.NMemberEnvFault && note.Note.Stream == sprint.MemberSubject(row), "the matching judgment closes with its environment fault")
	}

	afterBeats, err := afterStore.Beats(context.Background(), []string{row})
	require.NoError(t, err)
	assert.Equal(t, beforeBeats, afterBeats, "fleet up must not write a synthetic friend beat")
}
func TestFleetUpFriendRowCLIWithoutFaultRefusesByNameWithoutBeating(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	row := sprint.FriendRow("amy")
	ta.live, ta.noServer = nil, true
	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	before, err := st.Beats(context.Background(), []string{row})
	require.NoError(t, err)

	code, _, errs := ta.do("fleet up " + row + " --probe passed")
	require.Equal(t, 1, code)
	assert.Contains(t, errs, "a friend row is not a fleet member; fleet up "+row+" --probe passed clears only its environment fault")

	after, err := st.Beats(context.Background(), []string{row})
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refused friend-row up must not synthesize a beat")
}

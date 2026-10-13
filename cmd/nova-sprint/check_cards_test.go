package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// start refuses until a clean card check matches the cards (v1.2.17,
// start-refuses-until-a-clean-check; the owner, 2026-10-13: "if you start a sprint before
// checking, then it will fail and tell you to check."). The store is the in-memory twin on the
// test's own clock; the base trees are handed in (checkTree), so no git, socket or network is
// read (tla/MachineReady.tla Check, StartRefusedCheck, StartOnlyChecked).

// checkTreeFiles is the base tree every brief's REPO and BASE read in these tests.
var checkTreeFiles = []string{"internal/a.go", "internal/b.go", "internal/c.go", "internal/x/one.go", "internal/x/two.go", "internal/y/one.go"}

// gateOn is a gate-on app (newApp's default) after init and a clear: READY, no check record.
func gateOn(t *testing.T) *testApp {
	t.Helper()
	ta := newTestApp(t)
	ta.a.cardCheckGate = true
	ta.a.checkTree = func(context.Context, swarm.CardBase) sprint.AdmissionTree {
		return sprint.AdmissionTree{Files: checkTreeFiles}
	}
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("clear --confirm sprint")
	return ta
}

// lintedBrief is a brief that passes the card lint, naming its repository, base and PATHS.
func lintedBrief(id, base, paths string) string {
	return passingBrief("RESULT: " + id + " sha=0123456789ab tier: pro\nREPO: owner/repo\nBASE: " + base + "\nPATHS: " + paths + "\nTHE TASK. Do the work.")
}

// seed writes cards and streams straight to the tables, as a migration or an older binary
// would: the defects the card check exists to find are ones add refuses.
func (ta *testApp) seed(rows []string, ctls map[string]map[string]string, cards []ntable.BatchMemberEntry) {
	ta.t.Helper()
	st, err := ta.a.store(common{redis: "mem:0", actor: "migration"})
	require.NoError(ta.t, err)
	res, err := st.Run(context.Background(), store.Step{Verb: "seed cards", Load: []string{sprint.Work, sprint.Merge},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			var p sprint.Plan
			for _, r := range rows {
				if !s.Work.HasRow(r) {
					p.Rows = append(p.Rows, sprint.RowAdd{Table: sprint.Work, Row: r})
				}
				if !s.Merge.HasRow(r) {
					p.Rows = append(p.Rows, sprint.RowAdd{Table: sprint.Merge, Row: r})
				}
			}
			for stream, f := range ctls {
				e := ntable.BatchMemberEntry{ID: sprint.CtlID(stream), Set: f}
				if c := s.Merge.Card(sprint.CtlID(stream)); c != nil {
					e.Expect = &ntable.MemberExpect{Revision: itoa(int(c.Rev))}
				} else {
					e.Expect = &ntable.MemberExpect{Absent: true}
					e.Create = &ntable.MemberCreateOp{Row: stream, Col: sprint.Ctl}
				}
				p.Units = append(p.Units, sprint.Unit{Key: sprint.CtlID(stream), Stream: stream, Changes: []sprint.Change{{Table: sprint.Merge, Entry: e}}})
			}
			for _, e := range cards {
				if c := s.Work.Card(e.ID); c != nil {
					e.Expect, e.Create = &ntable.MemberExpect{Revision: itoa(int(c.Rev))}, nil
				}
				p.Units = append(p.Units, sprint.Unit{Key: e.ID, Changes: []sprint.Change{{Table: sprint.Work, Entry: e}}})
			}
			return p
		}})
	require.NoError(ta.t, err)
	require.Empty(ta.t, res.Refused)
}

// seedCard is a primary's entry: created in the stream and state with its fields.
func seedCard(id, stream, col string, fields map[string]string) ntable.BatchMemberEntry {
	set := map[string]string{"kind": "primary", "stream": stream, "attempt": "0"}
	for k, v := range fields {
		set[k] = v
	}
	return ntable.BatchMemberEntry{ID: id, Expect: &ntable.MemberExpect{Absent: true}, Create: &ntable.MemberCreateOp{Row: stream, Col: col, Score: 1}, Set: set}
}

// edit is a change of fields on a card already seeded.
func edit(id string, fields map[string]string) ntable.BatchMemberEntry {
	return ntable.BatchMemberEntry{ID: id, Set: fields}
}

// checkLines is the CHECK lines of check cards' output, the kind and the subject of each.
func checkLines(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) >= 3 && f[0] == "CHECK" && f[1] != "CARDS" {
			got = append(got, f[1]+" "+f[2])
		}
	}
	return got
}

// seedClean is a clean epoch of two streams: s1 and s2 on their own branches with readable
// display names, s1-stop s1's sentinel, a2 sharing a1's file and needing it.
func (ta *testApp) seedClean() {
	ta.seed([]string{"s1", "s2"}, map[string]map[string]string{
		"s1": {"kind": "stream", sprint.FieldDisplay: "The first stream", sprint.FieldStreamBranch: "sprint/s1", sprint.FieldTrunk: "dev"},
		"s2": {"kind": "stream", sprint.FieldDisplay: "The second stream", sprint.FieldStreamBranch: "sprint/s2", sprint.FieldTrunk: "dev"},
	}, []ntable.BatchMemberEntry{
		seedCard("a1", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a1", "sprint/s1", "internal/a.go")}),
		seedCard("a2", "s1", sprint.Waiting, map[string]string{"brief": lintedBrief("a2", "sprint/s1", "internal/a.go"), "needs": "a1"}),
		seedCard("s1-stop", "s1", sprint.Waiting, map[string]string{"kind": "sentinel"}),
		seedCard("b1", "s2", sprint.Ready, map[string]string{"brief": lintedBrief("b1", "sprint/s2", "internal/b.go")}),
	})
}

func TestStartRefusesUntilACleanCardCheckMatchesTheCards(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)

	// a start after a clear: no record for the epoch
	code, out, errs := ta.do("start")
	require.Equal(t, 1, code, "refused: %s%s", out, errs)
	assert.Contains(t, errs, "run nova-sprint check cards first: no check record for epoch ")
	assert.Contains(t, errs, "nothing was written")
	assert.False(t, ta.machineRunning(), "nothing was written")

	// one defect of each core kind, written as add never would
	ta.seedClean()
	ta.seed([]string{"s3"}, map[string]map[string]string{
		"s3": {"kind": "stream", sprint.FieldDisplay: "s3", sprint.FieldStreamBranch: "sprint/s3"},
	}, []ntable.BatchMemberEntry{
		seedCard("a3", "s1", sprint.Waiting, map[string]string{"brief": lintedBrief("a3", "sprint/s1", "internal/a3.go"), "needs": "gone"}),
		seedCard("a4", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a4", "sprint/s1", "internal/x/one.go")}),
		seedCard("b2", "s2", sprint.Ready, map[string]string{"brief": lintedBrief("b2", "sprint/s2", "internal/x/**")}),
		seedCard("a5", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a5", "sprint/s1", "internal/c.go")}),
		seedCard("a6", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a6", "sprint/s1", "internal/c.go")}),
		seedCard("b3", "s2", sprint.Waiting, map[string]string{"brief": lintedBrief("b3", "sprint/s2", "internal/b3.go"), "needs": "a1"}),
		seedCard("c1", "s3", sprint.Ready, map[string]string{"brief": lintedBrief("c1", "sprint/s3", "internal/c1.go")}),
		seedCard("a7", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a7", "sprint/s1", "internal/a7.go"), sprint.FieldPriority: sprint.PriorityFix}),
		seedCard("a8", "s1", sprint.Ready, map[string]string{"brief": "RESULT: a8 sha=0123456789ab tier: pro\nPATHS: internal/a8.go\nTHE TASK. Fix the empty case."}),
		// a brief under the store's field bound whose record, with the findings it carries, is
		// over the drain's
		seedCard("a9", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a9", "sprint/s1", "internal/a9.go") + "\n" + strings.Repeat("x", 10000),
			sprint.FieldFindings: strings.Repeat("attempt 1: the gate was red. ", 250)}),
	})
	code, out, errs = ta.do("check cards")
	require.Equal(t, 1, code, "findings: %s%s", out, errs)
	assert.Equal(t, []string{"needs a3", "files s1", "same-file a5", "bases b3", "names s3", "stale-fix a7", "lint a8", "brief-size a9"}, checkLines(out), out)
	kinds := "needs:1,files:1,same-file:1,bases:1,names:1,stale-fix:1,lint:1,brief-size:1"
	assert.Contains(t, out, "CHECK CARDS FAILED kinds="+kinds)
	code, out, errs = ta.do("start")
	require.Equal(t, 1, code, "refused: %s%s", out, errs)
	assert.Contains(t, errs, "run nova-sprint check cards: the last check found "+kinds)
	assert.False(t, ta.machineRunning())

	// fixing each clears its finding, and no other
	fixes := []struct {
		kind string
		fix  func()
	}{
		{"needs", func() { ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("a3", map[string]string{"needs": ""})}) }},
		{"files", func() {
			ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("b2", map[string]string{"brief": lintedBrief("b2", "sprint/s2", "internal/y/**")})})
		}},
		{"same-file", func() { ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("a6", map[string]string{"needs": "a5"})}) }},
		{"bases", func() {
			ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("b3", map[string]string{"needs": "s1-stop"})})
		}},
		{"names", func() { ta.ok("stream set s3 --display 'The third stream'") }},
		{"stale-fix", func() { ta.ok("priority a7 --high --reason 'the fix level was carried by the clear'") }},
		{"lint", func() {
			ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("a8", map[string]string{"brief": lintedBrief("a8", "sprint/s1", "internal/a8.go")})})
		}},
		{"brief-size", func() {
			ta.seed(nil, nil, []ntable.BatchMemberEntry{edit("a9", map[string]string{"brief": lintedBrief("a9", "sprint/s1", "internal/a9.go"),
				sprint.FieldFindings: "attempt 1: the gate was red."})})
		}},
	}
	for i, f := range fixes {
		f.fix()
		code, out, errs = ta.do("check cards")
		left := checkLines(out)
		require.Len(t, left, len(fixes)-i-1, "fixing %s clears its finding: %s%s", f.kind, out, errs)
		for _, l := range left {
			assert.NotContains(t, l, f.kind+" ", "fixed %s: %s", f.kind, out)
		}
	}
	require.Equal(t, 0, code, "a clean epoch: %s%s", out, errs)
	assert.Contains(t, out, "CHECK CARDS OK cards=")
	ta.ok("start")
	assert.True(t, ta.machineRunning(), "a clean check of the cards now starts the machine")
	ta.ok("stop --reason 'the card changes below' --until 30m")

	// an admit, a move, a brief edit, a priority change and a needs edit after a clean check
	// each make start refuse, naming the card changed
	dir := t.TempDir()
	n1 := filepath.Join(dir, "n1.md")
	require.NoError(t, os.WriteFile(n1, []byte(passingBrief("RESULT: n1 sha=0123456789ab tier: pro\nPATHS: internal/n1.go\nTHE TASK. Do the new work.")), 0o600))
	n1b := filepath.Join(dir, "n1b.md")
	require.NoError(t, os.WriteFile(n1b, []byte(passingBrief("RESULT: n1 sha=0123456789ab tier: pro\nPATHS: internal/n1.go\nTHE TASK. Do the new work, edited.")), 0o600))
	for _, c := range []struct {
		name, change, want string
	}{
		{"an admit", "add --stream s2 --brief-file " + n1 + " --one", "n1 added"},
		{"a move", "move n1 --stream s3", "n1 changed: stream (a move)"},
		{"a brief edit", "brief n1 --brief-file " + n1b, "n1 changed: brief"},
		{"a priority change", "priority n1 --high --reason 'next up'", "n1 changed: priority"},
		{"a needs edit", "needs set n1 --needs c1", "n1 changed: needs"},
	} {
		ta.ok(c.change)
		code, out, errs = ta.do("start")
		require.Equal(t, 1, code, "%s after a clean check: %s%s", c.name, out, errs)
		assert.Contains(t, errs, "run nova-sprint check cards: the cards changed since the check ("+c.want+")", c.name)
		assert.False(t, ta.machineRunning(), c.name)
		ta.ok("check cards")
	}
	ta.ok("start")
	assert.True(t, ta.machineRunning())

	// a start on a RUNNING machine changes nothing, whatever changed since the check
	ta.ok("priority n1 --normal --reason 'back in line'")
	out = ta.ok("start")
	assert.Contains(t, out, "unchanged: the machine is RUNNING already")
}

// A --stream check covers those streams alone, so its record never opens the start gate.
func TestAStreamLimitedCardCheckNeverOpensTheStart(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	out := ta.ok("check cards --stream s1")
	assert.Contains(t, out, "CHECK CARDS OK cards=")
	code, _, errs := ta.do("start")
	require.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "run nova-sprint check cards: the last check covered stream(s) s1 only")
	ta.ok("check cards")
	ta.ok("start")
}

// The record is written however the check ends, as JSON too; check (the invariants) is unchanged.
func TestCheckCardsJSONAndTheInvariantCheckApart(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	var got struct {
		Clean    bool                 `json:"clean"`
		Cards    int                  `json:"cards"`
		Findings []sprint.CardFinding `json:"findings"`
	}
	ta.json("check cards", &got)
	assert.True(t, got.Clean)
	assert.Equal(t, 4, got.Cards)
	assert.Empty(t, got.Findings)
	out := ta.ok("check")
	assert.NotContains(t, out, "CHECK CARDS", "check is the store's invariants, not the card check")
}

// newApp's gate is on: no flag, variable or setting turns it off (a class test).
func TestNewAppGatesStartOnTheCardCheck(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	assert.True(t, a.cardCheckGate, "the production app gates start on a clean card check")
	assert.Equal(t, classCoordinator, verbClasses["check cards"])
	assert.NotEmpty(t, verbExit["check cards"])
	assert.NotEmpty(t, verbEffect["check cards"])
}

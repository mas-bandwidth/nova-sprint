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
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
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
	ta.m.SetProviders(map[string]sprint.Provider{"fixture": {Name: "fixture", Billing: "plan", Funded: false}})
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
		// a brief under the drain's bound whose record, with the findings it carries, is over
		// it: no finding (v1.2.18: brief-size measures the brief the drain bounds, never the
		// record; a brief over the bound is one the store refuses, so its refusal is the unit
		// test's, TestBriefSizeMeasuresTheBriefTheDrainBounds)
		seedCard("a9", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a9", "sprint/s1", "internal/a9.go") + "\n" + strings.Repeat("x", 10000),
			sprint.FieldFindings: strings.Repeat("attempt 1: the gate was red. ", 250)}),
	})
	code, out, errs = ta.do("check cards")
	require.Equal(t, 1, code, "findings: %s%s", out, errs)
	assert.Equal(t, []string{"needs a3", "files s1", "same-file a5", "bases b3", "names s3", "stale-fix a7", "lint a8"}, checkLines(out), out)
	// a shared file is a warning: printed, failing nothing, ordering nothing
	kinds := "needs:1,bases:1,names:1,stale-fix:1,lint:1"
	assert.Contains(t, out, "CHECK CARDS FAILED kinds="+kinds+" warned=files:1,same-file:1")
	assert.Contains(t, out, "CHECK files s1 internal/x/one.go is written by streams s1 and s2 WARN serialized=work it out in the merge")
	assert.Contains(t, out, "CHECK same-file a5 internal/c.go is named in PATHS by a5 and a6 WARN serialized=work it out in the merge")
	assert.NotContains(t, out, "brief-size", "a record over the bound with a brief under it is no finding")
	code, out, errs = ta.do("start")
	require.Equal(t, 1, code, "refused: %s%s", out, errs)
	// the live needs gate comes first (a-missing-or-blocked-need-is-found-at-check-and-while-
	// running), then the record's
	assert.Contains(t, errs, "run nova-sprint check cards: a3 needs gone (absent)")
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
	}
	for i, f := range fixes {
		f.fix()
		if i == 0 {
			// the needs resolved, the failed record still refuses
			code, out, errs = ta.do("start")
			require.Equal(t, 1, code, "refused: %s%s", out, errs)
			assert.Contains(t, errs, "run nova-sprint check cards: the last check found "+kinds)
		}
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

// A shared file warns and does not fail the check, ordered or not, and start proceeds.
// A bad base still fails. The words name no wait path.
func TestStartProceedsOverWarningsAndRefusesOverAnUnorderedOverlap(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	ta.seed(nil, nil, []ntable.BatchMemberEntry{
		// same stream: a5 and a6 name internal/c.go with no need between them
		seedCard("a5", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a5", "sprint/s1", "internal/c.go")}),
		seedCard("a6", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a6", "sprint/s1", "internal/c.go")}),
		// across streams: b2 writes internal/x/** and waits for s1's stop, which needs a4
		seedCard("a4", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a4", "sprint/s1", "internal/x/one.go")}),
		edit("s1-stop", map[string]string{"needs": "a4"}),
		seedCard("b2", "s2", sprint.Waiting, map[string]string{"brief": lintedBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"}),
	})
	code, out, errs := ta.do("check cards")
	require.Equal(t, 0, code, "warnings alone are clean: %s%s", out, errs)
	assert.Equal(t, []string{"files s1", "same-file a5"}, checkLines(out), out)
	assert.Contains(t, out, "CHECK files s1 internal/x/one.go is written by streams s1 and s2 WARN serialized=work it out in the merge remedy=")
	assert.Contains(t, out, "CHECK same-file a5 internal/c.go is named in PATHS by a5 and a6 WARN serialized=work it out in the merge")
	assert.Contains(t, out, "CHECK CARDS OK cards=8 warned=files:1,same-file:1")

	var got struct {
		Clean    bool                 `json:"clean"`
		Kinds    string               `json:"kinds"`
		Warned   string               `json:"warned"`
		Findings []sprint.CardFinding `json:"findings"`
	}
	ta.json("check cards", &got)
	assert.True(t, got.Clean)
	assert.Empty(t, got.Kinds)
	assert.Equal(t, "files:1,same-file:1", got.Warned)
	require.Len(t, got.Findings, 2)
	for _, f := range got.Findings {
		assert.Equal(t, sprint.SeverityWarn, f.Severity, "%+v", f)
		assert.NotEmpty(t, f.Serialized, "%+v", f)
	}

	ta.ok("start")
	assert.True(t, ta.machineRunning(), "a check with warnings only starts the machine")
	ta.ok("stop --reason 'the overlap below' --until 30m")

	// a12 in s1 and b3 in s2 write internal/x/two.go with no need between them: still a
	// warning, and start proceeds. A stream on the trunk still fails.
	ta.seed(nil, nil, []ntable.BatchMemberEntry{
		seedCard("b3", "s2", sprint.Ready, map[string]string{"brief": lintedBrief("b3", "sprint/s2", "internal/x/two.go")}),
		seedCard("a12", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a12", "sprint/s1", "internal/x/two.go")}),
	})
	code, out, errs = ta.do("check cards")
	require.Equal(t, 0, code, "a shared file warns: %s%s", out, errs)
	assert.Contains(t, out, "CHECK files s1 internal/x/two.go is written by streams s1 and s2 WARN serialized=work it out in the merge")
	assert.Contains(t, out, "CHECK CARDS OK")
	assert.NotContains(t, out, "FAILED")
	ta.ok("start")
	assert.True(t, ta.machineRunning(), "a shared file does not stop the sprint")
}

// --dry-run runs the card check and prints its findings (text and --json), leaving the epoch's
// check record untouched: an unchecked epoch stays unchecked, and a clean record stays clean
// when a dry-run --stream check inspects one stream.
func TestCheckCardsDryRunWritesNoRecord(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()

	code, out, errs := ta.do("check cards --dry-run")
	require.Equal(t, 0, code, "dry-run clean: %s%s", out, errs)
	assert.Contains(t, out, "CHECK CARDS OK cards=4")
	assert.Equal(t, "no", ta.a.changed)

	var got struct {
		Clean  bool `json:"clean"`
		DryRun bool `json:"dry_run"`
		Cards  int  `json:"cards"`
	}
	ta.json("check cards --dry-run", &got)
	assert.True(t, got.Clean)
	assert.True(t, got.DryRun)
	assert.Equal(t, 4, got.Cards)

	// no check record was written, so start still refuses
	code, _, errs = ta.do("start")
	require.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "run nova-sprint check cards first: no check record for epoch ")

	// write a clean check record, then run a stream-limited --dry-run: the clean full record
	// is not overwritten, so start proceeds
	ta.ok("check cards")
	ta.ok("check cards --dry-run --stream s1")
	ta.ok("start")
	assert.True(t, ta.machineRunning())
}

// --from <export> runs the card check over an exported card set (card <id> --json lines,
// card --all --json with briefs/ and trees/, or a backup --file snapshot) with no live store,
// no --redis or --actor, and no check record written.
func TestCheckCardsFromExportRunsWithoutAStore(t *testing.T) {
	t.Parallel()
	ta := gateOn(t)
	ta.seedClean()
	ta.seed(nil, nil, []ntable.BatchMemberEntry{
		seedCard("a4", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a4", "sprint/s1", "internal/x/one.go")}),
		edit("s1-stop", map[string]string{"needs": "a4"}),
		seedCard("b2", "s2", sprint.Waiting, map[string]string{"brief": lintedBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"}),
	})

	// 1. card <id> --json JSONL export (one cardView per line)
	dir := t.TempDir()
	var jsonl strings.Builder
	for _, id := range []string{"a1", "a2", "a4", "s1-stop", "b1", "b2"} {
		jsonl.WriteString(ta.ok("card " + id + " --json"))
	}
	liveCards := filepath.Join(dir, "live-cards.jsonl")
	require.NoError(t, os.WriteFile(liveCards, []byte(jsonl.String()), 0o600))
	treesDir := filepath.Join(dir, "trees")
	require.NoError(t, os.MkdirAll(treesDir, 0o755))
	treeText := strings.Join(checkTreeFiles, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(treesDir, "owner__repo__sprint__s1.txt"), []byte(treeText), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(treesDir, "owner__repo__sprint__s2.txt"), []byte(treeText), 0o600))

	offline := newTestApp(t)
	offline.live = nil
	offline.a.getenv = func(string) string { return "" }
	offline.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		t.Fatal("check cards --from opened a store")
		return nil, nil
	}
	code, out, errs := offline.do("check cards --from " + liveCards)
	require.Equal(t, 0, code, "offline --from file: %s%s", out, errs)
	assert.Contains(t, out, "CHECK files s1 internal/x/one.go is written by streams s1 and s2 WARN serialized=work it out in the merge")
	assert.Contains(t, out, "CHECK CARDS OK cards=6 warned=files:1")
	assert.Equal(t, "no", offline.a.changed)

	// 2. card --all --json directory export with briefs/<id>.md and trees/
	saDir := filepath.Join(dir, "sa")
	require.NoError(t, os.MkdirAll(filepath.Join(saDir, "briefs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(saDir, "trees"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(saDir, "all.jsonl"), []byte(ta.ok("card --all --json")), 0o600))
	for _, id := range []string{"a1", "a2", "a4", "b1", "b2"} {
		require.NoError(t, os.WriteFile(filepath.Join(saDir, "briefs", id+".md"), []byte(ta.ok("card "+id+" --brief")), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(saDir, "trees", "owner__repo__sprint__s1.txt"), []byte(treeText), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(saDir, "trees", "owner__repo__sprint__s2.txt"), []byte(treeText), 0o600))

	code, out, errs = offline.do("check cards --from " + saDir + " --json")
	require.Equal(t, 0, code, "offline --from dir: %s%s", out, errs)
	assert.Contains(t, out, `"clean":true`)
	assert.Contains(t, out, `"dry_run":true`)
	assert.Contains(t, out, `"warned":"files:1"`)

	// 3. a shared file in the export warns and exits 0
	ta.seed(nil, nil, []ntable.BatchMemberEntry{
		seedCard("a12", "s1", sprint.Ready, map[string]string{"brief": lintedBrief("a12", "sprint/s1", "internal/x/two.go")}),
		seedCard("b3", "s2", sprint.Ready, map[string]string{"brief": lintedBrief("b3", "sprint/s2", "internal/x/two.go")}),
	})
	backupFile := filepath.Join(dir, "sprint.backup")
	ta.ok("backup --file " + backupFile)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trees", "owner__repo__sprint__s1.txt"), []byte(treeText), 0o600))
	code, out, errs = offline.do("check cards --from " + backupFile)
	require.Equal(t, 0, code, "offline --from backup with a shared file: %s%s", out, errs)
	assert.Contains(t, out, "CHECK files s1 internal/x/two.go is written by streams s1 and s2 WARN serialized=work it out in the merge")
	assert.Contains(t, out, "CHECK CARDS OK")
	assert.NotContains(t, out, "FAILED")
}

package sprint

import (
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The core pre-start card check (start-refuses-until-a-clean-check; docs/SPEC-SPRINT.md section
// 14, "The card check"): one fixture epoch with one defect of each core kind gives exactly those
// findings, and fixing each clears its finding and no other. The trees are handed in, the lint is
// a fake, and no git, clock, socket or store is read (tla/MachineReady.tla Check).

// checkBrief is a brief with a header naming the card's repository, base and PATHS.
func checkBrief(id, base, paths string) string {
	return checkBriefRepo(id, "owner/repo", base, paths)
}

// checkBriefRepo is checkBrief with the repository named.
func checkBriefRepo(id, repo, base, paths string) string {
	return "RESULT: " + id + " sha=0123456789ab tier: pro\nREPO: " + repo + "\nBASE: " + base + "\nPATHS: " + paths + "\n\nTHE TASK. Do the work.\n"
}

// cardCheckFixture is a clean epoch of three streams: s1 and s2 on their own branches, each with
// a readable display name, and s1-stop, s1's sentinel. a2 shares a1's file and needs it.
func cardCheckFixture() *Snapshot {
	s := &Snapshot{Now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator", Providers: map[string]Provider{"fixture": {Name: "fixture", Billing: "plan", Funded: false}}}
	s.Work.SetRows([]string{"s1", "s2"})
	s.Merge.SetRows([]string{"s1", "s2"})
	for _, st := range []struct{ id, display string }{{"s1", "The first stream"}, {"s2", "The second stream"}} {
		s.Merge.Put(&Card{ID: CtlID(st.id), Row: st.id, Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: st.display, FieldStreamBranch: "sprint/" + st.id, FieldTrunk: "dev"}})
	}
	put := func(id, stream, col string, fields map[string]string) {
		f := map[string]string{"kind": "primary", "stream": stream, "attempt": "0"}
		maps.Copy(f, fields)
		s.Work.Put(&Card{ID: id, Row: stream, Col: col, Fields: f})
	}
	put("a1", "s1", Ready, map[string]string{"brief": checkBrief("a1", "sprint/s1", "internal/a.go")})
	put("a2", "s1", Waiting, map[string]string{"brief": checkBrief("a2", "sprint/s1", "internal/a.go"), "needs": "a1"})
	put("s1-stop", "s1", Waiting, map[string]string{"kind": "sentinel"})
	put("b1", "s2", Ready, map[string]string{"brief": checkBrief("b1", "sprint/s2", "internal/b.go")})
	return s
}

// cardCheckIn is the check's input for the fixture: both streams' base trees, a lint that finds
// a brief holding LINTFAIL, and a store bound over the drain's.
func cardCheckIn() CardCheckIn {
	files := []string{"internal/a.go", "internal/b.go", "internal/c.go", "internal/x/one.go", "internal/x/two.go", "internal/y/one.go", "internal/a_test.go"}
	return CardCheckIn{
		Trees: map[CardTreeKey]AdmissionTree{
			{Repo: "owner/repo", Base: "sprint/s1"}: {Files: files},
			{Repo: "owner/repo", Base: "sprint/s2"}: {Files: files},
		},
		Lint: func(brief string) []string {
			if strings.Contains(brief, "LINTFAIL") {
				return []string{"rule-worktree: the brief names no worktree rule"}
			}
			return nil
		},
		MaxBriefBytes: 1 << 20,
	}
}

func TestCardCheckFindsEachCoreKind(t *testing.T) {
	t.Parallel()
	require.Empty(t, CheckCards(cardCheckFixture(), cardCheckIn()), "the fixture epoch is clean")

	put := func(s *Snapshot, id, stream, col string, fields map[string]string) {
		f := map[string]string{"kind": "primary", "stream": stream, "attempt": "0"}
		maps.Copy(f, fields)
		s.Work.Put(&Card{ID: id, Row: stream, Col: col, Fields: f})
	}
	set := func(s *Snapshot, id string, fields map[string]string) {
		c := *s.Work.Card(id)
		c.Fields = maps.Clone(c.Fields)
		maps.Copy(c.Fields, fields)
		s.Work.Put(&c)
	}
	defective := func() *Snapshot {
		s := cardCheckFixture()
		// 1 needs: a need that names no card of this epoch
		put(s, "a3", "s1", Waiting, map[string]string{"brief": checkBrief("a3", "sprint/s1", "internal/a3.go"), "needs": "gone"})
		// 3 files: s2 writes internal/x/** (expanded at its base), s1 writes internal/x/one.go
		put(s, "a4", "s1", Ready, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
		put(s, "b2", "s2", Ready, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**")})
		// 5 same-file: two s1 cards name internal/c.go and neither needs the other
		put(s, "a5", "s1", Ready, map[string]string{"brief": checkBrief("a5", "sprint/s1", "internal/c.go")})
		put(s, "a6", "s1", Ready, map[string]string{"brief": checkBrief("a6", "sprint/s1", "internal/c.go")})
		// 6 bases: an s2 card needs a primary of s1, which lands on sprint/s1, not sprint/s2
		put(s, "b3", "s2", Waiting, map[string]string{"brief": checkBrief("b3", "sprint/s2", "internal/b3.go"), "needs": "a1"})
		// 7 names: s3's display name is its id
		s.Work.SetRows([]string{"s1", "s2", "s3"})
		s.Merge.SetRows([]string{"s1", "s2", "s3"})
		s.Merge.Put(&Card{ID: CtlID("s3"), Row: "s3", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "s3", FieldStreamBranch: "sprint/s3"}})
		put(s, "c1", "s3", Ready, map[string]string{"brief": checkBrief("c1", "sprint/s3", "internal/c1.go")})
		// 8 stale fix: priority fix and no attempt this epoch
		put(s, "a7", "s1", Ready, map[string]string{"brief": checkBrief("a7", "sprint/s1", "internal/a7.go"), FieldPriority: PriorityFix})
		// 9 lint: a brief the card lint refuses
		put(s, "a8", "s1", Ready, map[string]string{"brief": checkBrief("a8", "sprint/s1", "internal/a8.go") + "LINTFAIL\n"})
		// 16 brief size: a brief over the drain's 16 KB
		put(s, "a9", "s1", Ready, map[string]string{"brief": checkBrief("a9", "sprint/s1", "internal/a9.go") + strings.Repeat("x", DrainBriefBytes)})
		return s
	}
	got := CheckCards(defective(), cardCheckIn())
	var lines []string
	for _, f := range got {
		lines = append(lines, f.Kind+" "+f.Subject)
	}
	require.Equal(t, []string{
		"needs a3", "files s1", "same-file a5", "bases b3", "names s3", "stale-fix a7", "lint a8", "brief-size a9",
	}, lines, "exactly one finding of each core kind, in kind order: %+v", got)
	assert.Equal(t, "needs:1,files:1,same-file:1,bases:1,names:1,stale-fix:1,lint:1,brief-size:1", CardCheckKinds(got))
	assert.Equal(t, "CHECK needs a3 gone absent: gone is no primary on the table or in this add remedy=add gone, or remove the need if gone landed before this epoch: nova-sprint needs set a3 --needs <the needs without gone>", got[0].Line())
	assert.Contains(t, got[1].Finding, "internal/x/one.go is written by streams s1 and s2", "the glob is expanded at the base tree")
	assert.Contains(t, got[2].Finding, "internal/c.go is named in PATHS by a5 and a6")
	assert.Contains(t, got[3].Finding, "needs a1 of stream s1, which lands on sprint/s1, not on sprint/s2")
	assert.Contains(t, got[4].Finding, "is displayed as its id")
	assert.Contains(t, got[7].Finding, "over the 16384 bytes a brief may be")

	for _, fix := range []struct {
		kind string
		fix  func(s *Snapshot)
	}{
		{CheckNeeds, func(s *Snapshot) { set(s, "a3", map[string]string{"needs": ""}) }},
		{CheckFiles, func(s *Snapshot) {
			set(s, "b2", map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/y/**")})
		}},
		{CheckSameFile, func(s *Snapshot) { set(s, "a6", map[string]string{"needs": "a5"}) }},
		{CheckBases, func(s *Snapshot) { set(s, "b3", map[string]string{"needs": "s1-stop"}) }},
		{CheckNames, func(s *Snapshot) {
			s.Merge.Put(&Card{ID: CtlID("s3"), Row: "s3", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "The third stream", FieldStreamBranch: "sprint/s3"}})
		}},
		{CheckStaleFix, func(s *Snapshot) { set(s, "a7", map[string]string{FieldPriority: PriorityHigh}) }},
		{CheckLint, func(s *Snapshot) {
			set(s, "a8", map[string]string{"brief": checkBrief("a8", "sprint/s1", "internal/a8.go")})
		}},
		{CheckBriefSize, func(s *Snapshot) {
			set(s, "a9", map[string]string{"brief": checkBrief("a9", "sprint/s1", "internal/a9.go")})
		}},
	} {
		s := defective()
		fix.fix(s)
		after := CheckCards(s, cardCheckIn())
		require.Len(t, after, 7, "fixing %s clears its finding and no other: %+v", fix.kind, after)
		for _, f := range after {
			assert.NotEqual(t, fix.kind, f.Kind, "fixed: %+v", f)
		}
	}

	t.Run("a cycle and a self-need are needs findings; a waived need is none", func(t *testing.T) {
		s := cardCheckFixture()
		set(s, "a1", map[string]string{"needs": "a2"}) // a2 needs a1: a cycle
		put(s, "a3", "s1", Ready, map[string]string{"brief": checkBrief("a3", "sprint/s1", "internal/a3.go"), "needs": "a3"})
		put(s, "a4", "s1", Ready, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/a4.go"), "needs": "gone", "waived": "gone"})
		var needs []string
		for _, f := range CheckCards(s, cardCheckIn()) {
			if f.Kind == CheckNeeds {
				needs = append(needs, f.Subject+": "+f.Finding)
			}
		}
		require.Len(t, needs, 3, "%v", needs)
		assert.Contains(t, needs[0], "a1: a2 cycle: the need closes a cycle: ")
		assert.Contains(t, needs[1], "a2: a1 cycle: the need closes a cycle: ")
		assert.Equal(t, "a3: a3 cycle: a3 is the card itself", needs[2])
	})

	t.Run("a stream on the trunk is a bases finding; the promotion stream is not", func(t *testing.T) {
		s := cardCheckFixture()
		s.Merge.Put(&Card{ID: CtlID("s2"), Row: "s2", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "The second stream", FieldStreamBranch: "dev"}})
		got := CheckCards(s, cardCheckIn())
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, "CHECK bases s2 lands on the trunk dev, not on a branch of its own remedy=nova-sprint stream set s2 --base sprint/s2", got[0].Line())
		s.Merge.Put(&Card{ID: CtlID("s2"), Row: "s2", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "The second stream", FieldStreamBranch: "main", FieldLandProtected: "any"}})
		assert.Empty(t, CheckCards(s, cardCheckIn()))
	})

	t.Run("--stream limits the check to the streams named", func(t *testing.T) {
		in := cardCheckIn()
		in.Streams = []string{"s3"}
		got := CheckCards(defective(), in)
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, "names s3", got[0].Kind+" "+got[0].Subject)
	})
}

// The check record's digest is over every card not dropped; the start gate names the first card
// added, removed or changed since the check, and which field.
func TestCardsChangeNamesTheFirstChange(t *testing.T) {
	t.Parallel()
	s := cardCheckFixture()
	was := CheckedCardsOf(s)
	require.Len(t, was, 4)
	assert.Equal(t, CardsDigest(was), CardsDigest(CheckedCardsOf(cardCheckFixture())), "the same cards, the same digest")
	assert.Empty(t, CardsChange(was, CheckedCardsOf(cardCheckFixture())))

	for _, c := range []struct {
		name   string
		change func(s *Snapshot)
		want   string
	}{
		{"an admit", func(s *Snapshot) {
			s.Work.Put(&Card{ID: "a0", Row: "s1", Col: Ready, Fields: map[string]string{"kind": "primary"}})
		}, "a0 added"},
		{"a drop", func(s *Snapshot) {
			c := *s.Work.Card("b1")
			c.Col = ""
			s.Work.Put(&c)
		}, "b1 removed"},
		{"a move", func(s *Snapshot) {
			c := *s.Work.Card("a2")
			c.Row = "s2"
			s.Work.Put(&c)
		}, "a2 changed: stream (a move)"},
		{"a brief edit", func(s *Snapshot) { setField(s, "a1", "brief", "another brief") }, "a1 changed: brief"},
		{"a needs edit", func(s *Snapshot) { setField(s, "a2", "needs", "") }, "a2 changed: needs"},
		{"a priority change", func(s *Snapshot) { setField(s, "b1", FieldPriority, PriorityHigh) }, "b1 changed: priority"},
		{"a tier change", func(s *Snapshot) { setField(s, "b1", FieldTier, "heavy") }, "b1 changed: tier"},
	} {
		now := cardCheckFixture()
		c.change(now)
		cards := CheckedCardsOf(now)
		assert.NotEqual(t, CardsDigest(was), CardsDigest(cards), c.name)
		assert.Equal(t, c.want, CardsChange(was, cards), c.name)
	}
}

// check cards' kind needs is every unresolved need (a-missing-or-blocked-need-is-found-at-check-
// and-while-running; tla/NeedsBlocked.tla Unresolved): one card per reason gives exactly five
// findings, each with its remedy; a landed need and a waived need give none.
func TestCheckCardsNamesEachUnresolvedNeed(t *testing.T) {
	t.Parallel()
	s := cardCheckFixture()
	put := func(id, stream, col string, fields map[string]string) {
		f := map[string]string{"kind": "primary", "stream": stream, "attempt": "0", "brief": checkBrief(id, "sprint/"+stream, "internal/"+id+".go")}
		maps.Copy(f, fields)
		s.Work.Put(&Card{ID: id, Row: stream, Col: col, Fields: f})
	}
	// the needs: one landed, one dropped, one in a stream with no control card, one held
	put("landed1", "s1", Landed, nil)
	put("dropped1", "s1", "", map[string]string{"outcome": "dropped: obsolete"})
	s.Work.SetRows([]string{"s1", "s2", "ghost"})
	put("ghost1", "ghost", Ready, nil)
	put("held1", "s1", Ready, nil)
	s.Open = []Open{{Key: OpenKey("n7", "held1"), Note: Note{ID: "n7", Alias: "j7", Kind: Judgment, Type: NBound, Stream: "s1",
		Primaries: []string{"held1"}, At: s.Now.Add(-3 * time.Hour), Decisions: []string{"rework with a fix", "drop"}}}}
	// one card per reason, and two that need nothing unresolved
	put("w-absent", "s1", Waiting, map[string]string{"needs": "gone"})
	put("w-dropped", "s1", Waiting, map[string]string{"needs": "dropped1"})
	put("w-unknown", "s1", Waiting, map[string]string{"needs": "ghost1"})
	put("w-held", "s1", Waiting, map[string]string{"needs": "held1"})
	put("w-cycle", "s1", Waiting, map[string]string{"needs": "w-cycle"})
	put("w-landed", "s1", Ready, map[string]string{"needs": "landed1"})
	put("w-waived", "s1", Ready, map[string]string{"needs": "dropped1", "waived": "dropped1"})

	var got []string
	for _, f := range CheckCards(s, CardCheckIn{Streams: []string{"s1"}}) {
		if f.Kind == CheckNeeds {
			got = append(got, f.Line())
		}
	}
	require.Len(t, got, 5, "absent, cycle, dropped, held, unknown stream: %v", got)
	assert.Equal(t, "CHECK needs w-absent gone absent: gone is no primary on the table or in this add remedy=add gone, or remove the need if gone landed before this epoch: nova-sprint needs set w-absent --needs <the needs without gone>", got[0])
	assert.Equal(t, "CHECK needs w-cycle w-cycle cycle: w-cycle is the card itself remedy=nova-sprint needs set w-cycle --needs <the needs without w-cycle>", got[1])
	assert.Equal(t, "CHECK needs w-dropped dropped1 dropped: dropped1, which was dropped: obsolete remedy=nova-sprint needs set w-dropped --needs <the needs without dropped1> or nova-sprint relink dropped1 <its twin>", got[2])
	assert.Equal(t, "CHECK needs w-held held1 held: held1 is held by judgment j7 ("+NBound+", open 3h0m0s) until it is decided remedy=nova-sprint rework held1 --fix <text> or nova-sprint drop held1 --reason <text> (then relink or drop what needs it)", got[3])
	assert.Equal(t, "CHECK needs w-unknown ghost1 unknown stream: ghost1's stream ghost has no control card in this epoch remedy=nova-sprint move ghost1 --stream <a stream of this epoch> or nova-sprint needs set w-unknown --needs <the needs without ghost1>", got[4])

	u := TableUnresolved(s)
	assert.NotContains(t, u, "w-landed", "a landed need is resolved")
	assert.NotContains(t, u, "w-waived", "a waived need is no need")
	reasons := map[string]bool{}
	for _, us := range u {
		for _, x := range us {
			reasons[x.Reason] = true
		}
	}
	assert.Equal(t, map[string]bool{UnresolvedAbsent: true, UnresolvedDropped: true, UnresolvedUnknownStream: true, UnresolvedHeld: true, UnresolvedCycle: true}, reasons, "exactly one reason each, the five")
}

func setField(s *Snapshot, id, name, value string) {
	c := *s.Work.Card(id)
	c.Fields = maps.Clone(c.Fields)
	c.Fields[name] = value
	s.Work.Put(&c)
}

// putCheckCard adds one ready primary on stream, in repo, writing paths.
func putCheckCard(s *Snapshot, id, stream, repo, paths string) {
	s.Work.Put(&Card{ID: id, Row: stream, Col: Ready, Fields: map[string]string{
		"kind": "primary", "stream": stream, "attempt": "0",
		"brief": checkBriefRepo(id, repo, "sprint/"+stream, paths),
	}})
}

func TestCardCheckSamePathInDifferentReposIsNoFinding(t *testing.T) {
	t.Parallel()
	const path = ".github/workflows/ci.yml"
	s := cardCheckFixture()
	putCheckCard(s, "a4", "s1", "robotgamedevelopers/delta", path)
	putCheckCard(s, "b2", "s2", "mas-bandwidth/rocketnet", path)
	require.Empty(t, CheckCards(s, cardCheckIn()), "the same path in two repositories is two files")

	s = cardCheckFixture()
	putCheckCard(s, "a4", "s1", "mas-bandwidth/rocketnet", path)
	putCheckCard(s, "b2", "s2", "mas-bandwidth/rocketnet", path)
	got := CheckCards(s, cardCheckIn())
	files := findingsOfKind(got, CheckFiles)
	require.Len(t, files, 1, "%+v", got)
	assert.Contains(t, files[0].Finding, path+" is written by streams s1 and s2")
	assert.Equal(t, SeverityWarn, files[0].Severity)
	assert.False(t, files[0].Refuses())
	assert.Equal(t, overlapWarn, files[0].Serialized)
	assert.Empty(t, RefusingFindings(got), "a shared file is a warning and the check would exit 0")

	s = cardCheckFixture()
	putCheckCard(s, "a4", "s1", "robotgamedevelopers/delta", path)
	putCheckCard(s, "b2", "s2", "mas-bandwidth/rocketnet", ".github/workflows/**")
	require.Empty(t, findingsOfKind(CheckCards(s, cardCheckIn()), CheckFiles), "a glob in another repository does not cover the file")

	s = cardCheckFixture()
	putCheckCard(s, "a4", "s1", "mas-bandwidth/rocketnet", path)
	putCheckCard(s, "b2", "s2", "mas-bandwidth/rocketnet", ".github/workflows/**")
	got = findingsOfKind(CheckCards(s, cardCheckIn()), CheckFiles)
	require.Len(t, got, 1, "%+v", got)
	assert.Contains(t, got[0].Finding, path+" is written by streams s1 and s2")
	assert.Equal(t, SeverityWarn, got[0].Severity)

	s = cardCheckFixture()
	s.Merge.Put(&Card{ID: CtlID("s2"), Row: "s2", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "The second stream", FieldStreamBranch: "dev"}})
	gotAll := CheckCards(s, cardCheckIn())
	bases := findingsOfKind(gotAll, CheckBases)
	require.Len(t, bases, 1, "%+v", gotAll)
	assert.True(t, bases[0].Refuses(), bases[0].Line())
	assert.NotEmpty(t, RefusingFindings(gotAll), "a bad base still fails the check")
}

func TestCardCheckMarkdownDoesNotOrderWriters(t *testing.T) {
	t.Parallel()
	s := cardCheckFixture()
	putCheckCard(s, "a4", "s1", "owner/repo", "CLAUDE.md")
	putCheckCard(s, "a5", "s1", "owner/repo", "CLAUDE.md")
	require.Empty(t, CheckCards(s, cardCheckIn()), "two cards writing the same markdown give no finding and no forced order")

	s = cardCheckFixture()
	putCheckCard(s, "a4", "s1", "owner/repo", "AGENTS.md")
	putCheckCard(s, "b2", "s2", "owner/repo", "AGENTS.md")
	require.Empty(t, CheckCards(s, cardCheckIn()), "two streams writing the same markdown do not chain")

	s = cardCheckFixture()
	putCheckCard(s, "a4", "s1", "owner/repo", "docs/guide.md")
	putCheckCard(s, "a5", "s1", "owner/repo", "internal/c.go")
	putCheckCard(s, "a6", "s1", "owner/repo", "internal/c.go")
	got := CheckCards(s, cardCheckIn())
	require.Empty(t, findingsOfKind(got, CheckFiles))
	same := findingsOfKind(got, CheckSameFile)
	require.Len(t, same, 1, "%+v", got)
	assert.Contains(t, same[0].Finding, "internal/c.go")
	assert.NotContains(t, same[0].Finding, ".md")
}

package sprint

import (
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shared file is a warning and never a failure, ordered or not (Glenn, 2026-10-15).
// Needs, bases, names and the other kinds still refuse. Pure, over the fixture snapshot: no
// git, clock, socket or store.

// warnPut places a primary (or, with kind sentinel, a stop) in the fixture at a score.
func warnPut(s *Snapshot, id, stream, col string, score float64, fields map[string]string) {
	f := map[string]string{"kind": "primary", "stream": stream, "attempt": "0"}
	maps.Copy(f, fields)
	s.Work.Put(&Card{ID: id, Row: stream, Col: col, Score: score, Fields: f})
}

// cardFindingsOf is the findings of one kind.
func cardFindingsOf(fs []CardFinding, kind string) []CardFinding {
	var out []CardFinding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestCardCheckWarnsOnlyOnSerializedOverlap(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		build func(s *Snapshot)
		kind  string
	}{
		{"same stream: two cards of s1 name one file, no need between them", func(s *Snapshot) {
			warnPut(s, "a5", "s1", Ready, 5, map[string]string{"brief": checkBrief("a5", "sprint/s1", "internal/c.go")})
			warnPut(s, "a6", "s1", Ready, 6, map[string]string{"brief": checkBrief("a6", "sprint/s1", "internal/c.go")})
		}, CheckSameFile},
		{"cross stream, ordered by a direct need", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "a4"})
		}, CheckFiles},
		{"cross stream, ordered through a sentinel's need", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			setField(s, "s1-stop", "needs", "a4")
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"})
		}, CheckFiles},
		{"cross stream, ordered through a sentinel's line (its place, no named need)", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "s1-stop", "s1", Waiting, 10, map[string]string{"kind": "sentinel"})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"})
		}, CheckFiles},
		{"cross stream, two writers in s1, both behind the stop", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "a10", "s1", Waiting, 5, map[string]string{"brief": checkBrief("a10", "sprint/s1", "internal/x/one.go"), "needs": "a4"})
			warnPut(s, "s1-stop", "s1", Waiting, 10, map[string]string{"kind": "sentinel"})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"})
		}, CheckFiles},
		{"cross stream, unordered: both ready", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "b2", "s2", Ready, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**")})
		}, CheckFiles},
		{"cross stream, one of two writers after the stop: still a warning", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "s1-stop", "s1", Waiting, 10, map[string]string{"kind": "sentinel"})
			warnPut(s, "a11", "s1", Ready, 11, map[string]string{"brief": checkBrief("a11", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"})
		}, CheckFiles},
		{"cross stream, through a landed (released) stop: orders nothing", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "s1-stop", "s1", Landed, 10, map[string]string{"kind": "sentinel", "needs": "a4"})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "s1-stop"})
		}, CheckFiles},
		{"cross stream, a waived need: orders nothing", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "a4", "waived": "a4"})
		}, CheckFiles},
		{"cross stream, a need on a dropped card: orders nothing", func(s *Snapshot) {
			warnPut(s, "a4", "s1", Ready, 4, map[string]string{"brief": checkBrief("a4", "sprint/s1", "internal/x/one.go")})
			warnPut(s, "gone1", "s1", "", 3, map[string]string{"outcome": "dropped: obsolete", "needs": "a4"})
			warnPut(s, "b2", "s2", Waiting, 2, map[string]string{"brief": checkBrief("b2", "sprint/s2", "internal/x/**"), "needs": "gone1"})
		}, CheckFiles},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := cardCheckFixture()
			c.build(s)
			all := CheckCards(s, cardCheckIn())
			got := cardFindingsOf(all, c.kind)
			require.Len(t, got, 1, "%+v", got)
			f := got[0]
			assert.False(t, f.Refuses(), "%s", f.Line())
			assert.Equal(t, SeverityWarn, f.Severity)
			assert.Equal(t, overlapWarn, f.Serialized)
			assert.NotContains(t, f.Line(), "ordered by waits")
			assert.Contains(t, f.Line(), " WARN serialized="+overlapWarn+" remedy=")
			assert.True(t, strings.HasPrefix(f.Line(), "CHECK "+c.kind+" "), "the kind stays the line's second word: %s", f.Line())
			for _, other := range RefusingFindings(all) {
				assert.NotEqual(t, CheckFiles, other.Kind, "a shared file does not fail the check: %s", other.Line())
				assert.NotEqual(t, CheckSameFile, other.Kind, "a shared file does not fail the check: %s", other.Line())
			}
		})
	}
}

// needs, bases, names, stale-fix, lint, brief-size and routes still refuse; the split helpers
// keep the order and lose nothing.
func TestEveryOtherKindStillRefuses(t *testing.T) {
	t.Parallel()
	s := cardCheckFixture()
	warnPut(s, "a3", "s1", Waiting, 3, map[string]string{"brief": checkBrief("a3", "sprint/s1", "internal/a3.go"), "needs": "gone"})
	warnPut(s, "b3", "s2", Waiting, 3, map[string]string{"brief": checkBrief("b3", "sprint/s2", "internal/b3.go"), "needs": "a1"})
	warnPut(s, "a7", "s1", Ready, 7, map[string]string{"brief": checkBrief("a7", "sprint/s1", "internal/a7.go"), FieldPriority: PriorityFix})
	warnPut(s, "a8", "s1", Ready, 8, map[string]string{"brief": checkBrief("a8", "sprint/s1", "internal/a8.go") + "LINTFAIL\n"})
	warnPut(s, "a9", "s1", Ready, 9, map[string]string{"brief": checkBrief("a9", "sprint/s1", "internal/a9.go") + strings.Repeat("x", DrainBriefBytes)})
	warnPut(s, "a5", "s1", Ready, 5, map[string]string{"brief": checkBrief("a5", "sprint/s1", "internal/c.go")})
	warnPut(s, "a6", "s1", Ready, 6, map[string]string{"brief": checkBrief("a6", "sprint/s1", "internal/c.go")})
	s.Work.SetRows([]string{"s1", "s2", "s3"})
	s.Merge.SetRows([]string{"s1", "s2", "s3"})
	s.Merge.Put(&Card{ID: CtlID("s3"), Row: "s3", Col: Ctl, Fields: map[string]string{"kind": "stream", FieldDisplay: "s3", FieldStreamBranch: "sprint/s3"}})
	warnPut(s, "c1", "s3", Ready, 1, map[string]string{"brief": checkBrief("c1", "sprint/s3", "internal/c1.go")})
	s.Providers = nil // no provider rows applied: a routes finding

	got := CheckCards(s, cardCheckIn())
	kinds := map[string]bool{}
	for _, f := range got {
		kinds[f.Kind] = true
		if f.Kind == CheckSameFile {
			assert.False(t, f.Refuses(), "%s", f.Line())
			continue
		}
		assert.True(t, f.Refuses(), "%s refuses: %s", f.Kind, f.Line())
		assert.Equal(t, SeverityRefuse, f.Severity, f.Line())
	}
	for _, k := range []string{CheckNeeds, CheckBases, CheckNames, CheckStaleFix, CheckLint, CheckBriefSize, CheckRoutes, CheckSameFile} {
		assert.True(t, kinds[k], "a finding of kind %s: %+v", k, got)
	}
	refusing, warning := RefusingFindings(got), WarningFindings(got)
	assert.Len(t, warning, 1)
	assert.Len(t, refusing, len(got)-1)
	assert.Equal(t, "same-file:1", CardCheckKinds(warning))
	assert.True(t, strings.HasPrefix(CardCheckKinds(refusing), "needs:1,bases:1,names:1,stale-fix:1,lint:1,brief-size:1,routes:"), CardCheckKinds(refusing))
	for _, p := range ProviderFindings(s) {
		assert.True(t, p.Refuses(), "start's own provider recheck refuses: %s", p.Line())
	}
	assert.True(t, CardFinding{Kind: CheckFiles}.Refuses(), "a finding with no severity refuses")
}

// brief-size measures the brief, the field the drain bounds at DrainBriefBytes
// (store/engine.go unwritableQueued, unwritable, TextBound), never the card's whole record: a
// card's attempt, consumer and cost history is not in its brief (v1.2.17 summed every field and
// refused four live cards whose briefs were under the bound).
func TestBriefSizeMeasuresTheBriefTheDrainBounds(t *testing.T) {
	t.Parallel()
	head := checkBrief("a9", "sprint/s1", "internal/a9.go")
	pad := func(n int) string { return head + strings.Repeat("x", n-len(head)) }
	history := map[string]string{}
	for i := range 40 {
		history["consumer:20:a9.r"+itoa(i)] = strings.Repeat("{\"outcome\":\"ok\",\"route\":\"heavy\"} ", 30)
	}
	history[FieldFindings] = strings.Repeat("attempt 1: the gate was red. ", 400)
	for _, c := range []struct {
		name  string
		brief string
		extra map[string]string
		max   int
		found bool
	}{
		{"a brief at the bound", pad(DrainBriefBytes), nil, 1 << 20, false},
		{"a brief one byte over the bound", pad(DrainBriefBytes + 1), nil, 1 << 20, true},
		{"a small brief with a large attempt history", pad(4000), history, 1 << 20, false},
		{"a brief at the bound with a large attempt history", pad(DrainBriefBytes), history, 1 << 20, false},
		{"no store bound named: the drain's", pad(DrainBriefBytes + 1), nil, 0, true},
		{"a caller's smaller bound is taken", pad(2001), nil, 2000, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := cardCheckFixture()
			f := map[string]string{"brief": c.brief}
			maps.Copy(f, c.extra)
			warnPut(s, "a9", "s1", Ready, 9, f)
			if c.extra != nil {
				total := 0
				for k, v := range s.Work.Card("a9").Fields {
					total += len(k) + len(v)
				}
				require.Greater(t, total, 2*DrainBriefBytes, "the record is well over the bound")
			}
			in := cardCheckIn()
			in.MaxBriefBytes = c.max
			got := cardFindingsOf(CheckCards(s, in), CheckBriefSize)
			if !c.found {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.True(t, got[0].Refuses())
			assert.Contains(t, got[0].Finding, "brief is "+itoa(len(c.brief))+" bytes")
		})
	}
}

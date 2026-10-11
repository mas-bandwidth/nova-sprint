package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ledger-only landing (docs/SPEC-SPRINT.md section 7, the shared ledgers; tla/LandLedgers.tla):
// a landing whose every conflicted path is a shared ledger is resolved by the lander and lands;
// a conflict anywhere else is refused as before. Measured on 2026-10-11 over 60 minutes of the
// live sprint: about 300 landing refusals against about 35 landings, most in the shared
// ledgers (tla/COVERAGE.tsv, which no resolver owned; docs/fixes.sexp where both sides put a
// fix at one place; the schema repository's evidence ledgers, one top-level form a line,
// which the evidence decoder refused as "trailing bytes after one form").

const runsHeader = "config\tmodule\tstarted_utc\tresult\n"
const coverageHeader = "machine\tcode\tmodule\tstatus\towed_by\tnote\n"

func runsRow(cfg string) string { return cfg + "\tM.tla\t2026-10-11T00:00:00Z\tPASS\n" }
func coverageRow(m string) string {
	return m + "\tinternal/x.go\t-\tmissing\t-\tnote " + m + "\n"
}

// firstFixFile is a fixes file whose :items list opens on its first entry's line, so a fix
// put first in the list changes that line on both sides (the "both sides change line N
// (((fix" refusal of the live log).
func firstFixFile(ids ...string) string {
	var items []string
	for _, id := range ids {
		items = append(items, `(fix "`+id+`" :release "v1.2.14" :status "planned" :title "`+id+`" :origin "o")`)
	}
	return "; fixes\n(fixes \"v1\"\n :title \"t\"\n :text \"x\"\n :releases\n ((release \"v1.2.14\" :status \"planned\" :text \"r\"))\n :items\n (" +
		strings.Join(items, "\n  ") + "))\n"
}

// TestALedgerOnlyConflictLandsWithBothSidesRows: two heads each add a row to tla/RUNS.tsv and
// tla/COVERAGE.tsv and a fix form to docs/fixes.sexp (put first in :items), and regenerate
// FIXES.md. Every conflicted path is a shared ledger, so the second lands too, with both
// rows, both forms, and FIXES.md regenerated from the merged data.
func TestALedgerOnlyConflictLandsWithBothSidesRows(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.a.ledgers = fixesLedgers(fakeFixesRun)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the ledgers", map[string]string{
		"tla/RUNS.tsv":     runsHeader + runsRow("MCBase.cfg"),
		"tla/COVERAGE.tsv": coverageHeader + coverageRow("base"),
		"docs/fixes.sexp":  firstFixFile("a"),
		"FIXES.md":         fixesPage("a"),
	})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	side := func(id string) map[string]string {
		return map[string]string{
			"tla/RUNS.tsv":     runsHeader + runsRow("MCBase.cfg") + runsRow("MC"+id+".cfg"),
			"tla/COVERAGE.tsv": coverageHeader + coverageRow("base") + coverageRow(id),
			"docs/fixes.sexp":  firstFixFile(id, "a"),
			"FIXES.md":         fixesPage(id, "a"),
		}
	}
	heads := map[string]string{"s1-1": r.card("s1-1", side("one")), "s1-2": r.card("s1-2", side("two"))}
	r.queued(heads, "s1-1", "s1-2")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
	runs := r.git(r.remote, "show", "main:tla/RUNS.tsv")
	assert.Equal(t, strings.TrimSuffix(runsHeader+runsRow("MCBase.cfg")+runsRow("MCone.cfg")+runsRow("MCtwo.cfg"), "\n"), runs, "both sides' rows, the header kept once")
	cov := r.git(r.remote, "show", "main:tla/COVERAGE.tsv")
	assert.Equal(t, strings.TrimSuffix(coverageHeader+coverageRow("base")+coverageRow("one")+coverageRow("two"), "\n"), cov, "both sides' rows, the header kept once")
	fixes := r.git(r.remote, "show", "main:docs/fixes.sexp")
	assert.Equal(t, 1, strings.Count(fixes, `(fix "one"`), fixes)
	assert.Equal(t, 1, strings.Count(fixes, `(fix "two"`), fixes)
	assert.Equal(t, 1, strings.Count(fixes, `(fix "a"`), fixes)
	page := r.git(r.remote, "show", "main:FIXES.md")
	for _, id := range []string{"one", "two", "a"} {
		assert.Contains(t, page, `(fix "`+id+`"`, "FIXES.md regenerated from the merged data")
	}
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
	r.clean()
}

// TestASameIdFixConflictIsRefused: both heads add a fix form with one id, differently. That
// is a real conflict: the second is refused, naming the id, and main holds the first's form.
func TestASameIdFixConflictIsRefused(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.a.ledgers = fixesLedgers(fakeFixesRun)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the fixes", map[string]string{"docs/fixes.sexp": firstFixFile("a"), "FIXES.md": fixesPage("a")})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	same := func(title string) map[string]string {
		f := strings.Replace(firstFixFile("same", "a"), `:title "same"`, `:title "`+title+`"`, 1)
		return map[string]string{"docs/fixes.sexp": f, "FIXES.md": fixesPage("same", "a")}
	}
	heads := map[string]string{"s1-1": r.card("s1-1", same("one")), "s1-2": r.card("s1-2", same("two"))}
	r.queued(heads, "s1-1", "s1-2")
	_, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out+errs, "ids=s1-2 fact=conflict reason=the head "+heads["s1-2"]+" of s1-2 does not merge")
	assert.Contains(t, out+errs, "fix:same")
	fixes := r.git(r.remote, "show", "main:docs/fixes.sexp")
	assert.Contains(t, fixes, `:title "one"`)
	assert.NotContains(t, fixes, `:title "two"`)
	assert.Equal(t, "landed/merged", r.places("s1-1", "s1-2")["s1-1"])
	assert.NotEqual(t, "landed/merged", r.places("s1-1", "s1-2")["s1-2"])
}

// TestACodeConflictIsStillRefused: both heads change one line of a code file and add ledger
// rows. A code conflict is never resolved: the second is refused as before, and none of its
// rows land.
func TestACodeConflictIsStillRefused(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the code", map[string]string{
		"internal/x/x.go":  "package x\n\nconst X = 0\n",
		"tla/COVERAGE.tsv": coverageHeader + coverageRow("base"),
	})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	side := func(id, v string) map[string]string {
		return map[string]string{
			"internal/x/x.go":  "package x\n\nconst X = " + v + "\n",
			"tla/COVERAGE.tsv": coverageHeader + coverageRow("base") + coverageRow(id),
		}
	}
	heads := map[string]string{"s1-1": r.card("s1-1", side("one", "1")), "s1-2": r.card("s1-2", side("two", "2"))}
	r.queued(heads, "s1-1", "s1-2")
	_, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out+errs, "ids=s1-2 fact=conflict reason=the head "+heads["s1-2"]+" of s1-2 does not merge")
	assert.Contains(t, out+errs, "internal/x/x.go")
	assert.Equal(t, "package x\n\nconst X = 1", r.git(r.remote, "show", "main:internal/x/x.go"))
	assert.NotContains(t, r.git(r.remote, "show", "main:tla/COVERAGE.tsv"), "two")
	assert.Equal(t, "landed/merged", r.places("s1-1", "s1-2")["s1-1"])
	assert.NotEqual(t, "landed/merged", r.places("s1-1", "s1-2")["s1-2"])
}

// schemaEvidence is an evidence ledger as the schema repository writes it: one top-level
// (:id ...) form a line, no wrapping form.
func schemaEvidence(ids ...string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(`(:id "go/` + id + `" :state :done :evidence ("x_test.go: Test` + id + `") :tested-revision "abc")` + "\n")
	}
	return b.String()
}

// TestEvidenceFormsKeepBoth: two heads each add forms to a schema-style evidence ledger, one
// form the same on both sides. The second lands: both sides' forms kept, the exact duplicate
// once.
func TestEvidenceFormsKeepBoth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		addAdd bool
	}{{"a content conflict", false}, {"an add/add conflict", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLandRig(t)
			const p = "docs/roadmap-evidence/fixed-go.sexp"
			r.git(r.worker, "switch", "-q", "--detach", "origin/main")
			var base []string
			if !tc.addAdd {
				base = []string{"F1"}
				r.files("the evidence", map[string]string{p: schemaEvidence(base...)})
				r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
				r.git(r.worker, "fetch", "-q", "origin")
			}
			r.ok("add --stream s1 --count 2")
			heads := map[string]string{
				"s1-1": r.card("s1-1", map[string]string{p: schemaEvidence(append(base, "F2", "F9")...)}),
				"s1-2": r.card("s1-2", map[string]string{p: schemaEvidence(append(base, "F3", "F9")...)}),
			}
			r.queued(heads, "s1-1", "s1-2")
			code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
			require.Equal(t, 0, code, out+errs)
			assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
			got := r.git(r.remote, "show", "main:"+p)
			for _, id := range append(base, "F2", "F3", "F9") {
				assert.Equal(t, 1, strings.Count(got, `"go/`+id+`"`), id+" once in:\n"+got)
			}
			assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
			r.clean()
		})
	}
}

// TestEvidenceHistoryKeepsEveryRevision: the schema evidence ledgers are append-only history
// (Stella's read of PR #100). A slice of the real fixed-java.sexp at schema ec49ddfb holds
// java/C7 done at two tested revisions; two heads each add an unrelated task. The second
// lands through the real landing path, every revision of java/C7 kept, both tasks added,
// the tip's forms first. Before, the merge by form keyed evidence by :id and refused
// "id:java/C7 is named twice".
func TestEvidenceHistoryKeepsEveryRevision(t *testing.T) {
	t.Parallel()
	history, err := os.ReadFile(filepath.Join("testdata", "fixed-java-history.sexp"))
	require.NoError(t, err)
	r := newLandRig(t)
	const p = "docs/roadmap-evidence/fixed-java.sexp"
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the evidence", map[string]string{p: string(history)})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	task := func(id string) string {
		return `(:id "java/` + id + `" :state :done :evidence ("x_test.go: Test` + id + `") :tested-revision "abc")` + "\n"
	}
	heads := map[string]string{
		"s1-1": r.card("s1-1", map[string]string{p: string(history) + task("T1")}),
		"s1-2": r.card("s1-2", map[string]string{p: string(history) + task("T2")}),
	}
	r.queued(heads, "s1-1", "s1-2")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
	got := r.git(r.remote, "show", "main:"+p)
	assert.Equal(t, 2, strings.Count(got, `"java/C7"`), got)
	assert.Contains(t, got, "bb83ab4dfac22dfe73c39d4e79b3e9664fa83b65")
	assert.Contains(t, got, "d446cee1512ff86479bc2137a4f794dab3529bc2")
	assert.Less(t, strings.Index(got, `"java/T1"`), strings.Index(got, `"java/T2"`), "the tip's forms first")
	assert.True(t, strings.HasPrefix(got, strings.TrimSuffix(string(history), "\n")), "the tip's text kept")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
	r.clean()
}

// TestAKeyedTableRefusesADivergentDuplicateKeyInOneSide: a side of a keyed table that names one
// key twice, differently, is malformed; the resolution refuses it naming the key, never
// silently keeping one of the rows (Stella's read of PR #100).
func TestAKeyedTableRefusesADivergentDuplicateKeyInOneSide(t *testing.T) {
	t.Parallel()
	base := coverageHeader
	ours := coverageHeader + "m\told\n" + "m\tnew\n"
	theirs := coverageHeader + "m\tnew\n" + "n\tx\n"
	_, _, _, err := resolveCoverageByMachine([]byte(base), []byte(ours), []byte(theirs))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "m")
	assert.Contains(t, err.Error(), "twice")
	// an exact duplicate row is one row, not a refusal
	_, _, _, err = resolveCoverageByMachine([]byte(base), []byte(coverageHeader+"m\tnew\nm\tnew\n"), []byte(theirs))
	assert.NoError(t, err)
}

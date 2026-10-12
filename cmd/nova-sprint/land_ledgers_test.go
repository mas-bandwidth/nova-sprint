package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

// fixesLedgers is the landLedgers with the FIXES.md family's run faked (the clone holds no
// Go toolchain): FIXES.md is the ids docs/fixes.sexp names, one a line.
func fixesLedgers(run string) []landLedger {
	fams := slices.Clone(landLedgers)
	for i := range fams {
		if fams[i].owns("FIXES.md") {
			fams[i].run = []string{"sh", "-c", run}
		}
	}
	return fams
}

// fixesPage is what fakeFixesRun writes for the given ids, one a line.
func fixesPage(ids ...string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(`(fix "` + id + `"` + "\n")
	}
	return b.String()
}

// evidenceEntry is one evidence ledger entry on one line; evidenceFile the whole file.
func evidenceEntry(id string) string {
	return `(fixed "` + id + `" :release "v1.2.14" :title "` + id + `" :origin "o")`
}

func evidenceFile(items ...string) string {
	return "; evidence\n(evidence \"v1\"\n :title \"t\"\n :items\n (" +
		strings.Join(items, "\n\n  ") + "))\n"
}

// sameReleaseFile is a fixes file with two releases: v1.2.14 (entries a) and v1.2.6
// (entry z), so an entry added to the v1.2.14 block sits in the middle of :items.
func sameReleaseFile(block []string) string {
	items := append([]string{mlFix("a", "v1.2.14")}, block...)
	items = append(items, mlFix("z", "v1.2.6"))
	return fixesFile2(items...)
}

func mlFix(id, release string) string {
	return `(fix "` + id + `" :release "` + release + `" :status "planned"` + "\n    :title \"" + id + "\"\n    :origin \"o\")"
}

func fixesFile2(items ...string) string {
	return "; fixes\n(fixes \"v1\"\n :title \"t\"\n :text \"x\"\n :releases\n ((release \"v1.2.14\" :status \"planned\" :text \"r\")\n  (release \"v1.2.6\" :status \"shipped\" :text \"r\"))\n :items\n (" +
		strings.Join(items, "\n\n  ") + "))\n"
}

// TestLandMergesSameReleaseBlockEntries is (0): two heads whose docs/fixes.sexp entries sit
// in the same release block (v1.2.14), and whose FIXES.md regenerations differ, land one
// after the other without a rework; the second's entry is merged into the block and FIXES.md
// regenerated from the merged data. Before, the second was refused "its head conflicts" and
// reworked at the tip.
func TestLandMergesSameReleaseBlockEntries(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.a.ledgers = fixesLedgers(fakeFixesRun)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the fixes", map[string]string{"docs/fixes.sexp": sameReleaseFile(nil), "FIXES.md": fixesPage("a", "z")})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	base := sameReleaseFile(nil)
	add := func(id string) string {
		return strings.Replace(base, `:origin "o")`+"\n\n  "+mlFix("z", "v1.2.6"), `:origin "o")`+"\n\n  "+mlFix(id, "v1.2.14")+"\n\n  "+mlFix("z", "v1.2.6"), 1)
	}
	heads := map[string]string{
		"s1-1": r.card("s1-1", map[string]string{"docs/fixes.sexp": add("card-one"), "FIXES.md": fixesPage("a", "card-one", "z")}),
		"s1-2": r.card("s1-2", map[string]string{"docs/fixes.sexp": add("card-two"), "FIXES.md": fixesPage("a", "card-two", "z")}),
	}
	r.queued(heads, "s1-1", "s1-2")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 0, code, out+errs)
	assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
	show := r.git(r.remote, "show", "main:docs/fixes.sexp")
	assert.Contains(t, show, "card-one")
	assert.Contains(t, show, "card-two")
	assert.Less(t, strings.Index(show, "card-one"), strings.Index(show, "card-two"), "the tip's entry first")
	assert.Equal(t, strings.TrimSpace(fixesPage("a", "card-one", "card-two", "z")), r.git(r.remote, "show", "main:FIXES.md"), "the page is the merged data's, regenerated")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
	r.clean()
}

// TestLandMergesEvidenceLedgers is (1): two heads that each add an entry to
// docs/roadmap-evidence/fixed-x.sexp, as a content conflict and as an add/add conflict,
// land one after the other without a rework: the lander merges the entries (both kept, in
// file order, no duplicate). Before, the second was reworked at the tip.
func TestLandMergesEvidenceLedgers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		addAdd        bool
		first, second map[string]string
		want          string
	}{
		{"a content conflict lands",
			false,
			map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("a"), evidenceEntry("b"))},
			map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("a"), evidenceEntry("c"))},
			evidenceFile(evidenceEntry("a"), evidenceEntry("b"), evidenceEntry("c"))},
		{"an add/add conflict lands",
			true,
			map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("a"))},
			map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("b"))},
			evidenceFile(evidenceEntry("a"), evidenceEntry("b"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLandRig(t)
			r.git(r.worker, "switch", "-q", "--detach", "origin/main")
			if !tc.addAdd {
				r.files("the evidence", map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("a"))})
				r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
				r.git(r.worker, "fetch", "-q", "origin")
			}
			r.ok("add --stream s1 --count 2")
			heads := map[string]string{"s1-1": r.card("s1-1", tc.first), "s1-2": r.card("s1-2", tc.second)}
			r.queued(heads, "s1-1", "s1-2")
			code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
			require.Equal(t, 0, code, out+errs)
			assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
			assert.Equal(t, strings.TrimSuffix(tc.want, "\n"), r.git(r.remote, "show", "main:docs/roadmap-evidence/fixed-x.sexp"))
			assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
			r.clean()
		})
	}
}

// TestLandRefusesAMalformedEvidenceLedger is (3): an evidence entry the lander cannot merge
// (both sides changing one entry, one malformed) stops the stream with the path and both
// entries in the durable judgment/inbox, as merge --conflict-kind ledger does; it never
// reworks the card (docs/SPEC-LANDING.md, the ledgers).
func TestLandRefusesAMalformedEvidenceLedger(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.git(r.worker, "switch", "-q", "--detach", "origin/main")
	r.files("the evidence", map[string]string{"docs/roadmap-evidence/fixed-x.sexp": evidenceFile(evidenceEntry("a"))})
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	r.ok("add --stream s1 --count 2")
	change := func(title string) string {
		return strings.Replace(evidenceFile(evidenceEntry("a")), `:title "a"`, `:title "`+title+`"`, 1)
	}
	// the card's side does not read (an evaluation form): neither the line merge nor the
	// merge by form can take it (two changed forms of one entry are both history, and land)
	heads := map[string]string{
		"s1-1": r.card("s1-1", map[string]string{"docs/roadmap-evidence/fixed-x.sexp": change("one")}),
		"s1-2": r.card("s1-2", map[string]string{"docs/roadmap-evidence/fixed-x.sexp": strings.Replace(change("two"), `:origin "o"`, `:origin #.(x)`, 1)}),
	}
	r.queued(heads, "s1-1", "s1-2")
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "ids=s1-2 fact=conflict reason=the head "+heads["s1-2"]+" of s1-2 does not merge")
	assert.Contains(t, errs, "docs/roadmap-evidence/fixed-x.sexp conflicts")
	assert.Contains(t, errs, "the stream is stopped")
	assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "merging/stuck"}, r.places("s1-1", "s1-2"))
	inbox := r.ok("inbox")
	assert.Contains(t, inbox, `:title "one"`, "the durable judgment shows the tip's conflicting entry")
	assert.Contains(t, inbox, `:title "two"`, "the durable judgment shows the card's conflicting entry")
	assert.Contains(t, inbox, `:origin #.(x)`, "the malformed form is preserved for diagnosis")
	r.clean()
}

// TestTheLedgerSetIsOnePlace is (2): cardgen.LedgerInPaths is the one list, and
// cardgen.AlwaysInPaths reads it, so the scope rule and the lander hold the same ledgers,
// now including the evidence ledgers.
func TestTheLedgerSetIsOnePlace(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		"docs/fixes.sexp", "FIXES.md", "docs/roadmap.sexp", "ROADMAP.md",
		"docs/roadmap-evidence/fixed-x.sexp", "docs/roadmap-evidence/fixed-m.tla.sexp",
		"tla/CASES.tsv", "tla/RUNS.tsv", "internal/sprint/TABLES.lock", "internal/docs/catalog.go",
		"internal/ci/testdata/generality/cmd/x.txt", "cmd/AGENTS.md",
	} {
		assert.True(t, cardgen.LedgerInPaths(p), p)
		assert.True(t, cardgen.AlwaysInPaths(p), "AlwaysInPaths reads the ledger set", p)
	}
	for _, p := range []string{"README.md", "docs/STANDARD.md", "internal/x/y.go", "docs/roadmap-evidence/note.txt"} {
		assert.False(t, cardgen.LedgerInPaths(p), p)
	}
	// a repository's own catalog names more ledgers with a LEDGERS line in its AGENTS.md or
	// nova.sexp, read at staging; LedgerInPaths consults them, and without them the built-in
	// set stands.
	t.Run("a repository's catalog names more ledgers", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# catalog\nLEDGERS: schema/ledgers.tsv schema/counts.txt\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "nova.sexp"), []byte("(catalog)\nLEDGERS \"schema/extra.sexp\"\n"), 0o644))
		catalog := cardgen.CatalogLedgers(dir)
		assert.Equal(t, []string{"schema/ledgers.tsv", "schema/counts.txt", "schema/extra.sexp"}, catalog)
		for _, p := range catalog {
			assert.True(t, cardgen.LedgerInPaths(p, catalog...), p)
			assert.False(t, cardgen.LedgerInPaths(p), "the built-in set alone does not name it: "+p)
		}
	})
}

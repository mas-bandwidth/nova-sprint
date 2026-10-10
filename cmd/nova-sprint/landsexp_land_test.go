package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fixItem is one point-release entry on one line; fixItemLines the same over three.
func fixItem(id string) string {
	return `(fix "` + id + `" :release "v1" :status "planned" :title "` + id + `" :origin "o")`
}

func fixItemLines(id string) string {
	return `(fix "` + id + `" :release "v1" :status "planned"` + "\n    :title \"" + id + "\"\n    :origin \"o\")"
}

// fixesFile is a fixes file shaped as docs/fixes.sexp is: the items one per stretch, a
// blank line between, the last closing the list and the form on its own line.
func fixesFile(items ...string) string {
	return "; the point releases\n(fixes \"v1\"\n :title \"t\"\n :text \"x\"\n :releases\n ((release \"v1\" :status \"planned\" :text \"r\"))\n :items\n (" +
		strings.Join(items, "\n\n  ") + "))\n"
}

// fakeFixesRun stands in for `go run ./tools/roadmap --kind fixes`: FIXES.md is the ids
// docs/fixes.sexp names, one a line.
const fakeFixesRun = `grep -o '(fix "[^"]*"' docs/fixes.sexp > FIXES.md`

// Two cards that each add their entry to docs/fixes.sexp and regenerate FIXES.md conflict
// in both; land merges the data entry by entry, takes the tip's FIXES.md and regenerates it
// from the merged data, and lands both (on 2026-10-10 the second was refused "its head
// conflicts" and reworked, round after round). Two cards changing one entry are refused
// as before.
func TestLandMergesTheRoadmapData(t *testing.T) {
	t.Parallel()
	page := func(ids ...string) string {
		var b strings.Builder
		for _, id := range ids {
			b.WriteString(`(fix "` + id + `"` + "\n")
		}
		return b.String()
	}
	for _, tc := range []struct {
		name          string
		first, second map[string]string
		why           string
	}{
		{"two added entries land",
			map[string]string{"docs/fixes.sexp": fixesFile(fixItem("a"), fixItem("b")), "FIXES.md": page("a", "b")},
			map[string]string{"docs/fixes.sexp": fixesFile(fixItem("a"), fixItem("c")), "FIXES.md": page("a", "c")}, ""},
		{"two changes of one entry are refused",
			map[string]string{"docs/fixes.sexp": fixesFile(strings.Replace(fixItem("a"), `:title "a"`, `:title "one"`, 1))},
			map[string]string{"docs/fixes.sexp": fixesFile(strings.Replace(fixItem("a"), `:title "a"`, `:title "two"`, 1))}, "both sides change line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newLandRig(t)
			fams := slices.Clone(landLedgers)
			for i := range fams {
				if fams[i].owns("FIXES.md") {
					fams[i].run = []string{"sh", "-c", fakeFixesRun}
				}
			}
			r.a.ledgers = fams
			r.git(r.worker, "switch", "-q", "--detach", "origin/main")
			r.files("the fixes", map[string]string{"docs/fixes.sexp": fixesFile(fixItem("a")), "FIXES.md": page("a")})
			r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
			r.git(r.worker, "fetch", "-q", "origin")
			r.ok("add --stream s1 --count 2")
			heads := map[string]string{"s1-1": r.card("s1-1", tc.first), "s1-2": r.card("s1-2", tc.second)}
			r.queued(heads, "s1-1", "s1-2")
			code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
			if tc.why != "" {
				assert.Equal(t, 1, code, out+errs)
				assert.Contains(t, errs, "ids=s1-2 fact=conflict reason=the head "+heads["s1-2"]+" of s1-2 does not merge")
				assert.Contains(t, errs, tc.why)
				assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "ready/returned"}, r.places("s1-1", "s1-2"))
				assert.Empty(t, r.git(r.clone, "status", "--porcelain", "--untracked-files=all"), "the refused merge is aborted")
				r.clean()
				return
			}
			assert.Equal(t, 0, code, out+errs)
			assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
			assert.Equal(t, strings.TrimSuffix(fixesFile(fixItem("a"), fixItem("b"), fixItem("c")), "\n"), r.git(r.remote, "show", "main:docs/fixes.sexp"))
			assert.Equal(t, strings.TrimSuffix(page("a", "b", "c"), "\n"), r.git(r.remote, "show", "main:FIXES.md"), "the page is the merged data's, regenerated")
			body := r.git(r.remote, "log", "-1", "--format=%b", "main")
			assert.Contains(t, body, "The data docs/fixes.sexp conflicted and was merged entry by entry (both sides' entries kept).")
			assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "landed/merged"}, r.places("s1-1", "s1-2"))
			assert.Contains(t, r.ok("card s1-2"), "the data docs/fixes.sexp conflicted and was merged entry by entry")
			r.clean()
		})
	}
}

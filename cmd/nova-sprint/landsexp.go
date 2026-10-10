package main

// landsexp.go is what land does with the roadmap data at a merge (docs/SPEC-SPRINT.md
// section 7, the generated ledgers; docs/fixes.sexp, land-merges-the-roadmap-data). Nearly
// every card adds its entry to docs/fixes.sexp (or moves one there out of
// docs/roadmap.sexp) and regenerates FIXES.md (ROADMAP.md) with `make roadmap`. The entries
// go in at the same place, so once the first card of a wave lands every other one met a
// conflict in the data and its page and was returned "its head conflicts", round after
// round, until it reached its bound: on 2026-10-10 the merging column stood at 15 to 21
// cards with nothing landed in 30 minutes.
//
// A conflict in the data is resolved, not refused, when each side, within each conflicted
// stretch, only adds whole lines or only removes them: the base's lines both sides keep,
// then the tip's added lines before the card's at each place. The run of closing
// parentheses that ends the file is set aside first, so an entry added after the last one is
// an added stretch, not a change of the last line. The result must then decode, and its
// entries must be exactly what the two sides did (an entry both kept, as the side that
// changed it left it; one either side added; none either side removed); anything else,
// both sides changing one entry or one line, an entry added inside one the other removed,
// is refused as any conflict is. The generated page is then taken from the tip and
// regenerated from the merged data by its family's run (landledger.go, landLedgers).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
)

// sexpData is the roadmap data a merge resolves, each with the decoder kind and the page
// generated from it.
var sexpData = map[string]struct{ kind, page string }{
	"docs/fixes.sexp":   {"fixes", "FIXES.md"},
	"docs/roadmap.sexp": {"roadmap", "ROADMAP.md"},
}

// roadmapPage says p is a page generated from the roadmap data (FIXES.md, ROADMAP.md).
// A conflict in one is a file conflict for its kind (land.go, mergeHeadLedgers): when its
// regeneration fails, the card is returned to redo, as before these were ledgers.
func roadmapPage(p string) bool {
	for _, d := range sexpData {
		if p == d.page {
			return true
		}
	}
	return false
}

// sexpMiddleCap bounds the line comparison of one side (the changed middle of base times
// the changed middle of the side): a side past it is refused, not compared.
const sexpMiddleCap = 16_000_000

// stageSexpUnion resolves the roadmap data among a merge's unmerged paths: each one's
// merged text written and staged. rest is paths without them, data the ones resolved,
// pages the generated pages their families must regenerate; card is why one is refused
// (the merge goes on as any conflict does), env a failure that is not the card's.
func (l *lander) stageSexpUnion(ctx context.Context, dir string, paths []string) (rest, data, pages []string, card, env string) {
	for _, p := range paths {
		d, ok := sexpData[p]
		if !ok {
			rest = append(rest, p)
			continue
		}
		if q := onDiskLink(dir, []string{p}); q != "" {
			return nil, nil, nil, p + " conflicts and " + q + " is a symlink, which the resolution would write through", ""
		}
		var sides [3][]byte
		for i := range sides {
			res, err := gitrun.Run(ctx, gitrun.Options{C: dir, Env: l.a.gitEnv, OwnRepo: true}, "show", ":"+strconv.Itoa(i+1)+":"+p)
			if err != nil {
				return nil, nil, nil, p + " conflicts and has no stage " + strconv.Itoa(i+1) + " to resolve from (added or removed on one side)", ""
			}
			sides[i] = res.Stdout
		}
		merged, err := unionSexp(d.kind, p, sides[0], sides[1], sides[2])
		if err != nil {
			return nil, nil, nil, p + " conflicts and " + err.Error(), ""
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), merged, 0o644); err != nil {
			return nil, nil, nil, "", "the merged " + p + " could not be written: " + err.Error()
		}
		if _, err := l.git(ctx, dir, "add", "--", p); err != nil {
			return nil, nil, nil, "", "the merged " + p + " could not be staged: " + firstLine("", err)
		}
		data = append(data, p)
		if !slices.Contains(pages, d.page) {
			pages = append(pages, d.page)
		}
	}
	return rest, data, pages, "", ""
}

// sexpUnionNote is the card's note for data resolved at the merge, on its timeline.
func sexpUnionNote(data []string) string {
	return "the data " + strings.Join(data, ", ") + " conflicted and was merged entry by entry (" + sexpKept + ")"
}

// sexpKept is what a merge of the data keeps, as its note and commit say.
const sexpKept = "each side's additions and removals kept"

// unionSexp is the roadmap data at a merge (the file's comment above): base, the tip's side
// (ours) and the card's (theirs) merged, or why they do not merge.
func unionSexp(kind, file string, base, ours, theirs []byte) ([]byte, error) {
	var bodies [3][]string
	var closers [3]int
	var tail string
	for i, side := range [][]byte{base, ours, theirs} {
		body, k, t := splitClosers(string(side))
		if k == 0 {
			return nil, errors.New("a side does not end in a closing parenthesis")
		}
		closers[i], bodies[i] = k, strings.Split(body, "\n")
		if i == 1 {
			tail = t
		}
	}
	if closers[0] != closers[1] || closers[0] != closers[2] {
		return nil, fmt.Errorf("the sides end in %d, %d and %d closing parentheses, which is no entry added or removed", closers[0], closers[1], closers[2])
	}
	ko, io, err := lineEdits(bodies[0], bodies[1])
	if err != nil {
		return nil, err
	}
	kt, it, err := lineEdits(bodies[0], bodies[2])
	if err != nil {
		return nil, err
	}
	lines, err := mergeLineEdits(bodies[0], ko, kt, io, it)
	if err != nil {
		return nil, err
	}
	lines = tidyBlanks(bodies[0], lines)
	out := []byte(strings.Join(lines, "\n") + strings.Repeat(")", closers[0]-1) + tail)
	if err := sexpEntriesMerged(kind, file, base, ours, theirs, out); err != nil {
		return nil, err
	}
	return out, nil
}

// splitClosers is s without its trailing whitespace and the run of closing parentheses
// before it, keeping one: the body (ending in the last entry's own parenthesis), how many
// were in the run, and the whitespace.
func splitClosers(s string) (body string, k int, tail string) {
	t := strings.TrimRight(s, " \t\r\n")
	tail = s[len(t):]
	trimmed := strings.TrimRight(t, ")")
	k = len(t) - len(trimmed)
	if k == 0 {
		return t, 0, tail
	}
	return trimmed + ")", k, tail
}

// lineEdits is side as base with whole lines removed and added: kept[i] says base line i
// is in side, added[i] the lines side adds before base line i (added[len(base)] after the
// last), from a longest common subsequence of the stretch between their common prefix and
// suffix.
func lineEdits(base, side []string) (kept []bool, added [][]string, err error) {
	n, m := len(base), len(side)
	kept, added = make([]bool, n), make([][]string, n+1)
	p := 0
	for p < n && p < m && base[p] == side[p] {
		kept[p] = true
		p++
	}
	s := 0
	for s < n-p && s < m-p && base[n-1-s] == side[m-1-s] {
		kept[n-1-s] = true
		s++
	}
	b, d := base[p:n-s], side[p:m-s]
	if (len(b)+1)*(len(d)+1) > sexpMiddleCap {
		return nil, nil, fmt.Errorf("a side changes %d lines across %d, too many to compare", len(d), len(b))
	}
	// t[i][j] is the longest common subsequence of b[i:] and d[j:]
	t := make([][]int32, len(b)+1)
	for i := range t {
		t[i] = make([]int32, len(d)+1)
	}
	for i := len(b) - 1; i >= 0; i-- {
		for j := len(d) - 1; j >= 0; j-- {
			switch {
			case b[i] == d[j]:
				t[i][j] = t[i+1][j+1] + 1
			case t[i+1][j] >= t[i][j+1]:
				t[i][j] = t[i+1][j]
			default:
				t[i][j] = t[i][j+1]
			}
		}
	}
	for i, j := 0, 0; i < len(b) || j < len(d); {
		switch {
		case i < len(b) && j < len(d) && b[i] == d[j]:
			kept[p+i] = true
			i, j = i+1, j+1
		case j < len(d) && (i == len(b) || t[i][j+1] >= t[i+1][j]):
			added[p+i] = append(added[p+i], d[j])
			j++
		default:
			i++ // base line p+i is removed
		}
	}
	return kept, added, nil
}

// mergeLineEdits is base with both sides' edits: a line kept by both stays, the tip's
// added lines go before the card's at each place. A line both sides remove that is not
// blank (both changed it), and lines one side adds between two lines the other removes
// (an addition inside a removed entry), are refused.
func mergeLineEdits(base []string, ko, kt []bool, io, it [][]string) ([]string, error) {
	n := len(base)
	var out []string
	for i := 0; i <= n; i++ {
		if i > 0 && i < n {
			if len(io[i]) > 0 && !kt[i-1] && !kt[i] {
				return nil, fmt.Errorf("the tip adds lines inside a stretch the card removes, at line %d", i+1)
			}
			if len(it[i]) > 0 && !ko[i-1] && !ko[i] {
				return nil, fmt.Errorf("the card adds lines inside a stretch the tip removes, at line %d", i+1)
			}
		}
		out = append(out, io[i]...)
		out = append(out, it[i]...)
		if i == n {
			break
		}
		switch {
		case ko[i] && kt[i]:
			out = append(out, base[i])
		case !ko[i] && !kt[i] && strings.TrimSpace(base[i]) != "":
			return nil, fmt.Errorf("both sides change line %d (%s)", i+1, strings.TrimSpace(base[i]))
		}
	}
	return out, nil
}

// tidyBlanks is lines with no run of blank lines longer than base's longest (at least
// one) and none at the end: two sides that remove neighbouring entries each take one of
// the blank lines between them, and the blank line both keep would otherwise stand
// doubled, or before the closing parentheses. Runs base already has are left as they are.
func tidyBlanks(base, lines []string) []string {
	most, run := 1, 0
	for _, l := range base {
		if strings.TrimSpace(l) != "" {
			run = 0
			continue
		}
		if run++; run > most {
			most = run
		}
	}
	var out []string
	run = 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			run = 0
		} else if run++; run > most {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 1 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out
}

// sexpEntries is a decoded data file's records by kind and id, each as its printed value:
// the file's own title and text ("top:title", "top:text"), and for fixes "release:<id>" and
// "fix:<id>", for the roadmap "group:<id>", "scheduled:<id>", "done:<id>" and "item:<id>".
// An id met twice is an error.
func sexpEntries(kind, file string, data []byte) (map[string]any, error) {
	out := map[string]any{}
	put := func(k string, v any) error {
		if _, dup := out[k]; dup {
			return fmt.Errorf("%s is named twice", k)
		}
		out[k] = fmt.Sprintf("%#v", v) // a string, so any record compares
		return nil
	}
	switch kind {
	case "fixes":
		fx, err := roadmapdoc.DecodeFixes(file, data)
		if err != nil {
			return nil, err
		}
		if err := errors.Join(put("top:title", fx.Title), put("top:text", fx.Text)); err != nil {
			return nil, err
		}
		for _, r := range fx.Releases {
			if err := put("release:"+r.ID, r); err != nil {
				return nil, err
			}
		}
		for _, f := range fx.Items {
			if err := put("fix:"+f.ID, f); err != nil {
				return nil, err
			}
		}
	default:
		doc, err := roadmapdoc.Decode(file, data)
		if err != nil {
			return nil, err
		}
		if err := errors.Join(put("top:title", doc.Title), put("top:text", doc.Text)); err != nil {
			return nil, err
		}
		for _, g := range doc.Groups {
			if err := put("group:"+g.ID, g); err != nil {
				return nil, err
			}
		}
		for _, n := range doc.Scheduled {
			if err := put("scheduled:"+n.ID, n); err != nil {
				return nil, err
			}
		}
		for _, n := range doc.Done {
			if err := put("done:"+n.ID, n); err != nil {
				return nil, err
			}
		}
		for _, it := range doc.Items {
			if err := put("item:"+it.ID, it); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// sexpEntriesMerged holds the merged data to what the two sides did, entry by entry: an
// entry in all three is the side's that changed it (both changing it differently is a
// conflict), one a side removed is gone (the other changing it is a conflict), one a side
// added is there as added (both adding one id differently is a conflict), and nothing else
// is there.
func sexpEntriesMerged(kind, file string, base, ours, theirs, merged []byte) error {
	var e [3]map[string]any
	for i, side := range [][]byte{base, ours, theirs} {
		m, err := sexpEntries(kind, file, side)
		if err != nil {
			return fmt.Errorf("a side does not decode: %s", oneLineErr(err))
		}
		e[i] = m
	}
	b, o, t := e[0], e[1], e[2]
	want := map[string]any{}
	for _, k := range unionKeys(b, o, t) {
		bv, inB := b[k]
		ov, inO := o[k]
		tv, inT := t[k]
		switch {
		case inB && inO && inT:
			switch {
			case ov == bv:
				want[k] = tv
			case tv == bv || tv == ov:
				want[k] = ov
			default:
				return fmt.Errorf("both sides change %s", k)
			}
		case inB:
			if inO && ov != bv || inT && tv != bv {
				return fmt.Errorf("one side removes %s and the other changes it", k)
			}
		case inO && inT && ov != tv:
			return fmt.Errorf("both sides add %s, differently", k)
		case inO:
			want[k] = ov
		default:
			want[k] = tv
		}
	}
	got, err := sexpEntries(kind, file, merged)
	if err != nil {
		return fmt.Errorf("the merged data does not decode: %s", oneLineErr(err))
	}
	for _, k := range unionKeys(want, got) {
		wv, inW := want[k]
		gv, inG := got[k]
		switch {
		case !inG:
			return fmt.Errorf("the merged data lacks %s", k)
		case !inW:
			return fmt.Errorf("the merged data holds %s, which a side removed", k)
		case wv != gv:
			return fmt.Errorf("the merged data changes %s", k)
		}
	}
	return nil
}

// unionKeys is the keys of the maps, sorted, each once.
func unionKeys(ms ...map[string]any) []string {
	var out []string
	for _, m := range ms {
		for k := range m {
			if !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.Sort(out)
	return out
}

// oneLineErr is err's words on one line.
func oneLineErr(err error) string { return strings.ReplaceAll(err.Error(), "\n", "; ") }

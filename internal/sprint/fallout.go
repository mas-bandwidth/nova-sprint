package sprint

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// Fallout is the card's own change landing in a file just outside its PATHS
// (tla/Fallout.tla, Land and Widen; docs/SPEC-SPRINT.md section 7, the fallout sweep).
// A rename or a deletion leaves references behind; a hunk that only follows the change
// (an old name replaced by its new one, a line dropped because its only reference is a
// removed name) is fallout, and the lander accepts it outside PATHS as the scope
// amendment is accepted. Anything else outside PATHS is still refused (E12).

// AdjFallout is why a file outside PATHS is fallout: every hunk follows the card's own
// rename or deletion (tla/Fallout.tla, FalloutOnly).
const AdjFallout = "the card's own fallout"

// CardRenames reads the card's own change from the hunks inside paths: the declarations
// it renames (a Go declaration or method, a TLA+ operator, a Markdown heading's anchor,
// and a git rename of a path) and the names and paths it removes. A declaration is a
// rename when one is removed and one is added in the same hunk with the same shape
// (the line without its name); a path is a rename when git reports one. Everything else
// a hunk removes is a name this change drops (tla/Fallout.tla, Renames and Removes).
func CardRenames(diff string, paths []string) (renames map[string]string, removed []string) {
	renames = map[string]string{}
	gone := map[string]bool{}
	inside := func(p string) bool {
		return slices.ContainsFunc(paths, func(g string) bool { return diffcheck.Covers(g, p) })
	}
	// a git rename inside PATHS is the card's own path change; a deleted file inside
	// PATHS removes every reference to its path
	old := ""
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			a, _, _ := strings.Cut(strings.TrimPrefix(l, "diff --git "), " b/")
			old = strings.TrimPrefix(a, "a/")
		case strings.HasPrefix(l, "+++ /dev/null"):
			if old != "" && inside(old) && !gone[old] {
				gone[old] = true
				removed = append(removed, old)
			}
		}
	}
	for _, f := range diffcheck.Parse(diff) {
		if !inside(f.Old) && !inside(f.New) {
			continue
		}
		if f.Old != f.New && f.Old != "" && f.New != "" && inside(f.Old) {
			renames[f.Old] = f.New
		}
		for _, h := range f.Hunks {
			declRenames(h, renames, gone)
		}
	}
	for name := range gone {
		if _, ok := renames[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	removed = slices.Compact(removed)
	return renames, removed
}

// declRename is one declaration a hunk moves: its name and its shape (the line with the
// name taken out), the shape that says the same declaration was renamed rather than
// replaced by another (tla/Fallout.tla, Renames: same place, same shape).
type declRename struct{ name, shape string }

// declRenames pairs the declarations one hunk removes and adds with the same shape, by
// kind and in the hunk: each pair is a rename; an unpaired removal is a name the change
// drops.
func declRenames(h diffcheck.Hunk, renames map[string]string, gone map[string]bool) {
	var dels, adds []declRename
	seenDel, seenAdd := map[string]bool{}, map[string]bool{}
	for _, l := range h.Lines {
		if len(l) == 0 {
			continue
		}
		rest := ""
		switch l[0] {
		case '-':
			rest = l[1:]
			if d, ok := declaration(rest); ok {
				if !seenDel[d.name] {
					seenDel[d.name] = true
					dels = append(dels, d)
				}
			}
		case '+':
			rest = l[1:]
			if d, ok := declaration(rest); ok {
				if !seenAdd[d.name] {
					seenAdd[d.name] = true
					adds = append(adds, d)
				}
			}
		default:
			continue
		}
	}
	used := map[string]bool{}
	for _, d := range dels {
		if seenAdd[d.name] {
			continue // declared before and after: no rename, no removal
		}
		paired := ""
		for _, a := range adds {
			if used[a.name] || seenDel[a.name] {
				continue
			}
			if a.shape == d.shape {
				paired = a.name
				break
			}
		}
		if paired != "" {
			used[paired] = true
			renames[d.name] = paired
			continue
		}
		gone[d.name] = true
	}
}

var (
	funcDeclRE  = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	typeDeclRE  = regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)`)
	valueDeclRE = regexp.MustCompile(`^\s*(?:const|var)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	opDeclRE    = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*==`)
	headingRE   = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
)

// declaration is the identifier a line declares, its line with that one occurrence taken
// out as its shape, and false for a line that declares none: a Go func, method, type,
// const or var; a TLA+ operator definition; a Markdown heading, by its anchor (tla/
// Fallout.tla, Renames: the identifiers and paths the card's own change names).
func declaration(line string) (declRename, bool) {
	if m := funcDeclRE.FindStringSubmatch(line); m != nil {
		return declRename{name: m[1], shape: removeToken(line, m[1])}, true
	}
	if m := typeDeclRE.FindStringSubmatch(line); m != nil {
		return declRename{name: m[1], shape: removeToken(line, m[1])}, true
	}
	if m := valueDeclRE.FindStringSubmatch(line); m != nil {
		return declRename{name: m[1], shape: removeToken(line, m[1])}, true
	}
	if m := opDeclRE.FindStringSubmatch(line); m != nil {
		return declRename{name: m[1], shape: removeToken(line, m[1])}, true
	}
	if m := headingRE.FindStringSubmatch(line); m != nil {
		return declRename{name: headingAnchor(m[2]), shape: m[1]}, true
	}
	return declRename{}, false
}

// removeToken is line with name taken out once: the shape two declarations share when
// only the name changed.
func removeToken(line, name string) string {
	if i := strings.Index(line, name); i >= 0 {
		line = line[:i] + line[i+len(name):]
	}
	return strings.TrimSpace(line)
}

// headingAnchor is a Markdown heading's anchor as the docs cite it: lower case, spaces
// to dashes, and every character that is not a letter, digit, dash or underscore gone.
func headingAnchor(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// FalloutHunk says a hunk only follows the card's change (tla/Fallout.tla, FalloutHunk):
// every removed line pairs with an added line equal to it after the old names are
// replaced by their new ones, word-bounded, or the hunk only deletes lines whose only
// reference is a removed name. A hunk that adds a line with no such pair changes
// something of its own and is no fallout.
func FalloutHunk(h diffcheck.Hunk, renames map[string]string, removed []string) bool {
	var dels, adds []string
	for _, l := range h.Lines {
		if len(l) == 0 {
			continue
		}
		switch l[0] {
		case '-':
			dels = append(dels, l[1:])
		case '+':
			adds = append(adds, l[1:])
		}
	}
	if len(dels) == 0 && len(adds) == 0 {
		return true
	}
	used := make([]bool, len(adds))
	var unpaired []string
	for _, d := range dels {
		want := renameNames(d, renames)
		at := -1
		for i, a := range adds {
			if !used[i] && a == want {
				at = i
				break
			}
		}
		if at >= 0 {
			used[at] = true
			continue
		}
		unpaired = append(unpaired, d)
	}
	if len(unpaired) > 0 {
		// a hunk that only drops references to a name the card removed is fallout too:
		// it adds nothing, and every line it drops names the removed name
		if len(adds) > 0 {
			return false
		}
		for _, d := range unpaired {
			if !namesOne(d, removed) {
				return false
			}
		}
	}
	for _, u := range used {
		if !u {
			return false // a line added with no removed line it follows
		}
	}
	return true
}

// renameNames is s with every old name replaced by its new one, word-bounded, so a
// renamed identifier or path is followed exactly and Foo is never replaced inside FooBar
// (tla/Fallout.tla, PureRefs).
func renameNames(s string, renames map[string]string) string {
	if len(renames) == 0 {
		return s
	}
	names := make([]string, 0, len(renames))
	for old := range renames {
		names = append(names, old)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(names[i]) != len(names[j]) {
			return len(names[i]) > len(names[j])
		}
		return names[i] < names[j]
	})
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	re := regexp.MustCompile(`\b(?:` + strings.Join(quoted, "|") + `)\b`)
	return re.ReplaceAllStringFunc(s, func(old string) string { return renames[old] })
}

// namesOne says s mentions one of names, word-bounded.
func namesOne(s string, names []string) bool {
	for _, n := range names {
		if n == "" {
			continue
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`)
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// falloutFiles is the files one change outside a card's PATHS whose every hunk follows
// the card's own rename or deletion (tla/Fallout.tla, FalloutOnly).
func falloutFiles(files []diffcheck.File, outside []string, renames map[string]string, removed []string) []string {
	var out []string
	for _, f := range files {
		if !slices.Contains(outside, f.New) {
			continue
		}
		if len(f.Hunks) == 0 {
			continue // a pure rename has no hunk to read, and is no fallout by itself
		}
		all := true
		for _, h := range f.Hunks {
			if !FalloutHunk(h, renames, removed) {
				all = false
				break
			}
		}
		if all {
			out = append(out, f.New)
		}
	}
	return out
}

// falloutProposal says the files a held attempt proposes are all fallout of its own
// change in the head: every proposed file the head changes has only FalloutHunk hunks,
// and the change it follows (a rename or a removal) is inside the card's PATHS. The head
// diff is the work card's recorded diff (the member's cardDiff), and no diff is no
// fallout: the rule never widens on a guess (tla/Fallout.tla, Widen).
func falloutProposal(s *Snapshot, pr *Card, globs []string) bool {
	if len(globs) == 0 {
		return false
	}
	wc := s.Fleet.Card(pr.F("work"))
	if wc == nil {
		return false
	}
	diff := wc.F("diff")
	if strings.TrimSpace(diff) == "" {
		return false
	}
	own := headerGlobs(pr.F("brief"), cardhdr.KeyPaths)
	renames, removed := CardRenames(diff, own)
	if len(renames) == 0 && len(removed) == 0 {
		return false
	}
	files := diffcheck.Parse(diff)
	seen := 0
	for _, g := range globs {
		matched := false
		for _, f := range files {
			if !diffcheck.Covers(g, f.New) && !diffcheck.Covers(g, f.Old) {
				continue
			}
			matched = true
			seen++
			if f.Old != f.New {
				continue // a git rename is the card's own path change
			}
			for _, h := range f.Hunks {
				if !FalloutHunk(h, renames, removed) {
					return false
				}
			}
		}
		if !matched {
			return false
		}
	}
	return seen > 0
}

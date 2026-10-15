// Package card is what a brief's writers hold every brief to before it leaves: its
// PATHS computed from its START files, never typed by a hand, and the card checks
// (docs/SPEC-CARD-CONTRACT.md section 6). nova-card generate computes and checks
// every brief it renders, nova-card lint checks a brief on file, and nova-sprint add
// holds every brief it admits to the same checks.
package card

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/cardtree"
)

// PackagePaths is a card's PATHS computed from what it starts from: every directory a
// START file lives in, as that package's Go files and its tests (<dir>/*.go,
// <dir>/*_test.go); a START entry that is a directory, the same; and each doc the card
// names, as itself. A START file that is no Go file (a ledger, a manifest) is kept as
// itself, and a bare word (no slash and no suffix: a tool name, a namedpaths word)
// names no path. A package is the unit a change lives in: a typed file list one file
// short holds the card at land (E12).
func PackagePaths(start, docs []string) []string {
	var paths []string
	for _, s := range start {
		s = path.Clean(strings.TrimPrefix(strings.TrimSpace(s), "./"))
		switch {
		case s == "" || s == ".":
		case strings.HasSuffix(s, ".go"):
			paths = append(paths, packageGlobs(path.Dir(s))...)
		case path.Ext(s) == "" && strings.Contains(s, "/"):
			paths = append(paths, packageGlobs(s)...)
		case strings.Contains(s, "/") || path.Ext(s) != "":
			paths = append(paths, s)
		}
	}
	paths = append(paths, docs...)
	return cardgen.MergePaths(paths)
}

// packageGlobs is a package directory's Go files and its tests.
func packageGlobs(dir string) []string {
	if dir == "." {
		return []string{"*.go", "*_test.go"}
	}
	return []string{dir + "/*.go", dir + "/*_test.go"}
}

// Docs are the entries of a planned PATHS that are neither Go nor a glob nor a
// directory: the ledger, docs/CLI.md. They ride beside the START packages.
func Docs(paths []string) []string {
	var docs []string
	for _, p := range paths {
		if strings.HasSuffix(p, ".go") || strings.ContainsAny(p, "*?") || path.Ext(p) == "" {
			continue
		}
		docs = append(docs, p)
	}
	return docs
}

// Start is a brief's START: line, its entries split on commas and blanks; nil when the
// brief has none.
func Start(brief string) []string {
	return headerList(brief, "START")
}

// Paths is the PATHS of the brief c renders to under h, computed: PackagePaths of the
// START line that brief carries, and the docs the planner named. No hand types the
// PATHS of a generated brief.
func Paths(h cardgen.Header, c cardgen.Card) []string {
	return PackagePaths(Start(cardgen.Render(h, c)), Docs(c.Paths))
}

// Answered says a PATHS entry names nothing at the base because the card creates it
// (its NEW: line). A package's Go files are not answered by a test file the card
// creates: a package with no Go file at the base is not there.
func Answered(c cardgen.Card, glob string) bool {
	for _, n := range c.New {
		if strings.HasSuffix(glob, "/*.go") && strings.HasSuffix(n, "_test.go") {
			continue
		}
		if ok, _ := path.Match(glob, n); ok {
			return true
		}
	}
	return false
}

// Shared says two cards name one PATHS entry and neither needs the other: the add
// wants --allow-shared-paths.
func Shared(cards []cardgen.Card) bool {
	owner := map[string][]int{}
	for i, c := range cards {
		for _, p := range c.Paths {
			for _, j := range owner[p] {
				if !needs(cards[i], cards[j].ID) && !needs(cards[j], c.ID) {
					return true
				}
			}
			owner[p] = append(owner[p], i)
		}
	}
	return false
}

func needs(c cardgen.Card, id string) bool {
	for _, d := range c.Deps {
		if d == id {
			return true
		}
	}
	return false
}

// Options are what the checks read from outside the brief: the names of the people,
// friends and machines of the deployment (none is written in this tree, internal/ci
// TestGeneralityText) and the ids of the cards dropped off the table.
type Options struct {
	Names   []string
	Dropped []string
}

// Lint is cardgen.Lint and Checks under o: everything a brief is held to before it
// leaves its writer.
func Lint(id, brief string, o Options) []cardgen.LintFinding {
	return append(cardgen.Lint(id, brief), Checks(id, brief, o)...)
}

// Checks are the card checks, past the add's own lint:
//
//   - tier-line: line 1 names a tier (`tier: flash|pro|heavy|frontier`);
//   - test-outside-paths: the TEST line's package is a directory PATHS names, so the
//     test the card lands with is one it may edit (a `ctest <Name>` TEST has no
//     package and is not held to a directory, its test file held by the PATHS checks);
//   - personal-name: no name of o.Names outside a double-quoted span (the owner's
//     words, quoted, keep theirs), the card's own id, the WHO: line that pins a
//     friend, or a path token (a whitespace- or comma-separated token containing a slash
//     or ending in .md); names inside paths are fine, names in prose are not;
//   - dropped-card: no id of o.Dropped but the card's own;
//   - author-name: no `By:` followed by a name of o.Names, quoted or not: a brief
//     never names its author, the friend who does the work signs with their own name
//     (cardgen.Attribution), and the WHO: line stays a preference.
//
// The tier and TEST checks hold a card brief, one with a PATHS: line; a brief with
// none is a free task.
func Checks(id, brief string, o Options) []cardgen.LintFinding {
	var out []cardgen.LintFinding
	add := func(check string, line int, excerpt string) {
		out = append(out, cardgen.LintFinding{ID: id, Check: check, Line: line, Excerpt: excerpt})
	}
	if paths := headerList(brief, "PATHS"); paths != nil {
		if m, _ := cardhdr.ReadModel(brief); m.Tier == "" {
			add("tier-line", 1, "line 1 names no tier; write tier: and one of "+cardhdr.RouteList+" on it")
		}
		if pkg := TestPackage(brief); pkg != "" && !covers(paths, pkg) {
			add("test-outside-paths", headerLine(brief, "TEST"), "the TEST package "+pkg+" is no directory PATHS names ("+strings.Join(paths, ", ")+"); a card lands with a test it may edit")
		}
	}
	for i, line := range strings.Split(brief, "\n") {
		if strings.HasPrefix(line, "WHO:") {
			continue // the friend a card is pinned to is named here (cardhdr.ReadWho)
		}
		scan := strings.ToLower(tokenExemptPaths(unquoted(line)))
		if id != "" {
			scan = strings.ReplaceAll(scan, strings.ToLower(id), " ")
		}
		for _, n := range o.Names {
			if n = strings.ToLower(strings.TrimSpace(n)); n != "" && hasWord(scan, n) {
				add("personal-name", i+1, "names "+n+" outside the owner's quoted words; a name inside a path is fine, but a name in prose is not")
			}
		}
		if n := authorName(line, o.Names); n != "" {
			add("author-name", i+1, "names "+n+" as the author on a By: line; a commit names the worker who did the work, so write By: your own name, the worker who does this attempt")
		}
		for _, d := range o.Dropped {
			if d != "" && d != id && hasWord(line, d) {
				add("dropped-card", i+1, "names "+d+", a card dropped off the table; a brief stands alone and owes nothing to a dropped card")
			}
		}
	}
	// The gate-host rule (docs/SPEC-SPRINT.md, the gate-host rule): the gate runs on the
	// member the card is dealt to, inside the wall, never over ssh, so a STEP that tells the
	// worker to ssh to a named host or run the gate "on <host>" is refused with the remedy
	// naming GATE-NEEDS.
	for i, line := range strings.Split(brief, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "STEP:") {
			continue
		}
		if gateNeedsHost(line, o.Names) {
			add(GateHostCheck, i+1, "this STEP tells the worker to ssh to a named host or run the gate on a named host, which the sprint forbids; "+GateHostRemedy)
		}
	}
	return out
}

// GateHostCheck is the check name of the gate-host lint finding, and GateHostRemedy its
// remedy (docs/SPEC-SPRINT.md, the gate-host rule): a brief's gate runs on the member it is
// dealt to, inside the wall, never over ssh to a named host, so the brief names the gate's
// needs on a GATE-NEEDS line instead of naming a host.
const (
	GateHostCheck  = "gate-needs-host"
	GateHostRemedy = "the gate runs on the member it is dealt to, inside the wall, never over ssh to a named host; name the gate's needs on a `GATE-NEEDS:` line (e.g., `GATE-NEEDS: linux, tla`) so the deal matches a member"
)

// Remedy returns the one-line remedy for a check token drawn by card lint.
func Remedy(check string) string {
	if check == GateHostCheck {
		return GateHostRemedy
	}
	if r, ok := cardtree.Remedies[check]; ok && r != "" {
		return r
	}
	if r, ok := swarm.CardHeaderRemedies[check]; ok && r != "" {
		return r
	}
	if r := swarm.ChildRemedy(swarm.DefaultChildRules, check); r != "" {
		return r
	}
	if r := swarm.BriefRemedy(check); r != "" {
		return r
	}
	switch check {
	case "tier-line":
		return "line 1 names the card's tier: write tier: and one of " + cardhdr.RouteList + " on it"
	case "test-outside-paths":
		return "the TEST package must be a directory that PATHS names; widen PATHS or change the TEST package"
	case "personal-name":
		return "remove the personal or friend name from prose; a name inside a path or a double-quoted word is fine"
	case "author-name":
		return "do not name the author on a By: line; sign the attempt with your own name"
	case "dropped-card":
		return "remove reference to the dropped card; a brief stands alone and owes nothing to a dropped card"
	case "model-lines":
		return "line 1 names the card's tier and model invariants: write tier: and model invariants on line 1"
	case "placeholder":
		return "fill it in before the card is handed out: replace each <...> with the card's value"
	case "sibling-path":
		return "name the sibling repository in a SIBLINGS header (owner/name@ref, comma separated)"
	case "start-named":
		return swarm.StartNamedRemedy
	case "stop-named":
		return swarm.StopNamedRemedy
	case "no-parent-path":
		return swarm.CardParentPathWanted
	case "depends-on":
		return "the card carries DEPENDS-ON: <id>[, <id>...] naming cards this card needs, or DEPENDS-ON: none"
	case "result-first":
		return "line 1 IS the contract: " + swarm.CardContractWanted + ". A title, a heading or a # comment on line 1 is this drift, however right the words are"
	case "clone-step":
		return "STEP 1 enters the repository from the working directory: the whole step, its line and the lines under it, holds a git clone -q <url> repo && cd repo, or a cd into a checkout that may already be there"
	case "steps-numbered":
		return "each step is its own line beginning STEP <n>., numbered 1, 2, 3 with no gap and no repeat"
	case "red-test":
		return "name the reproducing test by its own name -- TestSomething -- or, for a card that only reads, say probe or read in so many words"
	case "test-command":
		return "write the gate verbatim, exactly as the card is to run it"
	case "deadline":
		return "give the card its own bound: a deadline line, or finish within <n> minutes"
	case "files-named":
		return "name the file or the package the work lives in, so the change has a home to start from"
	case "scratch-absolute":
		return "spell scratch against a named root -- <job>/scratch, $PWD/scratch, an absolute path -- and never as the bare word"
	case "no-sandbox":
		return "a card runs INSIDE the wall and never invokes it; drop the nova-sandbox line"
	case "result-last":
		return "the LAST step writes RESULT.md, and RESULT.md's own line 1 is the contract line from line 1 of this card"
	case "size":
		return "keep the brief within budget: point at a file instead of pasting it, and drop quoted source"
	}
	return "resolve the " + check + " finding"
}

// gateNeedsHostRE finds `ssh <token>` or `on <token>` in a STEP line; the token is held
// host-like by gateNeedsHost, so an ordinary "on the member" (no host) passes.
var gateNeedsHostRE = regexp.MustCompile(`(?i)\b(ssh|on)\s+([a-z][a-z0-9-]*)\b`)

// gateNeedsHost says a STEP line names a host it runs its gate on: `ssh <host>` or
// `on <host>`, where host is host-like (hostLike).
func gateNeedsHost(line string, names []string) bool {
	for _, m := range gateNeedsHostRE.FindAllStringSubmatch(line, -1) {
		if hostLike(m[2], names) {
			return true
		}
	}
	return false
}

// hostLike says tok is a host the lint refuses to run a gate on: a fleet member/host
// name, or a token that carries a digit or hyphen and is not a stop word ("the", "a",
// "this", "its"). "the" in "on the member" names no host and passes.
func hostLike(tok string, names []string) bool {
	tok = strings.ToLower(tok)
	for _, n := range names {
		if strings.ToLower(strings.TrimSpace(n)) == tok {
			return true
		}
	}
	switch tok {
	case "the", "a", "this", "its":
		return false
	}
	return strings.ContainsAny(tok, "0123456789-")
}

// byRE is a By: trailer named in a line and the word after it, past any <, backquote,
// quote or asterisk; a By: inside a longer key (Co-Authored-By:) is no By:.
var byRE = regexp.MustCompile("(?i)(?:^|[^A-Za-z0-9_-])by:[\\s<`'\"*]*([A-Za-z0-9_.-]+)")

// authorName is the name of names a By: in line gives as the author ("" for none). A
// friend's row name may be written friend.<name>.
func authorName(line string, names []string) string {
	for _, m := range byRE.FindAllStringSubmatch(line, -1) {
		w := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(m[1]), "friend."), ".")
		for _, n := range names {
			if n = strings.ToLower(strings.TrimSpace(n)); n != "" && w == n {
				return n
			}
		}
	}
	return ""
}

// TestPackage is the package a brief's TEST line names, repository-relative with no
// ./ and no trailing slash: the first field that is a path (`pkg/bus TestY`,
// `./pkg/bus TestY`, `./tools TestY`, `go test ./pkg/bus -run TestY`); "" for
// TEST: none, a `ctest <Name>` line (a ctest test has no package and is not held
// to a directory), or a line that names none.
func TestPackage(brief string) string {
	value := ""
	for _, line := range strings.Split(brief, "\n") {
		if v, ok := strings.CutPrefix(line, "TEST:"); ok {
			value = strings.TrimSpace(v)
			break
		}
	}
	if tl, _ := cardhdr.ParseTest(value); tl.Runner == "ctest" {
		return ""
	}
	for _, f := range strings.Fields(value) {
		dotted := strings.HasPrefix(f, "./")
		f = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(f, "./"), "/..."), "/")
		if strings.HasPrefix(f, "-") || f == "" || !dotted && !strings.Contains(f, "/") {
			continue
		}
		return path.Clean(f)
	}
	return ""
}

// covers says some PATHS entry lies in the directory pkg or under it.
func covers(paths []string, pkg string) bool {
	for _, p := range paths {
		if dir := path.Dir(strings.TrimPrefix(p, "./")); dir == pkg || strings.HasPrefix(dir, pkg+"/") {
			return true
		}
	}
	return false
}

// headerList is a header line's comma- or space-separated entries, nil when the brief
// has no such line.
func headerList(brief, key string) []string {
	value, ok := swarm.CardHeaderValue([]byte(brief), key)
	if !ok {
		return nil
	}
	out := []string{}
	for _, p := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if p != "none" && p != "-" {
			out = append(out, p)
		}
	}
	return out
}

// headerLine is the 1-based line of a brief's KEY: line, 1 when it has none.
func headerLine(brief, key string) int {
	for i, line := range strings.Split(brief, "\n") {
		if strings.HasPrefix(line, key+":") {
			return i + 1
		}
	}
	return 1
}

var quotedRE = regexp.MustCompile(`"[^"]*"|“[^”]*”`)

// unquoted is a line with its double-quoted spans blanked: the owner's words, quoted,
// are theirs to keep.
func unquoted(line string) string {
	return quotedRE.ReplaceAllStringFunc(line, func(q string) string { return strings.Repeat(" ", len(q)) })
}

// tokenExemptPaths returns s with path tokens blanked: any whitespace- or comma-separated
// token containing a slash or ending in .md is a path token; names inside paths are fine.
func tokenExemptPaths(s string) string {
	var out []string
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if strings.Contains(tok, "/") || strings.HasSuffix(tok, ".md") {
			out = append(out, strings.Repeat(" ", len(tok)))
		} else {
			out = append(out, tok)
		}
	}
	return strings.Join(out, " ")
}

// hasWord says w is in s as a word of its own: no letter, digit or underscore on
// either side, so a name inside another word is no name.
func hasWord(s, w string) bool {
	for from := 0; ; {
		i := strings.Index(s[from:], w)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(w)
		if !wordRune(s, start-1) && !wordRune(s, end) {
			return true
		}
		from = start + 1
	}
}

func wordRune(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := rune(s[i])
	return c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c)
}

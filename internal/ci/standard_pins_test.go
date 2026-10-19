package ci

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/constant"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// The nova-sprint standard's checker (standard/STYLE.md; card sprint-spec-skeleton). The
// owner, 2026-10-18: "it seems like we really need to create a SPEC for nova-sprint don't
// you thinK? ... We keep drifting." NS-1 (standard/SPRINT.md and standard/sprint/<id>.md) is
// normative; every row with MUST, MUST NOT, SHALL or REQUIRED is pinned in
// standard/pins/<id>.tsv to a Go test, a TLA+ invariant, a Go constant, a test in another
// repository, or a divergence naming its fix card and the test it owes. These tests read
// files only (the unit tier): they fail on an unpinned row, a stale pin, a heading off the
// outline in standard/STYLE.md, and the language STYLE.md refuses. The reversed witnesses
// are the fixtures in internal/ci/testdata/standard/README.md.

// stdRoot is the repository root, read through the file system the checks are given.
const stdRoot = "../.."

// stdFixtures holds the reversed witnesses, one fixture a fenced block (README.md there).
const stdFixtures = "testdata/standard/README.md"

// TestTheStandardPinsEveryRequirement: every NS-1 row that binds is pinned, every pin names
// a row of its own section and holds against the code, and every hard row of STYLE.md's
// table is in its section, marked hard, binding and pinned. Each divergence prints one
// STANDARD DIVERGENCE line and does not fail; each pending section and each external pin is
// listed.
func TestTheStandardPinsEveryRequirement(t *testing.T) {
	t.Parallel()
	c := newStdCheck(os.DirFS(stdRoot))
	c.pins()
	for _, s := range c.sections {
		if s.pending {
			fmt.Printf("STANDARD PENDING %s %s\n", s.id, s.heading)
		}
	}
	for _, line := range c.external {
		fmt.Println(line)
	}
	for _, line := range c.divergences {
		fmt.Println(line)
	}
	require.Empty(t, c.problems, "the standard's pins:\n%s", strings.Join(c.problems, "\n"))
}

// TestTheStandardOutlineMatches: standard/SPRINT.md's index, every section file's heading
// and every rationale file's heading are the outline of standard/STYLE.md, in its order, and
// no section or pin file lies outside it.
func TestTheStandardOutlineMatches(t *testing.T) {
	t.Parallel()
	c := newStdCheck(os.DirFS(stdRoot))
	c.outline()
	require.Empty(t, c.problems, "the standard's outline:\n%s", strings.Join(c.problems, "\n"))
}

// TestTheStandardLanguage: NS-1 carries no lower-case must, shall, should or required, no
// key word outside a row, no history, dates, names, paths, test or model names and no Go
// identifiers, and states BCP 14 once, in s1; NS-R carries no capitalised key word.
func TestTheStandardLanguage(t *testing.T) {
	t.Parallel()
	c := newStdCheck(os.DirFS(stdRoot))
	c.language()
	require.Empty(t, c.problems, "the standard's language:\n%s", strings.Join(c.problems, "\n"))
}

// TestTheStandardRefusesEachReversedWitness: the clean fixture passes every check, and each
// witness fails with a line naming its row or section.
func TestTheStandardRefusesEachReversedWitness(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(stdFixtures)
	require.NoError(t, err)
	fixtures := parseStdFixtures(string(src))
	require.Contains(t, fixtures, "base")

	all := func(name string) []string {
		files := fstest.MapFS{}
		for p, body := range fixtures["base"] {
			files[p] = &fstest.MapFile{Data: []byte(body)}
		}
		if name != "base" {
			require.Contains(t, fixtures, name, "a fixture named %s", name)
			for p, body := range fixtures[name] {
				files[p] = &fstest.MapFile{Data: []byte(body)}
			}
		}
		c := newStdCheck(files)
		c.outline()
		c.language()
		c.pins()
		return c.problems
	}
	require.Empty(t, all("base"), "the clean fixture passes")

	// each witness and the line it fails with: who it names (its row or section), what it says
	cases := map[string]struct{ who, says string }{
		"unpinned-must":          {"R-LOOSE", "a binding row with no pin"},
		"missing-test":           {"R-ONE", "test ./pkg/x TestGone is declared in no _test.go file"},
		"wrong-constant":         {"R-ONE", "const ./pkg/x Width is 4, the pin says 5"},
		"divergence-test-exists": {"R-HARD-ONE", "divergence fix-card owes ./pkg/x TestXHolds, which exists"},
		"heading-off-outline":    {"s2", `standard/sprint/s2.md heading is "# s2. Other things", the outline says "# s2. Things"`},
		"lower-case-must":        {"R-ONE", `lower-case "must"`},
		"tla-undefined":          {"R-ONE", "tla X does not define Unsafe"},
		"tla-not-gated":          {"R-ONE", "tla X Unchecked: no required case of tla/CASES.tsv expecting pass checks it"},
		"tla-not-extended":       {"R-ONE", "tla X Safe: no required case of tla/CASES.tsv expecting pass checks it"},
		"tla-bench-only":         {"R-ONE", "tla X Safe: no required case of tla/CASES.tsv expecting pass checks it"},
		"tla-only-a-constant":    {"R-ONE", "tla X Safe: no required case of tla/CASES.tsv expecting pass checks it"},
		"skipped-test":           {"R-ONE", "test ./pkg/x TestXHolds does not run in the unit tier: pkg/x/x_test.go: it calls t.Skip"},
		"build-tagged-test":      {"R-ONE", "test ./pkg/x TestXHolds does not run in the unit tier: pkg/x/x_test.go: its file builds only under functional"},
		"owed-test-elsewhere":    {"R-HARD-ONE", "divergence fix-card owes ./pkg/x TestXNeverWaits, which exists in cmd/y/y_test.go"},
		"hard-row-removed":       {"R-HARD-ONE", "hard row H1 is not in s2"},
		"pin-in-wrong-section":   {"R-ONE", "pinned in standard/pins/s2.tsv, the row is in s1"},
		"key-word-outside-a-row": {"s1", "a key word outside a row"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := all(name)
			require.True(t, slices.ContainsFunc(got, func(p string) bool {
				return strings.HasPrefix(p, want.who+": ") && strings.Contains(p, want.says)
			}), "the witness %s fails naming %s: want a line %q, got:\n%s", name, want.who, want.says, strings.Join(got, "\n"))
		})
	}
}

// parseStdFixtures reads the fixtures: a block opens with a line "~~~ file <fixture> <path>"
// and ends with a line "~~~"; a fixture is the base with its own files laid over it.
func parseStdFixtures(src string) map[string]map[string]string {
	out := map[string]map[string]string{}
	var name, file string
	var body []string
	in := false
	for _, line := range strings.Split(src, "\n") {
		switch {
		case !in && strings.HasPrefix(line, "~~~ file "):
			f := strings.Fields(line)
			if len(f) == 4 {
				name, file, body, in = f[2], f[3], nil, true
			}
		case in && line == "~~~":
			if out[name] == nil {
				out[name] = map[string]string{}
			}
			out[name][file] = strings.Join(body, "\n") + "\n"
			in = false
		case in:
			body = append(body, line)
		}
	}
	return out
}

// stdSection is one row of the outline and what its NS-1 file says of it.
type stdSection struct {
	id, heading string
	pending     bool
}

// stdRow is one numbered requirement row of NS-1.
type stdRow struct {
	sec, name, text, file string
	num, line           int
	hard, binds         bool
}

// stdHard is one row of STYLE.md's hard table.
type stdHard struct{ id, name, sec string }

// stdCheck is one run of the checks over a file system holding a repository.
type stdCheck struct {
	fsys        fs.FS
	sections    []stdSection
	hard        []stdHard
	rows        map[string]*stdRow
	order       []string
	problems    []string
	divergences []string
	external    []string
	files       []stdFile // every _test.go file of the repository, read once
}

var (
	stdOutlineRE = regexp.MustCompile(`^\|\s*(s\d+)\s*\|\s*([^|]+?)\s*\|$`)
	stdHardRE    = regexp.MustCompile(`^\|\s*(H\d+)\s*\|\s*(R-[A-Z0-9-]+)\s*\|\s*(s\d+)\s*\|$`)
	stdIndexRE   = regexp.MustCompile(`^- \[(s\d+) (.+)\]\(sprint/(s\d+)\.md\)$`)
	stdRowRE     = regexp.MustCompile(`^\*\*NS-(\d+)\.(\d+) (R-[A-Z0-9-]+)\.\*\*(.*)$`)
	stdAnchorRE  = regexp.MustCompile(`^<a id="(R-[A-Z0-9-]+)"></a>$`)
	stdBindsRE   = regexp.MustCompile(`\b(MUST|SHALL|REQUIRED)\b`)
	stdKeyRE     = regexp.MustCompile(`\b(MUST|SHALL|SHOULD|REQUIRED|RECOMMENDED|MAY|OPTIONAL)\b`)
	stdCapsRE    = regexp.MustCompile(`\b(MUST|SHALL|SHOULD|REQUIRED)\b`)
	stdCardRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	stdRepoRE    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	stdTestRE    = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)
	stdDirRE     = regexp.MustCompile(`^\./[A-Za-z0-9_./-]+$`)
	stdNameRE    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// stdRefusals is NS-1's language rule (STYLE.md sections 5 and 6): each pattern and what it
// refuses.
var stdRefusals = []struct {
	re  *regexp.Regexp
	why string
}{
	{regexp.MustCompile(`\b(must|shall|should|required)\b`), "lower-case %q: write the key word in capitals or state a fact"},
	{regexp.MustCompile(`\b20\d\d-\d\d-\d\d\b`), "a date %q: history goes to NS-R"},
	{regexp.MustCompile(`\b[A-Za-z0-9_.-]+\.(go|tla|tsv|cfg|sh|py|json|yaml|yml|sexp)\b`), "a file name %q: paths go to the pins and NS-R"},
	{regexp.MustCompile(`\b(internal|cmd|pkg|tla|docs|tools)/[A-Za-z0-9_./-]*`), "a path %q: paths go to the pins and NS-R"},
	{regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9_]*`), "a test name %q: tests go to the pins"},
	{regexp.MustCompile(`TLA\+`), "%q: models go to the pins"},
	{regexp.MustCompile(`\b(Glenn|Fiedler|Patrick|Rowan|Stella|Cairn|Terra|hulk|captain|hetzner1|amd-\d+|mini-m\d+)\b`), "a name %q: names go to NS-R"},
	{regexp.MustCompile(`\b[a-z]+[A-Z][A-Za-z0-9]*\b`), "a Go identifier %q: identifiers go to the pins and NS-R"},
	{regexp.MustCompile(`\b[A-Z][a-z0-9]+[A-Z][A-Za-z0-9]*\b`), "a Go identifier %q: identifiers go to the pins and NS-R"},
	{regexp.MustCompile(`\b(previously|formerly|used to|no longer)\b`), "history %q: history goes to NS-R"},
}

func newStdCheck(fsys fs.FS) *stdCheck {
	c := &stdCheck{fsys: fsys, rows: map[string]*stdRow{}}
	style, err := fs.ReadFile(fsys, "standard/STYLE.md")
	if err != nil {
		c.fail("standard/STYLE.md: %v", err)
		return c
	}
	c.sections = stdMarked(string(style), "outline", func(line string) (stdSection, bool) {
		m := stdOutlineRE.FindStringSubmatch(line)
		if m == nil {
			return stdSection{}, false
		}
		return stdSection{id: m[1], heading: m[2]}, true
	})
	if len(c.sections) == 0 {
		c.fail("standard/STYLE.md: no outline between <!-- outline:begin --> and <!-- outline:end -->")
	}
	c.hard = stdMarked(string(style), "hard", func(line string) (stdHard, bool) {
		m := stdHardRE.FindStringSubmatch(line)
		if m == nil {
			return stdHard{}, false
		}
		return stdHard{id: m[1], name: m[2], sec: m[3]}, true
	})
	for i := range c.sections {
		c.readSection(&c.sections[i])
	}
	return c
}

// stdMarked parses the lines between <!-- <mark>:begin --> and <!-- <mark>:end -->.
func stdMarked[T any](src, mark string, parse func(string) (T, bool)) []T {
	var out []T
	in := false
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch line {
		case "<!-- " + mark + ":begin -->":
			in = true
			continue
		case "<!-- " + mark + ":end -->":
			in = false
		}
		if in {
			if v, ok := parse(line); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

func (c *stdCheck) fail(format string, args ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, args...))
}

func (c *stdCheck) read(p string) (string, bool) {
	b, err := fs.ReadFile(c.fsys, p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// readSection reads the section's NS-1 file: whether it is pending, and its rows.
func (c *stdCheck) readSection(s *stdSection) {
	file := "standard/sprint/" + s.id + ".md"
	src, ok := c.read(file)
	if !ok {
		return
	}
	lines := strings.Split(src, "\n")
	head := stdHeadingAt(lines)
	for _, l := range lines[min(head+1, len(lines)):] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		s.pending = strings.TrimSpace(l) == "Status: pending"
		break
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(s.id, "s"))
	for i := 0; i < len(lines); i++ {
		m := stdRowRE.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[2])
		r := &stdRow{sec: s.id, name: m[3], file: file, line: i + 1, num: num}
		text := []string{m[4]}
		for j := i + 1; j < len(lines) && strings.TrimSpace(lines[j]) != ""; j++ {
			text = append(text, lines[j])
		}
		r.text = strings.Join(text, " ")
		r.hard = strings.HasPrefix(strings.TrimSpace(m[4]), "(hard)")
		r.binds = stdBindsRE.MatchString(r.text)
		switch sec, _ := strconv.Atoi(m[1]); {
		case sec != n:
			c.fail("%s: %s:%d: its id NS-%s.%s is not of section %s", r.name, file, r.line, m[1], m[2], s.id)
		case i == 0 || stdAnchorRE.FindStringSubmatch(strings.TrimSpace(lines[i-1])) == nil || stdAnchorRE.FindStringSubmatch(strings.TrimSpace(lines[i-1]))[1] != r.name:
			c.fail("%s: %s:%d: the row has no anchor <a id=%q></a> on the line before it", r.name, file, r.line, r.name)
		}
		if was, dup := c.rows[r.name]; dup {
			c.fail("%s: %s:%d: the anchor is used again (first %s:%d)", r.name, file, r.line, was.file, was.line)
			continue
		}
		for _, o := range c.order {
			if o := c.rows[o]; o.sec == r.sec && o.num == r.num {
				c.fail("%s: %s:%d: NS-%d.%d is used again (first %s)", r.name, file, r.line, n, num, o.name)
			}
		}
		c.rows[r.name] = r
		c.order = append(c.order, r.name)
	}
}

// stdHeadingAt is the index of the file's first "# " heading line, -1 when none.
func stdHeadingAt(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, "# ") {
			return i
		}
	}
	return -1
}

// outline checks SPRINT.md's index and every section and rationale heading against the
// outline, and that no section, rationale or pin file lies outside it.
func (c *stdCheck) outline() {
	index, ok := c.read("standard/SPRINT.md")
	if !ok {
		c.fail("standard/SPRINT.md is missing")
	}
	var got []stdSection
	for _, line := range strings.Split(index, "\n") {
		if m := stdIndexRE.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if m[1] != m[3] {
				c.fail("%s: standard/SPRINT.md links %s to sprint/%s.md", m[1], m[1], m[3])
			}
			got = append(got, stdSection{id: m[1], heading: m[2]})
		}
	}
	for i, s := range c.sections {
		switch {
		case i >= len(got):
			c.fail("%s: standard/SPRINT.md's index has no line for %s %s", s.id, s.id, s.heading)
		case got[i].id != s.id || got[i].heading != s.heading:
			c.fail("%s: standard/SPRINT.md's index line %d is %s %s, the outline says %s %s", s.id, i+1, got[i].id, got[i].heading, s.id, s.heading)
		}
		for _, doc := range []struct{ dir, suffix string }{{"sprint", ""}, {"rationale", " (rationale)"}} {
			file := "standard/" + doc.dir + "/" + s.id + ".md"
			src, ok := c.read(file)
			if !ok {
				c.fail("%s: %s is missing", s.id, file)
				continue
			}
			lines := strings.Split(src, "\n")
			h := stdHeadingAt(lines)
			want := "# " + s.id + ". " + s.heading + doc.suffix
			switch {
			case h < 0:
				c.fail("%s: %s has no heading, the outline says %q", s.id, file, want)
			case lines[h] != want:
				c.fail("%s: %s heading is %q, the outline says %q", s.id, file, lines[h], want)
			case h == 0 || strings.TrimSpace(lines[h-1]) != `<a id="`+s.id+`"></a>`:
				c.fail("%s: %s has no anchor <a id=%q></a> on the line before its heading", s.id, file, s.id)
			}
		}
		if s.pending {
			src, _ := c.read("standard/sprint/" + s.id + ".md")
			if !regexp.MustCompile(`(?m)^Scope: \S`).MatchString(src) {
				c.fail("%s: a pending section carries a Scope: sentence", s.id)
			}
		}
	}
	for i := len(c.sections); i < len(got); i++ {
		c.fail("%s: standard/SPRINT.md's index names %s, which the outline does not", got[i].id, got[i].id)
	}
	known := map[string]bool{}
	for _, s := range c.sections {
		known[s.id] = true
	}
	for _, dir := range []struct{ name, ext string }{{"standard/sprint", ".md"}, {"standard/rationale", ".md"}, {"standard/pins", ".tsv"}} {
		entries, _ := fs.ReadDir(c.fsys, dir.name)
		for _, e := range entries {
			id := strings.TrimSuffix(e.Name(), dir.ext)
			if !e.IsDir() && strings.HasSuffix(e.Name(), dir.ext) && !known[id] {
				c.fail("%s: %s/%s is not a section of the outline", id, dir.name, e.Name())
			}
		}
	}
}

// language checks NS-1's and NS-R's words (STYLE.md sections 5 and 6).
func (c *stdCheck) language() {
	files := []string{"standard/SPRINT.md"}
	for _, s := range c.sections {
		files = append(files, "standard/sprint/"+s.id+".md")
	}
	bcp := 0
	for _, file := range files {
		src, ok := c.read(file)
		if !ok {
			continue
		}
		sec := strings.TrimSuffix(path.Base(file), ".md")
		lines := strings.Split(src, "\n")
		for start := 0; start < len(lines); {
			end := start
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
				end++
			}
			para := lines[start:end]
			row, isBCP := "", strings.Contains(strings.Join(para, " "), "BCP 14")
			for _, l := range para {
				if m := stdRowRE.FindStringSubmatch(l); m != nil {
					row = m[3]
				}
			}
			if isBCP {
				if file == "standard/sprint/s1.md" {
					bcp++
				} else {
					c.fail("%s: %s:%d: the BCP 14 sentence is stated once, in s1", sec, file, start+1)
				}
			}
			who := sec
			if row != "" {
				who = row
			}
			for k, line := range para {
				n := start + k + 1
				if isBCP || strings.HasPrefix(strings.TrimSpace(line), `<a id="`) {
					continue
				}
				if row == "" && stdKeyRE.MatchString(line) {
					c.fail("%s: %s:%d: a key word outside a row: %q", who, file, n, stdKeyRE.FindString(line))
				}
				for _, r := range stdRefusals {
					for _, hit := range r.re.FindAllString(line, -1) {
						c.fail("%s: %s:%d: "+r.why, who, file, n, hit)
					}
				}
			}
			start = end + 1
		}
	}
	if bcp != 1 {
		c.fail("s1: standard/sprint/s1.md states the BCP 14 sentence %d times, once is the rule", bcp)
	}
	for _, s := range c.sections {
		file := "standard/rationale/" + s.id + ".md"
		src, _ := c.read(file)
		for i, line := range strings.Split(src, "\n") {
			if k := stdCapsRE.FindString(line); k != "" {
				c.fail("%s: %s:%d: NS-R is informative and carries no key word %q", s.id, file, i+1, k)
			}
		}
	}
}

// pins checks every pin file against the rows and the code, every binding row for a pin,
// and the hard table.
func (c *stdCheck) pins() {
	pinned := map[string]int{}
	for _, s := range c.sections {
		file := "standard/pins/" + s.id + ".tsv"
		src, ok := c.read(file)
		if !ok {
			c.fail("%s: %s is missing", s.id, file)
			continue
		}
		if !strings.HasPrefix(src, "#") {
			c.fail("%s: %s begins with a comment naming its section", s.id, file)
		}
		sc := bufio.NewScanner(strings.NewReader(src))
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) != 4 {
				c.fail("%s: %s:%d: a pin is four tab-separated fields, got %d", s.id, file, n, len(f))
				continue
			}
			name, kind, a, b := f[0], f[1], f[2], f[3]
			r := c.rows[name]
			switch {
			case r == nil:
				c.fail("%s: %s:%d: the pin names no row of the standard", name, file, n)
				continue
			case r.sec != s.id:
				c.fail("%s: pinned in %s, the row is in %s", name, file, r.sec)
			}
			pinned[name]++
			c.pin(name, kind, a, b)
		}
	}
	for _, name := range c.order {
		if r := c.rows[name]; r.binds && pinned[name] == 0 {
			c.fail("%s: %s:%d: a binding row with no pin in standard/pins/%s.tsv", name, r.file, r.line, r.sec)
		}
	}
	listed := map[string]bool{}
	for _, h := range c.hard {
		listed[h.name] = true
		r := c.rows[h.name]
		switch {
		case r == nil || r.sec != h.sec:
			c.fail("%s: hard row %s is not in %s", h.name, h.id, h.sec)
		case !r.hard:
			c.fail("%s: hard row %s is not marked (hard)", h.name, h.id)
		case !strings.Contains(r.text, "MUST"):
			c.fail("%s: hard row %s carries no MUST or MUST NOT", h.name, h.id)
		case pinned[h.name] == 0:
			c.fail("%s: hard row %s has no pin", h.name, h.id)
		}
	}
	for _, name := range c.order {
		if c.rows[name].hard && !listed[name] {
			c.fail("%s: marked (hard) and not in STYLE.md's hard table", name)
		}
	}
}

// pin checks one pin against the code.
func (c *stdCheck) pin(name, kind, a, b string) {
	switch kind {
	case "test":
		switch {
		case !stdDirRE.MatchString(a) || !stdTestRE.MatchString(b):
			c.fail("%s: test pin %q %q is not <package dir> <TestName>", name, a, b)
		default:
			if ok, why := c.testDeclared(a, b); !ok && why == "" {
				c.fail("%s: test %s %s is declared in no _test.go file of the package", name, a, b)
			} else if !ok {
				c.fail("%s: test %s %s does not run in the unit tier: %s", name, a, b, why)
			}
		}
	case "tla":
		c.tla(name, a, b)
	case "const":
		k, v, ok := strings.Cut(b, "=")
		if !stdDirRE.MatchString(a) || !ok || !stdNameRE.MatchString(k) {
			c.fail("%s: const pin %q %q is not <package dir> <Name>=<value>", name, a, b)
			return
		}
		c.constant(name, a, k, v)
	case "external":
		f := strings.Fields(b)
		if !stdRepoRE.MatchString(a) || len(f) != 2 || !stdDirRE.MatchString(f[0]) || !stdTestRE.MatchString(f[1]) {
			c.fail("%s: external pin %q %q is not <owner/repo> <package dir> <TestName>", name, a, b)
			return
		}
		c.external = append(c.external, fmt.Sprintf("STANDARD EXTERNAL %s repo=%s test=%s %s", name, a, f[0], f[1]))
	case "divergence":
		f := strings.Fields(b)
		owed := b
		switch {
		case !stdCardRE.MatchString(a):
			c.fail("%s: divergence pin names no fix card: %q", name, a)
			return
		case len(f) == 4 && f[0] == "external" && stdRepoRE.MatchString(f[1]) && stdDirRE.MatchString(f[2]) && stdTestRE.MatchString(f[3]):
			// owed in another repository: its form is checked only
		case len(f) == 2 && stdDirRE.MatchString(f[0]) && stdTestRE.MatchString(f[1]):
			if at := c.testDecls("", f[1]); len(at) > 0 {
				c.fail("%s: divergence %s owes %s %s, which exists in %s: pin it as a test", name, a, f[0], f[1], at[0].file)
				return
			}
		default:
			c.fail("%s: divergence pin's owed test %q is not <package dir> <TestName> or external <owner/repo> <package dir> <TestName>", name, b)
			return
		}
		c.divergences = append(c.divergences, fmt.Sprintf("STANDARD DIVERGENCE %s fix=%s owed=%s", name, a, strings.Join(strings.Fields(owed), " ")))
	default:
		c.fail("%s: pin kind %q is none of test, tla, const, external, divergence", name, kind)
	}
}

// stdTestAt is where a test is declared and whether the unit tier runs it.
type stdTestAt struct {
	file string // the _test.go file that declares it, relative to the root
	why  string // "" when the unit tier runs it; else why it does not (a skip, a build constraint)
}

// testDecls is every top-level declaration of the test in the _test.go files of dir ("" for
// the whole repository), read with go/parser. Only the files whose text names the function are
// parsed, so the check stays inside the unit tier's time.
func (c *stdCheck) testDecls(dir, test string) []stdTestAt {
	var out []stdTestAt
	fset := token.NewFileSet()
	for _, tf := range c.testFiles() {
		if dir != "" && path.Dir(tf.name) != dir || !strings.Contains(tf.src, "func "+test+"(") {
			continue
		}
		f, err := parser.ParseFile(fset, tf.name, tf.src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name.Name != test {
				continue
			}
			at := stdTestAt{file: tf.name}
			if tags := stdBuildTagsOut(f); tags != "" {
				at.why = "its file builds only under " + tags + ", which the unit tier does not set"
			} else if skip := stdSkips(fn); skip != "" {
				at.why = "it calls " + skip + ", so the unit tier may not run it"
			}
			out = append(out, at)
		}
	}
	return out
}

// testDeclared says a _test.go file of the package directory declares the test, and the unit
// tier runs it: no build constraint leaves the file out and the test calls no Skip.
func (c *stdCheck) testDeclared(dir, test string) (declared bool, why string) {
	for _, at := range c.testDecls(path.Clean(strings.TrimPrefix(dir, "./")), test) {
		if at.why == "" {
			return true, ""
		}
		why = at.file + ": " + at.why
	}
	return false, why
}

// stdUnitTags is what the unit tier's build sets: the gate's platform and the toolchain, no
// custom tag (functional, slow and the rest are left out).
func stdUnitTags(tag string) bool {
	switch tag {
	case "linux", "amd64", "unix", "gc", "cgo":
		return true
	}
	return strings.HasPrefix(tag, "go1.")
}

// stdBuildTagsOut is the file's //go:build line when the unit tier's build leaves it out, ""
// when it builds.
func stdBuildTagsOut(f *ast.File) string {
	for _, g := range f.Comments {
		if g.Pos() > f.Package {
			break
		}
		for _, cm := range g.List {
			if !constraint.IsGoBuild(cm.Text) {
				continue
			}
			x, err := constraint.Parse(cm.Text)
			if err == nil && !x.Eval(stdUnitTags) {
				return strings.TrimPrefix(cm.Text, "//go:build ")
			}
		}
	}
	return ""
}

// stdSkips is the first Skip, Skipf or SkipNow the test calls, "" for none.
func stdSkips(fn *ast.FuncDecl) string {
	found := ""
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Skip", "Skipf", "SkipNow":
					found = "t." + sel.Sel.Name
				}
			}
		}
		return true
	})
	return found
}

// stdFile is one file's name, relative to the root, and text.
type stdFile struct{ name, src string }

// testFiles is every _test.go file of the repository (not under .git, testdata, tmp or
// vendor), read once a check.
func (c *stdCheck) testFiles() []stdFile {
	if c.files != nil {
		return c.files
	}
	c.files = []stdFile{}
	_ = fs.WalkDir(c.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "tmp", "vendor", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, "_test.go") {
			if src, err := fs.ReadFile(c.fsys, p); err == nil {
				c.files = append(c.files, stdFile{p, string(src)})
			}
		}
		return nil
	})
	return c.files
}

// tla checks the module defines the name and a required case of tla/CASES.tsv expecting pass
// checks it: the case's model extends the module, and its cfg names the property under
// INVARIANT(S) or PROPERTY(IES).
func (c *stdCheck) tla(name, module, prop string) {
	src, ok := c.read("tla/" + module + ".tla")
	if !ok {
		c.fail("%s: tla module tla/%s.tla is missing", name, module)
		return
	}
	if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(prop) + `\s*(\([^)]*\))?\s*==`).MatchString(src) {
		c.fail("%s: tla %s does not define %s", name, module, prop)
		return
	}
	plan, ok := c.read("tla/CASES.tsv")
	if !ok {
		c.fail("%s: tla/CASES.tsv is missing", name)
		return
	}
	cases, err := tlc.ParseCases(strings.NewReader(plan))
	if err != nil {
		c.fail("%s: tla/CASES.tsv: %v", name, err)
		return
	}
	extends := regexp.MustCompile(`(?m)^\s*EXTENDS\b[^\n]*\b` + regexp.QuoteMeta(module) + `\b`)
	for _, k := range cases {
		if k.Expected != "pass" || k.Gate != "required" {
			continue
		}
		cfg, _ := c.read("tla/" + k.Config)
		mc, _ := c.read("tla/" + k.Module)
		if (k.Module == module+".tla" || extends.MatchString(stdStripTLAComments(mc))) && slices.Contains(stdCfgChecks(cfg), prop) {
			return
		}
	}
	c.fail("%s: tla %s %s: no required case of tla/CASES.tsv expecting pass checks it (a model extending %s, the property under INVARIANTS or PROPERTIES)", name, module, prop, module)
}

// stdCfgChecks is the names a TLC config checks: those under INVARIANT, INVARIANTS, PROPERTY
// or PROPERTIES, until the next keyword.
func stdCfgChecks(cfg string) []string {
	var out []string
	checking := false
	for _, w := range strings.Fields(stdStripTLAComments(cfg)) {
		switch w {
		case "INVARIANT", "INVARIANTS", "PROPERTY", "PROPERTIES":
			checking = true
		case "SPECIFICATION", "INIT", "NEXT", "CONSTANT", "CONSTANTS", "SYMMETRY", "VIEW",
			"CHECK_DEADLOCK", "ALIAS", "POSTCONDITION", "ACTION_CONSTRAINT", "ACTION_CONSTRAINTS", "CONSTRAINT", "CONSTRAINTS":
			checking = false
		default:
			if checking {
				out = append(out, w)
			}
		}
	}
	return out
}

// stdStripTLAComments drops \* line comments from a TLC config.
func stdStripTLAComments(src string) string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		if i := strings.Index(l, `\*`); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// constant checks the package declares the constant and that it equals the pin's value, by
// go/constant over the declaring expression (literals, the package's own constants, the time
// package's units, conversions and the arithmetic operators).
func (c *stdCheck) constant(name, dir, k, want string) {
	clean := path.Clean(strings.TrimPrefix(dir, "./"))
	// the files that name the constant first; every file of the package only when its
	// expression refers to a constant declared elsewhere
	exprs := c.constExprs(clean, k)
	e, ok := exprs[k]
	if !ok {
		c.fail("%s: const %s %s is declared in no file of the package", name, dir, k)
		return
	}
	got, err := stdEval(e, exprs, 0)
	if err != nil {
		exprs = c.constExprs(clean, "")
		got, err = stdEval(exprs[k], exprs, 0)
	}
	if err != nil {
		c.fail("%s: const %s %s: %v", name, dir, k, err)
		return
	}
	w, err := stdPinValue(want)
	if err != nil {
		c.fail("%s: const %s %s: the pin's value %q: %v", name, dir, k, want, err)
		return
	}
	if !stdSameValue(got, w) {
		c.fail("%s: const %s %s is %s, the pin says %s", name, dir, k, got.ExactString(), want)
	}
}

// constExprs is every constant's declaring expression in the package's non-test files whose
// text names word ("" for every file), read with go/parser.
func (c *stdCheck) constExprs(dir, word string) map[string]ast.Expr {
	entries, _ := fs.ReadDir(c.fsys, dir)
	exprs := map[string]ast.Expr{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := fs.ReadFile(c.fsys, path.Join(dir, e.Name()))
		if err != nil || !strings.Contains(string(src), "const") || !strings.Contains(string(src), word) {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, sp := range g.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if i < len(vs.Values) {
						exprs[id.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	return exprs
}

// stdTimeUnits is the time package's duration constants, in nanoseconds.
var stdTimeUnits = map[string]int64{
	"Nanosecond": int64(time.Nanosecond), "Microsecond": int64(time.Microsecond), "Millisecond": int64(time.Millisecond),
	"Second": int64(time.Second), "Minute": int64(time.Minute), "Hour": int64(time.Hour),
}

// stdEval evaluates a constant expression of the package.
func stdEval(e ast.Expr, consts map[string]ast.Expr, depth int) (constant.Value, error) {
	if depth > 32 {
		return nil, fmt.Errorf("the constant refers to itself")
	}
	switch x := e.(type) {
	case *ast.BasicLit:
		v := constant.MakeFromLiteral(x.Value, x.Kind, 0)
		if v.Kind() == constant.Unknown {
			return nil, fmt.Errorf("the literal %s is not a constant", x.Value)
		}
		return v, nil
	case *ast.ParenExpr:
		return stdEval(x.X, consts, depth+1)
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return constant.MakeBool(x.Name == "true"), nil
		}
		if d, ok := consts[x.Name]; ok {
			return stdEval(d, consts, depth+1)
		}
		return nil, fmt.Errorf("%s is no constant of the package", x.Name)
	case *ast.SelectorExpr:
		if p, ok := x.X.(*ast.Ident); ok && p.Name == "time" {
			if n, ok := stdTimeUnits[x.Sel.Name]; ok {
				return constant.MakeInt64(n), nil
			}
		}
		return nil, fmt.Errorf("the selector %s is not evaluated", x.Sel.Name)
	case *ast.UnaryExpr:
		v, err := stdEval(x.X, consts, depth+1)
		if err != nil {
			return nil, err
		}
		return constant.UnaryOp(x.Op, v, 0), nil
	case *ast.BinaryExpr:
		l, err := stdEval(x.X, consts, depth+1)
		if err != nil {
			return nil, err
		}
		r, err := stdEval(x.Y, consts, depth+1)
		if err != nil {
			return nil, err
		}
		switch x.Op {
		case token.SHL, token.SHR:
			n, _ := constant.Uint64Val(r)
			return constant.Shift(l, x.Op, uint(n)), nil
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return constant.MakeBool(constant.Compare(l, x.Op, r)), nil
		case token.QUO:
			if l.Kind() == constant.Int && r.Kind() == constant.Int {
				return constant.BinaryOp(l, token.QUO_ASSIGN, r), nil
			}
		}
		return constant.BinaryOp(l, x.Op, r), nil
	case *ast.CallExpr:
		if len(x.Args) == 1 {
			return stdEval(x.Args[0], consts, depth+1) // a conversion: time.Duration(5), int64(n)
		}
	}
	return nil, fmt.Errorf("the expression is not evaluated")
}

// stdPinValue reads a pin's value: a Go literal, or a duration for a duration constant.
func stdPinValue(s string) (constant.Value, error) {
	switch {
	case strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "`"):
		if v := constant.MakeFromLiteral(s, token.STRING, 0); v.Kind() != constant.Unknown {
			return v, nil
		}
	case s == "true" || s == "false":
		return constant.MakeBool(s == "true"), nil
	}
	for _, k := range []token.Token{token.INT, token.FLOAT} {
		if v := constant.MakeFromLiteral(s, k, 0); v.Kind() != constant.Unknown {
			return v, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return constant.MakeInt64(int64(d)), nil
	}
	return nil, fmt.Errorf("neither a Go literal nor a duration")
}

// stdSameValue says two constants are equal: of one kind, or both numeric.
func stdSameValue(a, b constant.Value) bool {
	num := func(v constant.Value) bool { return v.Kind() == constant.Int || v.Kind() == constant.Float }
	if a.Kind() != b.Kind() && !(num(a) && num(b)) {
		return false
	}
	return constant.Compare(a, token.EQL, b)
}

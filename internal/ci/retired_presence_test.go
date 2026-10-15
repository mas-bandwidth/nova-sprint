package ci

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoRetiredPresenceNameReturns is the guard that the old friend presence does
// not come back (v1.3.5). The owner, 2026-10-14: "continue to remove the old stuff,
// so they don't come back, or get used when using the tool, or building new code".
// Whether a friend is there is the nova-hook record's alone (its per-minute
// challenge, which only the model answers); the sprint reads it and owns the roster,
// the hold and the work reports. Every name the presence before it went by (the
// coordinator's keepalive and ping verbs, the server's friends rule from beats,
// pongs and finishes, the friend health record, the daemon copy's session check,
// push proof and harness watch) is one row of internal/ci/testdata/retired-presence.txt
// with what replaced it, and this scan refuses each in the module's text: an `ident`
// row as a whole-word Go identifier in a .go file, a `text` row (a command, flag or
// label) in any text file a person or an AI reads. History is not scanned.
func TestNoRetiredPresenceNameReturns(t *testing.T) {
	t.Parallel()
	root := "../.."
	raw, err := os.ReadFile(filepath.Join(root, retiredPresenceTable))
	require.NoError(t, err)
	rows, faults := parseRetiredNames(string(raw))
	require.Empty(t, faults, "%s", retiredPresenceTable)
	require.NotEmpty(t, rows, "%s names nothing", retiredPresenceTable)

	var found []string
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "tmp", "bin", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !retiredScanned("text", rel) && !retiredScanned("ident", rel) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		found = append(found, retiredFindings(rel, src, rows)...)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(found)
	assert.Empty(t, found, "the old friend presence is retired (v1.3.5; %s): liveness is the nova-hook record's alone. Use the replacement each line names.", retiredPresenceTable)
}

const retiredPresenceTable = "internal/ci/testdata/retired-presence.txt"

// retiredName is one row of the table.
type retiredName struct {
	Kind, Name, Replacement string
}

// retiredHistory records what was, never what to do: it may name a retired thing.
var retiredHistory = []string{
	"CHANGELOG.md", "FIXES.md", "docs/fixes.sexp", "tmp/",
	retiredPresenceTable, "internal/ci/retired_presence_test.go",
	"internal/sprint/stale_stall_mark_test.go", // seeds the retired stall mark as a stale record (the 2026-10-15 regression)
}

// retiredTextExts are the files a text row is looked for in.
var retiredTextExts = map[string]bool{
	".go": true, ".md": true, ".sh": true, ".bash": true, ".zsh": true, ".sexp": true,
	".txt": true, ".tsv": true, ".yml": true, ".yaml": true, ".json": true,
	".plist": true, ".tla": true, ".cfg": true, ".tmpl": true,
}

// parseRetiredNames reads the table: `<kind> <name> <replacement>` rows, blank
// lines and # comments skipped. A row of another kind, or with no replacement, is
// a fault: every retirement names what took its place.
func parseRetiredNames(src string) ([]retiredName, []string) {
	var rows []retiredName
	var faults []string
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			faults = append(faults, fmt.Sprintf("line %d: want `<kind> <name> <what replaced it>`, got %q", i+1, line))
			continue
		}
		kind, name, rest := f[0], f[1], f[2:]
		if kind == "text" && len(f) >= 4 && strings.HasPrefix(f[1], "nova-") {
			name, rest = f[1]+" "+f[2], f[3:] // a command row: the tool and its verb are one name
		}
		if kind != "ident" && kind != "text" {
			faults = append(faults, fmt.Sprintf("line %d: kind %q is not ident or text", i+1, kind))
			continue
		}
		rows = append(rows, retiredName{Kind: kind, Name: name, Replacement: strings.Join(rest, " ")})
	}
	return rows, faults
}

// retiredMatcher is the regexp a row is found by.
func retiredMatcher(r retiredName) *regexp.Regexp {
	if r.Kind == "ident" {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(r.Name) + `\b`)
	}
	return regexp.MustCompile(regexp.QuoteMeta(r.Name) + `(?:[^A-Za-z0-9_]|$)`)
}

// retiredScanned says whether a row of kind is looked for in the file rel.
func retiredScanned(kind, rel string) bool {
	for _, h := range retiredHistory {
		if rel == h || strings.HasSuffix(h, "/") && strings.HasPrefix(rel, h) {
			return false
		}
	}
	if kind == "ident" {
		return path.Ext(rel) == ".go"
	}
	return retiredTextExts[path.Ext(rel)]
}

// retiredFindings is every `<rel>:<line>: <kind> <name>` the rows find in src.
func retiredFindings(rel string, src []byte, rows []retiredName) []string {
	var out []string
	lines := strings.Split(string(src), "\n")
	for _, r := range rows {
		if !retiredScanned(r.Kind, rel) {
			continue
		}
		re := retiredMatcher(r)
		for i, l := range lines {
			if re.MatchString(l) {
				out = append(out, fmt.Sprintf("%s:%d: %s %q is retired; use %s", rel, i+1, r.Kind, r.Name, r.Replacement))
			}
		}
	}
	return out
}

// The scan on known cases: an identifier only as a whole word, a command not when
// it is the start of a longer word, and history never.
func TestTheRetiredPresenceScanFindsEachFormAndNothingElse(t *testing.T) {
	t.Parallel()
	rows, faults := parseRetiredNames(`
# a comment
ident FriendEvidence the hook record
text nova-friend ping nova-hook serve
text --to-friends nova-hook serve
`)
	require.Empty(t, faults)
	require.Len(t, rows, 3)
	assert.Equal(t, retiredName{Kind: "text", Name: "nova-friend ping", Replacement: "nova-hook serve"}, rows[1])

	goSrc := []byte("package x\nfunc FriendEvidence() {}\nfunc FriendEvidenceOf() {}\n")
	assert.Equal(t, []string{`internal/sprint/x.go:2: ident "FriendEvidence" is retired; use the hook record`},
		retiredFindings("internal/sprint/x.go", goSrc, rows))

	md := []byte("run nova-friend ping --as ada\nnova-friend ping-install too\nnova-friend pings nothing\nnova-friend reach stays\nping --wake --to-friends\n")
	assert.Equal(t, []string{
		`docs/X.md:1: text "nova-friend ping" is retired; use nova-hook serve`,
		`docs/X.md:2: text "nova-friend ping" is retired; use nova-hook serve`,
		`docs/X.md:5: text "--to-friends" is retired; use nova-hook serve`,
	}, retiredFindings("docs/X.md", md, rows))
	assert.Empty(t, retiredFindings("CHANGELOG.md", md, rows), "history is not scanned")
	assert.Empty(t, retiredFindings("tmp/notes.md", md, rows), "history is not scanned")

	_, faults = parseRetiredNames("ident FriendEvidence\nverb ping x\n")
	assert.Len(t, faults, 2, "a row with no replacement, and a kind that is neither, are faults")
}

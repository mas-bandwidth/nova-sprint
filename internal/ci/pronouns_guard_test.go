package ci

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// personalPronoun is a pronoun for a person. A friend's pronouns are the
// friend row's `pronouns` setting (Pronouns.Say renders them), never a word typed
// into a text: a literal that says "her" or "she" calls every friend a she, and a
// literal that says "his" or "he" calls every friend a he. "its" is not here: a daemon, a
// session or a row is a thing.
var personalPronoun = regexp.MustCompile(`(?i)\b(she|her|hers|herself|he|him|himself|his)\b`)

// pronounGuardRoots cover sprint command output and the sprint rules that generate
// the friend judgments. Paths are from this package's directory. The friend
// daemon's records, the renderer (Pronouns.Say) and the configuration help are
// nova-tools pkg/friend's and pkg/config's, guarded by nova-tools' own copy of this
// test; this one moved here from nova-sprint's pkg/friend copy when it was deleted.
var pronounGuardRoots = []string{"../../cmd/nova-sprint", "../../internal/sprint"}

// pronounGuardAllowed are the literals that say a pronoun word for a reason that
// is not a friend: file (base name) to the literal's text.
var pronounGuardAllowed = map[string][]string{
	// the stop words the rework matcher drops from a finding before it compares two
	"rework.go": {"he", "her", "his", "him", "she"},
	// the helper that renders the pronouns holds the words themselves
	"pronouns.go": {"she/her", "he/him", "she", "her", "hers", "herself", "he", "him", "his", "himself"},
	// the pronoun sets nova-config lists, by their names
	"kind.go": {"she/her", "he/him"},
}

// These four Note.Type constants are persisted identifiers, not prose templates.
// They must keep their historical bytes for old judgments and release age to match.
// Only the exact file, const declaration name and literal are exempted.
var pronounGuardIdentifiers = map[string]map[string]string{
	"../../internal/sprint/stops.go": {"NStopPinWaits": "a card pinned to one friend waits while she is not up"},
	"../../internal/sprint/notes.go": {
		"NFriendNotWoken": "a friend was not told of her card",
		// the retired QUEUE.json reconcile's notes, kept for the history that holds them
		"NFriendDisagrees": "a friend's row disagrees with her QUEUE.json",
		"NFriendAgrees":    "a friend's row agrees with her QUEUE.json again",
	},
}

// TestNoStringLiteralCallsAFriendHerOrHim reads every non-test source of the
// roots with go/parser and fails on a string literal carrying a personal
// pronoun: a text about a friend says {their} and the like through Pronouns.Say,
// or names the friend, or says "the friend's".
func TestNoStringLiteralCallsAFriendHerOrHim(t *testing.T) {
	t.Parallel()
	var found []string
	fset := token.NewFileSet()
	for _, root := range pronounGuardRoots {
		var files []string
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
			return nil
		})
		require.NoError(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(fset, file, nil, 0)
			require.NoError(t, err, "parse %s", file)
			allowed := pronounGuardAllowed[filepath.Base(file)]
			stable := map[*ast.BasicLit]bool{}
			identifiers := pronounGuardIdentifiers[filepath.Clean(file)]
			for _, decl := range f.Decls {
				d, ok := decl.(*ast.GenDecl)
				if !ok || d.Tok != token.CONST {
					continue
				}
				for _, spec := range d.Specs {
					value := spec.(*ast.ValueSpec)
					for i, name := range value.Names {
						expected, ok := identifiers[name.Name]
						if !ok || i >= len(value.Values) {
							continue
						}
						lit, ok := value.Values[i].(*ast.BasicLit)
						if !ok {
							continue
						}
						s, err := strconv.Unquote(lit.Value)
						if err == nil && s == expected {
							stable[lit] = true
						}
					}
				}
			}

			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || stable[lit] {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !personalPronoun.MatchString(s) {
					return true
				}
				for _, a := range allowed {
					if s == a {
						return true
					}
				}
				excerpt := personalPronoun.FindStringIndex(s)
				from := max(0, excerpt[0]-30)
				to := min(len(s), excerpt[1]+30)
				found = append(found, fset.Position(lit.Pos()).String()+": "+strconv.Quote(s[from:to]))
				return true
			})
		}
	}
	sort.Strings(found)
	assert.Empty(t, found, "%d string literal(s) carry a personal pronoun; say {their}/{them}/{they} through Pronouns.Say (nova-tools pkg/friend/pronouns.go), name the friend, or say \"the friend's\":\n%s", len(found), strings.Join(found, "\n"))
}

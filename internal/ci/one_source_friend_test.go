package ci

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sprintFriendCopyImport is nova-sprint's copy of nova-tools pkg/friend; one true source:
// every importer imports nova-tools pkg/friend and the copy is deleted.
const sprintFriendCopyImport = "github.com/mas-bandwidth/nova-sprint/pkg/friend"

// friendCopyImporters is every .go file under root, outside testdata, that imports path,
// relative to root and sorted (the walk is in lexical order).
func friendCopyImporters(t *testing.T, root, path string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || (strings.HasPrefix(d.Name(), ".") && p != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if v, _ := strconv.Unquote(imp.Path.Value); v == path {
				rel, _ := filepath.Rel(root, p)
				out = append(out, rel)
				break
			}
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// TestNothingImportsTheSprintFriendCopy: no file imports nova-sprint's pkg/friend copy. The
// scan sees a known yes and a known no first, then the module itself.
func TestNothingImportsTheSprintFriendCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "yes.go"), []byte("package x\n\nimport \""+sprintFriendCopyImport+"\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "no.go"), []byte("package x\n\nimport \"github.com/mas-bandwidth/nova-tools/pkg/friend\"\n"), 0o644))
	require.Equal(t, []string{"yes.go"}, friendCopyImporters(t, dir, sprintFriendCopyImport))

	got := friendCopyImporters(t, filepath.Join("..", ".."), sprintFriendCopyImport)
	require.Empty(t, got, "one true source: these files import nova-sprint's copy of pkg/friend; import github.com/mas-bandwidth/nova-tools/pkg/friend")
}

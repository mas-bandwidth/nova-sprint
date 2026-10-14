package repotree

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is a small tree with nested directories, a skipped testdata
// directory, a non-Go file and a file that does not parse.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"a.go":          "package fixture\n",
		"bad.go":        "this does not parse\n",
		"note.txt":      "not Go\n",
		"sub/b.go":      "package sub\n",
		"sub/deep/c.go": "package deep\n",
		"testdata/d.go": "package testdata\n",
		"skip/x.go":     "package skip\n",
	}
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	return root
}

func paths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	sort.Strings(out)
	return out
}

func TestFilesSelectsFromAFixtureTree(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	cases := map[string]struct {
		q    Query
		want []string
	}{
		"go files, testdata and skip skipped": {
			q:    Query{Root: root, Ext: ".go", SkipDirs: []string{"testdata", "skip"}},
			want: []string{"a.go", "bad.go", "sub/b.go", "sub/deep/c.go"},
		},
		"one extension": {
			q:    Query{Root: root, Ext: ".txt"},
			want: []string{"note.txt"},
		},
		"one subtree": {
			q:    Query{Root: root, Under: []string{"sub"}, Ext: ".go"},
			want: []string{"sub/b.go", "sub/deep/c.go"},
		},
		"a skipped suffix": {
			q:    Query{Root: root, Ext: ".go", SkipSuffix: []string{"bad.go"}},
			want: []string{"a.go", "skip/x.go", "sub/b.go", "sub/deep/c.go", "testdata/d.go"},
		},
		"every file": {
			q:    Query{Root: root},
			want: []string{"a.go", "bad.go", "note.txt", "skip/x.go", "sub/b.go", "sub/deep/c.go", "testdata/d.go"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, paths(Files(t, c.q)))
		})
	}
}

func TestParsedLeavesOutAFileThatDoesNotParse(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	q := Query{Root: root, Ext: ".go"}
	parsed := Parsed(t, q)
	var got []string
	for _, p := range parsed {
		require.NotNil(t, p.AST)
		require.NotNil(t, p.Fset)
		got = append(got, p.Path)
	}
	sort.Strings(got)
	assert.Equal(t, []string{"a.go", "skip/x.go", "sub/b.go", "sub/deep/c.go", "testdata/d.go"}, got)
}

// TestRepotreeFilesReadsOnce asks one query from eight goroutines and sees one
// walk: the reads equal the files, not eight times them.
func TestRepotreeFilesReadsOnce(t *testing.T) {
	t.Parallel()
	root := fixture(t)
	q := Query{Root: root, Ext: ".go", SkipDirs: []string{"testdata", "skip"}}
	const callers = 8
	var wg sync.WaitGroup
	got := make([][]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = paths(Files(t, q))
		}(i)
	}
	wg.Wait()
	for i := 1; i < callers; i++ {
		assert.Equal(t, got[0], got[i])
	}
	e := loadFiles(root, q)
	assert.Equal(t, int64(len(e.files)), atomic.LoadInt64(&e.reads), "one read per file, however many callers ask")
}

func TestRootIsTheCheckout(t *testing.T) {
	t.Parallel()
	root := Root(t)
	assert.FileExists(t, filepath.Join(root, "go.mod"))
}

func Example() {
	root, err := os.MkdirTemp("", "repotree-example")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "a.go"), []byte("package internal\n"), 0o644); err != nil {
		panic(err)
	}
	var reads int64
	files, err := walk(root, Query{Under: []string{"internal"}, Ext: ".go"}, &reads)
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		fmt.Println(f.Path)
	}
	// Output: internal/a.go
}

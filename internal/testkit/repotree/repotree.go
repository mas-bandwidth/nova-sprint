// Package repotree walks the repository's files once for the class tests: the
// tests that read the whole tree and judge it (that no unit test sleeps, that a
// stamp is read only by its one deadline, that no secret reaches an error).
// Each of those tests used to walk the tree its own way, skip its own
// directories, and parse its own files; here one walker, cached for the
// process, answers them all.
//
// Root is the checkout's root, found by walking up from the working directory
// to the module's go.mod. Files returns the files a Query selects, each read
// once by os.ReadFile; Parsed returns the same files parsed with go/parser once.
// The caches are per-process and per query, each guarded by a sync.Once, so
// tests running in parallel share one walk; they hold read-only data, and a
// caller must not write to the bytes a File carries or the AST a ParsedFile
// carries. A file whose bytes go/parser cannot read is left out of Parsed.
//
//	root := repotree.Root(t)
//	q := repotree.Query{Under: []string{"cmd", "internal", "pkg"}, Ext: ".go", SkipDirs: []string{"testdata"}}
//	for _, f := range repotree.Files(t, q) {
//		_ = f.Path
//	}
package repotree

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// Query selects the files a walk reads. Root is the directory walked, empty
// for the checkout's root from Root(t): a test of this package points it at its
// own fixture. Under lists slash subdirectories, empty for the root itself.
// Ext keeps only names ending with it, empty for every name. SkipDirs skips a
// directory of that name wherever it appears under a walked root. SkipSuffix
// skips a name ending with it.
type Query struct {
	Root       string
	Under      []string
	Ext        string
	SkipDirs   []string
	SkipSuffix []string
}

// File is one selected file: its slash path from the walked root, and its bytes
// read once. The bytes are read-only; a caller must not write to them.
type File struct {
	Path string
	Data []byte
}

// ParsedFile is one file parsed by go/parser: its slash path from the walked root,
// its AST, and the file set the AST's positions resolve in. The AST and file set
// are read-only, shared by every test that asks for the same query.
type ParsedFile struct {
	Path string
	AST  *ast.File
	Fset *token.FileSet
}

var (
	rootOnce sync.Once
	rootDir  string
)

// Root returns the checkout's root: the nearest directory at or above the
// working directory that holds the module's go.mod. The walk runs once for the
// process.
func Root(t testing.TB) string {
	t.Helper()
	rootOnce.Do(func() { rootDir = findRoot() })
	require.NotEmpty(t, rootDir, "repotree: no go.mod at or above the working directory")
	return rootDir
}

// findRoot walks up from the working directory to the first go.mod.
func findRoot() string {
	d, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

// filesEntry is one query's cached walk: the files, the error, and how many
// files were read from disk (one per file, when the walk ran once).
type filesEntry struct {
	once  sync.Once
	files []File
	err   error
	reads int64
}

// parsedEntry is one query's cached parse.
type parsedEntry struct {
	once   sync.Once
	parsed []ParsedFile
	err    error
}

var (
	filesCache  sync.Map // string -> *filesEntry
	parsedCache sync.Map // string -> *parsedEntry
)

// queryKey is the query's cache key: the resolved root, the roots, and the
// filters, so two different queries never share an entry.
func queryKey(root string, q Query) string {
	return strings.Join([]string{
		root,
		strings.Join(q.Under, "\x01"),
		q.Ext,
		strings.Join(q.SkipDirs, "\x01"),
		strings.Join(q.SkipSuffix, "\x01"),
	}, "\x00")
}

// Files returns the files q selects under its root, each read once. The walk
// and the reads run once per query per process.
func Files(t testing.TB, q Query) []File {
	t.Helper()
	root := resolveRoot(t, q)
	e := loadFiles(root, q)
	e.once.Do(func() { e.files, e.err = walk(root, q, &e.reads) })
	require.NoError(t, e.err, "repotree: walking %s", root)
	return e.files
}

// Parsed returns the files q selects under its root, each parsed once with
// go/parser and shared by every caller of the same query. A file whose bytes
// do not parse is left out.
func Parsed(t testing.TB, q Query) []ParsedFile {
	t.Helper()
	root := resolveRoot(t, q)
	e := loadParsed(root, q)
	e.once.Do(func() { e.parsed, e.err = parseAll(t, root, q) })
	require.NoError(t, e.err, "repotree: parsing %s", root)
	return e.parsed
}

// resolveRoot is the query's root: its own, or the checkout's.
func resolveRoot(t testing.TB, q Query) string {
	if q.Root != "" {
		return q.Root
	}
	return Root(t)
}

func loadFiles(root string, q Query) *filesEntry {
	e, _ := filesCache.LoadOrStore(queryKey(root, q), &filesEntry{})
	return e.(*filesEntry)
}

func loadParsed(root string, q Query) *parsedEntry {
	e, _ := parsedCache.LoadOrStore(queryKey(root, q), &parsedEntry{})
	return e.(*parsedEntry)
}

// walk selects q's files under root, reading each once and counting the reads.
func walk(root string, q Query, reads *int64) ([]File, error) {
	skipDir := map[string]bool{}
	for _, d := range q.SkipDirs {
		skipDir[d] = true
	}
	under := q.Under
	if len(under) == 0 {
		under = []string{""}
	}
	seen := map[string]bool{}
	var out []File
	for _, u := range under {
		start := filepath.Join(root, filepath.FromSlash(u))
		err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != start && skipDir[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if q.Ext != "" && !strings.HasSuffix(d.Name(), q.Ext) {
				return nil
			}
			for _, s := range q.SkipSuffix {
				if strings.HasSuffix(d.Name(), s) {
					return nil
				}
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				return nil
			}
			seen[rel] = true
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			atomic.AddInt64(reads, 1)
			out = append(out, File{Path: rel, Data: data})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// parseAll parses q's files under root with go/parser, leaving out a file that
// does not parse.
func parseAll(t testing.TB, root string, q Query) ([]ParsedFile, error) {
	fset := token.NewFileSet()
	files := Files(t, q)
	out := make([]ParsedFile, 0, len(files))
	for _, f := range files {
		full := filepath.Join(root, filepath.FromSlash(f.Path))
		astFile, err := parser.ParseFile(fset, full, f.Data, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		out = append(out, ParsedFile{Path: f.Path, AST: astFile, Fset: fset})
	}
	return out, nil
}

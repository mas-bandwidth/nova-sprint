package ci

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// THE DASHBOARD PAGE HAS ONE HOME (Glenn, 2026-10-14: "make sure we never
// create another copy"). The page lives only in internal/sprintdash/page,
// embedded by internal/sprintdash/pageview.go, and only the dashboard verb
// (cmd/nova-sprint/dashboard.go) serves it: the page listener is the Server
// itself (pageview.go) and the pull listener is Server.Pull (pull.go).
//
// TestDashboardPageExistsOnlyInSprintdashPage fails on any other index.html
// or app.js in nova-sprint — or in the nova-tools version go.mod requires,
// reached the way the one-source guard reaches it (novaToolsDir, the module
// cache) — whose content carries the page's marker or digests to the page's
// digest. TestOnlyTheDashboardRoutesServeAPISprint fails on any handler of
// /api/sprint outside the two allowlisted serving files. A copy made under
// another name, or a handler registered through a mux, is the same copy.

const (
	dashPageDir     = "internal/sprintdash/page"
	dashIndexMarker = "<title>nova-sprint</title>"
	dashAppMarker   = "Sprint dashboard: polls /api/sprint"
)

// dashSprintHandlers is every non-test Go file allowed to handle /api/sprint:
// the page's own handler and the pull routes, both wired only by the
// dashboard verb. A third file here is a second server: refuse it in review,
// do not extend this list to silence the guard.
var dashSprintHandlers = map[string]bool{
	"internal/sprintdash/pageview.go": true, // case "/api/sprint" in (Server).ServeHTTP
	"internal/sprintdash/pull.go":     true, // path == "/api/sprint" in (Server).servePull
}

// dashPageSums is the canonical page's digests and markers, by file name.
type dashPageSums struct {
	digest map[string]string
	marker map[string]string
}

// dashCanonicalPage reads the embedded page's index.html and app.js and
// proves each still carries its marker, so a marker that rots is red here
// rather than a scan that quietly stops seeing copies.
func dashCanonicalPage(t *testing.T, root string) dashPageSums {
	t.Helper()
	sums := dashPageSums{digest: map[string]string{}, marker: map[string]string{
		"index.html": dashIndexMarker, "app.js": dashAppMarker,
	}}
	for name, marker := range sums.marker {
		b, err := os.ReadFile(filepath.Join(root, dashPageDir, name))
		require.NoError(t, err)
		require.Contains(t, string(b), marker, "the canonical %s no longer carries its marker %q: pick the page's new marker", name, marker)
		sum := sha256.Sum256(b)
		sums.digest[name] = hex.EncodeToString(sum[:])
	}
	return sums
}

// dashCopyProblems walks root and returns every index.html or app.js outside
// allowedDir (a slash path under root, "" for none) that carries the page's
// marker or digests to the page's digest.
func dashCopyProblems(root, allowedDir string, sums dashPageSums) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		marker, ok := sums.marker[name]
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if allowedDir != "" && (rel == allowedDir || strings.HasPrefix(rel, allowedDir+"/")) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) == sums.digest[name] || bytes.Contains(b, []byte(marker)) {
			problems = append(problems, rel+" is a copy of the dashboard page ("+dashPageDir+"/"+name+"): the page has one home; serve it through the dashboard verb, never copy it")
		}
		return nil
	})
	sort.Strings(problems)
	return problems, err
}

// dashHandlerRE sees a handler of /api/sprint: a switch case on it, a path
// compared to it, or a mux registration of it. A client naming the URL
// (building it, fetching it) matches none of these.
var dashHandlerRE = regexp.MustCompile(`case\s+"/api/sprint"\s*:|==\s*"/api/sprint"|"/api/sprint"\s*==|Handle(?:Func)?\s*\(\s*"(?:[A-Z]+\s+)?/api/sprint"`)

// dashHandlerProblems walks root's non-test Go files and returns every file
// outside dashSprintHandlers that handles /api/sprint.
func dashHandlerProblems(root string) ([]string, error) {
	var problems []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if dashSprintHandlers[rel] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if dashHandlerRE.Match(b) {
			problems = append(problems, rel+" handles /api/sprint outside the dashboard verb's routes (internal/sprintdash/pageview.go, internal/sprintdash/pull.go): only the dashboard verb serves it")
		}
		return nil
	})
	sort.Strings(problems)
	return problems, err
}

func TestDashboardPageExistsOnlyInSprintdashPage(t *testing.T) {
	t.Parallel()
	root := "../.."
	sums := dashCanonicalPage(t, root)
	problems, err := dashCopyProblems(root, dashPageDir, sums)
	require.NoError(t, err)
	tools, err := dashCopyProblems(novaToolsDir(t, root), "", sums)
	require.NoError(t, err)
	for _, p := range tools {
		problems = append(problems, "nova-tools/"+p)
	}
	require.Empty(t, problems, "one true source: the dashboard page exists only in %s:\n%s", dashPageDir, strings.Join(problems, "\n"))
}

func TestOnlyTheDashboardRoutesServeAPISprint(t *testing.T) {
	t.Parallel()
	problems, err := dashHandlerProblems("../..")
	require.NoError(t, err)
	require.Empty(t, problems, "only the dashboard verb serves /api/sprint:\n%s", strings.Join(problems, "\n"))
}

// TestTheDashboardPageScanSeesAKnownYesAndAKnownNo plants copies in a fake
// tree and proves the scan refuses them: a test that cannot fail is not
// acceptable, so the planted marker copy, the planted digest copy and the
// planted handler are each red, while the canonical page, a client naming
// the URL and an unrelated index.html are green.
func TestTheDashboardPageScanSeesAKnownYesAndAKnownNo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	index := []byte("<html><head>" + dashIndexMarker + "</head></html>")
	app := []byte("// " + dashAppMarker + "\nrender()\n")
	write := func(rel string, b []byte) {
		require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), b, 0o644))
	}
	write(dashPageDir+"/index.html", index)
	write(dashPageDir+"/app.js", app)
	sums := dashPageSums{digest: map[string]string{}, marker: map[string]string{
		"index.html": dashIndexMarker, "app.js": dashAppMarker,
	}}
	for name, b := range map[string][]byte{"index.html": index, "app.js": app} {
		sum := sha256.Sum256(b)
		sums.digest[name] = hex.EncodeToString(sum[:])
	}

	problems, err := dashCopyProblems(root, dashPageDir, sums)
	require.NoError(t, err)
	require.Empty(t, problems, "known yes: the canonical page alone")

	write("elsewhere/index.html", []byte("<html><head>"+dashIndexMarker+"</head><body>copy</body></html>"))
	write("elsewhere/app.js", app) // the digest copy: byte for byte
	write("elsewhere/other.html", index)
	problems, err = dashCopyProblems(root, dashPageDir, sums)
	require.NoError(t, err)
	require.Len(t, problems, 2, "known no: the planted marker copy and the planted digest copy are both red")
	require.Contains(t, problems[0], "elsewhere/app.js is a copy")
	require.Contains(t, problems[1], "elsewhere/index.html is a copy")

	write("internal/sprintdash/pageview.go", []byte("package x\nfunc h() { switch p { case \"/api/sprint\": } }\n"))
	write("cmd/other/main.go", []byte("package x\nfunc h() { mux.HandleFunc(\"/api/sprint\", nil) }\n"))
	write("cmd/other/client.go", []byte("package x\nfunc c() { get(\"http://\" + dash + \"/api/sprint\") }\n"))
	problems, err = dashHandlerProblems(root)
	require.NoError(t, err)
	require.Len(t, problems, 1, "known no: the planted handler is red; the allowlisted route and the client are green")
	require.Contains(t, problems[0], "cmd/other/main.go handles /api/sprint")
}

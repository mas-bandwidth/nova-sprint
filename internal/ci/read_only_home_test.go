package ci

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheUnitSuiteRunsWithAReadOnlyHome pins Makefile's unit tier: it saves
// the Go cache paths before replacing HOME, runs both unit test commands with
// a read-only HOME and no XDG config/cache directories, then refuses any file
// left in that HOME.
func TestTheUnitSuiteRunsWithAReadOnlyHome(t *testing.T) {
	t.Parallel()

	b, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	text := string(b)
	start := strings.Index(text, "test: vet\n")
	require.NotEqual(t, -1, start, "Makefile has no test: vet target")
	recipe := text[start+len("test: vet\n"):]
	var body strings.Builder
	for _, line := range strings.Split(recipe, "\n") {
		if line != "" && !strings.HasPrefix(line, "\t") {
			break
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	recipe = body.String()
	require.NotEmpty(t, strings.TrimSpace(recipe), "test target has no recipe")

	goEnv := strings.Index(recipe, "$(GO) env GOPATH GOMODCACHE GOCACHE")
	export := strings.Index(recipe, "export GOPATH GOMODCACHE GOCACHE")
	first := strings.Index(recipe, "$(GO) test -p $(GOTEST_P)")
	second := strings.Index(recipe, "$(GO) test -count=1 -run '$(TICK_GATE_TEST)'")
	find := strings.Index(recipe, `find "$$readonly_home" -mindepth 1 -print`)
	require.NotEqual(t, -1, goEnv, "test does not record the Go cache environment")
	require.NotEqual(t, -1, export, "test does not export the Go cache environment")
	require.NotEqual(t, -1, first, "test does not run the unticked unit suite")
	require.NotEqual(t, -1, second, "test does not run the tick gate separately")
	require.NotEqual(t, -1, find, "test does not check the read-only HOME after both suites")
	require.Less(t, goEnv, export, "Go cache environment is not recorded before it is exported")
	require.Less(t, export, first, "Go cache environment is not exported before go test")
	require.Less(t, first, second, "the tick gate must run after the rest of the unit suite")
	require.Less(t, second, find, "the HOME emptiness check must follow both go test commands")

	tempHome := strings.Index(recipe, "mktemp -d")
	readonly := strings.Index(recipe, `chmod 0555 "$$readonly_home"`)
	require.NotEqual(t, -1, tempHome, "test does not create a fresh HOME")
	require.NotEqual(t, -1, readonly, "test does not make HOME read-only")
	require.Less(t, tempHome, readonly, "HOME must exist before it is made read-only")
	require.Less(t, readonly, first, "HOME must be read-only before go test starts")
	homeEnv := `env -u XDG_CONFIG_HOME -u XDG_CACHE_HOME HOME="$$readonly_home"`
	require.Equal(t, 2, strings.Count(recipe, homeEnv), "both go test commands must clear XDG paths and use the read-only HOME")
	require.Contains(t, recipe, "a test wrote outside its working directory:", "test does not name files found in HOME")
}

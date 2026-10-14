//go:build functional

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestTheBuiltExecutableRecordsGoModsNovaTools: go.mod's nova-tools requirement is the
// one source of the version seat check requires, and the executable reads it back
// from its own build record (sprint.NovaToolsRequired). This builds the real
// nova-sprint executable and reads its embedded record (go version -m): the nova-tools
// dependency it names is exactly go.mod's requirement. The unit tier injects the
// record (TestNovaToolsRequiredFromReadsTheInjectedRecord), since a test binary's
// record need not list the main module's dependencies.
func TestTheBuiltExecutableRecordsGoModsNovaTools(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	require.NoError(t, err)
	f, err := modfile.Parse("go.mod", data, nil)
	require.NoError(t, err)
	want := ""
	for _, r := range f.Require {
		if r.Mod.Path == sprint.NovaToolsModule {
			want = r.Mod.Version
		}
	}
	require.NotEmpty(t, want, "go.mod requires %s", sprint.NovaToolsModule)

	bin := filepath.Join(t.TempDir(), "nova-sprint")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	out, err = exec.Command("go", "version", "-m", bin).CombinedOutput()
	require.NoError(t, err, "go version -m: %s", out)
	got := ""
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "dep" && fields[1] == sprint.NovaToolsModule {
			got = fields[2]
		}
	}
	require.Equal(t, want, got, "the built nova-sprint records nova-tools %q; go.mod requires %s:\n%s", got, want, out)
}

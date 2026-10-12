package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The hosted CI guard only reaches the client: local transport strips
// GIT_CONFIG_PARAMETERS before spawning receive-pack. Trace the real child
// lifecycle rather than hoping TempDir cleanup wins a race with maintenance.
func TestLandRigPushStartsNoRemoteAutoMaintenance(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	trace := filepath.Join(r.dir, "git-trace")
	r.env = append(slices.DeleteFunc(slices.Clone(r.env), func(e string) bool {
		return strings.HasPrefix(e, "GIT_CONFIG_PARAMETERS=") || strings.HasPrefix(e, "GIT_TRACE=")
	}), "GIT_CONFIG_PARAMETERS='maintenance.auto'='false'", "GIT_TRACE="+trace)
	r.a.gitEnv = r.env
	r.ok("add --stream s1 --one")
	head := r.head("s1-1", "main", "work.txt", "work\n")
	r.queued(map[string]string{"s1-1": head}, "s1-1")
	assert.Contains(t, r.ok("land --repo-dir "+r.clone+" --base main"), "LAND OK stream=s1 cards=1 base=main")
	b, err := os.ReadFile(trace)
	require.NoError(t, err)
	text := string(b)
	require.Contains(t, text, "built-in: git receive-pack", "exercise the local remote child")
	assert.NotContains(t, text, "built-in: git maintenance run --auto", "the remote must not launch automatic maintenance")
	assert.NotContains(t, text, "built-in: git gc --auto", "older Git must not launch automatic GC either")
}

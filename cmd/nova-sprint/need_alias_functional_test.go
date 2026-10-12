//go:build functional

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A dropped card with a successor is a permanent alias on a real store
// (docs/SPEC-SPRINT.md section 2; need_alias.go): A has a successor A2, a card B
// needs A, and a brief edit of B whose text still names A stores the successor
// instead and says so, so no path can bring the old id back as a need
// (cmd/nova-sprint/RELINK.md, the NOTE line).
func TestADroppedCardWithASuccessorIsAnAliasOnTheStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_ACTOR": "coordinator", "HOME": home, "XDG_CONFIG_HOME": t.TempDir()}
	a := newApp(func(k string) string { return env[k] })
	a.home = func() (string, error) { return home, nil }
	defer a.close()
	run := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(append(args, "--redis", addr), &out, &errb)
		require.Equal(t, 0, code, "%v: %d %s%s", args, code, out.String(), errb.String())
		return out.String()
	}
	dir := t.TempDir()
	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	run("add", "--one", "--stream", "a", "A", "--brief-file", writeHeaderBrief(t, dir, "A", "-", "internal/a.go"))
	run("add", "--one", "--stream", "a", "A2", "--brief-file", writeHeaderBrief(t, dir, "A2", "-", "internal/a2.go"))
	run("add", "--one", "--stream", "a", "B", "--brief-file", writeHeaderBrief(t, dir, "B", "A", "internal/b.go"))

	// drop A replaced by its successor: A becomes an alias of A2
	assert.Contains(t, run("drop", "-A", "--replaced-by", "A2", "--reason", "recut"), "MOVED")
	assert.Contains(t, run("card", "A", "--fields"), "replaced_by:", "the alias is recorded on A's kept record")

	// a brief edit of B whose text still names A stores the successor and NOTEs it
	out := run("brief", "B", "--brief-file", writeHeaderBrief(t, t.TempDir(), "B", "A", "internal/b.go,internal/b_test.go"))
	assert.Contains(t, out, "NOTE need A is now A2", "the brief verb names the successor it kept against the text")
	assert.Contains(t, run("card", "B", "--fields"), "Xeds: A2", "the stored need is the successor, never the old id")
}

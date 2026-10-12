//go:build functional

package main

import (
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
// (cmd/nova-sprint/RELINK.md, the NOTE line). A2 lands, B is dealt, and no
// judgment is raised: a need whose alias target has landed is met.
func TestADroppedCardWithASuccessorIsAnAliasOnTheStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	ta := newFunctionalTestApp(t, addr)
	run := func(args ...string) string {
		t.Helper()
		return ta.runVerb(args...)
	}
	dir := t.TempDir()
	run("init", "--readers", "reader-a,reader-b", "--members", "m1")
	run("add", "--one", "--stream", "a", "A", "--brief-file", writeHeaderBrief(t, dir, "A", "-", "internal/a.go"))
	run("add", "--one", "--stream", "a", "A2", "--brief-file", writeHeaderBrief(t, dir, "A2", "-", "internal/a2.go"))
	run("add", "--one", "--stream", "a", "B", "--brief-file", writeHeaderBrief(t, dir, "B", "A", "internal/b.go"))

	// drop A replaced by its successor: A becomes an alias of A2
	assert.Contains(t, run("drop", "A", "--replaced-by", "A2", "--reason", "recut"), "MOVED")
	assert.Contains(t, run("card", "A", "--fields"), "replaced_by=A2", "the alias is recorded on A's kept record")

	// land A2: deal, finish, read, accept, merge
	run("fleet", "beat", "m1")
	run("start")
	run("tick") // the fleet update brings m1 up
	run("tick") // the pump deals A2
	run("take", "--as", "m1", "--limit", "5", "--epoch", "0")
	run("finish", "--as", "m1", "A2.w1@1", "--epoch", "0")
	run("queue", "--as", "reader-a")
	run("queue", "--as", "reader-b")
	run("tick") // the pump moves A2 to review; the readers are asked
	run("read", "--as", "reader-a", "--begin", "--limit", "10", "--epoch", "0")
	run("read", "--as", "reader-a", "--ok", "--limit", "10", "--epoch", "0")
	run("read", "--as", "reader-b", "--begin", "--limit", "10", "--epoch", "0")
	run("read", "--as", "reader-b", "--ok", "--limit", "10", "--epoch", "0")
	run("accept", "A2")
	recordMerge(t, ta.a, "a", 10, "0")

	// a brief edit of B whose text still names A stores the successor and NOTEs it
	out := run("brief", "B", "--brief-file", writeHeaderBrief(t, t.TempDir(), "B", "A", "internal/b.go,internal/b_test.go"))
	assert.Contains(t, out, "NOTE need A is now A2", "the brief verb names the successor it kept against the text")
	assert.Contains(t, run("card", "B", "--fields"), "NEEDS A2", "the stored need is the successor, never the old id")

	// B resolves to ready (its need A2 landed) and is dealt; no judgment raised
	run("tick")
	run("tick")
	assert.Contains(t, run("check"), "CHECK OK violations=0", "no judgment raised")
}

//go:build functional

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
)

// On a real store whose sprint row holds a git_identity that does not parse, start refuses,
// names git_identity and the nova-config verb that fixes it, and writes nothing; with no row
// applied (testutil.StartRaw: epoch 20's store, whose sprint:git_identity was empty) the
// machine identity is read (sprint.Defaults, clear-sets-every-working-default) and start
// starts (start-refuses-a-non-working-setting; read 2 of PR 188).
func TestStartRefusesAMalformedGitIdentityOnARealStore(t *testing.T) {
	t.Parallel()
	addr := testutil.StartRaw(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, fn.Load(context.Background(), c))
	ta := newFunctionalTestApp(t, addr)
	ta.runVerb("init", "--readers", "reader-a,reader-b", "--members", "m1")
	declareFunctionalRoutes(t, c, "flash")
	ta.runVerb("add", "--stream", "s1", "--count", "2")

	require.NoError(t, c.Set(context.Background(), config.SprintKey(config.FieldGitIdentity), "nobody", 0).Err())
	out, code, errs := ta.runVerbOut("start")
	require.Equal(t, 1, code, "refused: %s%s", out, errs)
	var lines []string
	for _, l := range strings.Split(errs, "\n") {
		if strings.HasPrefix(l, "START REFUSED") {
			lines = append(lines, l)
		}
	}
	require.Len(t, lines, 1, errs)
	assert.Contains(t, lines[0], "START REFUSED setting=git_identity ")
	assert.Contains(t, lines[0], "run: "+sprint.GitIdentityRemedy)
	where := ta.runVerb("where")
	assert.NotContains(t, where, "RUNNING", "nothing was written: the machine is not RUNNING")

	// no row applied: the machine identity is read, and start starts
	require.NoError(t, c.Del(context.Background(), config.SprintKey(config.FieldGitIdentity)).Err())
	out, code, errs = ta.runVerbOut("start")
	require.Equal(t, 0, code, "no row applied: the machine identity: %s%s", out, errs)
	assert.NotContains(t, errs, "START REFUSED")
}

//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/bus"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// An overdue card-holding judgment offers no wait on a real store: the inbox's printed
// commands and the server's message to the seat both render from sprint.commands (cmd/nova-sprint/reads.go
// groupText), so one fix covers them. A failed work judgment holds its card; past its
// deadline the act line names the action WaitRefused gives for the kind, never a wait.
func TestAnOverdueCardHoldingJudgmentOffersNoWaitOnTheStore(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator",
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	a.serveAddr = addr
	run := func(line string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, append(strings.Fields(line), "--redis", addr), &out, &errb)
		require.Zero(t, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
		return out.String()
	}
	dir := t.TempDir()
	run("init --readers reader-a,reader-b --members m1")
	run("add --stream s1 c1 --one --brief-file " + writeHeaderBrief(t, dir, "c1", "-", "internal/c1.go"))
	run("fleet beat m1")
	run("start")
	run("tick")
	run("tick")
	run("take --as m1 --max 100 --epoch 0")
	var q struct {
		Cards []queueCard `json:"cards"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("queue --as m1 --json")), &q))
	require.Len(t, q.Cards, 1, "the work card is on m1's queue")
	card := q.Cards[0]
	run(fmt.Sprintf("finish --as m1 %s@%d --epoch 0 --failed --report failed", card.ID, card.Gen))

	// the failed-work judgment is the overdue card-holding group, the one the hook must push
	var ib struct {
		Groups []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"groups"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("inbox --deadline 100ns --json")), &ib))
	gid := ""
	for _, g := range ib.Groups {
		if g.Type == sprint.NWorkFailed {
			gid = g.ID
		}
	}
	require.NotEmpty(t, gid, "the failed-work judgment is open")

	out := run("inbox --deadline 100ns")
	assert.NotContains(t, out, "nova-sprint wait", "the overdue card-holding inbox offers no wait:\n%s", out)
	assert.Contains(t, out, sprint.WaitRefused[sprint.NWorkFailed], "the act line names the action WaitRefused gives:\n%s", out)

	// the server's message to the seat renders the same overdue group with no wait either:
	// step past the judgment's deadline and run one look of the coordinator push
	var mu sync.Mutex
	var sent []bus.Message
	a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, m)
		return nil
	}
	now := time.Now().UTC()
	a.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	mu.Lock()
	now = now.Add(defaultDeadline + 2*time.Minute)
	mu.Unlock()
	a.coordinatorPass(context.Background())
	mu.Lock()
	defer mu.Unlock()
	for _, m := range sent {
		if len(m.To) == 1 && m.To[0] == "coordinator" && strings.Contains(m.Body, gid) {
			assert.NotContains(t, m.Body, "nova-sprint wait", "the message offers no wait for %s:\n%s", gid, m.Body)
			assert.Contains(t, m.Body, sprint.WaitRefused[sprint.NWorkFailed], "the message names the action WaitRefused gives:\n%s", m.Body)
			return
		}
	}
	t.Fatalf("the overdue group %s was not sent to the seat: %+v", gid, sent)
}

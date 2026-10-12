//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/member"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

// realSprint runs the member's verbs on the test's app over the private store, as the
// server would: stdout is the answer, stderr joins it only for a refusal.
type realSprint struct{ a *app }

func (s realSprint) Run(args ...string) (int, []byte) {
	var out, errb bytes.Buffer
	code := s.a.run(args, &out, &errb)
	if code != 0 {
		out.Write(errb.Bytes())
	}
	return code, out.Bytes()
}

// exitChild is a work child that has already ended as an environment exit: it left no
// RESULT.md, its exit code is the member's machine's, and its harness output is what the
// finish must keep on the card, never the harness's name.
type exitChild struct{ res member.Result }

func (c exitChild) Done() bool            { return true }
func (c exitChild) Result() member.Result { return c.res }

// exitRunner starts every card as the one already-ended child.
type exitRunner struct{ res member.Result }

func (r *exitRunner) Start(member.Packet) (member.Child, error) { return exitChild{res: r.res}, nil }

// TestMemberExitHarnessSendsTheChildsOutputNotItsName runs a real member (pkg/member) over
// the real nova-sprint verbs and a private redis-server, not a fake sprint: the member takes
// the one card, its child ends 134 with a known harness output, and the member's finish
// carries --harness with that output (never the harness's name), so the card's record holds
// the output. It is red on main, where the member sends no --harness at all, and green once
// pkg/member sends the child's harness output (docs/SPEC-SPRINT.md, "An environment exit";
// a-member-sends-the-harness-output-not-its-name).
func TestMemberExitHarnessSendsTheChildsOutputNotItsName(t *testing.T) {
	t.Parallel()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	defer c.Close()
	require.NoError(t, fn.Load(context.Background(), c))
	home := t.TempDir()
	env := map[string]string{
		"NOVA_SPRINT_ACTOR": "coordinator",
		"NOVA_SPRINT_REDIS": addr,
		"HOME":              home,
		"XDG_CONFIG_HOME":   filepath.Join(home, ".config"),
	}
	a := newApp(func(k string) string { return env[k] })
	defer a.close()
	a.home = func() (string, error) { return home, nil }
	a.forward = func(_ context.Context, addr string, verbs ...[]string) ([]sprintwire.Result, error) {
		t.Fatalf("a verb was forwarded to a sprint server at %s: %q", addr, verbs)
		return nil, errors.New("forwarding is refused in this test")
	}
	ok := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := a.run(args, &out, &errb)
		require.Zero(t, code, "%v: exit %d\n%s%s", args, code, out.String(), errb.String())
		return out.String()
	}

	ok("init", "--members", "m1")
	ok("add", "--stream", "s1", "--count", "1", "--one")
	ok("start")

	const harnessOut = "line one: the child's own output\nline two: it found an error\nAborted (core dumped)"
	run := &exitRunner{res: member.Result{Exit: 134, Harness: harnessOut}}
	var log bytes.Buffer
	m := member.New(member.Config{As: "m1"}, realSprint{a}, run, nil, &log)

	require.NoError(t, m.Beat()) // m1 comes up
	ok("tick")
	require.NoError(t, m.Beat())
	ok("tick") // and is dealt the card

	now := time.Now()
	_, err := m.Tick(now) // the member takes the card and starts the child
	require.NoError(t, err, log.String())
	_, err = m.Tick(now) // the child ended: the member finishes it
	require.NoError(t, err, log.String())
	ok("tick") // the finish is drained: the environment exit is recorded

	var view struct {
		Primary *sprint.Card   `json:"primary"`
		Work    []*sprint.Card `json:"work_cards"`
	}
	require.NoError(t, json.Unmarshal([]byte(ok("card", "s1-1", "--json")), &view))
	require.NotNil(t, view.Primary)
	var wc *sprint.Card
	for _, w := range view.Work {
		if w.ID == "s1-1.w1" {
			wc = w
		}
	}
	require.NotNil(t, wc, "the primary's work card: %s", ok("card", "s1-1"))
	assert.Equal(t, "environment exit=134", wc.Fields[sprint.FieldEnvExit])
	assert.Equal(t, harnessOut, wc.Fields[sprint.FieldHarnessTail], "the card keeps the child's harness output, never its name")
}

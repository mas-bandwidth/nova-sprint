//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// consumersOutQuiet is the quiet judgments of one tick --json, as the check
// leaves them on the tick result.
type consumersOutQuiet struct {
	Tick struct {
		Quiet []struct {
			Type    string `json:"type"`
			Subject string `json:"subject"`
			Why     string `json:"why"`
		} `json:"quiet"`
	} `json:"tick"`
}

// consumersOutApp is a real store (throwaway redis) with the function library
// loaded, the app over it, and a run helper.
type consumersOutApp struct {
	t  *testing.T
	a  *app
	c  *redis.Client
	do func(args ...string) string
}

func newConsumersOutApp(t *testing.T) *consumersOutApp {
	t.Helper()
	addr := testutil.Start(t)
	c := redis.NewClient(&redis.Options{Addr: addr})
	require.NoError(t, fn.Load(context.Background(), c))
	env := map[string]string{"NOVA_SPRINT_REDIS": addr, "NOVA_SPRINT_ACTOR": "coordinator"}
	a := newApp(func(k string) string { return env[k] })
	do := func(args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := runOnStore(a, addr, args, &out, &errb)
		require.Zero(t, code, "%v: exit %d\n%s%s", args, code, out.String(), errb.String())
		return out.String()
	}
	return &consumersOutApp{t: t, a: a, c: c, do: do}
}

func (w *consumersOutApp) close() { w.a.close(); w.c.Close() }

// A review card whose reads are emptied with the readers table raises one
// consumers-out judgment within one tick (docs/SPEC-SPRINT.md, "The tick
// postconditions"): the tick's first read sees the card with no read out for
// it and no open judgment naming it, and names the card and what is missing.
func TestEveryProducerCard(t *testing.T) {
	t.Parallel()
	w := newConsumersOutApp(t)
	defer w.close()
	ctx := context.Background()

	w.do("init", "--members", "m1:4", "--readers", "r1")
	dir := t.TempDir()
	w.do("add", "--stream", "s1", "s1-1", "--one", "--brief-file", writeHeaderBrief(t, dir, "s1-1", "-", "internal/s1.go"))
	w.do("fleet", "beat", "m1")
	w.do("start")
	w.do("tick") // presence: m1 up
	w.do("tick") // dealt to m1
	w.do("take", "--as", "m1", "--max", "1")
	w.do("finish", "--as", "m1", "s1-1.w1@1", "--epoch", "0", "--report", "done")
	w.do("tick") // the primary moves to review and its read is asked

	// empty the readers table: the review card's read goes with it
	_, err := ntable.Clear(ctx, w.c, "readers")
	require.NoError(t, err)

	// one tick: the check raises one consumers-out judgment naming the card
	var out consumersOutQuiet
	require.NoError(t, json.Unmarshal([]byte(w.do("tick", "--json")), &out))

	var found *struct {
		Type    string `json:"type"`
		Subject string `json:"subject"`
		Why     string `json:"why"`
	}
	for i := range out.Tick.Quiet {
		if out.Tick.Quiet[i].Type == "consumers-out" && out.Tick.Quiet[i].Subject == "s1-1" {
			q := out.Tick.Quiet[i]
			found = &q
		}
	}
	require.NotNil(t, found, "expected a consumers-out judgment for s1-1: %+v", out.Tick.Quiet)
}

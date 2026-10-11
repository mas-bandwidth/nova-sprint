package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mercury = "inception/mercury-3-preview-1002"

func mercuryApp(t *testing.T) *testApp {
	ta := newTestApp(t)
	ta.ok("init --members m1,m2 --readers reader-a,reader-b")
	ta.m.SetRoutes([]sprint.Route{
		{Name: "pro-mercury3", Tier: "pro", Provider: "inception", Model: "mercury-3-preview-1002", Enabled: true},
		{Name: "pro-deepseek", Tier: "pro", Provider: "deepseek", Model: "deepseek-chat", Enabled: true},
	})
	return ta
}

// The verbs end to end on the store: an unlimited model is granted at once; routes limit
// sets Mercury 3's rpm and routes shows it; a take over the window is told its wait; the
// window passes and the take is granted.
func TestBudgetTakeHoldsTheModelsWindow(t *testing.T) {
	t.Parallel()
	ta := mercuryApp(t)
	out := ta.ok("budget take " + mercury + " --as m1 --holder h1")
	assert.Contains(t, out, "granted=yes")
	assert.Contains(t, out, "rpm=0 concurrent=0 open=no", "an unlimited model reads no budget")

	ta.ok("routes limit pro-mercury3 --rpm 2 --reason preview-under-10-a-minute")
	for _, h := range []string{"h2", "h3"} {
		got := ta.ok("budget take " + mercury + " --as m1 --holder " + h)
		assert.Contains(t, got, "granted=yes", h)
		assert.Contains(t, got, "rpm=2", h)
	}
	over := ta.ok("budget take " + mercury + " --as m2 --holder h4 --json")
	var got struct {
		Granted bool  `json:"granted"`
		WaitMS  int64 `json:"wait_ms"`
		Open    bool  `json:"open"`
	}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(over)), &got), over)
	assert.False(t, got.Granted, "a third take in the minute waits: %s", over)
	assert.False(t, got.Open)
	assert.Greater(t, got.WaitMS, int64(50_000), "told to wait for the oldest grant to leave: %s", over)
	assert.Contains(t, ta.ok("routes"), "LIMIT rpm "+mercury+" budget=2 in_use=2")

	ta.mu.Lock()
	ta.now = ta.now.Add(63 * time.Second)
	ta.mu.Unlock()
	assert.Contains(t, ta.ok("budget take "+mercury+" --as m2 --holder h4"), "granted=yes", "granted once the window passed")
}

// A provider's concurrency budget held per request: one slot; a second request waits; the
// first releases and the second is granted; a renewal of a held slot holds it.
func TestBudgetTakeHoldsTheProvidersSlots(t *testing.T) {
	t.Parallel()
	ta := mercuryApp(t)
	ta.ok("routes limit deepseek --concurrent 1 --reason account-limit")
	const ds = "deepseek/deepseek-chat"
	first := ta.ok("budget take " + ds + " --as m1 --holder a")
	assert.Contains(t, first, "granted=yes")
	assert.Contains(t, first, "concurrent=1")
	assert.Contains(t, ta.ok("budget take "+ds+" --as m2 --holder b"), "granted=no")
	assert.Contains(t, ta.ok("budget renew "+ds+" --as m1 --holder a"), "held=yes")
	ta.ok("budget release " + ds + " --as m1 --holder a")
	assert.Contains(t, ta.ok("budget take "+ds+" --as m2 --holder b"), "granted=yes")
}

// The store cannot be read: the take is granted open, exit 0, with the words why; a
// release and a renewal exit 0 too. The limiter is never a source of errors.
func TestBudgetTakeWithTheStoreDownIsGrantedOpen(t *testing.T) {
	t.Parallel()
	ta := mercuryApp(t)
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return nil, errors.New("connection refused")
	}
	code, out, errs := ta.do("budget take " + mercury + " --as m1 --holder h1")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, out, "granted=yes")
	assert.Contains(t, out, "open=yes")
	assert.Contains(t, out, "why=the\\x20store\\x20could\\x20not\\x20be\\x20opened", "the words why")
	for _, verb := range []string{"release", "renew"} {
		code, out, errs := ta.do("budget " + verb + " " + mercury + " --as m1 --holder h1")
		assert.Equal(t, 0, code, "%s: %s%s", verb, out, errs)
	}
}

// routes limit refuses what is not a budget: both flags, neither, an rpm on a provider.
func TestRoutesLimitRefusals(t *testing.T) {
	t.Parallel()
	ta := mercuryApp(t)
	for _, line := range []string{
		"routes limit pro-mercury3 --rpm 10 --concurrent 2 --reason r",
		"routes limit pro-mercury3 --reason r",
		"routes limit pro-mercury3 --rpm 10",
		"routes limit inception --rpm 10 --reason r",
	} {
		code, out, errs := ta.do(line)
		assert.NotEqual(t, 0, code, "%s: %s%s", line, out, errs)
	}
}

// The server runs a budget's verbs as a worker's, beside the line on the beat lane, and
// only in their own words.
func TestTheServerAdmitsTheBudgetVerbs(t *testing.T) {
	t.Parallel()
	for _, verb := range []string{"take", "renew", "release"} {
		argv := []string{"budget", verb, mercury, "--as", "hetzner", "--holder", "c1.g2.r7"}
		as, words, why := workerVerb(argv)
		require.Empty(t, why, verb)
		assert.Equal(t, "hetzner", as)
		assert.Equal(t, 2, words)
		assert.True(t, isBudget(argv), verb)
		_, _, why = workerVerb(append(argv, "--json"))
		assert.Empty(t, why, "--json is allowed")
	}
	for _, bad := range [][]string{
		{"budget", "take", "nomodel", "--as", "m1", "--holder", "h"},
		{"budget", "take", mercury, "--holder", "h", "--as", "m1"},
		{"budget", "take", mercury, "--as", "m1", "--holder", "h", "--redis", "x"},
		{"budget", "take", mercury, "--as", "a b", "--holder", "h"},
	} {
		_, _, why := workerVerb(bad)
		assert.NotEmpty(t, why, "%v", bad)
	}
}

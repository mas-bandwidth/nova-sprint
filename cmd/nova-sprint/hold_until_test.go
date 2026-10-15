package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Holding a fleet member requires one of --until <RFC3339>, --for <duration>, or
// --indefinite (or --extend <duration> on an active timed hold); invalid times,
// non-positive durations, conflicting flags, extending an unheld or indefinitely-held
// member, and giving member-end flags on streams, readers, or friends are all refused
// with nothing written.
func TestHoldWantsAnEndOrIndefinite(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 2")
	ta.ok("friend sync")

	// 1. Missing --until, --for, and --indefinite on a member hold (top-level and fleet hold)
	for _, cmd := range []string{"hold m1 --reason r", "fleet hold m1 --reason r"} {
		code, _, errs := ta.do(cmd)
		assert.Contains(t, []int{1, 2}, code, "%s must fail: %s", cmd, errs)
		for _, flag := range []string{"--until", "--for", "--indefinite"} {
			assert.Contains(t, errs, flag, "%s refusal must name %s: %s", cmd, flag, errs)
		}
		assert.Empty(t, holdsOf(t, ta), "%s wrote a hold", cmd)
	}

	// 2. --until in the past, --for 0s, --for -5m, --until + --for, --extend on unheld member,
	// and --for / --until / --indefinite / --extend on a stream, reader, or friend
	past := ta.a.now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	future := ta.a.now().UTC().Add(time.Hour).Format(time.RFC3339)
	refused := []struct {
		cmd  string
		want string
	}{
		{"hold m1 --until " + past + " --reason r", "--until"},
		{"hold m1 --until not-a-time --reason r", "--until"},
		{"hold m1 --for 0s --reason r", "--for"},
		{"hold m1 --for -5m --reason r", "--for"},
		{"hold m1 --until " + future + " --for 30m --reason r", "one of"},
		{"hold m1 --for 30m --indefinite --reason r", "one of"},
		{"hold m1 --until " + future + " --indefinite --reason r", "one of"},
		{"hold m1 --extend 15m", "not held"},
		{"hold m1 --extend 0s", "--extend"},
		{"hold m1 --extend -5m", "--extend"},
		{"hold m1 --extend 15m --for 30m --reason r", "--extend"},
		{"hold s1 --for 30m --reason r", "stream"},
		{"hold reader-a --for 30m --reason r", "reader"},
		{" hold amy --for 30m --reason r", "friend"},
		{"friend hold amy --for 30m --reason r", "--for"},
		{"hold s1 --until " + future + " --reason r", "stream"},
		{"hold reader-a --indefinite --reason r", "reader"},
		{"hold s1 --extend 15m", "stream"},
	}
	for _, tc := range refused {
		code, _, errs := ta.do(tc.cmd)
		assert.Contains(t, []int{1, 2}, code, "%s should be refused: %s", tc.cmd, errs)
		assert.Contains(t, errs, tc.want, "%s stderr: %s", tc.cmd, errs)
		assert.Empty(t, holdsOf(t, ta), "%s wrote a hold", tc.cmd)
	}

	// 3. --extend on an indefinite hold is refused and leaves the indefinite hold unchanged
	ta.ok("fleet hold m1 --indefinite --reason 'hardware check'")
	code, _, errs := ta.do("hold m1 --extend 15m")
	assert.Contains(t, []int{1, 2}, code, "extending an indefinite hold must be refused: %s", errs)
	assert.Contains(t, errs, "indefinite", "stderr: %s", errs)
	h, ok := ta.heldMember("m1")
	require.True(t, ok)
	assert.Empty(t, h.Until, "indefinite hold must still have no until")
	ta.ok("fleet unhold m1")
}

// nova-sprint where and nova-sprint where --json show when each held member's hold
// releases: "releases at <RFC3339> (in <d>)" (or "overdue by <d>" once past and before
// the tick releases it) for a timed hold and "indefinite" for an indefinite hold, and
// holds[].until carries the RFC3339 timestamp for the timed hold and is absent for the
// indefinite hold.
func TestWhereShowsWhenAHoldReleases(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 2")

	now := ta.a.now().UTC().Truncate(time.Second)
	wantUntil := now.Add(time.Hour).Format(time.RFC3339)

	ta.ok("hold m1 --for 1h --reason 'cooling check'")
	ta.ok("hold m2 --indefinite --reason 'cable swap'")

	// 1. Plain nova-sprint where text frame
	text := ta.ok("where")
	assert.Contains(t, text, "releases at "+wantUntil+" (in 1h)", "where text for timed hold:\n%s", text)
	assert.Contains(t, text, "indefinite", "where text for indefinite hold:\n%s", text)

	// 2. nova-sprint where --json (and where --json --cards) holds[].until
	for _, cmd := range []string{"where --json", "where --json --cards"} {
		raw := ta.ok(cmd)
		var v struct {
			Holds []sprint.HoldView      `json:"holds"`
			Raw   []map[string]any       `json:"-"`
			Table map[string]interface{} `json:"tables"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &v), cmd)
		var rawMap struct {
			Holds []map[string]any `json:"holds"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &rawMap), cmd)
		byName := map[string]sprint.HoldView{}
		rawByName := map[string]map[string]any{}
		for i, h := range v.Holds {
			byName[h.Name] = h
			rawByName[h.Name] = rawMap.Holds[i]
		}
		require.Contains(t, byName, "m1", "%s must list m1 in holds", cmd)
		require.Contains(t, byName, "m2", "%s must list m2 in holds", cmd)
		assert.Equal(t, wantUntil, byName["m1"].Until, "%s m1.until", cmd)
		assert.Empty(t, byName["m2"].Until, "%s m2.until must be empty", cmd)
		_, hasUntilKey := rawByName["m2"]["until"]
		assert.False(t, hasUntilKey, "%s m2 must omit until when indefinite: %v", cmd, rawByName["m2"])
	}

	// 3. Advance clock past m1's release time without ticking -> where shows overdue by 5m
	ta.mu.Lock()
	ta.now = ta.now.Add(65 * time.Minute)
	ta.mu.Unlock()
	overdueText := ta.ok("where")
	assert.Contains(t, overdueText, "releases at "+wantUntil+" (overdue by 5m)", "where text when overdue:\n%s", overdueText)
}

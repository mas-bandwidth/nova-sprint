//go:build functional

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// --- the idle alarm counts work, not reads (#102, #106) ---

// TestStallTheIdleAlarmCountsWorkNotReadsOnTheStore is #106 on a real store: the fleet's
// lanes held read cards while cards waited and no work card ran, and the idle count took the
// working reads for work, so the alarm never told the coordinator the fleet was idle. Two
// members of width 1 each hold a read of the other's card and nothing else while three cards
// wait behind an unlanded need: past the idle window the tick raises one idle note (once an
// episode, #102), naming the fleet's work as 0, and no more while the episode holds. The
// clock is the app's, stepped a minute a tick (the window is 5 minutes of running time).
func TestStallTheIdleAlarmCountsWorkNotReadsOnTheStore(t *testing.T) {
	t.Parallel()
	addr, a, do, _ := releaseStore(t, "coordinator")
	var mu sync.Mutex
	now := time.Now().UTC()
	a.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	step := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	members := []string{"m1", "m2"}
	do("init", "--readers", "reader-m1,reader-m2", "--members", "m1:1,m2:1")
	c := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = c.Close() })
	declareFunctionalRoutes(t, c, "flash")
	do("set", "--read-cards", "on", "--reads", "1")
	do("reader", "set", "reader-m1", "reader-m2", "--tiers", "all")
	do("add", "--stream", "s1", "--count", "2")
	do("add", "--stream", "s2", "--count", "3", "--needs", "s1-1")
	tick := func() string {
		for _, m := range members {
			do("fleet", "beat", m, "--load", "0")
		}
		return do("tick", "--idle-alarm")
	}
	take := func(m string) []sprint.Packet {
		var out output
		raw := do("take", "--as", m, "--max", "100", "--epoch", "0", "--json")
		require.NoError(t, json.Unmarshal([]byte(raw), &out), raw)
		return out.Packets
	}
	do("start")
	tick()
	tick()
	for _, m := range members {
		for _, p := range take(m) {
			require.Equal(t, "work", p.Kind)
			do("finish", "--as", m, p.Card+"@"+strconv.Itoa(p.Gen), "--epoch", "0", "--head", stallHead(p.Primary), "--report", "done", "--usage", "input=1000 output=100")
		}
	}
	reads := 0
	for i := 0; i < 3 && reads < 2; i++ {
		tick()
		for _, m := range members {
			for _, p := range take(m) {
				require.Equal(t, "read", p.Kind, "only reads are dealt: %+v", p)
				reads++ // taken and held: a read working on the member's lane
			}
		}
	}
	require.Equal(t, 2, reads, "each member holds a read of the other's card")

	st, err := a.store(common{redis: addr, actor: "tester"})
	require.NoError(t, err)
	idle := func() []sprint.Note {
		all, _, err := st.B.NotesSince(context.Background(), "", 100000)
		require.NoError(t, err)
		var out []sprint.Note
		for _, n := range all {
			if n.Type == sprint.NIdle && n.Kind == sprint.Happened && n.To != "" {
				out = append(out, n)
			}
		}
		return out
	}
	for i := 0; i < 9; i++ {
		step(time.Minute)
		tick()
	}
	notes := idle()
	require.Len(t, notes, 1, "a fleet whose lanes hold only reads while cards wait is idle: one alarm an episode")
	assert.True(t, strings.HasPrefix(notes[0].What, "fleet 0/"), "the fleet's work is 0: %s", notes[0].What)
}

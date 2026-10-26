package main

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// TestLaneTakeAutoReadsNeeds: the auto take's --needs is read with the words a
// GATE-NEEDS line carries, kept to the bench whose facts meet them, and refused when a
// word is not a fact word. The store is the in-memory fake, the clock is the test's at
// 1000.0 seconds, and no host fact is read.
func TestLaneTakeAutoReadsNeeds(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.mu.Lock()
	ta.now = time.Unix(1000, 0).UTC()
	ta.mu.Unlock()
	b := ta.a.landState()
	b.mu.Lock()
	b.hostName = func() (string, error) { return "coordinator.local", nil }
	b.mu.Unlock()

	var initOut, initErr bytes.Buffer
	initCode := runInto(ta.a, split("init --members m1,m2"), &initOut, &initErr)
	require.Equal(t, 0, initCode, initOut.String()+initErr.String())
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	beat := func() {
		t.Helper()
		zero := 0.0
		_, err := st.Beat(context.Background(), "m1", &zero, hostload.Source{NCPU: 64})
		require.NoError(t, err)
		_, err = st.Beat(context.Background(), "m2", &zero, hostload.Source{NCPU: 8})
		require.NoError(t, err)
	}
	run := func(line string) (int, string, string) {
		t.Helper()
		beat()
		var out, stderr bytes.Buffer
		code := runInto(ta.a, ta.withEpoch(split(line)), &out, &stderr)
		return code, out.String(), stderr.String()
	}
	for _, m := range []string{"m1", "m2"} {
		code, out, stderr := run("fleet up " + m)
		require.Equal(t, 0, code, out+stderr)
	}
	for member, facts := range map[string]string{"m1": "darwin,go", "m2": "linux,go"} {
		setLaneFacts(t, st, member, facts)
	}

	code, out, stderr := run("lane take go --machine auto --as alice --needs linux")
	require.Equal(t, 0, code, out+stderr)
	require.Contains(t, out, "LANE-TAKE OK go machine=m2 as=alice", "the linux take is granted the linux bench")
	require.NotContains(t, out, "machine=m1", "never the emptier darwin bench")

	code, out, stderr = run("lane take go --machine auto --as bob")
	require.Equal(t, 0, code, out+stderr)
	require.Contains(t, out, "LANE-TAKE OK go machine=m1 as=bob", "with no needs the emptiest bench is still the pick")

	code, out, stderr = run("lane take go --machine auto --as carol --needs windows")
	require.Equal(t, 1, code, out)
	wait := out + stderr
	require.Contains(t, wait, "LANE-TAKE WAIT go as=carol")
	require.Contains(t, wait, "GATE-NEEDS windows", "the wait names the need")
	require.Contains(t, wait, "m1 (facts: darwin,go)", "and the facts the benches carry")
	require.Contains(t, wait, "m2 (facts: linux,go)")

	code, out, stderr = run("lane take go --machine auto --as dave --needs 'linux,bad need'")
	require.Equal(t, 2, code, out)
	require.Contains(t, stderr, "--needs wants need words", "a word that is not a fact word is refused")
}

func setLaneFacts(t *testing.T, st *store.Store, member, facts string) {
	t.Helper()
	_, err := st.Run(context.Background(), store.Step{
		Verb: "test facts",
		Load: []string{sprint.Fleet},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			c := s.MemberCtl(member)
			if c == nil {
				return sprint.Plan{}
			}
			return sprint.Plan{Units: []sprint.Unit{{Key: c.ID, Changes: []sprint.Change{{
				Table: sprint.Fleet,
				Entry: ntable.BatchMemberEntry{
					ID:     c.ID,
					Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(c.Rev, 10), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}},
					Set:    map[string]string{sprint.FieldFacts: facts},
				},
			}}}}}
		},
	})
	require.NoError(t, err)
}

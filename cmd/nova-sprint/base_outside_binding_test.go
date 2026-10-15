package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The tree-gate result and git tips are fixtures; this runs the real binding's
// report against the memory store, without git, clocks, sockets or a bench.
func TestOutsideBaseRedBindingOpensOnTheFirstGate(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --members m1,m2 --readers reader-a,reader-b")
	ta.ok("add --stream s1 --count 1 --one")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	ctx := context.Background()
	epoch, err := st.EpochNow(ctx)
	require.NoError(t, err)
	const repo, base = "mas-bandwidth/nova-sprint", "v1.3/comms"
	landed, outside := strings.Repeat("a", 40), strings.Repeat("b", 40)
	_, err = st.Run(ctx, store.Step{Verb: "fixture", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		return sprint.Plan{Props: []sprint.PropWrite{{Table: sprint.Work, Name: fmt.Sprintf("base_landed.%x", sha256.Sum256([]byte(repo+"\x00"+base))), Value: landed, WasAbsent: true}}}
	}})
	require.NoError(t, err)
	l := &lander{a: ta.a, st: st, epoch: epoch.N, c: common{actor: "coordinator"}, baseWhy: "class gofmt is red", now: func() time.Time { return ta.now }}
	l.baseRefused(landBatch{Repo: repo, Base: base, Tip: outside}, "s1", "the outside tip failed its tree gate")
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(t, err)
	require.Equal(t, sprint.StreamStopped, s.StreamCtl("s1").F("state"), "first outside red gate must stop immediately")
	require.Empty(t, s.StreamCtl("s1").F(sprint.FieldBaseGateRefused), "the outside red is an episode, not a counted card refusal")
	var notes []sprint.Note
	for _, o := range s.Open {
		if o.Note.Type == sprint.NBaseRed {
			notes = append(notes, o.Note)
		}
	}
	require.Len(t, notes, 1)
	require.Contains(t, notes[0].What, outside)
	require.Contains(t, notes[0].What, "gofmt")
}

func TestOutsideBaseBindingGatesBeforeTryingACard(t *testing.T) {
	for _, known := range []bool{true, false} {
		for _, red := range []bool{false, true} {
			t.Run(fmt.Sprint("known=", known, "/red=", red), func(t *testing.T) {
				ta := newTestApp(t)
				ta.ok("init --members m1,m2 --readers reader-a,reader-b")
				ta.ok("add --stream s1 --count 1 --one")
				st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
				require.NoError(t, err)
				const repo, base = "mas-bandwidth/nova-sprint", "v1.3/comms"
				landed, outside := strings.Repeat("a", 40), strings.Repeat("b", 40)
				if known {
					_, err = st.Run(t.Context(), store.Step{Verb: "fixture", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
						return sprint.Plan{Props: []sprint.PropWrite{{Table: sprint.Work, Name: sprint.BaseLandedKey(repo, base), Value: landed, WasAbsent: true}}}
					}})
					require.NoError(t, err)
				}
				dir := t.TempDir() // no git repository: any attempted cure/merge would fail
				require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26.6\n"), 0600))
				calls := 0
				ta.a.landState().gateBench = func(_ context.Context, _, got string, _ [][]string, _ bool) (string, int, error) {
					require.Equal(t, dir, got)
					calls++
					if red {
						return "gofmt: example.go needs formatting", 1, nil
					}
					return "", 0, nil
				}
				l := &lander{a: ta.a, st: st, c: common{actor: "coordinator"}, now: func() time.Time { return ta.now },
					baseGateCache: map[string]string{landed: ""}, baseGateFails: map[string]*baseGateFail{}}
				merged, first, env, why := l.gateBase(t.Context(), dir, "s1", []landCard{{repo: repo, base: base, id: "s1-1", head: "queued-head"}}, outside)
				require.Equal(t, 1, calls, "the old tip's green cache cannot certify the outside tip")
				require.Empty(t, env, "no cure or git command is attempted")
				require.Empty(t, merged)
				require.Zero(t, first)
				if red {
					require.Contains(t, why, outside)
					require.Contains(t, why, "gofmt")
					require.True(t, l.baseCount)
					require.True(t, l.baseStop)
					require.Equal(t, outside, l.baseOutsideTip)
				} else {
					require.Empty(t, why, "green allows the caller to merge its next card")
					require.False(t, l.baseCount)
				}
			})
		}
	}
}

// Upgrades/restarts may have no durable landing record. That is unknown
// provenance, never permission to wait for three failures on a red tip.
func TestUnknownBaseSignalsItsFirstRedGate(t *testing.T) {
	ta := newTestApp(t)
	ta.ok("init --members m1,m2 --readers reader-a,reader-b")
	ta.ok("add --stream s1 --count 1 --one")
	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	epoch, err := st.EpochNow(t.Context())
	require.NoError(t, err)
	const repo, base = "mas-bandwidth/nova-sprint", "v1.3/comms"
	outside := strings.Repeat("b", 40)
	l := &lander{a: ta.a, st: st, epoch: epoch.N, c: common{actor: "coordinator"}, baseWhy: "class gofmt is red", now: func() time.Time { return ta.now }}
	l.baseRefused(landBatch{Repo: repo, Base: base, Tip: outside}, "s1", "unknown tip failed its gate")
	s, err := st.Load(t.Context(), []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(t, err)
	require.Equal(t, sprint.StreamStopped, s.StreamCtl("s1").F("state"), "unknown provenance must signal its first red gate")
	require.Empty(t, s.StreamCtl("s1").F(sprint.FieldBaseGateRefused))
	require.Len(t, s.Open, 1)
	require.Equal(t, sprint.NBaseRed, s.Open[0].Note.Type)
	require.Contains(t, s.Open[0].Note.What, outside)
}

// seedKnownLanderBase limits legacy cure/retry fixtures to a tip whose provenance
// is known. Unseeded startup tips intentionally use first-red protection.
func seedKnownLanderBase(r *landRig, base string) {
	r.t.Helper()
	tip := r.git(r.remote, "rev-parse", base)
	st, err := r.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(r.t, err)
	_, err = st.Run(r.t.Context(), store.Step{Verb: "fixture", Load: []string{sprint.Work}, Plan: func(s *sprint.Snapshot) sprint.Plan {
		key := sprint.BaseLandedKey(r.clone, base)
		was, had := s.Work.Prop(key)
		return sprint.Plan{Props: []sprint.PropWrite{{Table: sprint.Work, Name: key, Value: tip, Was: was, WasAbsent: !had}}}
	}})
	require.NoError(r.t, err)
}

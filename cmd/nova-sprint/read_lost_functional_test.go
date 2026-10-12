//go:build functional

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/member"
)

// Ghost reads, end to end (v1.2.14 BLOCKER, a-member-hands-back-a-read-it-is-not-running): a
// real member process (pkg/member) over the real verbs, on the sprint's clock. 2026-10-11 ~23:00Z
// about 20 reads sat working with no child for up to an hour.

// appSprint runs the member's verbs on the test's app, as the server would: stdout is the
// answer, stderr joins it only for a refusal.
type appSprint struct{ ta *testApp }

func (s appSprint) Run(args ...string) (int, []byte) {
	var out, errb bytes.Buffer
	code := runInto(s.ta.a, args, &out, &errb)
	if code != 0 {
		out.Write(errb.Bytes())
	}
	return code, out.Bytes()
}

// wallRunner starts each read as a child that runs, except the cards named in wall, whose
// first child's stage fails as b2's did (the wall refused a write outside the job).
type wallRunner struct {
	mu     sync.Mutex
	wall   map[string]bool
	starts map[string]int
}

type fnChild struct {
	done bool
	res  member.Result
}

func (c fnChild) Done() bool            { return c.done }
func (c fnChild) Result() member.Result { return c.res }

func (r *wallRunner) Start(p member.Packet) (member.Child, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts[p.Card]++
	if r.wall[p.Card] && r.starts[p.Card] == 1 {
		return fnChild{done: true, res: member.Result{End: member.EndStaging, Staging: "the wall refused a write outside the job"}}, nil
	}
	return fnChild{}, nil
}

// memberProc is one process of member m2: a new one is a restart.
func memberProc(ta *testApp, run *wallRunner) *member.Member {
	return member.New(member.Config{As: "m2", Now: ta.a.now}, appSprint{ta}, run, nil, io.Discard)
}

// readsOn is the read cards working on m2, and the beat's word on each (running or held).
func readsOn(t *testing.T, ta *testApp, primaries ...string) (working []*sprint.Card, all []*sprint.Card) {
	t.Helper()
	for _, p := range primaries {
		var card cardView
		ta.json("card "+p, &card)
		for _, rc := range card.Reads {
			all = append(all, rc)
			if rc.Placed() && rc.Row == "m2" && rc.Col == sprint.Working {
				working = append(working, rc)
			}
		}
	}
	return working, all
}

// readsOnM2 is a sprint whose two primaries (worked by m1) are each in review with their one
// read dealt to m2, the one reader not their worker, and m2's first member process a.
func readsOnM2(t *testing.T) (*testApp, *wallRunner, *member.Member, []*sprint.Card) {
	t.Helper()
	ta := newTestApp(t)
	ta.live = []string{"m1"} // m2 beats only from its member process (its live set)
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-m2 --members m1:8,m2:8")
	ta.ok("set --read-cards on")
	ta.ok("reader set reader-m2 --tiers all")
	dir := t.TempDir()
	for _, id := range []string{"s1-1", "s1-2"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"), []byte(passingBrief(id+": a flash card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nHOW THIS CARD IS JUDGED\nthe flag is reachable from the verb\n")), 0o644))
	}
	ta.ok("add --stream s1 --brief-dir " + dir)
	run := &wallRunner{wall: map[string]bool{}, starts: map[string]int{}}
	a := memberProc(ta, run)
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 2")
	require.NoError(t, a.Beat()) // m2 comes up only now: the work was m1's, the reads are m2's
	ta.ok("finish --as m1 s1-1.w1@1 --report done --head " + landHead)
	ta.ok("finish --as m1 s1-2.w1@1 --report done --head " + landHead)
	require.NoError(t, a.Beat())
	ta.ok("tick") // m2 is up from this tick's presence part
	require.NoError(t, a.Beat())
	ta.ok("tick") // and is dealt the reads
	_, all := readsOn(t, ta, "s1-1", "s1-2")
	if len(all) != 2 {
		t.Log(ta.ok("card s1-1"))
		t.Log(ta.ok("where --all"))
		t.Log(ta.ok("log"))
	}
	require.Len(t, all, 2, "each primary's read dealt to m2, the one reader not its worker")
	return ta, run, a, all
}

func TestAMemberProcessKilledOrRestartedLeavesNoGhostRead(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	ta, run, a, all := readsOnM2(t)
	run.wall[all[0].ID] = true // its first child's stage fails, as b2's did

	pass := func(m *member.Member) {
		t.Helper()
		_, err := m.Tick(ta.a.now())
		require.NoError(t, err)
		require.NoError(t, m.Beat())
		ta.ok("tick")
	}
	step := func(d time.Duration) {
		ta.mu.Lock()
		ta.now = ta.now.Add(d)
		ta.mu.Unlock()
	}

	// 1. the member takes both reads; one child's stage fails, and the read runs again from
	// the launch's own packet after ReadStageRetry: its beat never names a read it runs no
	// child for, so none is a ghost
	pass(a)
	working, _ := readsOn(t, ta, "s1-1", "s1-2")
	require.Len(t, working, 2, "the member took both reads")
	step(member.ReadStageRetry + time.Second)
	pass(a)
	step(sprint.BeatDeadline)
	pass(a)
	require.Equal(t, 2, run.starts[all[0].ID], "the stage failure ran again, once")
	working, _ = readsOn(t, ta, "s1-1", "s1-2")
	require.Len(t, working, 2, "both reads still run on m2: named running, never returned")

	// 2. the process is restarted: the new one recovers what the queue lists working (a
	// restart loses nothing and deals nothing twice)
	b := memberProc(ta, run)
	require.NoError(t, b.Beat()) // its first beat, before its first pass: no live set
	pass(b)
	step(sprint.LiveGrace + sprint.BeatDeadline)
	pass(b)
	working, _ = readsOn(t, ta, "s1-1", "s1-2")
	require.Len(t, working, 2, "the restart ran its reads again, named by its beat")

	// 3. the process is killed and its replacement runs nothing for them (it drains: its
	// binary was replaced again mid-rollout); its beats carry its live set, which is empty.
	// Within one beat and the grace both reads are back in the deal, no reader spent.
	c := memberProc(ta, run)
	c.Drain()
	killed := ta.a.now()
	require.NoError(t, c.Beat())
	for ta.a.now().Sub(killed) <= sprint.ReadCardDeadline {
		pass(c)
		if working, _ = readsOn(t, ta, "s1-1", "s1-2"); len(working) == 0 {
			break
		}
		step(sprint.BeatDeadline / 3)
	}
	require.Empty(t, working, "no read working on m2 that no child of m2's runs")
	require.LessOrEqual(t, ta.a.now().Sub(killed), sprint.LiveGrace+2*sprint.BeatDeadline, "back within a beat and the grace, never at the 60-minute read deadline")
	_, all = readsOn(t, ta, "s1-1", "s1-2")
	for _, rc := range all {
		require.Equal(t, sprint.RetiredByLost, rc.F("retired_by"), "%s: taken back lost, spending no one", rc.ID)
	}
	// the deal cut each read again at its next generation, to m2 (the one reader not the
	// worker: the lost read spent nothing of it)
	var q struct {
		Cards []struct {
			ID  string `json:"id"`
			Col string `json:"col"`
		} `json:"cards"`
	}
	ta.json("queue --as m2", &q)
	var again []string
	for _, c := range q.Cards {
		if strings.HasSuffix(c.ID, ".g1") {
			again = append(again, c.ID)
		}
	}
	require.ElementsMatch(t, []string{"s1-1.r1.m2.g1", "s1-2.r1.m2.g1"}, again, "each read dealt again at its next generation: the lost member's reader is not spent")
	t.Logf("wall time %s", time.Since(wall).Round(time.Millisecond))
}

// A member process killed with no replacement (SIGKILL: its children die with it, its beats
// stop): a silent member is no evidence for the reconcile (a partition may still run its
// children), so its reads come back by the down path, once MissedBeatsDown beat windows have
// passed with no beat, never at the 60-minute read deadline, spending no reader.
func TestAMemberKilledSilentGivesItsReadsBackByTheDownPath(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	ta, _, a, _ := readsOnM2(t)
	_, err := a.Tick(ta.a.now())
	require.NoError(t, err)
	require.NoError(t, a.Beat())
	ta.ok("tick")
	working, _ := readsOn(t, ta, "s1-1", "s1-2")
	require.Len(t, working, 2, "the member took both reads")

	killed := ta.a.now() // a is gone: no pass, no beat from here
	for ta.a.now().Sub(killed) <= sprint.ReadCardDeadline {
		ta.mu.Lock()
		ta.now = ta.now.Add(sprint.BeatDeadline / 3)
		ta.mu.Unlock()
		ta.ok("tick")
		if working, _ = readsOn(t, ta, "s1-1", "s1-2"); len(working) == 0 {
			break
		}
	}
	require.Empty(t, working, "a killed member's reads leave it")
	bound := sprint.MissedBeatsDown*sprint.BeatDeadline + sprint.BeatDeadline
	require.LessOrEqual(t, ta.a.now().Sub(killed), bound, "back by the down path within its bound, never at the read deadline")
	_, all := readsOn(t, ta, "s1-1", "s1-2")
	for _, rc := range all {
		require.NotContains(t, []string{"read", sprint.RetiredByReturned, sprint.RetiredByLate}, rc.F("retired_by"), "%s: a down member's read spends no one", rc.ID)
	}
	t.Logf("back after %s on the sprint clock; wall time %s", ta.a.now().Sub(killed), time.Since(wall).Round(time.Millisecond))
}

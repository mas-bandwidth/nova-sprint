package sprint_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Merge health in the coordinator's pass (docs/SPEC-SPRINT.md section 8, "The
// coordinator's pass", merge health), on the twin store with a fake clock, a twin
// repository (the dev sync's: a bare origin with main, dev and the base) and a fake forge:
// one judgment about the sprint naming the base red at its tip, the promotion PR's state
// and the branches off the base, rewritten in place as they move, raised again with a push
// every ten minutes of running time while any line holds, and closed when none does.

// fakeForge is the forge's half of the drift facts: the open promotion PR, the last gate
// run at the base (its tip is the base's tip as read, unless it names one), and the branch
// the running server was built from.
type fakeForge struct {
	pr     *sprint.PromotionPR
	gate   *sprint.DriftGate
	server string
}

// readDrift is the binding's read: the branches the store's cards and the server name,
// measured in the land clone, the forge's facts added, recorded in one step.
func (r *passRig) readDrift(repo *syncRepo, forge *fakeForge) {
	r.t.Helper()
	named := sprint.BranchesNamed(r.snap(), "", syncBase, forge.server)
	f, err := sprint.ReadDrift(r.ctx, sprint.DriftGitReq{RepoDir: repo.land, Env: repo.env, Base: syncBase, Branches: named})
	require.NoError(r.t, err)
	f.Promotion = forge.pr
	if forge.gate != nil {
		g := *forge.gate
		if g.Tip == "" {
			g.Tip = f.BaseTip
		}
		f.Gate = &g
	}
	r.must(store.Step{Verb: "drift-read", Load: []string{sprint.Work, sprint.Merge}, Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.DriftRead(s, f) }})
}

func TestTheCoordinatorPassCarriesMergeHealthEveryTenMinutes(t *testing.T) {
	t.Parallel()
	r := newPassRig(t)
	repo := newSyncRepo(t)
	forge := &fakeForge{server: syncBase}
	sprintWide := sprint.StreamSubject("")
	// every friend's session answers: the pass judges merge health alone here
	step := func(d time.Duration) {
		r.t.Helper()
		r.readDrift(repo, forge)
		r.pongs["amy"], r.pongs["bob"] = r.clock(), r.clock()
		r.tick(d)
	}
	health := func() *sprint.Note { return r.open(sprint.NMergeHealth, sprintWide) }
	pushes := func() int { return r.count(sprint.Happened, sprint.NRaisedAgain, sprint.NMergeHealth) }

	// stitched: the base on origin, no PR open, the gate green at the tip, the server on the base
	forge.gate = &sprint.DriftGate{Whole: true, Functional: true}
	step(time.Second)
	assert.Nil(t, health(), "nothing is wrong: no judgment")

	// drift: a side branch one commit ahead named by an open card, the server built from a
	// branch the remote does not have, the promotion PR failing, the base red at its tip
	repo.git(repo.dev, "push", "-q", "origin", "refs/remotes/origin/"+syncBase+":refs/heads/tmp/rescue")
	repo.commit("tmp/rescue", "rescue.go", "package rescue\n", "a rescue on a side branch")
	r.must(store.AddStep(sprint.AddReq{Stream: "side", Cards: []sprint.CardAdd{{ID: "side-1", Base: "tmp/rescue",
		Brief: "c: a side card tier: pro\nREPO: mas-bandwidth/nova-sprint\nBASE: tmp/rescue\n\nThe task."}}}))
	r.tick(time.Second) // the card is placed by the tick after its add; the facts recorded say nothing yet
	require.Nil(t, health())
	forge.server = "tmp/live"
	forge.pr = &sprint.PromotionPR{Number: 42, State: sprint.PRFailing}
	forge.gate = &sprint.DriftGate{Whole: true, Functional: true, Red: true, Failed: []string{"TestTheBaseHolds"}}
	step(time.Second)
	h := health()
	require.NotNil(t, h, "merge health is a judgment once anything is wrong")
	opened := r.clock()
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NMergeHealth, ""), "one judgment about the sprint")
	assert.True(t, h.StreamLevel)
	assert.Equal(t, []string{"act", "wait"}, h.Decisions, "no ack: it holds until the branches are stitched")
	assert.Contains(t, h.What, "base "+syncBase+" is red at its tip")
	assert.Contains(t, h.What, "TestTheBaseHolds")
	assert.Contains(t, h.What, "promotion PR #42 is failing")
	assert.Contains(t, h.What, "branch tmp/rescue (named by side-1) is not on "+syncBase+": 1 ahead, 0 behind")
	assert.Contains(t, h.What, "branch tmp/live (named by the running server) is not on the remote")
	assert.Less(t, strings.Index(h.What, "is red at its tip"), strings.Index(h.What, "promotion PR"), "the base first")
	assert.Less(t, strings.Index(h.What, "promotion PR"), strings.Index(h.What, "branch tmp/live"), "the branches last, by name")
	assert.Less(t, strings.Index(h.What, "branch tmp/live"), strings.Index(h.What, "branch tmp/rescue"))
	assert.Zero(t, pushes())

	// the facts move inside the ten minutes: the base gains a commit, the PR is queued;
	// rewritten in place, no second judgment and no push
	for range 4 {
		step(time.Minute)
	}
	repo.commit(syncBase, "base.go", "package base\n", "the base moves on")
	forge.pr.State = sprint.PRQueued
	for range 5 {
		step(time.Minute)
	}
	require.Less(t, r.clock().Sub(opened), sprint.PassEvery)
	h = health()
	require.NotNil(t, h)
	assert.Contains(t, h.What, "promotion PR #42 is queued", "the latest state")
	assert.Contains(t, h.What, "1 ahead, 1 behind", "the latest count")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NMergeHealth, ""), "one judgment an episode")
	assert.Zero(t, pushes(), "not raised again within ten minutes")

	// ten minutes of running time after it was written: raised again once, a push to the
	// coordinator that wakes inbox --wait; two ticks that minute push once
	ends := r.tickEnds()
	step(sprint.PassEvery - r.clock().Sub(opened))
	step(time.Second)
	assert.Equal(t, 1, pushes(), "raised again every ten minutes, never every tick")
	assert.Greater(t, r.tickEnds(), ends, "the push wakes inbox --wait")
	for _, n := range r.notes() {
		if n.Type == sprint.NRaisedAgain {
			assert.Equal(t, "coordinator", n.To, "the push is the coordinator's")
		}
	}
	h = health()
	require.NotNil(t, h)
	assert.Equal(t, 1, h.Before, "it counts its raises again")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NMergeHealth, ""))

	// twenty minutes: a second push
	step(sprint.PassEvery)
	assert.Equal(t, 2, pushes())

	// stitched again: the base merged into the side branch's work (the side branch merged
	// onto the base), the server rebuilt on the base, the PR merged, the gate green: closed
	repo.git(repo.dev, "fetch", "-q", "origin")
	repo.git(repo.dev, "checkout", "-q", "-B", syncBase, "origin/"+syncBase)
	repo.git(repo.dev, "merge", "-q", "--no-edit", "origin/tmp/rescue")
	repo.git(repo.dev, "push", "-q", "origin", "HEAD:refs/heads/"+syncBase)
	forge.server = syncBase
	forge.pr = nil
	forge.gate = &sprint.DriftGate{Whole: true, Functional: true}
	step(time.Second)
	assert.Nil(t, health(), "nothing is wrong: closed")

	// closed stays closed: ten quiet minutes push nothing
	step(sprint.PassEvery)
	assert.Equal(t, 2, pushes(), "nothing holds, nothing is pushed")
	assert.Equal(t, 1, r.count(sprint.Judgment, sprint.NMergeHealth, ""))

	// a narrow red gate, or a red run at an older tip, judges nothing
	forge.gate = &sprint.DriftGate{Whole: false, Functional: true, Red: true, Failed: []string{"TestNarrow"}}
	step(time.Second)
	assert.Nil(t, health(), "the lander's narrow gate alone never judges the base")
	forge.gate = &sprint.DriftGate{Tip: repo.tip("dev"), Whole: true, Functional: true, Red: true, Failed: []string{"TestOld"}}
	step(time.Second)
	assert.Nil(t, health(), "a run at another tip judges nothing")

	// a new episode is a new judgment: the PR conflicts
	forge.pr = &sprint.PromotionPR{Number: 43, State: sprint.PRConflicted}
	step(time.Second)
	h = health()
	require.NotNil(t, h)
	assert.Contains(t, h.What, "promotion PR #43 is conflicted")
	assert.Equal(t, 2, r.count(sprint.Judgment, sprint.NMergeHealth, ""), "a new episode, a new judgment")
}

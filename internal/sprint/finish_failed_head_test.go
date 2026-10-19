package sprint_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
)

// failedFinishRig is a fake store and clock. Its first observation is Unix 1000s:
// every Finish below is planned and applied without wall time, sockets, git, or Redis.
type failedFinishRig struct {
	t   *testing.T
	ctx context.Context
	m   *store.Mem
	st  *store.Store
	mu  sync.Mutex
	now time.Time
}

func newFailedFinishRig(t *testing.T, cards int) *failedFinishRig {
	t.Helper()
	r := &failedFinishRig{t: t, ctx: context.Background(), m: store.NewMem(), now: time.Unix(1000, 0).UTC()}
	n := 0
	r.st = &store.Store{B: r.m, Names: sprint.Names{Prefix: "f-"}, Actor: "coordinator",
		Now:   func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		NewID: func() string { r.mu.Lock(); defer r.mu.Unlock(); n++; return fmt.Sprint(n) },
		Sleep: func(time.Duration) {}}
	require.NoError(t, r.st.Init(r.ctx))
	require.NoError(t, r.m.RowsAdd(r.ctx, "f-readers", []string{"reader-a", "reader-b", "reader-c"}))
	require.NoError(t, r.m.SetCoordinator(r.ctx, "coordinator"))
	r.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "provider-a", Model: "model-a", Tokens: 1000, Deadline: 600, Enabled: true},
		{Name: "pro-a", Tier: "pro", Provider: "provider-b", Model: "model-b", Tokens: 1000, Deadline: 600, Enabled: true},
	})
	require.NoError(t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	_, err := r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(t, err)
	r.must(store.FleetStep(sprint.FleetReq{Op: "up", Probe: sprint.EnvProbePassed, Member: "m1", Width: 4}))
	brief := "f: finish head (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\nPATHS: internal/x/a.go\n\nTHE TASK. Check the pushed head.\n"
	r.must(store.AddStep(sprint.AddReq{Stream: "s1", Count: cards, Brief: brief}))
	_, _, _, err = r.st.SetMachine(r.ctx, true)
	require.NoError(t, err)
	return r
}

func (r *failedFinishRig) must(step store.Step) store.Result {
	r.t.Helper()
	res, err := r.st.Run(r.ctx, step)
	require.NoError(r.t, err, step.Verb)
	require.Empty(r.t, res.Refused, "%s refused", step.Verb)
	return res
}

func (r *failedFinishRig) snap() *sprint.Snapshot {
	r.t.Helper()
	s, err := r.st.Load(r.ctx, store.All, nil)
	require.NoError(r.t, err)
	return s
}

func (r *failedFinishRig) take(id string) *sprint.Card {
	r.t.Helper()
	req := sprint.DealReq{Sel: sprint.Sel{IDs: []string{id}}}
	res := r.must(store.Step{Args: store.ArgsOf(req), Verb: "deal", Load: []string{sprint.Work, sprint.Fleet, sprint.Merge}, Mirrors: true, Routes: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.Deal(s, req) }})
	require.NotEmpty(r.t, res.Moved, "deal did not place the work card")
	r.tick()
	s := r.snap()
	wc := s.Fleet.Card(sprint.WorkCardID(id, 1))
	require.NotNil(r.t, wc, "the deal cuts the work card into fleet ready")
	require.Equal(r.t, sprint.Ready, wc.Col)
	r.must(store.TakeStep(sprint.TakeReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}}, Gens: map[string]int{wc.ID: wc.Int("gen")}}))
	r.tick()
	return r.snap().Fleet.Card(wc.ID)
}

func (r *failedFinishRig) tick() {
	r.t.Helper()
	_, _, _, err := r.st.SetMachine(r.ctx, true)
	require.NoError(r.t, err)
	r.mu.Lock()
	r.now = r.now.Add(time.Second)
	r.mu.Unlock()
	require.NoError(r.t, r.st.BeatReaders(r.ctx))
	zero := 0.0
	_, err = r.st.Beat(r.ctx, "m1", &zero, hostload.Source{})
	require.NoError(r.t, err)
	_, err = r.st.Tick(r.ctx)
	require.NoError(r.t, err)
}

func (r *failedFinishRig) finish(wc *sprint.Card, report, head string, exit int) {
	r.t.Helper()
	r.must(store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}},
		Gens: map[string]int{wc.ID: wc.Int("gen")}, Failed: true, Head: head, Report: report, Exit: exit, Who: wc.Row}))
	r.tick() // the running machine applies the work-table change at its next pump
}

func TestAFailedFinishRecordsItsHead(t *testing.T) {
	t.Parallel()
	const pushed = "0123456789abcdef0123456789abcdef01234567"

	t.Run("a failed finish with its pushed head records both table rows and both next starts", func(t *testing.T) {
		t.Parallel()
		r := newFailedFinishRig(t, 1)
		wc := r.take("s1-1")
		r.finish(wc, "HOLD: gate owed", pushed, 0)

		s := r.snap()
		pr := s.Work.Card("s1-1")
		finished := s.Fleet.Card(wc.ID)
		require.Equal(t, sprint.Review, pr.Col)
		assert.Equal(t, pushed, finished.F("head"), "the work card records the commit it pushed")
		assert.Equal(t, pushed, pr.F("head"), "the primary records the same pushed commit")
		assert.Equal(t, sprint.Finished, finished.Col)

		// rework --carry is the ordinary next attempt and brief --widen's source reads this
		// same primary head; both prove that a HOLD can resume from its pushed commit.
		r.must(store.ReworkStep(sprint.ReworkReq{Sel: sprint.Sel{IDs: []string{pr.ID}}, Carry: true, Fix: "continue from pushed head"}))
		r.tick()
		next := r.snap().Work.Card(pr.ID)
		assert.Contains(t, next.F("brief"), "CARRY: s1-1 attempt 1 head="+pushed)
		assert.Equal(t, "continue from pushed head", next.F("fix"))
	})

	t.Run("a failed finish with no pushed work records no head", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name, report string
			exit         int
		}{
			{"take ended", "provider failure: runner could not start", 0},
			{"environment exit", "child ended before it could report", 137},
			{"staging refusal", "staging refused: the launch was not accepted", 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				// These routes never pushed a commit, so their outcome must not preserve the
				// supplied head as if it were a source for the next attempt. The tick after
				// the finish may place the returned card again; the head checks are the route's.
				r := newFailedFinishRig(t, 1)
				wc := r.take("s1-1")
				r.finish(wc, tc.report, pushed, tc.exit)
				s := r.snap()
				assert.Empty(t, s.Fleet.Card(wc.ID).F("head"))
				assert.Empty(t, s.Work.Card("s1-1").F("head"))
			})
		}
	})

	t.Run("needs-stop returns a card without recording an unpushed head", func(t *testing.T) {
		t.Parallel()
		// This was a coordination return before judging the attempt; no change was pushed.
		r := newFailedFinishRig(t, 2)
		wc := r.take("s1-1")
		brief := r.snap().Work.Card("s1-1").F("brief")
		r.must(store.BriefStep(sprint.BriefReq{ID: "s1-1", Brief: brief + "\n\nThe dependency was added after this take.\n", Needs: []string{"s1-2"}, ReplaceInFlight: true}))
		r.tick()
		r.finish(wc, "HOLD: gate owed", pushed, 0)
		s := r.snap()
		assert.Equal(t, sprint.Waiting, s.Work.Card("s1-1").Col)
		assert.Empty(t, s.Fleet.Card(wc.ID).F("head"))
		assert.Empty(t, s.Work.Card("s1-1").F("head"))
	})

	t.Run("no head retains the card id default, which is not a widenable sha", func(t *testing.T) {
		t.Parallel()
		r := newFailedFinishRig(t, 1)
		wc := r.take("s1-1")
		r.finish(wc, "HOLD: gate owed", "", 0)
		s := r.snap()
		assert.Equal(t, wc.ID, s.Fleet.Card(wc.ID).F("head"))
		assert.Equal(t, wc.ID, s.Work.Card("s1-1").F("head"))
		assert.False(t, fullSHA(s.Work.Card("s1-1").F("head")), "widen requires a full commit SHA")
	})

	t.Run("a failed finish refuses a head that is neither a full SHA nor the card id default", func(t *testing.T) {
		t.Parallel()
		r := newFailedFinishRig(t, 1)
		wc := r.take("s1-1")
		res, err := r.st.Run(r.ctx, store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}},
			Gens: map[string]int{wc.ID: wc.Int("gen")}, Failed: true, Head: "branch-name", Report: "HOLD: gate owed", Who: wc.Row}))
		require.NoError(t, err)
		require.Len(t, res.Refused, 1)
		assert.Contains(t, res.Refused[0].Why, "full commit SHA")
		assert.Equal(t, sprint.Working, r.snap().Fleet.Card(wc.ID).Col, "a bad head never reaches the next attempt")
	})

	t.Run("a no-push finish carrying a branch-name head is routed, not refused", func(t *testing.T) {
		t.Parallel()
		// An older JOB.md's FINISH-FAILED line passes the staged branch as --head. A take
		// that ended with nothing pushed records no head on its route, so the head rule
		// has nothing to guard there and must not turn the route into a refusal.
		r := newFailedFinishRig(t, 1)
		wc := r.take("s1-1")
		res, err := r.st.Run(r.ctx, store.FinishStep(sprint.FinishReq{As: wc.Row, Sel: sprint.Sel{IDs: []string{wc.ID}},
			Gens: map[string]int{wc.ID: wc.Int("gen")}, Failed: true, Head: "sprint/s1",
			Report: "provider failure: runner could not start", Who: wc.Row}))
		require.NoError(t, err)
		assert.Empty(t, res.Refused, "the take-ended route is taken, not refused for its head")
		s := r.snap()
		assert.NotEqual(t, sprint.Working, s.Fleet.Card(wc.ID).Col, "the take ended: the work card left working")
		assert.Empty(t, s.Fleet.Card(wc.ID).F("head"))
		assert.Empty(t, s.Work.Card("s1-1").F("head"))
	})

	t.Run("brief widen carries the failed finish head", func(t *testing.T) {
		t.Parallel()
		r := newFailedFinishRig(t, 1)
		wc := r.take("s1-1")
		r.finish(wc, "HOLD: PATHS is too narrow; needs docs/NOTE.md.\nPATHS-PROPOSED: docs/NOTE.md\n", pushed, 0)
		s := r.snap()
		last := s.Fleet.Card(wc.ID)
		pr := s.Work.Card("s1-1")
		// brief --widen reads the latest work card's head before it builds the CARRY line.
		assert.Equal(t, pushed, last.F("head"))
		assert.Equal(t, pushed, pr.F("head"))
		assert.Contains(t, last.F("report"), "PATHS-PROPOSED: docs/NOTE.md")
	})
}

func fullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

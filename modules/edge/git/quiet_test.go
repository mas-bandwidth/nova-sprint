package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// The landing's CI comes first (LANDING.md; Glenn 2026-11-15): a pass that
// pushes a group gate cancels every unfinished run that gates no landing
// (the request's and the quiet cards' branches, a replaced group head) and
// leaves the gate's own run, base's, another stream's and a card still
// being worked.
func TestLandCancelsTheRunsThatGateNoLanding(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, c2 := landWorld_internal()
	ff.SetRuns(
		forge.Run{ID: 1, Branch: "sprint/s1/c1", HeadSHA: c1, Status: "queued", Event: "pull_request"},
		forge.Run{ID: 2, Branch: "sprint/s1/c2", HeadSHA: c2, Status: "in_progress", Event: "pull_request"},
		forge.Run{ID: 3, Branch: "sprint/s1/c9", HeadSHA: "9999999999999999999999999999999999999999", Status: "queued", Event: "pull_request"},
		forge.Run{ID: 4, Branch: GroupBranch("s1"), HeadSHA: "8888888888888888888888888888888888888888", Status: "queued", Event: "pull_request"},
		forge.Run{ID: 5, Branch: "v2", HeadSHA: c1, Status: "queued", Event: "push"},
		forge.Run{ID: 6, Branch: "sprint/s2/d1", HeadSHA: c1, Status: "queued", Event: "pull_request"},
		forge.Run{ID: 7, Branch: "sprint/s1/w1", HeadSHA: c1, Status: "queued", Event: "pull_request"},
	)
	req := group_internal(c1, c2)
	req.Quiet = []string{"sprint/s1/c9"}
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Rung != 2 || res.GatedSHA == "" {
		t.Fatalf("pass = %+v err=%v, want a group gate pushed", res, err)
	}
	if left := runsOf(ff); len(left) != 3 {
		t.Fatalf("runs left %+v, want base's, the other stream's and the worked card's", left)
	}
	if got := ff.Cancelled(); !slices.Equal(got, []int64{1, 2, 3, 4}) || !slices.Equal(res.Cancelled, []int64{1, 2, 3, 4}) {
		t.Fatalf("cancelled %v (result %v), want the card, quiet and replaced group runs 1 2 3 4", got, res.Cancelled)
	}
}

func runsOf(ff *fakeForge_internal) []forge.Run {
	ff.world.mu.Lock()
	defer ff.world.mu.Unlock()
	return slices.Clone(ff.world.runs)
}

// The gate's own run is never cancelled: at rung 2 the group branch at the
// gated sha, at rung 1 the card's branch at its head; an older head of the
// card's branch is.
func TestLandKeepsTheGatesOwnRun(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, c2 := landWorld_internal()
	res, err := LandPass(context.Background(), gf, ff, group_internal(c1, c2))
	if err != nil {
		t.Fatal(err)
	}
	gate := res.GatedSHA
	ff.SetRuns(forge.Run{ID: 10,
		Branch: GroupBranch("s1"), HeadSHA: gate, Status: "in_progress"})
	if res, err = LandPass(context.Background(), gf, ff, group_internal(c1, c2)); err != nil || res.GatedSHA != gate {
		t.Fatalf("pass 2 = %+v err=%v, want the same gate %s", res, err, gate)
	}
	if len(ff.Cancelled()) != 0 {
		t.Fatalf("cancelled %v, want the group gate's own run kept", ff.Cancelled())
	}

	gf1, ff1, _, d1, _ := landWorld_internal()
	ff1.SetRuns(
		forge.Run{ID: 20, Branch: "sprint/s1/c1", HeadSHA: d1, Status: "queued"},
		forge.Run{ID: 21, Branch: "sprint/s1/c1", HeadSHA: "7777777777777777777777777777777777777777", Status: "queued"},
	)
	res, err = LandPass(context.Background(), gf1, ff1, one_internal(d1))
	if err != nil || res.Rung != 1 {
		t.Fatalf("rung 1 pass = %+v err=%v", res, err)
	}
	if got := ff1.Cancelled(); !slices.Equal(got, []int64{21}) {
		t.Fatalf("rung 1 cancelled %v, want only the older head's run 21", got)
	}
}

// Each refusal: a run list that cannot be read, or a cancel that fails,
// is said in the reason and never stops the pass; a kept gate cancels
// nothing (once per gate pushed).
func TestLandCancelRefusals(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, c2 := landWorld_internal()
	ff.world.runsErr = errors.New("HTTP 502")
	res, err := LandPass(context.Background(), gf, ff, group_internal(c1, c2))
	if err != nil || res.GatedSHA == "" || !strings.Contains(res.CancelFault, "the unfinished runs could not be read to cancel those that gate no landing: HTTP 502") {
		t.Fatalf("pass = %+v err=%v, want the read failure in the reason and the pass going on", res, err)
	}

	gf2, ff2, _, e1, e2 := landWorld_internal()
	cf := &cancelFails{fakeForge_internal: ff2}
	ff2.SetRuns(forge.Run{ID: 30, Branch: "sprint/s1/c1", HeadSHA: e1, Status: "queued"})
	res, err = LandPass(context.Background(), gf2, cf, group_internal(e1, e2))
	if err != nil || res.GatedSHA == "" || len(res.Cancelled) != 0 || !strings.Contains(res.CancelFault, "1 runs could not be cancelled; the first: run 30 on sprint/s1/c1: HTTP 403") {
		t.Fatalf("pass = %+v err=%v, want the cancel failure in the reason", res, err)
	}

	gf3, ff3, _, k1, k2 := landWorld_internal()
	first, err := LandPass(context.Background(), gf3, ff3, group_internal(k1, k2))
	if err != nil || first.Kept().SHA == "" {
		t.Fatalf("pass 1 = %+v err=%v, want a kept gate", first, err)
	}
	ff3.SetRuns(forge.Run{ID: 40, Branch: "sprint/s1/c1", HeadSHA: k1, Status: "queued"})
	req := group_internal(k1, k2)
	req.Gate = first.Kept()
	if _, err := LandPass(context.Background(), gf3, ff3, req); err != nil {
		t.Fatal(err)
	}
	if len(ff3.Cancelled()) != 0 {
		t.Fatalf("a kept gate cancelled %v: want nothing until a new gate is pushed", ff3.Cancelled())
	}
}

type cancelFails struct{ *fakeForge_internal }

func (c *cancelFails) CancelRun(context.Context, string, int64) error {
	return errors.New("HTTP 403: Resource not accessible")
}

// A Fresh request (every landing the tick asks since 2026-11-15) gates
// even one head that contains the base tip on the group branch: its own
// head's runs may have been cancelled.
func TestLandFreshGatesOneHeadOnTheGroupBranch(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, _ := landWorld_internal()
	req := one_internal(c1)
	req.Fresh = true
	res, err := LandPass(context.Background(), gf, ff, req)
	if err != nil || res.Rung != 2 || res.Gate.Branch != GroupBranch("s1") {
		t.Fatalf("pass = %+v err=%v, want rung 2 on the group branch (Client.MergeGroup merges --no-ff: a fresh sha)", res, err)
	}
}

// A rate limit stops the cancelling for the pass (one failure said, the
// rest left for the next gate), and one pass cancels at most CancelMax.
func TestLandCancelStopsAtARateLimitAndAtCancelMax(t *testing.T) {
	t.Parallel()
	gf, ff, _, c1, c2 := landWorld_internal()
	ff.SetRuns(
		forge.Run{ID: 50, Branch: "sprint/s1/c1", HeadSHA: c1, Status: "queued"},
		forge.Run{ID: 51, Branch: "sprint/s1/c2", HeadSHA: c2, Status: "queued"},
	)
	rl := &rateLimited{fakeForge_internal: ff}
	res, err := LandPass(context.Background(), gf, rl, group_internal(c1, c2))
	if err != nil || rl.calls != 1 || !strings.HasPrefix(res.CancelFault, "1 runs could not be cancelled; the first: run 50") {
		t.Fatalf("pass = %+v err=%v calls=%d, want one cancel tried and the rate limit said", res, err, rl.calls)
	}

	gf2, ff2, _, d1, d2 := landWorld_internal()
	var runs []forge.Run
	for i := range CancelMax + 5 {
		runs = append(runs, forge.Run{ID: int64(100 + i), Branch: "sprint/s1/c1", HeadSHA: d1, Status: "queued"})
	}
	ff2.SetRuns(runs...)
	res, err = LandPass(context.Background(), gf2, ff2, group_internal(d1, d2))
	if err != nil || len(res.Cancelled) != CancelMax || res.CancelFault != "" {
		t.Fatalf("cancelled %d fault %q err=%v, want exactly CancelMax %d", len(res.Cancelled), res.CancelFault, err, CancelMax)
	}
}

type rateLimited struct {
	*fakeForge_internal
	calls int
}

func (r *rateLimited) CancelRun(context.Context, string, int64) error {
	r.calls++
	return &forge.FaultError{Kind: forge.FaultRateLimit, Identity: forge.HostLogin, Reason: "HTTP 403: API rate limit exceeded"}
}

package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// takeW1 is alice's take of p1.w1, which most work cases start from.
func takeW1(t *testing.T, w *world) { w.ok(t, "alice", "take", "p1.w1@1") }

// takeR1 is bob's take of p2.r1.
func takeR1(t *testing.T, w *world) { w.ok(t, "bob", "take", "p2.r1@1") }

func TestTake(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	first := w.do("alice", "take", "p1.w1@1", "--epoch", "1")
	want(t, first, 0, "TAKEN p1.w1 repo=o/r base="+base+" branch=sprint/s/p1 deadline=1800s forge=none", "do p1",
		"next: nova-sprint finish p1.w1@1 --head <sha> --as alice --epoch 1")
	if c := w.snap(t).Cards["p1.w1"]; c.State != "working" {
		t.Fatalf("card %+v", c)
	}
	if again := w.do("alice", "take", "p1.w1@1"); again != first {
		t.Fatalf("a repeat take is the same receipt:\n%s\n%s", again.Output, first.Output)
	}
}

// The head an attempt starts from is its branch's tip at origin when the
// branch holds work past the base: an earlier attempt pushed it and was
// withdrawn, or reported without it, so the store's prior head is stale or
// none (johnny's four cards, 2026-11-09). A tip at the base, or no branch,
// leaves the store's prior head; an origin that cannot be read refuses.
func TestTakeHandsOverTheBranchTip(t *testing.T) {
	t.Parallel()
	prior := func(t *testing.T, w *world, ref string) string {
		t.Helper()
		r := w.do("alice", "take", ref, "--json")
		want(t, r, 0)
		var got takeReceipt
		if err := json.Unmarshal([]byte(r.Output), &got); err != nil {
			t.Fatal(err)
		}
		return got.PriorHead
	}
	for _, kase := range []struct {
		name, storePrior, originTip, want string
		absent                            bool
	}{
		{name: "pushed, store knows none", originTip: tip, want: tip},
		{name: "pushed past a stale prior", storePrior: head, originTip: tip, want: tip},
		{name: "the tip is the prior", storePrior: head, originTip: head, want: head},
		{name: "the tip is the base", storePrior: head, originTip: base, want: head},
		{name: "no branch at origin", storePrior: head, absent: true, want: head},
	} {
		t.Run(kase.name, func(t *testing.T) {
			t.Parallel()
			s := sprint()
			setCard("p1.w1", func(c *store.Card) { c.Prior = kase.storePrior })(&s)
			w := worldFrom(t, s)
			w.origin.tips["o/r sprint/s/p1"] = kase.originTip
			if kase.absent {
				delete(w.origin.tips, "o/r sprint/s/p1")
			}
			if got := prior(t, w, "p1.w1@1"); got != kase.want {
				t.Fatalf("prior_head %q, want %q", got, kase.want)
			}
		})
	}
	w := newWorld(t)
	w.origin.tips["o/r sprint/s/p1"] = tip
	want(t, w.do("alice", "take", "p1.w1@1"), 0, "continue from "+tip, "git checkout -b sprint/s/p1 "+tip, "never from the base")
	w = newWorld(t)
	w.origin.err = errBroken
	want(t, w.do("alice", "take", "p1.w1@1"), 2, "git ls-remote of o/r failed", "take again")
	w.origin.err = nil
	want(t, w.do("alice", "take", "p1.w1@1"), 0, "TAKEN p1.w1")
	// A read starts from the head it reads, never a branch tip.
	w = newWorld(t)
	w.origin.err = errBroken
	want(t, w.do("bob", "take", "p2.r1@1"), 0, "TAKEN p2.r1")
}

// The receipt says whether an attempt finished at the prior head it hands
// over: the card machine refuses a finished one as this attempt's finish
// (TestFinishAtThePriorHead), and only it can tell, so a bench's worker is
// told at the take instead of finding out from a refused report.
func TestTakeSaysWhetherThePriorHeadIsFinished(t *testing.T) {
	t.Parallel()
	for result, finished := range map[string]bool{"fault": false, "ok": true} {
		s := sprint()
		secondAttempt(result)(&s)
		r := worldFrom(t, s).do("alice", "take", "p1.w2@1", "--json")
		want(t, r, 0)
		var got takeReceipt
		if err := json.Unmarshal([]byte(r.Output), &got); err != nil {
			t.Fatal(err)
		}
		if got.PriorHead != head || got.PriorFinished != finished {
			t.Errorf("the first attempt ended %s at %s: prior_head %q prior_finished %v, want %v", result, head, got.PriorHead, got.PriorFinished, finished)
		}
	}
}

func TestTakeJSONIsTheReceipt(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.f.Apply(t.Context(), store.Op{ID: "forge", Epoch: 1, Actor: "carol", Kind: "stream-set", At: t0, Writes: []store.Write{store.SetStreamForge{Stream: "s", Forge: "github"}}})
	r := w.do("bob", "take", "p2.r1@1", "--json")
	want(t, r, 0)
	var got takeReceipt
	if err := json.Unmarshal([]byte(r.Output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Consumer != "p2.r1" || got.Head != head || got.Deadline != 1800 || got.Forge != "github" || got.Paths == nil || got.StreamRef != "main" {
		t.Fatalf("%+v", got)
	}
	// The model is the one the deal named, carried by the consumer (defect
	// 57); take never reads it from the roster.
	if got.Model != "inception/mercury-3" || strings.Contains(r.Output, `"models"`) {
		t.Fatalf("a bench's receipt: model %q\n%s", got.Model, r.Output)
	}
	delete(w.roster.ro.Workers, "bob")
	w.roster.ro.Kin["bob"] = roster.Kin{Name: "bob", Model: "m-bob", Width: 2, Tiers: []string{"pro"}}
	want(t, w.do("bob", "take", "p2.r1@1", "--json"), 0, `"model":"inception/mercury-3"`)
}

// A consumer dealt before v2.0.1 has no model and is working in its row's
// hand: take does not refuse it (PR 641 F2).
func TestTakeModelless(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "dealt with no model", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 0, snap: setCard("p1.w1", func(c *store.Card) { c.Model = "" }),
			want: []string{"TAKEN p1.w1"}},
	})
}

func TestTakeRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "no gen", as: "alice", args: []string{"take", "p1.w1"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "a bad gen", as: "alice", args: []string{"take", "p1.w1@x"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "another row's card", as: "alice", args: []string{"take", "p2.r1@1"}, exit: 1, want: []string{"p2.r1 is not dealt to this row", "nova-sprint next --as alice"}},
		{name: "another gen", as: "alice", args: []string{"take", "p1.w1@2"}, exit: 1, want: []string{"not dealt to this row at this generation"}},
		{name: "a producer", as: "alice", args: []string{"take", "p1@1"}, exit: 1, want: []string{"p1 is not dealt"}},
		{name: "an old epoch", as: "alice", args: []string{"take", "p1.w1@1", "--epoch", "2"}, exit: 1, want: []string{"epoch 2 is over", "nova-sprint next"}},
		{name: "a bad epoch", as: "alice", args: []string{"take", "p1.w1@1", "--epoch", "x"}, exit: 2, want: []string{"--epoch takes a positive whole number"}},
		{name: "not in nova-config", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1,
			prep: func(_ *testing.T, w *world) { delete(w.roster.ro.Workers, "alice") }, want: []string{"row alice is not in nova-config"}},
		{name: "a tier not served", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1,
			prep: func(_ *testing.T, w *world) {
				w.roster.ro.Workers["alice"] = roster.Worker{Name: "alice", Width: 1, Tiers: []string{"flash"}}
			},
			want: []string{"row alice does not serve tier pro", "nova-sprint release p1.w1@1 --as alice"}},
		{name: "another row's acknowledgement", as: "bob", args: []string{"take", "p1.w1@1"}, exit: 1, prep: takeW1, want: []string{"p1.w1 is not dealt to this row", "nova-sprint next --as bob"}},
		{name: "dealt before the deal was the take", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1, snap: setCard("p1.w1", func(c *store.Card) { c.State = "ready" }),
			want: []string{"the next tick withdraws it and deals it again, in hand"}},
		{name: "a finished card", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1, snap: setCard("p1.w1", func(c *store.Card) { c.State, c.Reason = "done", "" }),
			want: []string{"p1.w1 is done: no reason given"}},
		{name: "the roster fails", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.roster.err = errors.New("down") }, want: []string{"nova-config's roster cannot be read"}},
		{name: "no policy engine", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.e.Answers = nil }, want: []string{"the policy engine is not configured"}},
		{name: "no roster", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 2,
			prep: func(_ *testing.T, w *world) { w.e.Roster = nil }, want: []string{"nova-config is not configured"}},
	})
}

// busyAlice gives alice a second card, in hand.
func busyAlice(s *store.Snapshot) {
	s.Cards["p3"] = store.Card{ID: "p3", Kind: "producer", Stream: "s", State: "working", Gen: 1, Attempt: 1, Tier: "pro", Priority: "normal", Branch: "sprint/s/p3"}
	s.Cards["p3.w1"] = store.Card{ID: "p3.w1", Kind: "work", Of: "p3", Stream: "s", State: "working", Gen: 1, Attempt: 1, Holder: "alice", Tier: "pro", Branch: "sprint/s/p3", BaseSHA: base, Priority: "normal"}
}

func TestFinish(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeW1(t, w)
	first := w.do("alice", "finish", "p1.w1@1", "--head", head, "--cost", "0.04")
	want(t, first, 0, "FINISHED p1.w1 head="+head, "next: "+waitNext)
	if c := w.snap(t).Cards["p1.w1"]; c.State != "done" || c.Cost != 0 || c.Priced || c.Usage != "billing=api charged=0.04 unpriced=no-tokens" {
		t.Fatalf("a bench's --cost is the harness's evidence, never its price; no tokens is unpriced: %+v", c)
	}
	if again := w.do("alice", "finish", "p1.w1@1", "--head", head, "--cost", "0.04"); again != first {
		t.Fatalf("a repeat finish is the same receipt:\n%s", again.Output)
	}
	want(t, w.do("bob", "report", "p1.w1@1", "--result", "ok", "--head", head), 1, "p1.w1 was reported by alice")
}

func TestFinishFailedAndReportForms(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeW1(t, w)
	want(t, w.do("alice", "finish", "p1.w1@1", "--failed", "--reason", "gate red: go test exit 1"), 0, "FAILED p1.w1")
	w = newWorld(t)
	takeW1(t, w)
	r := w.do("alice", "report", "p1.w1@1", "--result", "fault", "--reason", "bench down", "--json")
	want(t, r, 0, `"accepted":true`)
	if c := w.snap(t).Cards["p1.w1"]; c.Result != "fault" || c.Cost != 0 || c.Priced || c.Usage != "billing=api charged=- unpriced=no-tokens" {
		t.Fatalf("card %+v", c)
	}
}

// A failed finish with no --head records the dealt branch's tip at origin,
// so the card goes back with the pushed work and the next attempt continues
// from it; a tip at the base records none; an unreadable origin refuses.
func TestFinishFailedRecordsPushedHead(t *testing.T) {
	t.Parallel()
	failed := []string{"finish", "p1.w1@1", "--failed", "--reason", "gate red: go test exit 1"}
	w := newWorld(t)
	takeW1(t, w)
	want(t, w.do("alice", failed...), 0, "FAILED p1.w1 head="+head+" ")
	if p := w.snap(t).Cards["p1"]; p.State != "ready" || p.Head != head || p.Reason != "gate red: go test exit 1" {
		t.Fatalf("the producer goes back with the pushed head and the reason: %+v", p)
	}
	w = newWorld(t)
	takeW1(t, w)
	w.origin.tips["o/r sprint/s/p1"] = base
	want(t, w.do("alice", failed...), 0, "FAILED p1.w1 head= ")
	s := sprint()
	setCard("p1.w1", func(c *store.Card) { c.Prior = head })(&s)
	w = worldFrom(t, s)
	takeW1(t, w)
	want(t, w.do("alice", failed...), 0, "FAILED p1.w1 head= ") // the prior head: nothing new was pushed
	w = newWorld(t)
	takeW1(t, w)
	delete(w.origin.tips, "o/r sprint/s/p1")
	want(t, w.do("alice", failed...), 0, "FAILED p1.w1 head= ")
	w = newWorld(t)
	takeW1(t, w)
	w.origin.err = errBroken
	want(t, w.do("alice", failed...), 2, "git ls-remote of o/r failed", "--head <sha>")
}

// secondAttempt is p1 at attempt 2: p1.w1 reported result at head, which
// p1.w2, dealt to alice, continues from as its prior head.
func secondAttempt(result string) func(*store.Snapshot) {
	return func(s *store.Snapshot) {
		p, w1 := s.Cards["p1"], s.Cards["p1.w1"]
		w2 := w1
		p.Attempt, p.Head = 2, head
		w1.State, w1.Result, w1.Head = "done", result, head
		w2.ID, w2.Attempt, w2.Prior = "p1.w2", 2, head
		s.Cards["p1"], s.Cards["p1.w1"], s.Cards["p1.w2"] = p, w1, w2
	}
}

// A later attempt finishes at a prior head no attempt finished (an earlier
// attempt pushed it, then faulted); a prior head already finished, sent back
// for a finding, is refused: nothing was done about it.
func TestFinishAtThePriorHead(t *testing.T) {
	t.Parallel()
	take := func(t *testing.T, w *world) { w.ok(t, "alice", "take", "p1.w2@1") }
	finish := []string{"finish", "p1.w2@1", "--head", head}
	runCases(t, []tcase{
		{name: "never finished", as: "alice", snap: secondAttempt("fault"), prep: take, args: finish, exit: 0, want: []string{"FINISHED p1.w2 head=" + head}},
		{name: "already finished", as: "alice", snap: secondAttempt("ok"), prep: take, args: finish, exit: 1, want: []string{"head " + head + " is the prior head; nothing new was pushed"}},
	})
}

func TestFinishRefusals(t *testing.T) {
	t.Parallel()
	finish := []string{"finish", "p1.w1@1", "--head", head}
	runCases(t, []tcase{
		{name: "no head", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@1"}, exit: 1, want: []string{"a finish names its head", "--head"}},
		{name: "not a sha", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@1", "--head", "abc"}, exit: 1, want: []string{"head abc is not a full commit sha"}},
		{name: "the base", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@1", "--head", base}, exit: 1, want: []string{"where this attempt started"}},
		{name: "not pushed", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@1", "--head", tip}, exit: 1, want: []string{"is not the tip of sprint/s/p1", "git push origin sprint/s/p1"}},
		{name: "origin fails", as: "alice", args: finish, exit: 2, prep: func(t *testing.T, w *world) { takeW1(t, w); w.origin.err = errBroken }, want: []string{"git ls-remote of o/r failed"}},
		{name: "failed with no reason", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@1", "--failed"}, exit: 1, want: []string{"a fail needs its reason"}},
		{name: "not the holder", as: "bob", prep: takeW1, args: finish, exit: 1, want: []string{"p1.w1 is not held by bob"}},
		{name: "a read card", as: "bob", prep: takeR1, args: []string{"finish", "p2.r1@1", "--head", head}, exit: 1, want: []string{"p2.r1 is a read card", "nova-sprint read p2.r1@1 yes|fix"}},
		{name: "a bad cost", as: "alice", prep: takeW1, args: append(finish, "--cost", "1.0000001"), exit: 2, want: []string{"--cost is dollars"}},
		{name: "not in hand", as: "alice", args: []string{"finish", "p1.w1@1", "--head", head}, exit: 1, snap: setCard("p1.w1", func(c *store.Card) { c.State = "ready" }), want: []string{"nova-sprint show p1.w1"}},
	})
}

func TestRead(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "fix", "--finding", "a.go:1: off by one; use <="), 0, "READ p2.r1 head="+head+" result=fix")
}

// A read's spend reaches its consumer card as a finish's does, priced by the
// engine: a kin on a subscription seat is free (priced, $0), its --cost kept
// as charged evidence (the owner, 2026-11-07: "friend sub rate should be
// free").
func TestReadCarriesCost(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.roster.ro.Kin["bob"] = roster.Kin{Name: "bob", Model: "m-bob", Width: 2, Tiers: []string{"pro"}, Harness: "claude"}
	delete(w.roster.ro.Workers, "bob")
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--cost", "0.015"), 0, "READ p2.r1 head="+head+" result=ok")
	if c := w.snap(t).Cards["p2.r1"]; c.State != "done" || c.Cost != 0 || !c.Priced || c.Usage != "billing=subscription charged=0.015" {
		t.Fatalf("read card %+v", c)
	}
	w = newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes"), 0, "READ p2.r1")
	if c := w.snap(t).Cards["p2.r1"]; c.Priced || c.Cost != 0 {
		t.Fatalf("a read with no --cost is unpriced, never a priced $0: %+v", c)
	}
}

// racingRoster applies op to the store on its first read: another row's op
// landing between a report's snapshot and its apply (a report reads the
// roster to price itself).
type racingRoster struct {
	*fakeRoster
	f    *store.Fake
	op   store.Op
	done bool
	r    store.Receipt
	err  error
}

func (r *racingRoster) ReadRosterAt(ctx context.Context, n int) (roster.Roster, error) {
	if !r.done {
		r.done = true
		r.r, r.err = r.f.Apply(ctx, r.op)
	}
	return r.fakeRoster.ReadRosterAt(ctx, n)
}

// A report the store refuses (the producer moved between the report's
// snapshot and its apply) leaves the consumer in hand at its gen, and the
// retry at that gen applies a fresh plan: the same answer or another. An
// accepted report sent again is its first receipt and writes nothing.
func TestReportRetriedAfterARefusalApplies(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		retry  []string
		result string
	}{
		{[]string{"yes"}, "ok"},
		{[]string{"fix", "--finding", "a.go:1: off by one; use <="}, "fix"},
	} {
		t.Run(c.result, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			takeR1(t, w)
			race := &racingRoster{fakeRoster: w.roster, f: w.f, op: store.Op{ID: "priority-p2", Epoch: 1, Actor: "carol", Kind: "priority", At: t0,
				Expect: []store.Expect{store.CardGen{Card: "p2", Gen: 1}}, Writes: []store.Write{store.SetCardPriority{Card: "p2", Priority: "high"}}}}
			w.e.Roster = race
			want(t, w.do("bob", "read", "p2.r1@1", "yes"), 1, "card p2 is at gen 2, expected gen 1")
			if race.err != nil || !race.r.Accepted {
				t.Fatalf("the racing op did not land: %+v %v", race.r, race.err)
			}
			if k := w.snap(t).Cards["p2.r1"]; k.State != "working" || k.Gen != 1 || k.Holder != "bob" {
				t.Fatalf("a refused report moved the consumer: %+v", k)
			}
			retry := w.do("bob", append([]string{"read", "p2.r1@1"}, c.retry...)...)
			want(t, retry, 0, "READ p2.r1 head="+head+" result="+c.result)
			if k := w.snap(t).Cards["p2.r1"]; k.State != "done" || k.Result != c.result {
				t.Fatalf("the retry did not apply as its answer: %+v", k)
			}
			seq := w.snap(t).Seq
			if again := w.do("bob", append([]string{"read", "p2.r1@1"}, c.retry...)...); again != retry {
				t.Fatalf("an accepted report sent again is its first receipt:\n%s\n%s", again.Output, retry.Output)
			}
			if w.snap(t).Seq != seq {
				t.Fatalf("an accepted report sent again wrote to the log")
			}
		})
	}
}

func TestReadRefusals(t *testing.T) {
	t.Parallel()
	aliceReads := both(setCard("p2.r1", func(c *store.Card) { c.Holder, c.State = "alice", "working" }))
	runCases(t, []tcase{
		{name: "no answer", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@1", "maybe"}, exit: 2, want: []string{"a read answers yes or fix"}},
		{name: "fix with no finding", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@1", "fix"}, exit: 1, want: []string{"a fix needs a finding", "--finding"}},
		{name: "the author", as: "alice", snap: aliceReads, args: []string{"read", "p2.r1@1", "yes"}, exit: 1, want: []string{"alice wrote the head and cannot read it"}},
		{name: "another head", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@1", "yes", "--head", tip}, exit: 1, want: []string{"producer p2 is at head " + head}},
		{name: "a work card", as: "alice", prep: takeW1, args: []string{"read", "p1.w1@1", "yes"}, exit: 1, want: []string{"p1.w1 is a work card", "nova-sprint finish p1.w1@1"}},
		{name: "a bad result", as: "bob", prep: takeR1, args: []string{"report", "p2.r1@1", "--result", "maybe"}, exit: 2, want: []string{"--result is ok, fix, fail or fault"}},
		{name: "a work card's fix", as: "alice", prep: takeW1, args: []string{"report", "p1.w1@1", "--result", "fix", "--finding", "x"}, exit: 2, want: []string{"fix is a read's answer"}},
		{name: "a fault with no reason", as: "alice", prep: takeW1, args: []string{"report", "p1.w1@1", "--result", "fault"}, exit: 1, want: []string{"a fault needs its reason"}},
	})
}

func TestRelease(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "tier"), 0, "RELEASED p1.w1", "branch sprint/s/p1 kept", "next: "+waitNext)
	w = newWorld(t)
	want(t, w.do("carol", "release", "p1.w1@1", "--reason", "bench down"), 0, "RELEASED p1.w1")
}

// A release is neither ok nor fail: the consumer keeps no result and says
// released, and the producer goes back with no bounce (no blame).
func TestReleaseIsNoBlame(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	before := w.snap(t).Cards["p1"]
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "tier"), 0, "RELEASED p1.w1")
	snap := w.snap(t)
	c, p := snap.Cards["p1.w1"], snap.Cards["p1"]
	if c.Result != "" || c.Reason != ReleasedPrefix+"tier" {
		t.Fatalf("released consumer result %q reason %q", c.Result, c.Reason)
	}
	if p.State != "ready" || p.Bounces != before.Bounces || p.Score != before.Score {
		t.Fatalf("producer after release %s bounces %d score %d, before %d %d", p.State, p.Bounces, p.Score, before.Bounces, before.Score)
	}
}

// A worker that restarted releases a working work card with the partial
// head it pushed (--head), checked as a finish's is; the producer goes back
// at that head with no bounce.
func TestReleaseCarriesThePushedHead(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeW1(t, w)
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "runner restarted", "--head", tip), 1, "is not the tip of sprint/s/p1")
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "runner restarted", "--head", head), 0, "RELEASED p1.w1")
	if p := w.snap(t).Cards["p1"]; p.State != "ready" || p.Head != head || p.Bounces != 0 {
		t.Fatalf("the producer after a release with its head: %+v", p)
	}
}

func TestReleaseRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "no reason", as: "alice", args: []string{"release", "p1.w1@1"}, exit: 1, want: []string{"a release needs its reason"}},
		{name: "no card", as: "alice", args: []string{"release", "--reason", "r"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "not held", as: "alice", args: []string{"release", "p1.w1@2", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is not held at generation 2"}},
		{name: "another's card", as: "bob", args: []string{"release", "p1.w1@1", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is held by alice, not bob"}},
		{name: "an old epoch", as: "alice", args: []string{"release", "p1.w1@1", "--reason", "r", "--epoch", "3"}, exit: 1, want: []string{"epoch 3 is over"}},
	})
}

func TestShowAndQueue(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	want(t, w.do("alice", "show", "p1"), 0, `"id": "p1"`)
	want(t, w.do("alice", "show", "s"), 0, `"repo": "o/r"`)
	want(t, w.do("alice", "show", "bob"), 0, `"role": "worker"`)
	want(t, w.do("alice", "show", "run"), 0, `"studio"`)
	want(t, w.do("alice", "show"), 0, `"Seq": 10`)
	want(t, w.do("carol", "show", "s", "--stitch"), 0, `"diff_range": "origin/main..origin/sprint-group/s"`, `"id": "p2"`)
	want(t, w.do("alice", "queue"), 0, "p1 stream=s state=working", "holders=alice", "p2 stream=s state=review")
	want(t, w.do("alice", "queue", "--stream", "s"), 0, "p1 stream=s")
}

func TestShowAndQueueRefusals(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "no record", as: "alice", args: []string{"show", "zz"}, exit: 1, want: []string{"no card, stream or row zz", "nova-sprint show"}},
		{name: "a stitch of no stream", as: "alice", args: []string{"show", "zz", "--stitch"}, exit: 2, want: []string{"--stitch names a stream"}},
		{name: "a queue of no stream", as: "alice", args: []string{"queue", "--stream", "zz"}, exit: 1, want: []string{"no stream zz"}},
	})
}

// beat is the worker's heartbeat: it sets the row's load columns, a repeat
// writes nothing, and a bad flag is bad usage that names the flag.
func TestBeat(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	args := []string{"beat", "--load1", "2.505", "--cpu-peak", "87.25", "--cores", "8", "--slots-used", "3", "--slots-total", "8"}
	want(t, w.do("alice", args...), 0, "BEAT alice applied at seq", "next: ")
	r := w.snap(t).Rows["alice"]
	if r.Load1Centi != 251 || r.CPUPeakDeci != 873 || r.Cores != 8 || r.SlotsUsed != 3 || r.SlotsTotal != 8 || r.Version != "v2.0.0-test" {
		t.Fatalf("the row after the beat (the version is the beating build's, defect D3): %+v", r)
	}
	seq := w.snap(t).Seq
	want(t, w.do("alice", args...), 0, "BEAT alice unchanged")
	if w.snap(t).Seq != seq {
		t.Fatalf("a repeated beat wrote to the log")
	}
	want(t, w.do("alice", "beat", "--load1", "0", "--cores", "8", "--slots-used", "0", "--slots-total", "8"), 0, "applied")
	want(t, w.do("alice", "beat", "--load1", "0", "--cores", "8", "--slots-used", "0", "--slots-total", "8", "--tiers", "pro,flash"), 0, "applied")
	if r := w.snap(t).Rows["alice"]; r.Tiers != "flash,pro" {
		t.Fatalf("the advertised tiers are kept in the store's order: %q", r.Tiers)
	}
}

// A beat a worker process runs records that process's build
// (worker.BuildEnv), not the beat binary's: nova-fleet install replaces the
// file on disk under a running worker, and the row must say what runs.
func TestBeatRecordsTheWorkerProcessBuild(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.e.WorkerBuild = "v1.9.9"
	want(t, w.do("alice", "beat", "--load1", "1", "--cores", "8", "--slots-used", "0", "--slots-total", "8"), 0, "BEAT alice applied at seq")
	if r := w.snap(t).Rows["alice"]; r.Version != "v1.9.9" {
		t.Fatalf("the row's build is the worker process's: %q", r.Version)
	}
}

func TestBeatRefusals(t *testing.T) {
	t.Parallel()
	good := []string{"--load1", "1", "--cores", "4", "--slots-used", "1", "--slots-total", "4"}
	with := func(flag, val string) []string {
		out := slices.Clone(good)
		out[slices.Index(out, flag)+1] = val
		return append([]string{"beat"}, out...)
	}
	runCases(t, []tcase{
		{name: "not a load", as: "alice", args: with("--load1", "fast"), exit: 2, want: []string{"--load1 takes the 1-minute load average"}},
		{name: "a negative load", as: "alice", args: with("--load1", "-1"), exit: 2, want: []string{"--load1 takes"}},
		{name: "a cpu peak over 100", as: "alice", args: append(with("--load1", "1"), "--cpu-peak", "100.1"), exit: 2, want: []string{"--cpu-peak takes the CPU busy percent as a number from 0 to 100"}},
		{name: "a cpu peak that is not a number", as: "alice", args: append(with("--load1", "1"), "--cpu-peak", "hot"), exit: 2, want: []string{"--cpu-peak takes"}},
		{name: "no cores", as: "alice", args: with("--cores", "0"), exit: 2, want: []string{"--cores takes a whole number, 1 or more"}},
		{name: "slots over the total", as: "alice", args: with("--slots-used", "5"), exit: 2, want: []string{"--slots-used 5 is more than --slots-total 4"}},
		{name: "an unknown tier", as: "alice", args: append(with("--load1", "1"), "--tiers", "flash,turbo"), exit: 2, want: []string{"--tiers takes a comma list of flash, pro, heavy, frontier"}},
		{name: "a flag missing", as: "alice", args: []string{"beat", "--load1", "1"}, exit: 2, want: []string{"--cores takes a whole number"}},
		{name: "not hooked", as: "dave", args: append([]string{"beat"}, good...), exit: 1, want: []string{"hook in first", "nova-sprint hook --as dave --role worker"}},
	})
}

// A bench's read carries its harness's tokens (--usage); the engine prices
// them exactly by the policy's prices() and keeps --cost as charged evidence.
// A model the policy has no price for is unpriced=no-price, never $0, and
// words that do not read are refused before anything is applied.
func TestReadPricedFromUsage(t *testing.T) {
	t.Parallel()
	sheet := map[string]policy.Price{"deepseek-flash": {In: 270_000, CacheRead: 70_000, CacheWrite: 270_000, Out: 1_100_000}}
	w := newWorld(t)
	w.e.Answers = fakeAnswers{prices: sheet}
	takeR1(t, w)
	words := "input=19541 cache_read=36336 output=692 reasoning=70 model=deepseek/deepseek-flash"
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--cost", "0.015", "--usage", words), 0, "READ p2.r1 head="+head+" result=ok")
	if c := w.snap(t).Cards["p2.r1"]; c.Cost != 8658 || !c.Priced || c.Usage != words+" billing=api charged=0.015" {
		t.Fatalf("read card %+v", c)
	}
	w = newWorld(t)
	w.e.Answers = fakeAnswers{prices: sheet}
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", "input=10 output=2 model=openai/sol"), 0, "READ p2.r1")
	if c := w.snap(t).Cards["p2.r1"]; c.Cost != 0 || c.Priced || c.Usage != "input=10 output=2 model=openai/sol billing=api charged=- unpriced=no-price" {
		t.Fatalf("read card %+v", c)
	}
	w = newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", "input=lots"), 2, "--usage: usage input=lots is not a whole number")
	if c := w.snap(t).Cards["p2.r1"]; c.State != "working" {
		t.Fatalf("a refused report applies nothing: %+v", c)
	}
	want(t, w.do("bob", "help", "read"), 0, "--usage is the run's tokens", "a kin on a subscription seat (claude, codex, ...): free; pass nothing")
}

// A card release's --dry-run prints what it would withdraw and applies nothing.
func TestReleaseDryRun(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	before := w.snap(t)
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "tier", "--dry-run"), 0, "RELEASE p1.w1@1 (dry run)", "without --dry-run")
	if c := w.snap(t).Cards["p1.w1"]; c.State != before.Cards["p1.w1"].State || w.snap(t).Seq != before.Seq {
		t.Fatalf("a dry run released the card: %+v", c)
	}
}

// A card stored with a shell character in its id (admission refuses one
// now; an older store may hold it) is printed as one quoted shell word in
// the command a verb tells its caller to run. Seeded below admission.
func TestPrintedRefIsOneShellWord(t *testing.T) {
	t.Parallel()
	s := sprint()
	c := s.Cards["p1.w1"]
	delete(s.Cards, "p1.w1")
	c.ID = "a;b.w1"
	s.Cards[c.ID] = c
	w := worldFrom(t, s)
	want(t, w.do("alice", "release", "a;b.w1@1", "--reason", "tier", "--dry-run"), 0, "RELEASE 'a;b.w1@1' (dry run)", "nova-sprint release 'a;b.w1@1' --reason")
}

// failedAnswers is a policy engine whose active version failed: it answers
// from the built-in fallback and says so.
type failedAnswers struct{ fakeAnswers }

func (f failedAnswers) AnswerContext(ctx context.Context, vs policy.Versions, snap store.Snapshot, ro roster.Roster, q policy.Questions) (policy.Answers, []policy.Failure) {
	a, _ := f.fakeAnswers.AnswerContext(ctx, vs, snap, ro, q)
	return a, []policy.Failure{{Version: 1, Label: "v1", Msg: "boom"}}
}

// policyless is the store with its active policy version unreadable.
type policyless struct{ *store.Fake }

func (policyless) Policy(context.Context, int) (store.PolicyVersion, error) {
	return store.PolicyVersion{}, errors.New("connection reset")
}

// A report whose billing or price sheet cannot be read is recorded unpriced
// with the reason, never priced from the wrong sheet or billed by guess.
func TestReadUnpricedWhenBillingCannotBeRead(t *testing.T) {
	t.Parallel()
	sheet := map[string]policy.Price{"deepseek-flash": {In: 270_000, Out: 1_100_000}}
	words := "input=1000 output=10 model=deepseek/deepseek-flash"
	for _, c := range []struct {
		name, why string
		prep      func(*world)
	}{
		{"roster unreadable", "roster-unreadable", func(w *world) { w.roster.err = errors.New("nova-config down") }},
		{"policy failed", "policy-unreadable", func(w *world) { w.e.Answers = failedAnswers{fakeAnswers{prices: sheet}} }},
		{"policy version unreadable", "policy-unreadable", func(w *world) { activePolicy(t, w); w.e.Store = policyless{w.f} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.e.Answers = fakeAnswers{prices: sheet}
			takeR1(t, w)
			c.prep(w)
			want(t, w.do("bob", "read", "p2.r1@1", "yes", "--usage", words), 0, "READ p2.r1")
			if card := w.snap(t).Cards["p2.r1"]; card.Priced || card.Cost != 0 || !strings.HasSuffix(card.Usage, "unpriced="+c.why) {
				t.Fatalf("read card %+v", card)
			}
		})
	}
}

// refusingStore refuses every op with reason, writing nothing.
type refusingStore struct {
	*store.Fake
	reason string
}

func (s refusingStore) Apply(_ context.Context, op store.Op) (store.Receipt, error) {
	return store.Receipt{Op: op.ID, Reason: s.reason}, nil
}

// A take's acknowledgement refused is a refusal unless the cursor is in fact
// written: the refusal's words are never read as success.
func TestTakeRefusedAcknowledgementIsARefusal(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.e.Store = refusingStore{w.f, "cursor ack is already at 3"}
	want(t, w.do("alice", "take", "p1.w1@1"), 1, "cursor ack is already at 3", "nova-sprint show p1.w1")
}

func TestFinishTakesAnUppercaseHead(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	takeW1(t, w)
	want(t, w.do("alice", "finish", "p1.w1@1", "--head", strings.ToUpper(head)), 0, "FINISHED p1.w1 head="+head)
	if c := w.snap(t).Cards["p1.w1"]; c.Head != head {
		t.Fatalf("head recorded %q", c.Head)
	}
}

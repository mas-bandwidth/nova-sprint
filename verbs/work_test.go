package verbs

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// takeW1 is alice's take of p1.w1, which most work cases start from.
func takeW1(t *testing.T, w *world) { w.ok(t, "alice", "take", "p1.w1@1") }

// takeR1 is bob's take of p2.r1.
func takeR1(t *testing.T, w *world) { w.ok(t, "bob", "take", "p2.r1@1") }

func TestTake(t *testing.T) {
	w := newWorld(t)
	first := w.do("alice", "take", "p1.w1@1", "--epoch", "1")
	want(t, first, 0, "TAKEN p1.w1 repo=o/r base="+base+" branch=sprint/s/p1 deadline=1800s forge=none", "do p1",
		"next: nova-sprint finish p1.w1@2 --head <sha> --as alice --epoch 1")
	if c := w.snap(t).Cards["p1.w1"]; c.State != "working" {
		t.Fatalf("card %+v", c)
	}
	if again := w.do("alice", "take", "p1.w1@1"); again != first {
		t.Fatalf("a repeat take is the same receipt:\n%s\n%s", again.Output, first.Output)
	}
}

func TestTakeJSONIsTheReceipt(t *testing.T) {
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

func TestTakeRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no gen", as: "alice", args: []string{"take", "p1.w1"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "a bad gen", as: "alice", args: []string{"take", "p1.w1@x"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "another row's card", as: "alice", args: []string{"take", "p2.r1@1"}, exit: 1, want: []string{"p2.r1 is not dealt to this row", "nova-sprint show p2.r1"}},
		{name: "another gen", as: "alice", args: []string{"take", "p1.w1@2"}, exit: 1, want: []string{"not dealt to this row at this generation"}},
		{name: "a producer", as: "alice", args: []string{"take", "p1@1"}, exit: 1, want: []string{"p1 is not dealt"}},
		{name: "an old epoch", as: "alice", args: []string{"take", "p1.w1@1", "--epoch", "2"}, exit: 1, want: []string{"epoch 2 is over", "nova-sprint next"}},
		{name: "a bad epoch", as: "alice", args: []string{"take", "p1.w1@1", "--epoch", "x"}, exit: 2, want: []string{"--epoch takes a positive whole number"}},
		{name: "another id", as: "alice", args: []string{"take", "p1.w1@1", "--id", "x"}, exit: 1, want: []string{"--id x is not this take's id", "pass --id consumer:"}},
		{name: "not in nova-config", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1,
			prep: func(_ *testing.T, w *world) { delete(w.roster.ro.Workers, "alice") }, want: []string{"row alice is not in nova-config"}},
		{name: "a tier not served", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1,
			prep: func(_ *testing.T, w *world) {
				w.roster.ro.Workers["alice"] = roster.Worker{Name: "alice", Width: 1, Tiers: []string{"flash"}}
			},
			want: []string{"row alice does not serve tier pro", "nova-sprint release p1.w1@1 --as alice"}},
		{name: "at width", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1, snap: busyAlice, want: []string{"row alice is at width 1", "finish p3.w1 first"}},
		{name: "dealt with no model", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1, snap: setCard("p1.w1", func(c *store.Card) { c.Model = "" }),
			want: []string{"p1.w1 was dealt with no model", `nova-sprint release p1.w1@1 --as alice --reason "dealt with no model"`}},
		{name: "taken by another", as: "bob", args: []string{"take", "p1.w1@1"}, exit: 1, prep: takeW1, want: []string{"p1.w1 was taken by alice", "nova-sprint next --as bob"}},
		{name: "a working card", as: "alice", args: []string{"take", "p1.w1@1"}, exit: 1, snap: setCard("p1.w1", func(c *store.Card) { c.State = "working" }), want: []string{"finish p1.w1 first"}},
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
	w := newWorld(t)
	takeW1(t, w)
	first := w.do("alice", "finish", "p1.w1@2", "--head", head, "--cost", "0.04")
	want(t, first, 0, "FINISHED p1.w1 head="+head, "next: "+waitNext)
	if c := w.snap(t).Cards["p1.w1"]; c.State != "done" || c.Cost != 40000 || !c.Priced {
		t.Fatalf("card %+v", c)
	}
	if again := w.do("alice", "finish", "p1.w1@2", "--head", head, "--cost", "0.04"); again != first {
		t.Fatalf("a repeat finish is the same receipt:\n%s", again.Output)
	}
	want(t, w.do("bob", "report", "p1.w1@2", "--result", "ok", "--head", head), 1, "p1.w1 was reported by alice")
}

func TestFinishFailedAndReportForms(t *testing.T) {
	w := newWorld(t)
	takeW1(t, w)
	want(t, w.do("alice", "finish", "p1.w1@2", "--failed", "--reason", "gate red: go test exit 1"), 0, "FAILED p1.w1")
	w = newWorld(t)
	takeW1(t, w)
	r := w.do("alice", "report", "p1.w1@2", "--result", "fault", "--reason", "bench down", "--cost-microusd", "12", "--priced", "false", "--json")
	want(t, r, 0, `"accepted":true`)
	if c := w.snap(t).Cards["p1.w1"]; c.Result != "fault" || c.Cost != 12 || c.Priced {
		t.Fatalf("card %+v", c)
	}
}

func TestFinishRefusals(t *testing.T) {
	finish := []string{"finish", "p1.w1@2", "--head", head}
	runCases(t, []tcase{
		{name: "no head", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@2"}, exit: 1, want: []string{"a finish names its head", "--head"}},
		{name: "not a sha", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@2", "--head", "abc"}, exit: 1, want: []string{"head abc is not a full commit sha"}},
		{name: "the base", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@2", "--head", base}, exit: 1, want: []string{"where this attempt started"}},
		{name: "not pushed", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@2", "--head", tip}, exit: 1, want: []string{"is not the tip of sprint/s/p1", "git push origin sprint/s/p1"}},
		{name: "origin fails", as: "alice", args: finish, exit: 2, prep: func(t *testing.T, w *world) { takeW1(t, w); w.origin.err = errBroken }, want: []string{"git ls-remote of o/r failed"}},
		{name: "failed with no reason", as: "alice", prep: takeW1, args: []string{"finish", "p1.w1@2", "--failed"}, exit: 1, want: []string{"a fail needs its reason"}},
		{name: "not the holder", as: "bob", prep: takeW1, args: finish, exit: 1, want: []string{"p1.w1 is not held by bob"}},
		{name: "a read card", as: "bob", prep: takeR1, args: []string{"finish", "p2.r1@2", "--head", head}, exit: 1, want: []string{"p2.r1 is a read card", "nova-sprint read p2.r1@2 yes|fix"}},
		{name: "costs mixed", as: "alice", prep: takeW1, args: append(finish, "--cost", "1", "--priced", "true"), exit: 1, want: []string{"--cost mixes with --cost-microusd"}},
		{name: "a bad cost", as: "alice", prep: takeW1, args: append(finish, "--cost", "1.0000001"), exit: 2, want: []string{"--cost is dollars"}},
		{name: "micro-dollars unpriced", as: "alice", prep: takeW1, args: append(finish, "--cost-microusd", "5"), exit: 2, want: []string{"--priced true|false go together"}},
		{name: "not taken", as: "alice", args: []string{"finish", "p1.w1@1", "--head", head}, exit: 1, want: []string{"nova-sprint show p1.w1"}},
	})
}

func TestRead(t *testing.T) {
	w := newWorld(t)
	takeR1(t, w)
	want(t, w.do("bob", "read", "p2.r1@2", "fix", "--finding", "a.go:1: off by one; use <="), 0, "READ p2.r1 head="+head+" result=fix")
}

func TestReadRefusals(t *testing.T) {
	aliceReads := both(setCard("p2.r1", func(c *store.Card) { c.Holder, c.State = "alice", "working" }))
	runCases(t, []tcase{
		{name: "no answer", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@2", "maybe"}, exit: 2, want: []string{"a read answers yes or fix"}},
		{name: "fix with no finding", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@2", "fix"}, exit: 1, want: []string{"a fix needs a finding", "--finding"}},
		{name: "the author", as: "alice", snap: aliceReads, args: []string{"read", "p2.r1@1", "yes"}, exit: 1, want: []string{"alice wrote the head and cannot read it"}},
		{name: "another head", as: "bob", prep: takeR1, args: []string{"read", "p2.r1@2", "yes", "--head", tip}, exit: 1, want: []string{"producer p2 is at head " + head}},
		{name: "a work card", as: "alice", prep: takeW1, args: []string{"read", "p1.w1@2", "yes"}, exit: 1, want: []string{"p1.w1 is a work card", "nova-sprint finish p1.w1@2"}},
		{name: "a bad result", as: "bob", prep: takeR1, args: []string{"report", "p2.r1@2", "--result", "maybe"}, exit: 2, want: []string{"--result is ok, fix, fail or fault"}},
		{name: "a work card's fix", as: "alice", prep: takeW1, args: []string{"report", "p1.w1@2", "--result", "fix", "--finding", "x"}, exit: 2, want: []string{"fix is a read's answer"}},
		{name: "a fault with no reason", as: "alice", prep: takeW1, args: []string{"report", "p1.w1@2", "--result", "fault"}, exit: 1, want: []string{"a fault needs its reason"}},
	})
}

func TestRelease(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("alice", "release", "p1.w1@1", "--reason", "tier"), 0, "RELEASED p1.w1", "branch sprint/s/p1 kept", "next: "+waitNext)
	w = newWorld(t)
	want(t, w.do("carol", "release", "p1.w1@1", "--reason", "bench down"), 0, "RELEASED p1.w1")
	w = newWorld(t)
	want(t, w.do("carol", "release", "--row", "alice", "--all", "--reason", "r", "--dry-run"), 0, "withdraws p1.w1@1 (dry run)")
	want(t, w.do("carol", "release", "--row", "alice", "--all", "--reason", "r"), 0, "RELEASED row=alice consumers=1")
	want(t, w.do("carol", "release", "--row", "alice", "--all", "--reason", "r"), 0, "holds nothing")
}

// A release is neither ok nor fail: the consumer keeps no result and says
// released, and the producer goes back with no bounce (no blame).
func TestReleaseIsNoBlame(t *testing.T) {
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

func TestReleaseRefusals(t *testing.T) {
	runCases(t, []tcase{
		{name: "no reason", as: "alice", args: []string{"release", "p1.w1@1"}, exit: 1, want: []string{"a release needs its reason"}},
		{name: "no card", as: "alice", args: []string{"release", "--reason", "r"}, exit: 2, want: []string{"<card>@<gen>"}},
		{name: "not held", as: "alice", args: []string{"release", "p1.w1@2", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is not held at generation 2"}},
		{name: "another's card", as: "bob", args: []string{"release", "p1.w1@1", "--reason", "r"}, exit: 1, want: []string{"p1.w1 is held by alice, not bob"}},
		{name: "a row as a worker", as: "bob", args: []string{"release", "--row", "alice", "--all", "--reason", "r"}, exit: 1, want: []string{"release --row is the coordinator's"}},
		{name: "no such row", as: "carol", args: []string{"release", "--row", "erin", "--all", "--reason", "r"}, exit: 1, want: []string{"no row erin"}},
		{name: "a row without --all", as: "carol", args: []string{"release", "--row", "alice", "--reason", "r"}, exit: 2, want: []string{"confirm with --all"}},
		{name: "an old epoch", as: "alice", args: []string{"release", "p1.w1@1", "--reason", "r", "--epoch", "3"}, exit: 1, want: []string{"epoch 3 is over"}},
	})
}

func TestShowAndQueue(t *testing.T) {
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
	runCases(t, []tcase{
		{name: "no record", as: "alice", args: []string{"show", "zz"}, exit: 1, want: []string{"no card, stream or row zz", "nova-sprint show"}},
		{name: "a stitch of no stream", as: "alice", args: []string{"show", "zz", "--stitch"}, exit: 2, want: []string{"--stitch names a stream"}},
		{name: "a queue of no stream", as: "alice", args: []string{"queue", "--stream", "zz"}, exit: 1, want: []string{"no stream zz"}},
	})
}

// beat is the worker's heartbeat: it sets the row's load columns, a repeat
// writes nothing, and a bad flag is bad usage that names the flag.
func TestBeat(t *testing.T) {
	w := newWorld(t)
	args := []string{"beat", "--load1", "2.505", "--cores", "8", "--slots-used", "3", "--slots-total", "8"}
	want(t, w.do("alice", args...), 0, "BEAT alice applied at seq", "next: ")
	r := w.snap(t).Rows["alice"]
	if r.Load1Centi != 251 || r.Cores != 8 || r.SlotsUsed != 3 || r.SlotsTotal != 8 {
		t.Fatalf("the row after the beat: %+v", r)
	}
	seq := w.snap(t).Seq
	want(t, w.do("alice", args...), 0, "BEAT alice unchanged")
	if w.snap(t).Seq != seq {
		t.Fatalf("a repeated beat wrote to the log")
	}
	want(t, w.do("alice", "beat", "--load1", "0", "--cores", "8", "--slots-used", "0", "--slots-total", "8"), 0, "applied")
}

func TestBeatRefusals(t *testing.T) {
	good := []string{"--load1", "1", "--cores", "4", "--slots-used", "1", "--slots-total", "4"}
	with := func(flag, val string) []string {
		out := slices.Clone(good)
		out[slices.Index(out, flag)+1] = val
		return append([]string{"beat"}, out...)
	}
	runCases(t, []tcase{
		{name: "not a load", as: "alice", args: with("--load1", "fast"), exit: 2, want: []string{"--load1 takes the 1-minute load average"}},
		{name: "a negative load", as: "alice", args: with("--load1", "-1"), exit: 2, want: []string{"--load1 takes"}},
		{name: "no cores", as: "alice", args: with("--cores", "0"), exit: 2, want: []string{"--cores takes a whole number, 1 or more"}},
		{name: "slots over the total", as: "alice", args: with("--slots-used", "5"), exit: 2, want: []string{"--slots-used 5 is more than --slots-total 4"}},
		{name: "a flag missing", as: "alice", args: []string{"beat", "--load1", "1"}, exit: 2, want: []string{"--cores takes a whole number"}},
		{name: "not hooked", as: "dave", args: append([]string{"beat"}, good...), exit: 1, want: []string{"hook in first", "nova-sprint hook --as dave --role worker"}},
	})
}

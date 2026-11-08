package card

import (
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// sweepSteps is every single move of the snapshot that the machine plans.
func sweepSteps(b *bk) []cold4Step {
	all, _ := b.w.all()
	var ids []string
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []cold4Step
	add := func(s cold4Step) {
		if _, r := s.plan(b.w); r == nil {
			out = append(out, s)
		}
	}
	var prods []string
	for _, id := range ids {
		if all[id].Kind == "producer" || all[id].Kind == "sentinel" {
			prods = append(prods, id)
		}
	}
	for _, id := range prods {
		p := all[id]
		w := workID(id, p.Attempt)
		evs := []Event{Resolve{}, Park{}, Reached{}, Start{Row: "r1", Branch: "b", Base: base}, Start{Row: "r2", Branch: "b", Base: base},
			Finished{Consumer: w}, Broken{Consumer: w}, Faulted{Consumer: w}, AskRead{Row: "r1"}, AskRead{Row: "r2"}, AskRead{Row: "r3"},
			AskReads{Rows: []string{"r2", "r3"}}, ReadYes{}, ReadFix{Finding: "f"}, WaiveRed{Reason: "w"}, Landed{Head: p.Head}, Red{Head: p.Head, Reason: "red"},
			Released{}, Cancel{Reason: "c"}, Edit{Prose: "e" + id}, Rescore{Score: 7}, SetPriority{Level: "blocker", Reason: "b"}, SetPriority{Level: "low"}, HoldCard{Reason: "held: b"}, UnholdCard{Reason: "u"}}
		for _, o := range prods {
			if o != id {
				evs = append(evs, Replace{By: o, Reason: "r"})
			}
		}
		for _, n := range p.Needs {
			evs = append(evs, NeedFailed{Need: n}, NeedFailed{Need: n, InThisOp: true}, Waive{Need: n, Reason: "w"}, Waive{Need: n, Reason: "w", InThisOp: true})
			for _, o := range prods {
				if o != id && o != n {
					evs = append(evs, Relink{Need: n, To: o, Reason: "x"}, Relink{Need: n, To: o, Reason: "x", InThisOp: true})
				}
			}
		}
		for _, ev := range evs {
			add(cold4P(id, ev))
		}
	}
	for _, id := range ids {
		c := all[id]
		if !consumer(c.Kind) {
			continue
		}
		for _, ev := range []Event{Report{Result: "ok", Head: hB}, Report{Result: "ok", Head: c.Head}, Report{Result: "fail", Head: hD}, Report{Result: "fault"}, Report{Result: "fix", Head: c.Head},
			Withdraw{Reason: "silent"}, Withdraw{Reason: "preempted by blocker x"}, Withdraw{Reason: "silent", Head: hE}} {
			add(b.C(id, ev))
			if _, ok := ev.(Report); ok {
				add(b.reportPlan(id, ev.(Report)))
			}
		}
	}
	for _, r := range []string{"r1", "r2", "r3"} {
		for _, ev := range []Event{Seen{}, Gone{}, Hold{Reason: "h"}, Unhold{Reason: "u"}} {
			add(cold4R(r, ev))
		}
	}
	add(cold4Hold())
	add(cold4Unhold())
	add(b.addStep("na", prodsNeeds(all), nil))
	return out
}

func prodsNeeds(all map[string]Card) []string {
	if _, ok := all["p1"]; ok {
		return []string{"p1"}
	}
	return nil
}

var sweepNorm = regexp.MustCompile(`\b[pngr][0-9a-z]*(\.[wr][0-9.]*)?\b|[0-9a-f]{40}|\d+`)

func sweepKey(label string) string {
	f := strings.Fields(label)
	return f[0]
}

// sweepPairs tries every ordered pair of single moves, merged and one by one.
func sweepPairs(t *testing.T, name string, setup func(*testing.T) *bk) {
	t.Helper()
	probe := setup(t)
	steps := sweepSteps(probe)
	type hit struct {
		kind, example string
		n             int
	}
	hits := map[string]*hit{}
	note := func(cat, a, b, why, example string) {
		k := cat + " | " + sweepKey(a) + " + " + sweepKey(b) + " | " + sweepNorm.ReplaceAllString(why, "_")
		if h, ok := hits[k]; ok {
			h.n++
			return
		}
		hits[k] = &hit{cat, example, 1}
	}
	for i := range steps {
		for j := range steps {
			if i == j || testing.Short() && (i*len(steps)+j)%20 != 0 { // -short: every 20th pair
				continue
			}
			a, s := setup(t), setup(t)
			sa, sb := steps[i], steps[j]
			merged := a.merge(sa, sb)
			mwhy := a.last
			seqOK, seqWhy := true, ""
			for _, st := range []cold4Step{sa, sb} {
				if !s.step(st) {
					seqOK, seqWhy = false, st.label+": "+s.last
					break
				}
			}
			ex := fmt.Sprintf("%s + %s", sa.label, sb.label)
			switch {
			case merged && !seqOK:
				note("MERGE-ACCEPTED-SEQ-REFUSED", sa.label, sb.label, seqWhy, ex+" :: "+seqWhy)
			case !merged && seqOK:
				note("over-refusal", sa.label, sb.label, mwhy, ex+" :: "+mwhy)
			case merged && seqOK:
				if d := cold4Diff(a.snap(), s.snap()); d != "" {
					note("DIFFER", sa.label, sb.label, d[:min(60, len(d))], ex+" :: "+d)
				}
			}
		}
	}
	var keys []string
	for k := range hits {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h := hits[k]
		sweepReport(t, k, "[%s] x%d %s\n      e.g. %s", name, h.n, k, h.example)
	}
	t.Logf("[%s] %d steps, %d pairs", name, len(steps), len(steps)*(len(steps)-1))
}

func TestBreakSweep(t *testing.T) {
	t.Parallel()
	worlds := map[string]func(*testing.T) *bk{
		"fresh": func(t *testing.T) *bk {
			b := newBk(t, producer("n1"), producer("p1"), producer("p2", "p1"), producer("g", "p1"))
			b.ok(b.P("p1", Resolve{}))
			return b
		},
		"inflight": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"), producer("pb"), producer("p3", "p1"))
			b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
			for _, id := range []string{"p1", "p2", "pb"} {
				b.ok(b.P(id, Resolve{}))
			}
			b.take(b.start("p1", "r2"))
			b.start("p2", "r3")
			return b
		},
		"reported": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"))
			b.ok(b.P("p1", Resolve{}))
			b.ok(b.P("p2", Resolve{}))
			b.run("p1", "r1", "ok", hA)
			b.run("p2", "r2", "fail", hD)
			return b
		},
		"review": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2", "p1"))
			b.review("p1", "r1", hA)
			b.readOf("p1", "r2", "ok")
			b.ok(b.P("p1", AskRead{Row: "r3"}))
			return b
		},
		"merging": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"))
			b.review("p1", "r1", hA)
			b.readOf("p1", "r2", "ok")
			b.ok(b.P("p1", ReadYes{}))
			b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
			// the red sent p1 to fix: a new attempt, a new read, merging again
			b.ok(b.P("p1", Finished{Consumer: b.run("p1", "r1", "ok", hD)}))
			b.readOf("p1", "r3", "ok")
			b.ok(b.P("p1", ReadYes{}))
			b.review("p2", "r1", hB)
			return b
		},
		"drop": func(t *testing.T) *bk {
			b := newBk(t, producer("n1"), producer("m1"), producer("p2", "n1"), producer("p3", "n1"), producer("p1"))
			b.ok(b.sentinel("g", []string{"n1"}))
			return b
		},
		"held": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"), producer("p3", "p1"))
			b.ok(b.P("p1", Resolve{}))
			b.take(b.start("p1", "r1"))
			b.ok(b.P("p2", Resolve{}))
			b.hold()
			return b
		},
		"redwaive": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("pb"))
			b.review("p1", "r1", hA)
			b.readOf("p1", "r2", "ok")
			b.ok(b.P("p1", ReadYes{}))
			b.ok(b.P("p1", Red{Head: hA, Reason: "ci"}))
			b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
			b.ok(b.P("pb", Resolve{}))
			return b
		},
	}
	var names []string
	for n := range worlds {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		n := n
		t.Run(n, func(t *testing.T) { t.Parallel(); sweepPairs(t, n, worlds[n]) })
	}
}

// sweepTriples samples ordered triples of single moves.
func sweepTriples(t *testing.T, name string, setup func(*testing.T) *bk, n int, seed int64) {
	t.Helper()
	steps := sweepSteps(setup(t))
	rng := rand.New(rand.NewSource(seed))
	hits := map[string]string{}
	count := map[string]int{}
	for k := 0; k < n; k++ {
		i, j, l := rng.Intn(len(steps)), rng.Intn(len(steps)), rng.Intn(len(steps))
		if i == j || j == l || i == l {
			continue
		}
		a, s := setup(t), setup(t)
		trio := []cold4Step{steps[i], steps[j], steps[l]}
		merged := a.merge(trio...)
		mwhy := a.last
		seqOK, seqWhy := true, ""
		for _, st := range trio {
			if !s.step(st) {
				seqOK, seqWhy = false, st.label+": "+s.last
				break
			}
		}
		ex := fmt.Sprintf("%s + %s + %s", trio[0].label, trio[1].label, trio[2].label)
		var key, val string
		switch {
		case merged && !seqOK:
			key, val = "MERGE-ACCEPTED-SEQ-REFUSED", ex+" :: "+seqWhy
		case !merged && seqOK:
			key, val = "over-refusal "+sweepNorm.ReplaceAllString(mwhy, "_"), ex+" :: "+mwhy
		case merged && seqOK:
			if d := cold4Diff(a.snap(), s.snap()); d != "" {
				key, val = "DIFFER", ex+" :: "+d
			}
		}
		if key != "" {
			count[key]++
			if _, ok := hits[key]; !ok {
				hits[key] = val
			}
		}
	}
	for k, v := range hits {
		sweepReport(t, k, "[%s] x%d %s\n      e.g. %s", name, count[k], k, v)
	}
}

// sweepReport logs an over-refusal (a merge may refuse what the moves one by
// one accept: one op writes a field once) and fails on the two unsound
// outcomes: a merge accepted that the sequence refuses, or a different board.
func sweepReport(t *testing.T, key, format string, args ...any) {
	t.Helper()
	if strings.HasPrefix(key, "MERGE-ACCEPTED-SEQ-REFUSED") || strings.HasPrefix(key, "DIFFER") {
		t.Errorf(format, args...)
		return
	}
	t.Logf(format, args...)
}

func TestBreakSweepTriples(t *testing.T) {
	t.Parallel()
	setups := map[string]func(*testing.T) *bk{
		"drop": func(t *testing.T) *bk {
			b := newBk(t, producer("n1"), producer("m1"), producer("p2", "n1"), producer("p3", "n1"), producer("p1"))
			b.ok(b.sentinel("g", []string{"n1"}))
			return b
		},
		"inflight": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2"), producer("pb"), producer("p3", "p1"))
			b.w.raw("prio", store.SetCardPriority{Card: "pb", Priority: "blocker"})
			for _, id := range []string{"p1", "p2", "pb"} {
				b.ok(b.P(id, Resolve{}))
			}
			b.take(b.start("p1", "r2"))
			b.start("p2", "r3")
			return b
		},
		"review": func(t *testing.T) *bk {
			b := newBk(t, producer("p1"), producer("p2", "p1"))
			b.review("p1", "r1", hA)
			b.readOf("p1", "r2", "ok")
			b.ok(b.P("p1", AskRead{Row: "r3"}))
			return b
		},
	}
	// 12000 samples per setup, in 8 shards of 1500 run in parallel, each
	// shard its own seed (7..14): the same coverage in an eighth of the time.
	// -short (the per-commit unit job) samples a tenth; the nightly all.
	samples := 1500
	if testing.Short() {
		samples = 150
	}
	for n, f := range setups {
		for shard := int64(0); shard < 8; shard++ {
			n, f, shard := n, f, shard
			t.Run(fmt.Sprintf("%s/%d", n, shard), func(t *testing.T) { t.Parallel(); sweepTriples(t, n, f, samples, 7+shard) })
		}
	}
}

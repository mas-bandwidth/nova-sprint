package sprintreplay

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The failure classes of an attempt.
const (
	ClassIdleReap      = "idle-reap"
	ClassE12           = "E12"
	ClassWrongHost     = "wrong-host"
	ClassBriefDefect   = "brief-defect"
	ClassHarnessFault  = "harness-fault"
	ClassWorkerNotDone = "worker-not-done"
	ClassConflict      = "conflict"
	ClassOther         = "other"
)

// Classes is every class, in the order the report counts them.
var Classes = []string{ClassIdleReap, ClassE12, ClassWrongHost, ClassBriefDefect, ClassHarnessFault, ClassWorkerNotDone, ClassConflict, ClassOther}

// ClassRow is one row of the class table: the class, the log's words that trigger it, and
// the test on an attempt's evidence. The first row that matches wins.
type ClassRow struct {
	Class  string
	Phrase string
	match  func(a *Attempt, ev string) bool
}

func has(ev string, words ...string) bool {
	ev = strings.ToLower(ev)
	for _, w := range words {
		if strings.Contains(ev, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// ran is the attempt's ran_attempts record as the sprint reads it (sprint.RanAttempt): a
// worker's own "not-done: ..." line is read there as not ran (ranAttempts reads any status
// beginning "no"), so the table reads such a line as the worker's report, never the harness's.
func ran(a *Attempt) (bool, bool) {
	ok, _, recorded := sprint.RanAttempt("attempt "+strconv.Itoa(a.N)+": "+a.Ran, a.N)
	if strings.HasPrefix(a.Ran, "not-done") || strings.HasPrefix(a.Ran, "not done") {
		return true, recorded
	}
	return ok, recorded
}

// ClassTable maps a failed attempt to its class from the log's own words: the attempt's
// ran_attempts record (attempts.go, read through sprint.RanAttempt), its failed finish's
// report (sprint.FailureClass, sprint.BriefDefectOf, sprint.ClassifyEnd), the hold cause and
// idle-reap words of the line that began the next attempt (its why, which the attempts table
// carries too). The evidence is the why, then the report.
var ClassTable = []ClassRow{
	{ClassIdleReap, `"idle-reap" / "idle reap" with the attempt's ran_attempts "no" (ClassifyEnd harness)`, func(a *Attempt, ev string) bool {
		r, rec := ran(a)
		return rec && !r && has(ev, "idle-reap", "idle reap", "idle-reaped")
	}},
	{ClassE12, `"(E12)" / "outside its PATHS" (the lander's scope check, land_scope.go)`, func(a *Attempt, ev string) bool {
		return has(ev, "(E12)", "outside its PATHS", "lander E12")
	}},
	{ClassWrongHost, `"working Mac" / "not the target bench" / "non-target" / "wrong host"`, func(a *Attempt, ev string) bool {
		return has(ev, "working Mac", "not the target bench", "non-target", "wrong host", "isa_neon: no")
	}},
	{ClassConflict, `"the landing refused" with "conflict" / "does not merge"`, func(a *Attempt, ev string) bool {
		return has(ev, "the landing refused") && has(ev, "conflict", "does not merge")
	}},
	{ClassBriefDefect, `BriefDefectOf(report) != "" (ClassifyEnd brief), or "brief edited in place" / "brief replaced" (the brief re-cut under the attempt)`, func(a *Attempt, ev string) bool {
		return sprint.ClassifyEnd(true, a.Report) == "brief" || strings.HasPrefix(a.Why, "brief edited in place") || strings.HasPrefix(a.Why, "brief replaced")
	}},
	{ClassHarnessFault, `ran_attempts "no: <fault>" (ClassifyEnd harness), FailureClass "no result", "harness fault", "deadline: no RESULT", "push refused"`, func(a *Attempt, ev string) bool {
		r, rec := ran(a)
		return rec && sprint.ClassifyEnd(r, a.Report) == "harness" || sprint.FailureClass(a.Report) == "no result" ||
			has(ev, "harness fault", "deadline: no RESULT", "push refused", "idle-reap", "idle reap")
	}},
	{ClassWorkerNotDone, `"not-done" / "not done" / FailureClass "verdict ..." / "found it broken" / "sent back by the coordinator" / "the landing refused its head: gate|checks"`, func(a *Attempt, ev string) bool {
		return has(ev, "not-done", "not done", "found it broken", "sent back by the coordinator", "the landing refused", "HOLD", "no commit") ||
			strings.HasPrefix(sprint.FailureClass(a.Report), "verdict")
	}},
}

// classify sets a failed attempt's class from the class table, and the rule code's own
// reading of its end (FailureClass, ClassifyEnd).
func classify(a *Attempt) {
	ev := a.Why + "\n" + a.Failure + "\n" + a.Report
	a.FailureClass = sprint.FailureClass(a.Report)
	r, _ := ran(a)
	a.EndKind = sprint.ClassifyEnd(r, a.Report)
	a.Class, a.Phrase = ClassOther, "none of the above"
	for _, row := range ClassTable {
		if row.match(a, ev) {
			a.Class, a.Phrase = row.Class, row.Phrase
			return
		}
	}
}

// RateWindow is how many of a member's (a route's) last known outcomes its recent failure
// rate counts, over the outcomes known before the deal.
const RateWindow = 10

// DealView is what the log held about an attempt at its deal, beyond who it was dealt to.
type DealView struct {
	PriorClass  string `json:"prior_class"` // the class of the attempt before, when known by the deal
	PriorFails  int    `json:"prior_fails"` // the primary's failed attempts known by the deal
	MemberFails int    `json:"member_fails"`
	MemberEnds  int    `json:"member_ends"`
	RouteFails  int    `json:"route_fails"`
	RouteEnds   int    `json:"route_ends"`
	BriefBytes  int    `json:"brief_bytes"`
	BriefEdits  int    `json:"brief_edits"`
}

// features fills each attempt's deal view from the attempts whose outcome was known at or
// before its deal: a function of the lines at or before the deal alone.
func features(r *Result) {
	known := append([]*Attempt(nil), r.Attempts...)
	sort.SliceStable(known, func(i, j int) bool { return known[i].OutcomeAt.Before(known[j].OutcomeAt) })
	for _, a := range r.Attempts {
		var mem, route []bool
		for _, k := range known {
			if k.Outcome == Open || k.OutcomeAt.After(a.Dealt) {
				continue
			}
			f := k.Outcome == Failed
			if k.Primary == a.Primary && k.N < a.N && f {
				a.Deal.PriorFails++
				if k.N == a.N-1 {
					a.Deal.PriorClass = k.Class
				}
			}
			if k.Member == a.Member {
				mem = append(mem, f)
			}
			if k.Route == a.Route {
				route = append(route, f)
			}
		}
		a.Deal.MemberFails, a.Deal.MemberEnds = tail(mem)
		a.Deal.RouteFails, a.Deal.RouteEnds = tail(route)
	}
}

func tail(xs []bool) (fails, ends int) {
	if len(xs) > RateWindow {
		xs = xs[len(xs)-RateWindow:]
	}
	for _, f := range xs {
		if f {
			fails++
		}
	}
	return fails, len(xs)
}

// AttemptView is an attempt as Predict reads it at a moment of its life: who and what it was
// dealt as, its deal view, and what its life had shown by the moment (Since*, NoResultTakes).
// Every field is knowable at that moment; the view holds no clock.
type AttemptView struct {
	Stream, Priority, Tier, Route, Provider, Member, Kind string
	N                                                     int
	Deal                                                  DealView
	SinceAlive                                            time.Duration // since the last take or progress line (since the deal before any take)
	Taken                                                 bool
	NoResultTakes                                         int
}

// View is the attempt's view at its deal.
func (a *Attempt) View() AttemptView {
	return AttemptView{Stream: a.Stream, Priority: a.Priority, Tier: a.Tier, Route: a.Route, Provider: a.Provider,
		Member: a.Member, Kind: a.Kind, N: a.N, Deal: a.Deal}
}

// Rule is one candidate signal: a feature and a value (a categorical feature) or a threshold
// (a counted one, fired at or above it).
type Rule struct {
	Feature   string
	Value     string
	Threshold float64
}

func (r Rule) String() string {
	if r.Value != "" {
		return r.Feature + "=" + r.Value
	}
	return r.Feature + ">=" + strconv.FormatFloat(r.Threshold, 'f', -1, 64)
}

// inFlight says the rule's feature is known only during the attempt's life.
func (r Rule) inFlight() bool {
	return r.Feature == "silent_s" || r.Feature == "untaken_s" || r.Feature == "no_result_takes"
}

func rate(f, n int) float64 {
	if n < 3 {
		return -1 // too few outcomes to say
	}
	return float64(f) / float64(n)
}

// Fires says the rule fires on the view.
func (r Rule) Fires(v AttemptView) bool {
	cat := map[string]string{"stream": v.Stream, "priority": v.Priority, "tier": v.Tier, "route": v.Route,
		"provider": v.Provider, "member": v.Member, "kind": v.Kind, "prior_class": v.Deal.PriorClass}
	if s, ok := cat[r.Feature]; ok {
		return s == r.Value
	}
	var x float64
	switch r.Feature {
	case "attempt":
		x = float64(v.N)
	case "prior_fails":
		x = float64(v.Deal.PriorFails)
	case "member_fail_rate":
		x = rate(v.Deal.MemberFails, v.Deal.MemberEnds)
	case "route_fail_rate":
		x = rate(v.Deal.RouteFails, v.Deal.RouteEnds)
	case "brief_bytes":
		x = float64(v.Deal.BriefBytes)
	case "brief_edits":
		x = float64(v.Deal.BriefEdits)
	case "silent_s":
		if !v.Taken {
			return false
		}
		x = v.SinceAlive.Seconds()
	case "untaken_s":
		if v.Taken {
			return false
		}
		x = v.SinceAlive.Seconds()
	case "no_result_takes":
		x = float64(v.NoResultTakes)
	default:
		return false
	}
	return x >= 0 && x >= r.Threshold
}

// knownAt is the first moment before the attempt's end at which the rule fires, and whether
// it does. A deal feature is known at the deal; an in-flight one at the moment its condition
// first holds, read off the attempt's own events (time is each line's At, never a clock).
func (r Rule) knownAt(a *Attempt) (time.Time, bool) {
	v := a.View()
	if !r.inFlight() {
		return a.Dealt, r.Fires(v)
	}
	alive, taken := a.Dealt, false
	check := func(now time.Time) (time.Time, bool) {
		if r.Feature == "no_result_takes" {
			return now, r.Fires(v)
		}
		if t := alive.Add(time.Duration(r.Threshold * float64(time.Second))); t.Before(now) || t.Equal(now) {
			v.SinceAlive, v.Taken = t.Sub(alive), taken
			if r.Fires(v) {
				return t, true
			}
		}
		return time.Time{}, false
	}
	for _, e := range a.Events {
		if t, ok := check(e.At); ok {
			return t, true
		}
		switch e.What {
		case "take", "progress":
			alive, taken = e.At, true
		case "no-result":
			v.NoResultTakes++
			if t, ok := check(e.At); ok {
				return t, true
			}
			alive, taken = e.At, false
		case "redeal":
			alive, taken = e.At, false
		}
	}
	return check(a.End)
}

// Counts is a signal's confusion counts on a half, its base rate and lead time.
type Counts struct {
	N, Failed      int
	TP, FP, FN, TN int
	LeadMedianS    int // median seconds from when the signal is known to the attempt's end, over its true positives
}

func (c Counts) Precision() float64 { return div(c.TP, c.TP+c.FP) }
func (c Counts) Recall() float64    { return div(c.TP, c.TP+c.FN) }
func (c Counts) Base() float64      { return div(c.Failed, c.N) }

func div(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Candidate is one rule's counts on the train half.
type Candidate struct {
	Name  string
	Rule  Rule
	Train Counts
}

// Half is a signal's counts on one half, overall and per class (caught of failed).
type Half struct {
	Counts
	Caught map[string][2]int // class -> [caught, failed]
}

// SearchReport is the train-half search and the held-out judgement.
type SearchReport struct {
	TrainN, TestN int
	SplitAt       time.Time // the deal of the first test attempt
	Top           []Candidate
	Chosen        Candidate
	ChosenClass   string
	Train, Test   Half
	Classes       map[string]int // class -> failed attempts, both halves
}

// The selection rule, fixed before the search: a candidate qualifies on the train half when
// its recall is at least RecallFloor and its precision clears the train base rate b by
// Margin of what is left above it (precision >= b + Margin*(1-b): with Margin 0.5, its false
// alarms are at most half the share of flagging every attempt);
// the earliest qualifying one (the largest median lead time) is chosen, ties to the higher
// precision, then the name.
const (
	TrainShare  = 0.6
	RecallFloor = 0.10
	Margin      = 0.5
	MinSupport  = 5 // a categorical value seen on fewer train attempts is not a candidate
)

// scored is the attempts with a known outcome, in deal order.
func scored(r *Result) []*Attempt {
	var out []*Attempt
	for _, a := range r.Attempts {
		if a.Outcome != Open {
			out = append(out, a)
		}
	}
	return out
}

// candidates is every rule the train half offers: each categorical value seen on MinSupport
// train attempts, each counted feature at each distinct train value, each in-flight feature
// at a fixed ladder of thresholds.
func candidates(train []*Attempt) []Rule {
	cats := []string{"stream", "priority", "tier", "route", "provider", "member", "kind", "prior_class"}
	seen := map[Rule]int{}
	nums := map[string]map[float64]bool{}
	for _, a := range train {
		v := a.View()
		for _, f := range cats {
			val := map[string]string{"stream": v.Stream, "priority": v.Priority, "tier": v.Tier, "route": v.Route,
				"provider": v.Provider, "member": v.Member, "kind": v.Kind, "prior_class": v.Deal.PriorClass}[f]
			if val != "" {
				seen[Rule{Feature: f, Value: val}]++
			}
		}
		for f, x := range map[string]float64{"attempt": float64(v.N), "prior_fails": float64(v.Deal.PriorFails),
			"member_fail_rate": rate(v.Deal.MemberFails, v.Deal.MemberEnds), "route_fail_rate": rate(v.Deal.RouteFails, v.Deal.RouteEnds),
			"brief_bytes": float64(v.Deal.BriefBytes), "brief_edits": float64(v.Deal.BriefEdits)} {
			if x > 0 {
				if nums[f] == nil {
					nums[f] = map[float64]bool{}
				}
				nums[f][x] = true
			}
		}
	}
	var out []Rule
	for r, n := range seen {
		if n >= MinSupport {
			out = append(out, r)
		}
	}
	for f, xs := range nums {
		for x := range xs {
			out = append(out, Rule{Feature: f, Threshold: x})
		}
	}
	for _, s := range []float64{60, 120, 180, 240, 300, 600, 900, 1800, 3600} {
		out = append(out, Rule{Feature: "silent_s", Threshold: s}, Rule{Feature: "untaken_s", Threshold: s})
	}
	out = append(out, Rule{Feature: "no_result_takes", Threshold: 1}, Rule{Feature: "no_result_takes", Threshold: 2})
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// judge is the rule's counts on the attempts.
func judge(r Rule, as []*Attempt) Half {
	h := Half{Caught: map[string][2]int{}}
	var leads []int
	for _, a := range as {
		h.N++
		f := a.Outcome == Failed
		t, fires := r.knownAt(a)
		if f {
			h.Failed++
			c := h.Caught[a.Class]
			c[1]++
			if fires {
				c[0]++
			}
			h.Caught[a.Class] = c
		}
		switch {
		case fires && f:
			h.TP++
			leads = append(leads, int(a.End.Sub(t).Seconds()))
		case fires:
			h.FP++
		case f:
			h.FN++
		default:
			h.TN++
		}
	}
	sort.Ints(leads)
	if len(leads) > 0 {
		h.LeadMedianS = leads[len(leads)/2]
	}
	return h
}

// Search splits the scored attempts by deal time, chooses the earliest qualifying signal on
// the train half alone, and judges it on the test half.
func Search(r *Result) SearchReport {
	as := scored(r)
	cut := int(float64(len(as)) * TrainShare)
	train, test := as[:cut], as[cut:]
	rep := SearchReport{TrainN: len(train), TestN: len(test), Classes: map[string]int{}}
	if len(test) > 0 {
		rep.SplitAt = test[0].Dealt
	}
	for _, a := range as {
		if a.Outcome == Failed {
			rep.Classes[a.Class]++
		}
	}
	base := judge(Rule{Feature: "none"}, train).Base()
	var all []Candidate
	for _, rule := range candidates(train) {
		h := judge(rule, train)
		all = append(all, Candidate{Name: rule.String(), Rule: rule, Train: h.Counts})
	}
	// the top five by precision at the recall floor
	sort.SliceStable(all, func(i, j int) bool {
		pi, pj := all[i].Train.Precision(), all[j].Train.Precision()
		if pi != pj {
			return pi > pj
		}
		return all[i].Name < all[j].Name
	})
	var chosen *Candidate
	for i := range all {
		c := &all[i]
		if c.Train.Recall() < RecallFloor {
			continue
		}
		if len(rep.Top) < 5 {
			rep.Top = append(rep.Top, *c)
		}
		if c.Train.Precision() < base+Margin*(1-base) {
			continue
		}
		if chosen == nil || c.Train.LeadMedianS > chosen.Train.LeadMedianS ||
			c.Train.LeadMedianS == chosen.Train.LeadMedianS && c.Train.Precision() > chosen.Train.Precision() {
			chosen = c
		}
	}
	if chosen == nil {
		return rep
	}
	rep.Chosen = *chosen
	rep.Train, rep.Test = judge(chosen.Rule, train), judge(chosen.Rule, test)
	best := ""
	for _, c := range Classes {
		if best == "" || rep.Train.Caught[c][0] > rep.Train.Caught[best][0] {
			best = c
		}
	}
	rep.ChosenClass = best
	return rep
}

// String is the report: the class counts, the top five train candidates, the chosen signal,
// and its held-out numbers per class and overall.
func (s SearchReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scored attempts %d: train %d, test %d (test from %s)\n", s.TrainN+s.TestN, s.TrainN, s.TestN, s.SplitAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "classes:")
	for _, c := range Classes {
		fmt.Fprintf(&b, " %s=%d", c, s.Classes[c])
	}
	fmt.Fprintf(&b, "\ntop five on train (precision at recall >= %.2f):\n", RecallFloor)
	for _, c := range s.Top {
		fmt.Fprintf(&b, "  %-40s precision %.3f recall %.3f tp %d fp %d lead %ds\n", c.Name, c.Train.Precision(), c.Train.Recall(), c.Train.TP, c.Train.FP, c.Train.LeadMedianS)
	}
	fmt.Fprintf(&b, "chosen: %s (class %s), train base %.3f, bar %.3f (base + %.2f of the rest)\n", s.Chosen.Name, s.ChosenClass, s.Train.Base(), s.Train.Base()+Margin*(1-s.Train.Base()), Margin)
	for _, h := range []struct {
		name string
		h    Half
	}{{"train", s.Train}, {"test", s.Test}} {
		fmt.Fprintf(&b, "  %s: n %d failed %d base %.3f precision %.3f recall %.3f tp %d fp %d fn %d tn %d lead %ds\n", h.name, h.h.N, h.h.Failed, h.h.Base(), h.h.Precision(), h.h.Recall(), h.h.TP, h.h.FP, h.h.FN, h.h.TN, h.h.LeadMedianS)
		fmt.Fprintf(&b, "    caught/failed by class:")
		for _, c := range Classes {
			fmt.Fprintf(&b, " %s=%d/%d", c, h.h.Caught[c][0], h.h.Caught[c][1])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Predict is the chosen signal as a pure function of an attempt's view: whether it predicts
// the attempt fails, the class it most often caught on the train half, and why.
func Predict(rec AttemptView) (fail bool, class string, why string) {
	if !ChosenRule.Fires(rec) {
		return false, "", ""
	}
	return true, ChosenClass, "the earliest signal chosen on epoch 21's train half fires: " + ChosenRule.String()
}

// ChosenRule and ChosenClass are the signal Search chose on epoch 21's train half, pinned so
// Predict needs no replay (TestEarliestSignalPredictsHeldOut fails when the search moves).
var (
	ChosenRule  = Rule{Feature: "brief_bytes", Threshold: 14954}
	ChosenClass = ClassIdleReap
)

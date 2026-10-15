package sprintreplay

import (
	"maps"
	"testing"
	"time"
)

func TestEarliestSignalPredictsHeldOut(t *testing.T) {
	t.Parallel()
	res := replayed(t)
	rep := Search(res)
	if rep.Chosen.Name != pinned.Chosen {
		t.Fatalf("the train half chose %q, pinned %q", rep.Chosen.Name, pinned.Chosen)
	}
	if rep.Chosen.Rule != ChosenRule {
		t.Fatalf("the search's rule %+v is not Predict's %+v", rep.Chosen.Rule, ChosenRule)
	}
	got := Pinned{
		Chosen: rep.Chosen.Name, ChosenClass: rep.ChosenClass,
		TrainDeals: rep.TrainDeals, TestDeals: rep.TestDeals,
		Train: rep.Train.Counts, Test: rep.Test.Counts,
		TrainCaught: caughtByClass(rep.Train), TestCaught: caughtByClass(rep.Test),
		Classes: rep.Classes, Other: res.Other(), Labelled: res.Labelled(),
	}
	if got.ChosenClass != pinned.ChosenClass || got.TrainDeals != pinned.TrainDeals ||
		got.TestDeals != pinned.TestDeals || got.Train != pinned.Train || got.Test != pinned.Test ||
		!maps.Equal(got.TrainCaught, pinned.TrainCaught) || !maps.Equal(got.TestCaught, pinned.TestCaught) ||
		!maps.Equal(got.Classes, pinned.Classes) || got.Other != pinned.Other || got.Labelled != pinned.Labelled {
		t.Errorf("held-out numbers moved:\n got %+v\nwant %+v", got, pinned)
	}
	if res.Other()*10 >= res.Labelled() {
		t.Errorf("%d of %d failed attempts fall in other: 10%% or more", res.Other(), res.Labelled())
	}
	if rep.Test.Precision() >= rep.Test.Base() {
		t.Errorf("the chosen signal now beats the test half's base rate (%.3f >= %.3f): the pinned result changed", rep.Test.Precision(), rep.Test.Base())
	}
	// Predict is the chosen rule, pure: a view is all it reads.
	if fail, class, why := Predict(AttemptView{Deal: DealView{BriefBytes: 14599}}); !fail || class != ClassIdleReap || why == "" {
		t.Errorf("Predict at the threshold = %v %q %q, want a failure of class %s", fail, class, why, ClassIdleReap)
	}
	if fail, _, _ := Predict(AttemptView{Deal: DealView{BriefBytes: 14598}}); fail {
		t.Error("Predict below the threshold predicts a failure")
	}
	t.Log(rep.String())
}

// Pinned is the held-out result TestEarliestSignalPredictsHeldOut pins.
type Pinned struct {
	Chosen, ChosenClass   string
	TrainDeals, TestDeals int
	Train, Test           Counts
	TrainCaught           map[string][2]int
	TestCaught            map[string][2]int
	Classes               map[string]int
	Other, Labelled       int
}

func caughtByClass(h Half) map[string][2]int {
	out := make(map[string][2]int, len(Classes))
	for _, class := range Classes {
		out[class] = h.Caught[class]
	}
	return out
}

// pinned is epoch 21's result, chosen on the train half before the test half was read. The
// chosen signal does not beat the test half's base rate (precision 4/9 = 0.444 against a base
// of 93/144 = 0.646): that is the result, pinned as it is and never retuned on the test half.
var pinned = Pinned{
	Chosen: "brief_bytes>=14599", ChosenClass: ClassIdleReap,
	TrainDeals: 259, TestDeals: 174,
	Train: Counts{N: 252, Failed: 210, TP: 26, FP: 1, FN: 184, TN: 41, LeadMedianS: 1539},
	Test:  Counts{N: 144, Failed: 93, TP: 4, FP: 5, FN: 89, TN: 46, LeadMedianS: 2758},
	TrainCaught: map[string][2]int{
		ClassIdleReap: {16, 117}, ClassE12: {0, 0}, ClassWrongHost: {0, 0},
		ClassBriefDefect: {1, 12}, ClassHarnessFault: {3, 13}, ClassWorkerNotDone: {3, 56},
		ClassConflict: {1, 3}, ClassOther: {2, 9},
	},
	TestCaught: map[string][2]int{
		ClassIdleReap: {0, 2}, ClassE12: {0, 3}, ClassWrongHost: {0, 1},
		ClassBriefDefect: {0, 25}, ClassHarnessFault: {1, 3}, ClassWorkerNotDone: {2, 50},
		ClassConflict: {1, 7}, ClassOther: {0, 2},
	},
	Classes: map[string]int{
		ClassIdleReap: 119, ClassE12: 3, ClassWrongHost: 1, ClassBriefDefect: 37,
		ClassHarnessFault: 16, ClassWorkerNotDone: 106, ClassConflict: 10, ClassOther: 11,
	},
	Other: 11, Labelled: 303,
}

func TestInFlightSilenceStopsAtWithdrawalAndNeedsLeadTime(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	silence := Rule{Feature: "silent_s", Threshold: 5}
	withdrawn := &Attempt{Dealt: base, End: base.Add(20 * time.Second), Events: []Event{
		{At: base.Add(time.Second), What: "take"},
		{At: base.Add(3 * time.Second), What: "take-ended"},
	}}
	if at, ok := silence.knownAt(withdrawn); ok {
		t.Errorf("silence fires at %s after its take ended at 3s", at)
	}
	untaken := Rule{Feature: "untaken_s", Threshold: 5}
	if at, ok := untaken.knownAt(withdrawn); ok {
		t.Errorf("untaken silence fires at %s while the work is withdrawn", at)
	}
	finished := &Attempt{Dealt: base, End: base.Add(6 * time.Second), Events: []Event{
		{At: base.Add(time.Second), What: "take"},
		{At: base.Add(6 * time.Second), What: "finish"},
	}}
	if at, ok := silence.knownAt(finished); ok {
		t.Errorf("silence fires at %s exactly when the attempt ends, with no lead", at)
	}
}

func TestStagingRefusalIsKnownOnlyBeforeAttemptEnd(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	rule := Rule{Feature: "staging_takes", Threshold: 1}
	a := &Attempt{Dealt: base, End: base.Add(10 * time.Second), Events: []Event{
		{At: base.Add(time.Second), What: "take"},
		{At: base.Add(3 * time.Second), What: "staging"},
		{At: base.Add(3 * time.Second), What: "take-ended"},
		{At: base.Add(5 * time.Second), What: "redeal"},
	}}
	if at, ok := rule.knownAt(a); !ok || !at.Equal(base.Add(3*time.Second)) {
		t.Errorf("first staging refusal known at %s, ok=%v; want 3s before end", at, ok)
	}
	a.End = base.Add(3 * time.Second)
	if at, ok := rule.knownAt(a); ok {
		t.Errorf("staging refusal at attempt end predicts with no lead at %s", at)
	}
}

func TestSearchSplitsAllDealsBeforeScoringKnownOutcomes(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	res := &Result{}
	for n := 0; n < 5; n++ {
		a := &Attempt{Primary: "card", N: n + 1, Dealt: base.Add(time.Duration(n) * time.Second),
			End: base.Add(time.Duration(n+10) * time.Second), Outcome: Failed, Class: ClassOther}
		if n == 3 {
			a.Outcome = Open // an unresolved deal belongs to the test 40% timeline
		}
		res.Attempts = append(res.Attempts, a)
	}
	rep := Search(res)
	if rep.TrainN != 3 || rep.TestN != 1 || !rep.SplitAt.Equal(base.Add(3*time.Second)) {
		t.Errorf("split train=%d test=%d at=%s, want 3 known train, 1 known test, fourth deal boundary",
			rep.TrainN, rep.TestN, rep.SplitAt)
	}
}

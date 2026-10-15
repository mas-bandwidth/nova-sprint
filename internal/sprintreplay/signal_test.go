package sprintreplay

import "testing"

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
		Chosen: rep.Chosen.Name, Train: rep.Train.Counts, Test: rep.Test.Counts,
		Other: res.Other(), Labelled: res.Labelled(),
	}
	if got.Train != pinned.Train || got.Test != pinned.Test || got.Other != pinned.Other || got.Labelled != pinned.Labelled {
		t.Errorf("held-out numbers moved:\n got %+v\nwant %+v", got, pinned)
	}
	if res.Other()*10 >= res.Labelled() {
		t.Errorf("%d of %d failed attempts fall in other: 10%% or more", res.Other(), res.Labelled())
	}
	if rep.Test.Precision() >= rep.Test.Base() {
		t.Errorf("the chosen signal now beats the test half's base rate (%.3f >= %.3f): the pinned result changed", rep.Test.Precision(), rep.Test.Base())
	}
	// Predict is the chosen rule, pure: a view is all it reads.
	if fail, class, why := Predict(AttemptView{Deal: DealView{BriefBytes: 14954}}); !fail || class != ClassIdleReap || why == "" {
		t.Errorf("Predict at the threshold = %v %q %q, want a failure of class %s", fail, class, why, ClassIdleReap)
	}
	if fail, _, _ := Predict(AttemptView{Deal: DealView{BriefBytes: 14953}}); fail {
		t.Error("Predict below the threshold predicts a failure")
	}
	t.Log(rep.String())
}

// Pinned is the held-out result TestEarliestSignalPredictsHeldOut pins.
type Pinned struct {
	Chosen          string
	Train, Test     Counts
	Other, Labelled int
}

// pinned is epoch 21's result, chosen on the train half before the test half was read. The
// chosen signal does not beat the test half's base rate (precision 6/10 = 0.600 against a base
// of 104/159 = 0.654): that is the result, pinned as it is and never retuned on the test half.
var pinned = Pinned{
	Chosen:   "brief_bytes>=14954",
	Train:    Counts{N: 237, Failed: 199, TP: 20, FP: 1, FN: 179, TN: 37, LeadMedianS: 1539},
	Test:     Counts{N: 159, Failed: 104, TP: 6, FP: 4, FN: 98, TN: 51, LeadMedianS: 3579},
	Other:    11,
	Labelled: 303,
}

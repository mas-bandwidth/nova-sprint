package run

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestEventNoticesUseCommittedImagesAndRecipients(t *testing.T) {
	t.Parallel()
	e := store.Event{Accepted: true, Epoch: 2, Before: &store.State{Cards: map[string]store.Card{"p": {ID: "p", Kind: "producer", State: "merging"}}}, After: &store.State{Cards: map[string]store.Card{
		"p":    {ID: "p", Kind: "producer", Stream: "s", State: "landed", Head: "abc"},
		"p.w1": {ID: "p.w1", Kind: "work", Stream: "s", State: "working", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 1, Model: "deepseek/deepseek-flash"},
	}, Judgements: map[string]store.Judgement{"j": {ID: "j", State: "open", Group: "g", Question: "review", Command: "nova-sprint judge j retry"}}}}
	n := EventNotices{Coordinator: "rowan"}
	a, err := n.ForEvent(e, store.Snapshot{}, "alice")
	if err != nil || len(a) != 1 || a[0].Subject != "DEAL" || !strings.Contains(a[0].Line, "take p.w1@1 --epoch 2 --as alice") || !strings.Contains(a[0].Line, " model=deepseek/deepseek-flash ") {
		t.Fatalf("alice=%+v err=%v", a, err)
	}
	r, err := n.ForEvent(e, store.Snapshot{}, "rowan")
	if err != nil || len(r) != 1 || r[0].Group != "g" { // a card's landing is no message; its stream's last is
		t.Fatalf("rowan=%+v err=%v", r, err)
	}
	again, _ := n.ForEvent(e, store.Snapshot{}, "rowan")
	if !reflect.DeepEqual(r, again) {
		t.Fatal("retry rendered differently")
	}
	e.Accepted = false
	got, _ := n.ForEvent(e, store.Snapshot{}, "alice")
	if len(got) != 0 {
		t.Fatal("refused event emitted notice")
	}
}

// TestReturnedHandTellsTheCoordinator: a card returned from a row that went
// held or down is one RETURNED line to the coordinator per card; a card a
// row released itself is not.
func TestReturnedHandTellsTheCoordinator(t *testing.T) {
	t.Parallel()
	n := EventNotices{Coordinator: "rowan"}
	working := store.Card{ID: "p.w1", Kind: "work", State: "working", Holder: "aurora"}
	reading := store.Card{ID: "q.r1", Kind: "read", State: "working", Holder: "aurora"}
	e := store.Event{Accepted: true, Before: &store.State{Cards: map[string]store.Card{"p.w1": working, "q.r1": reading}}, After: &store.State{Cards: map[string]store.Card{
		"p.w1": {ID: "p.w1", Kind: "work", State: "fail", Holder: "aurora", Reason: card.ReleasedPrefix + card.Returned("aurora", "held")},
		"q.r1": {ID: "q.r1", Kind: "read", State: "fail", Holder: "aurora", Reason: card.ReleasedPrefix + card.Returned("aurora", "held")},
	}}}
	got, err := n.ForEvent(e, store.Snapshot{}, "rowan")
	if err != nil || len(got) != 2 || got[0].Subject != "RETURNED" || !strings.HasPrefix(got[0].Line, "RETURNED card=p.w1 from=aurora: released: returned from row aurora (held)") ||
		!strings.HasPrefix(got[1].Line, "RETURNED card=q.r1 from=aurora: ") {
		t.Fatalf("rowan=%+v err=%v", got, err)
	}
	e.After.Cards["p.w1"] = store.Card{ID: "p.w1", Kind: "work", State: "fail", Holder: "aurora", Reason: card.ReleasedPrefix + "my harness died"}
	delete(e.After.Cards, "q.r1")
	if got, _ := n.ForEvent(e, store.Snapshot{}, "rowan"); len(got) != 0 {
		t.Fatalf("a row's own release told the coordinator: %+v", got)
	}
}

func TestJudgementCloseAndBoundedText(t *testing.T) {
	t.Parallel()
	n := EventNotices{Coordinator: "rowan"}
	e := store.Event{Accepted: true, Before: &store.State{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "open"}}}, After: &store.State{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "expired", Answer: strings.Repeat("é", 200)}}}}
	got, err := n.ForEvent(e, store.Snapshot{}, "rowan")
	if err != nil || len(got) != 1 || got[0].Subject != "EXPIRED" || !utf8.ValidString(got[0].Line) || !strings.Contains(got[0].Line, "full text: nova-sprint judge") {
		t.Fatalf("notice=%+v err=%v", got, err)
	}
	e.After.Judgements["j"] = store.Judgement{ID: "j", State: "answered", Actor: "emma", Answer: "retry"}
	got, _ = n.ForEvent(e, store.Snapshot{}, "rowan")
	if len(got) != 1 || got[0].Subject != "ANSWERED" {
		t.Fatalf("notice=%+v", got)
	}
	e.After.Judgements["j"] = store.Judgement{ID: "j", State: "answered", Actor: "rowan", Answer: "retry"}
	got, _ = n.ForEvent(e, store.Snapshot{}, "rowan")
	if len(got) != 0 {
		t.Fatal("coordinator's own answer was echoed")
	}
}

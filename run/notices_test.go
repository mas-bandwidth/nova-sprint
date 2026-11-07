package run

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestEventNoticesUseCommittedImagesAndRecipients(t *testing.T) {
	e := store.Event{Accepted: true, Epoch: 2, Before: &store.State{Cards: map[string]store.Card{"p": {ID: "p", Kind: "producer", State: "merging"}}}, After: &store.State{Cards: map[string]store.Card{
		"p":    {ID: "p", Kind: "producer", Stream: "s", State: "landed", Head: "abc"},
		"p.w1": {ID: "p.w1", Kind: "work", Stream: "s", State: "working", Holder: "alice", Tier: "pro", Attempt: 1, Gen: 1, Model: "deepseek/deepseek-flash"},
	}, Judgements: map[string]store.Judgement{"j": {ID: "j", State: "open", Group: "g", Question: "review", Command: "nova-sprint judge j retry"}}}}
	n := EventNotices{Coordinator: "coord"}
	a, err := n.ForEvent(e, store.Snapshot{}, "alice")
	if err != nil || len(a) != 1 || a[0].Subject != "DEAL" || !strings.Contains(a[0].Line, "take p.w1@1 --epoch 2 --as alice") || !strings.Contains(a[0].Line, " model=deepseek/deepseek-flash ") {
		t.Fatalf("alice=%+v err=%v", a, err)
	}
	r, err := n.ForEvent(e, store.Snapshot{}, "coord")
	if err != nil || len(r) != 2 || r[0].Subject != "LANDED" || r[1].Group != "g" {
		t.Fatalf("coord=%+v err=%v", r, err)
	}
	again, _ := n.ForEvent(e, store.Snapshot{}, "coord")
	if !reflect.DeepEqual(r, again) {
		t.Fatal("retry rendered differently")
	}
	e.Accepted = false
	got, _ := n.ForEvent(e, store.Snapshot{}, "alice")
	if len(got) != 0 {
		t.Fatal("refused event emitted notice")
	}
}
func TestJudgementCloseAndBoundedText(t *testing.T) {
	n := EventNotices{Coordinator: "coord"}
	e := store.Event{Accepted: true, Before: &store.State{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "open"}}}, After: &store.State{Judgements: map[string]store.Judgement{"j": {ID: "j", State: "expired", Answer: strings.Repeat("é", 200)}}}}
	got, err := n.ForEvent(e, store.Snapshot{}, "coord")
	if err != nil || len(got) != 1 || got[0].Subject != "EXPIRED" || !utf8.ValidString(got[0].Line) || !strings.Contains(got[0].Line, "full text: nova-sprint judge") {
		t.Fatalf("notice=%+v err=%v", got, err)
	}
	e.After.Judgements["j"] = store.Judgement{ID: "j", State: "answered", Actor: "carol", Answer: "retry"}
	got, _ = n.ForEvent(e, store.Snapshot{}, "coord")
	if len(got) != 1 || got[0].Subject != "ANSWERED" {
		t.Fatalf("notice=%+v", got)
	}
	e.After.Judgements["j"] = store.Judgement{ID: "j", State: "answered", Actor: "coord", Answer: "retry"}
	got, _ = n.ForEvent(e, store.Snapshot{}, "coord")
	if len(got) != 0 {
		t.Fatal("coordinator's own answer was echoed")
	}
}

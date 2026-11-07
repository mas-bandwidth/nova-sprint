package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type judgeFake struct{}

func (judgeFake) Judge(_ store.Snapshot, id, choice, reason, actor string, expected []string) (card.Plan, *card.Refusal) {
	if choice != "retry" {
		return card.Plan{}, &card.Refusal{Reason: "choice must be retry"}
	}
	return card.Plan{Expect: []store.Expect{store.JudgementOpen{ID: id}}, Writes: []store.Write{store.AnswerJudgement{ID: id, Answer: choice, Actor: actor}}}, nil
}
func TestJudgeClosesOnce(t *testing.T) {
	f := store.NewFake()
	at := time.Unix(1000, 0)
	ctx := context.Background()
	applyTest(t, f, store.Op{ID: "j", Epoch: 1, Actor: "step", Kind: "open", At: at, Writes: []store.Write{store.PutJudgement{ID: "j1", Kind: "red", About: "p", Group: "g", Question: "kind=red card=p group=g CI failed", Command: "nova-sprint judge j1 retry --reason <why>"}}})
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Judges: judgeFake{}, Actor: "rowan", Now: func() time.Time { return at }}
	r := e.Execute(ctx, []string{"judge", "j1", "retry", "--reason", "ready"})
	if r.Exit != 0 {
		t.Fatal(r)
	}
	r = e.Execute(ctx, []string{"judge", "j1", "retry", "--reason", "again"})
	if r.Exit != 1 || r.Output != "SPRINT REFUSED judge: judgement j1 is answered by rowan: nova-sprint judge\n" {
		t.Fatal(r)
	}
	snap, _ := f.Snapshot(ctx)
	if snap.Judgements["j1"].State != "answered" {
		t.Fatal(snap.Judgements)
	}
}

func TestJudgeGroupChecksExactMembers(t *testing.T) {
	s := store.Snapshot{Judgements: map[string]store.Judgement{"j1": {ID: "j1", Group: "g", State: "open"}, "j2": {ID: "j2", Group: "g", State: "open"}}}
	m, why := judgeMembers(s, judgeInput{id: "g", expected: []string{"j2", "j1"}, hasExpect: true})
	if why != "" || len(m) != 2 {
		t.Fatal(m, why)
	}
	_, why = judgeMembers(s, judgeInput{id: "g", expected: []string{"j1"}, hasExpect: true})
	if why != "judgement group membership changed" {
		t.Fatal(why)
	}
}

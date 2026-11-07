package verbs

import (
	"context"
	"errors"
	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
	"strings"
	"testing"
	"time"
)

type policyEvalFake struct{ refusals []string }

func (p policyEvalFake) Check(_ context.Context, _ store.Snapshot, _ string) (PolicyCheck, error) {
	return PolicyCheck{Differences: []string{"width worker"}, Refusals: p.refusals}, nil
}
func (policyEvalFake) Default(context.Context) (string, error) { return "built in", nil }

type policyFilesFake map[string]string

func (p policyFilesFake) ReadPolicy(_ context.Context, path string, _ int64) ([]byte, error) {
	v, ok := p[path]
	if !ok {
		return nil, &OperationError{Problem: "cannot read policy file " + path, Fix: "provide a readable policy file", Exit: 1}
	}
	return []byte(v), nil
}

type policyActivatorFake struct{ close bool }

func (p policyActivatorFake) PlanActivation(_ context.Context, s store.Snapshot, _ int, actor, reason string) (card.Plan, error) {
	var plan card.Plan
	if p.close {
		for _, j := range s.Judgements {
			if j.Kind == "policy" && j.State == "open" {
				plan.Expect = append(plan.Expect, store.JudgementOpen{ID: j.ID})
				plan.Writes = append(plan.Writes, store.AnswerJudgement{ID: j.ID, Answer: reason, Actor: actor})
			}
		}
	}
	return plan, nil
}
func policyFixture(t *testing.T) (*store.Fake, *Engine) {
	t.Helper()
	f := store.NewFake()
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "policy-seed", Epoch: 1, Actor: "rowan", Kind: "seed", At: at, Note: "initial", Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "default", Body: "old"}, store.SetActivePolicy{Version: 1}}})
	e := &Engine{Store: f, Env: fakeEnv{hooked: true}, Actor: "rowan", Now: func() time.Time { return at }, PolicyEvaluator: policyEvalFake{}, PolicyFiles: policyFilesFake{"new.star": "new"}, PolicyActivator: policyActivatorFake{close: true}}
	return f, e
}
func TestRemainingPolicyLifecycle(t *testing.T) {
	ctx := context.Background()
	f, e := policyFixture(t)
	r := e.policy(ctx, []string{"set", "new.star", "--reason", "update"})
	if r.Exit != 0 || !strings.Contains(r.Output, "policy check v2") {
		t.Fatal(r)
	}
	r = e.policy(ctx, []string{"set", "new.star", "--reason", "again"})
	if r.Output != "SPRINT REFUSED policy set: v2 has this body: nova-sprint policy activate 2 --reason <why>\n" {
		t.Fatal(r)
	}
	applyTest(t, f, store.Op{ID: "policy-judge", Epoch: 1, Actor: "rowan", Kind: "judge", At: time.Unix(1000, 0), Writes: []store.Write{store.PutJudgement{ID: "j", Kind: "policy", Group: "policy:v1"}}})
	e.PolicyActivator = policyActivatorFake{}
	if r = e.policy(ctx, []string{"activate", "2", "--reason", "use"}); r.Exit != 2 {
		t.Fatal(r)
	}
	e.PolicyActivator = policyActivatorFake{close: true}
	r = e.policy(ctx, []string{"activate", "2", "--reason", "use"})
	if r.Exit != 0 || !strings.Contains(r.Output, "answers-changed=1") {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Policy != 2 || s.Judgements["j"].State != "answered" {
		t.Fatal(s)
	}
	r = e.policy(ctx, []string{"show", "2"})
	if r.Exit != 0 || !strings.Contains(r.Output, "reason=update") || !strings.Contains(r.Output, "new") {
		t.Fatal(r)
	}
	r = e.policy(ctx, []string{"diff", "1", "2"})
	if r.Exit != 0 || !strings.Contains(r.Output, "-old") || !strings.Contains(r.Output, "+new") {
		t.Fatal(r)
	}
}
func TestRemainingPolicyRefusals(t *testing.T) {
	_, e := policyFixture(t)
	ctx := context.Background()
	cases := []struct {
		args []string
		text string
	}{{[]string{"set", "missing", "--reason", "x"}, "cannot read policy file missing"}, {[]string{"activate", "3", "--reason", "x"}, "unknown version 3"}, {[]string{"activate", "1", "--reason", "x"}, "already active"}, {[]string{"show", "8"}, "unknown version v8"}, {[]string{"check", "v8"}, "unknown version v8"}}
	for _, tc := range cases {
		r := e.policy(ctx, tc.args)
		if r.Exit != 1 || !strings.Contains(r.Output, tc.text) || strings.Count(r.Output, "\n") != 1 {
			t.Fatal(tc.args, r)
		}
	}
	e.PolicyEvaluator = policyEvalFake{refusals: []string{"width invalid"}}
	r := e.policy(ctx, []string{"set", "new.star", "--reason", "x"})
	if r.Exit != 1 || !strings.Contains(r.Output, "width invalid") {
		t.Fatal(r)
	}
	r = e.policy(ctx, []string{"check", "new.star"})
	if r.Exit != 1 || !strings.Contains(r.Output, "DIFF width worker") || !strings.Contains(r.Output, "REFUSED width invalid") {
		t.Fatal(r)
	}
}
func TestRemainingPolicyDataAndPrerequisites(t *testing.T) {
	ctx := context.Background()
	f, e := policyFixture(t)
	at := time.Unix(1000, 0)
	applyTest(t, f, store.Op{ID: "policy-data", Epoch: 1, Actor: "rowan", Kind: "seed", At: at, Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "config", Body: "data"}}})
	r := e.policy(ctx, []string{"activate", "2", "--reason", "x"})
	if r.Output != "SPRINT REFUSED policy activate: v2 is data, not script: nova-sprint policy show --list\n" {
		t.Fatal(r)
	}
	e.PolicyFiles = policyFilesFake{"data.star": "data"}
	r = e.policy(ctx, []string{"set", "data.star", "--reason", "x"})
	if r.Output != "SPRINT REFUSED policy set: v2 has this body: nova-sprint policy show 2\n" {
		t.Fatal(r)
	}
	e.PolicyFiles = policyFilesFake{"new.star": "new"}
	e.Env = fakeEnv{}
	r = e.policy(ctx, []string{"show"})
	if r.Output != "SPRINT REFUSED policy show: rowan is not hooked in: nova-bus hook --as rowan\n" {
		t.Fatal(r)
	}
	e.Env = fakeEnv{hooked: true}
	e.PolicyEvaluator = nil
	r = e.policy(ctx, []string{"set", "new.star", "--reason", "x"})
	if r.Exit != 2 {
		t.Fatal(r)
	}
	s, _ := f.Snapshot(ctx)
	if s.Policy != 1 {
		t.Fatal(s.Policy)
	}
}

type policyEvalRacing struct{ f *store.Fake }

func (p policyEvalRacing) Default(context.Context) (string, error) { return "default", nil }
func (p policyEvalRacing) Check(ctx context.Context, s store.Snapshot, _ string) (PolicyCheck, error) {
	_, err := p.f.Apply(ctx, store.Op{ID: "policy-race", Epoch: s.Epoch, Actor: "test", Kind: "race", At: time.Unix(1000, 0), Writes: []store.Write{store.PutPolicy{Kind: "data", Name: "other", Body: "data"}}})
	return PolicyCheck{}, err
}
func TestRemainingPolicySetFencesEvaluation(t *testing.T) {
	ctx := context.Background()
	f, e := policyFixture(t)
	e.PolicyEvaluator = policyEvalRacing{f}
	r := e.policy(ctx, []string{"set", "new.star", "--reason", "x"})
	if r.Exit != 1 {
		t.Fatal(r)
	}
	if _, err := f.Policy(ctx, 3); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

type policyEvalError struct{}

func (policyEvalError) Default(context.Context) (string, error) {
	return "", errors.New("secret-value")
}
func (policyEvalError) Check(context.Context, store.Snapshot, string) (PolicyCheck, error) {
	return PolicyCheck{}, errors.New("secret-value")
}
func TestRemainingPolicyEvaluatorErrorsAreSanitized(t *testing.T) {
	_, e := policyFixture(t)
	e.PolicyEvaluator = policyEvalError{}
	for _, args := range [][]string{{"set", "new.star", "--reason", "x"}, {"check", "new.star"}, {"diff", "default"}} {
		r := e.policy(context.Background(), args)
		if r.Exit != 2 || strings.Contains(r.Output, "secret-value") {
			t.Fatal(r)
		}
	}
}

type policyGapStore struct {
	*store.Fake
	events []store.Event
}

func (s policyGapStore) Events(context.Context, int64, int) ([]store.Event, error) {
	return s.events, nil
}
func TestRemainingPolicyHistoryRejectsGapAndNoProgress(t *testing.T) {
	for _, events := range [][]store.Event{{{Seq: 2}}, {{Seq: 3}}} {
		st := policyGapStore{Fake: store.NewFake(), events: events}
		if _, err := policyHistory(context.Background(), st, store.Snapshot{Seq: 2}); err == nil {
			t.Fatal("accepted incomplete history")
		}
	}
}

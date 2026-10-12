package decide

import (
	"context"
	"time"
)

// The shadow of the judgment decision (SPEC-NOVA-DECIDE section 13, jev-shadow-judgments.w1):
// Jev is asked the judgment decision for every judgment as it is raised, and the answer is
// recorded beside the coordinator's real one with every answer marked shadow. A shadow
// record is never applied: it has no act, and nothing reads it but ShadowAgreement.

const (
	// ShadowName is the shadow records' decision name; nova-sprint run --decide keeps them in
	// <dir>/judgment-shadow.jsonl.
	ShadowName = "judgment-shadow"
	// ShadowMethod marks each answer of a shadow record.
	ShadowMethod = "shadow"
	// HighP is the probability at and above which agreement is also counted.
	HighP = 0.95
)

// ShadowAsk asks the judgment decision over in through b and appends the answer, marked
// shadow, to record under an id per note, card and state. A judgment already shadowed is
// returned as recorded (existing) and nothing is asked.
func ShadowAsk(ctx context.Context, b Backend, in JudgmentInput, note, record string, at time.Time) (d Decision, existing bool, err error) {
	s, state := JudgmentSchema(), JudgmentState(in)
	s.Name = ShadowName
	id := Op(in.Card+"@shadow."+note, state)
	ds, err := Load(record)
	if err != nil {
		return Decision{}, false, err
	}
	if have := Find(ds, id); have != nil {
		return *have, true, nil
	}
	answers, usage, err := Ask(ctx, b, s, state)
	if err != nil {
		return Decision{}, false, &BackendError{Backend: b.Name(), Err: err}
	}
	for q, a := range answers {
		a.Method = ShadowMethod
		answers[q] = a
	}
	d = Decision{ID: id, Decision: ShadowName, Schema: s.Hash(), Backend: b.Name(), At: at.UTC().Format(time.RFC3339), State: state, Answers: answers, Usage: usage,
		Inputs: map[string]string{"card": in.Card, "note": note, "kind": in.Kind, "verb": answers["verb"].Value, "shadow": "true"}}
	have, err := Append(record, d)
	if have != nil {
		return *have, true, err
	}
	return d, false, err
}

// KindAgreement is how a judgment kind's shadow answers agreed with the real ones: Count
// pairs joined, Agree of them the same verb, Pct that as a percent; HighCount of the pairs
// had the shadow verb at p HighP or above, HighAgree of those agreed, HighPct as a percent.
type KindAgreement struct {
	Kind      string
	Count     int
	Agree     int
	Pct       int
	HighCount int
	HighAgree int
	HighPct   int
}

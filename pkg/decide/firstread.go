package decide

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The first read of a flash card (docs/SPEC-SPRINT.md section 6, the decide read;
// SPEC-NOVA-DECIDE section 8): the read decision asked over the card and its diff,
// then routed by p(defect) against two bars the sprint's configuration holds
// (nova-config's sprint row, decide_bounce and decide_review). At or above the bounce
// bar the read bounces the work; below the review bar it lands the work with no model
// read; between the two the card goes to a strings read, whose verdict is attached to
// the decision as its outcome, so the record trains.

// The routes a first read takes.
const (
	RouteBounce  = "bounce"
	RouteStrings = "strings"
	RouteLand    = "land"
)

// Bars are the two bars on p(defect): Bounce at or above, Review below.
type Bars struct {
	Bounce float64 `json:"bounce"`
	Review float64 `json:"review"`
}

// ParseBars reads the bars as the sprint row holds them, decimals as text. Both are
// probabilities and the review bar is at most the bounce bar; every problem is named.
func ParseBars(bounce, review string) (Bars, error) {
	var b Bars
	var p []string
	for _, f := range []struct {
		name, raw string
		to        *float64
	}{{"decide_bounce", bounce, &b.Bounce}, {"decide_review", review, &b.Review}} {
		v, err := strconv.ParseFloat(strings.TrimSpace(f.raw), 64)
		switch {
		case err != nil:
			p = append(p, fmt.Sprintf("%s %q is not a decimal", f.name, f.raw))
		case v < 0 || v > 1:
			p = append(p, fmt.Sprintf("%s %v is not a probability in [0, 1]", f.name, v))
		default:
			*f.to = v
		}
	}
	if len(p) == 0 && b.Review > b.Bounce {
		p = append(p, fmt.Sprintf("decide_review %v is above decide_bounce %v; the strings read is the band between them", b.Review, b.Bounce))
	}
	if len(p) > 0 {
		return Bars{}, errors.New(strings.Join(p, "; "))
	}
	return b, nil
}

// BackendError is a backend that gave no answer within the schema: nothing was recorded.
type BackendError struct {
	Backend string
	Err     error
}

func (e *BackendError) Error() string { return e.Err.Error() }
func (e *BackendError) Unwrap() error { return e.Err }

// Make asks s over state through b and appends the decision to the record under id. An
// id the record holds over the same decision, schema and state is that decision
// (existing), and nothing is asked; over another it is a ConflictError.
func Make(ctx context.Context, b Backend, s Schema, state, record, id string, inputs map[string]string, at time.Time) (d Decision, existing bool, err error) {
	ds, err := Load(record)
	if err != nil {
		return Decision{}, false, err
	}
	if have := Find(ds, id); have != nil {
		return *have, true, Replays(*have, s, state)
	}
	answers, usage, err := Ask(ctx, b, s, state)
	if err != nil {
		return Decision{}, false, &BackendError{Backend: b.Name(), Err: err}
	}
	d = Decision{ID: id, Decision: s.Name, Schema: s.Hash(), Backend: b.Name(), At: at.UTC().Format(time.RFC3339),
		Inputs: inputs, State: state, Answers: answers, Usage: usage}
	have, err := Append(record, d)
	if have != nil {
		if err != nil {
			return *have, true, err
		}
		return *have, true, Replays(*have, s, state)
	}
	return d, false, err
}

// Replays says the recorded decision is the one an ask of s over state under its id
// would make, so the ask is answered from the record; else a ConflictError.
func Replays(have Decision, s Schema, state string) error {
	if err := replays(have, s.Name, s.Hash(), state); err != nil {
		return err
	}
	if err := s.Check(have.Answers); err != nil {
		return &ConflictError{fmt.Sprintf("the op id %s: %s", have.ID, err.Error())}
	}
	return nil
}

func replays(have Decision, decision, schema, state string) error {
	if have.State != state || have.Decision != decision || have.Schema != schema {
		return &ConflictError{fmt.Sprintf("the op id %s is recorded for another decision, schema or state; an op id names one operation, so choose another", have.ID)}
	}
	return nil
}

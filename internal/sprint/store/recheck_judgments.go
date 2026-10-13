package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// RecheckJudgments reads current evidence in the caller's pinned epoch and
// commits only closures, retaining the ordinary engine's revision fence.
// tla/JudgmentRecheck.tla ClosedOnlyProven.
func (st *Store) RecheckJudgments(ctx context.Context, verb string) (Result, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return Result{Verb: verb}, err
	}
	res, err := pinned.Run(ctx, pinned.recheckJudgmentsStep(verb))
	err = recheckError(res, err)
	if err != nil {
		res.Said = nil
	}
	return res, err
}

func (st *Store) recheckJudgmentsStep(verb string) Step {
	epoch := st.epoch
	return Step{Verb: verb, Load: All, Extras: tickExtras, Epoch: &epoch,
		Routes: true, Readers: true, Friends: true,
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RecheckJudgments(s, st.Actor) }}
}

// A successful machine transition is not proof its closure transaction committed.
func recheckError(res Result, err error) error {
	if err != nil {
		return err
	}
	if len(res.Refused) > 0 {
		var reasons []string
		for _, refusal := range res.Refused {
			reasons = append(reasons, refusal.Why)
		}
		return fmt.Errorf("judgment recheck refused: %s", strings.Join(reasons, "; "))
	}
	if res.Lost || res.Pending != "" {
		return fmt.Errorf("judgment recheck did not finish; run the command again")
	}
	return nil
}

package sprint

import (
	"context"
	"os"
	"slices"
)

// LandCycleSync is the land round's dev sync (docs/SPEC-SPRINT.md, "Dev sync every
// cycle"): when one is due on the snapshot the round read (DevSyncDue), it merges the
// development branch into the base in the round's clone (RunDevSync), and due is true. The
// round then records the facts in one step whose plan is DevSynced(s, facts, req.Streams)
// on that step's own snapshot, before it lands any batch: a conflict stops every stream, so
// the batches after it are refused as any stopped stream's are. Not due: no git runs.
func LandCycleSync(ctx context.Context, s *Snapshot, req DevSyncReq) (facts DevSyncFacts, due bool, err error) {
	if due, _ := DevSyncDue(s); !due {
		return DevSyncFacts{}, false, nil
	}
	req.Env = noDetachEnv(req.Env)
	facts, err = RunDevSync(ctx, req)
	return facts, true, err
}

func noDetachEnv(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	return append(slices.Clone(env),
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=gc.autoDetach",
		"GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=maintenance.autoDetach",
		"GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=gc.auto",
		"GIT_CONFIG_VALUE_2=0",
	)
}

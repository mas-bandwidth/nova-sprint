package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runPong is `pong`, a kin session's answer to the run's challenge (v1's
// nova-hook prove, brought back): one op of kind pong by the row, its id
// fixed by the epoch, the row and the nonce (steps.PongOpID), writing
// nothing. The run counts it only for a nonce it issued to this row; a
// repeat applies once.
func runPong(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	nonce := in.str("nonce")
	if nonce == "" || len(nonce) > 64 || strings.Trim(nonce, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return "", misuse("pong", "--nonce takes the nonce of the CHALLENGE line, as printed")
	}
	op := store.Op{ID: steps.PongOpID(snap.Epoch, in.actor, nonce), Epoch: snap.Epoch, Actor: in.actor, Kind: "pong", At: e.Now()}
	r, err := e.commit(ctx, op, "nova-sprint pong --nonce "+nonce+" --as "+in.actor+" again")
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("PONG %s nonce=%s at seq %d", in.actor, nonce, r.Seq)), nil
}

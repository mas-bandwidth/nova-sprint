package verbs

import (
	"context"
	"fmt"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// unseenFix is the UNSEEN line's fix: the coordinator's pushes reach a bus
// hook that wakes no one (a background shell writing to a file, a harness
// watch that expired), so its judgements go unanswered (Glenn 2026-11-08:
// 60 judgements sat over an hour behind such a hook and nothing landed).
const unseenFix = "your pushes are not waking you: run your bus hook under your harness's wake tool (nova-sprint help playbook coordinator)"

// unseenOf is the UNSEEN report of snap at now: how many open judgements
// with a deadline have waited longer than timeouts judgement_unseen, and
// the oldest's age; "" when none has. A judgement with no deadline (a held
// card, a land judgement, the no-policy gate: steps' put) sits until
// answered by design, so its age says nothing about the coordinator's wake
// and it never counts; judge, next and check still list it. A read-only
// report: it moves nothing.
func unseenOf(snap store.Snapshot, now time.Time, limit int) string {
	if limit <= 0 {
		limit = policy.DefaultJudgementUnseen
	}
	n, oldest := 0, now
	for _, j := range snap.Judgements {
		if j.State != "open" || j.Deadline.IsZero() || now.Sub(j.OpenedAt) <= time.Duration(limit)*time.Second {
			continue
		}
		n++
		if j.OpenedAt.Before(oldest) {
			oldest = j.OpenedAt
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("UNSEEN %d judgements open, oldest %s", n, now.Sub(oldest).Round(time.Second))
}

// unseen is the UNSEEN report for actor, read fresh from the store: only
// the coordinator answers judgements, so only it is told ("" actor is
// anyone: check's reader). A store or policy that cannot be read says
// nothing here: the verb that ran already said what it could not read, and
// check reds it on its own line.
func (e *Engine) unseen(ctx context.Context, actor string) string {
	snap, err := e.read(ctx, store.Scope{OpenJudgements: true}) // unseenOf counts only the open ones
	if err != nil || (actor != "" && snap.Rows[actor].Role != "coordinator") {
		return ""
	}
	limit := policy.DefaultJudgementUnseen
	if a, err := e.answers(ctx, snap); err == nil && a.Timeouts.JudgementUnseen > 0 {
		limit = a.Timeouts.JudgementUnseen
	}
	return unseenOf(snap, e.Now(), limit)
}

// withUnseen is out with the UNSEEN line first, for the coordinator; JSON
// output is left whole (a program parses it).
func (e *Engine) withUnseen(ctx context.Context, in *input, out string) string {
	if in.actor == "" || in.has("json") {
		return out
	}
	if line := e.unseen(ctx, in.actor); line != "" {
		return line + ": " + unseenFix + "\n" + out
	}
	return out
}

package main

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The verb result printer's refusals. The default text output promises a
// refusal with its reason and its remedy: `<TOKEN> REFUSED: <why>; run:
// <remedy>` (docs/STANDARD.md section 3, point 1; docs/CLI.md: "refusals with
// reasons (REFUSED, on stderr)"), and the banner says it of every verb: "Each
// verb prints what moved (MOVED), what did not and why (REFUSED, on stderr)".
// The dogfood of 2026-10-10 ran brief and return on a working card and read
// only `BRIEF FAILED moved=0 refused=1` and `RETURN FAILED moved=0 refused=1`:
// the reason (a working card keeps its brief; not merging) and the remedy were
// in --json's refused array alone. The printer now owes every refused id its
// why and, when the step's reason names no remedy, the remedy: the moves that
// change the card, by the lifecycle (docs/SPEC-SPRINT.md section 3, the moves
// table; internal/sprint/lifecycle.go Moves, mirrored by tla/SprintTables.tla)
// -- the same answer sprint.briefStarted names for a brief refused on a working
// card and sprint.reworkWhy for a rework refused on one.

// refusalLines is a step's refusals as the printer prints them, one line a
// refused id: `REFUSED <key>: <why>`, with `; run: <remedy>` appended when the
// why names no remedy of its own and the key is a card on the work table. A
// refusal whose why already carries "; run: " (brief's, rework's, the store's
// fences, refuse's own) prints as the step said it, and a step whose reasons
// all name their remedies reads nothing: the work table is read once, only for
// a refusal that would otherwise print bare. A read that fails remedies
// nothing: the reasons stand.
func refusalLines(ctx context.Context, st *store.Store, refused []sprint.Refusal) []string {
	lines := make([]string, len(refused))
	need := false
	for i, r := range refused {
		lines[i] = r.Key + ": " + r.Why
		need = need || !strings.Contains(r.Why, "; run: ")
	}
	if !need || st == nil {
		return lines
	}
	s, err := st.Load(ctx, []string{sprint.Work}, nil)
	if err != nil {
		return lines
	}
	for i, r := range refused {
		if strings.Contains(r.Why, "; run: ") {
			continue
		}
		if remedy := cardRemedy(s.Work.Placed(r.Key)); remedy != "" {
			lines[i] += "; run: " + remedy
		}
	}
	return lines
}

// cardRemedy is the remedy of a refusal of the card c whose reason names none:
// what changes the card from the state it is in, by the lifecycle's moves
// (docs/SPEC-SPRINT.md section 3; internal/sprint/lifecycle.go Moves), in the
// commands the verbs take. "" for a key that is no placed card -- a refusal
// that names no card ("no such card", a whole-call refusal of the store) stands
// on its reason -- and for a sentinel, whose refusal says release.
func cardRemedy(c *sprint.Card) string {
	if c == nil || !c.Placed() || sprint.IsSentinel(c) {
		return ""
	}
	switch c.Col {
	case sprint.Waiting, sprint.Ready:
		return "nova-sprint brief " + c.ID + " --brief-file <path> (change its task), or nova-sprint drop " + c.ID + " --reason '<why>'"
	case sprint.Working:
		return "nova-sprint card " + c.ID + " (its attempt is running: once it finishes (review), accept, rework or drop it), or now: nova-sprint drop " + c.ID + " --reason '<why>'"
	case sprint.Review:
		return "nova-sprint card " + c.ID + ", then accept, rework or drop it"
	case sprint.Merging:
		return "nova-sprint return " + c.ID + " --reason '<why>' (back to review), or nova-sprint card " + c.ID + " for what holds it"
	case sprint.Landed:
		return "nova-sprint add --stream " + c.Row + " <new id> --brief-file <path> (it landed: a change is a new card)"
	}
	return ""
}

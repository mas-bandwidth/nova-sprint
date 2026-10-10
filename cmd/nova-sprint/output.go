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
// why and, when the step's reason names no remedy of its own and is about the
// card's state, the remedy: the moves that change the card, by the lifecycle
// (docs/SPEC-SPRINT.md section 3, the moves table; internal/sprint/lifecycle.go
// Moves, mirrored by tla/SprintTables.tla) -- the same answer sprint.briefStarted
// names for a brief refused on a working card and sprint.reworkWhy for a rework
// refused on one. A refusal that is not about the card's state stands on its
// reason: the seat cold read of 2026-10-10 found a read of a primary (`read
// --ok <primary>`) refused "no such card on the table" with the work table's
// remedy appended, advice for the wrong verb -- a read acts on read cards, not
// on the work table (docs/SPEC-SPRINT.md, "Read card": one reader's read of one
// primary at one attempt, identity <primary>.r<attempt>.<reader>), and its why
// names the read card to call.

// refusalLines is a step's refusals as the printer prints them, one line a
// refused id: `REFUSED <key>: <why>`, with `; run: <remedy>` appended when the
// why names no remedy of its own and the refusal is about the card's state
// (takesRemedy). A refusal whose why already carries "; run: " (brief's,
// rework's, the store's fences, refuse's own) prints as the step said it, and a
// step whose reasons all name their remedies, or are not about a state, reads
// nothing: the work table is read once, only for a refusal that would otherwise
// print bare. A read that fails remedies nothing: the reasons stand.
func refusalLines(ctx context.Context, st *store.Store, refused []sprint.Refusal) []string {
	lines := make([]string, len(refused))
	need := false
	for i, r := range refused {
		lines[i] = r.Key + ": " + r.Why
		need = need || takesRemedy(r.Why)
	}
	if !need || st == nil {
		return lines
	}
	s, err := st.Load(ctx, []string{sprint.Work}, nil)
	if err != nil {
		return lines
	}
	for i, r := range refused {
		if !takesRemedy(r.Why) {
			continue
		}
		if remedy := cardRemedy(s.Work.Placed(r.Key)); remedy != "" {
			lines[i] += "; run: " + remedy
		}
	}
	return lines
}

// takesRemedy says whether a refusal's why takes the card's remedy appended
// (refusalLines's): the refusal is about the card's state, and its why names no
// remedy of its own. A why that carries "; run: " prints as the step said it
// (brief's, rework's, the store's fences, refuse's own). A why that is not
// about the card's state stands on its reason, the state remedying nothing
// there: "no such card on the table" (select.go noSuchCard) says the card is
// not on the table the verb acts on -- a read's as much as any verb's, a read
// acting on read cards, not on the work table its key is placed on (docs/
// SPEC-SPRINT.md "Read card") -- and "a read names its read card" is that
// refusal's remedy, already said: name <primary>.r<attempt>.<reader>.
func takesRemedy(why string) bool {
	return !strings.Contains(why, "; run: ") &&
		!strings.Contains(why, "no such card") &&
		!strings.Contains(why, "a read names its read card")
}

// cardRemedy is the remedy of a refusal of the card c whose reason names none:
// what changes the card from the state it is in, by the lifecycle's moves
// (docs/SPEC-SPRINT.md section 3; internal/sprint/lifecycle.go Moves), in the
// commands the verbs take. Its caller asks it only of a refusal about the
// card's state (takesRemedy). "" for a key that is no placed card -- a refusal
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

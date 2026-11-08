package steps

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// What the deal carries and when the tick deals (defects 53, 54, 57).
//
// The model: every consumer is cut with the model its deal names, and the
// take receipt reads it from the consumer, never inventing one. A friend's
// row runs its own model (nova-config's friends.model); a bench's is the
// policy's answer for the card's tier (the catalog's bench key, in the
// provider/model form a harness takes: Answers.Models). A row the deal
// cannot name a model for is not dealt to; when no serving row can be, the
// card stays and one no-model judgement per tier says why.
//
// The gate: layer 6 sets Answers.NoDeal when no policy is active, or the
// active one failed and another version answered, and then nothing is dealt
// (no deal, no read, no preemption) and one blocking no-policy judgement on
// the coordinator's row says why; everything else in the tick still runs.

// model is the model row runs c on, "" when none can be named.
func (t *tick) model(c store.Card, row string) string {
	if t.kind(row) == roster.KindKin {
		return t.in.Roster.Kin[row].Model
	}
	return t.o.a.Models[c.Tier]
}

// named keeps the rows of rows that c can be dealt to with a model.
func (t *tick) named(c store.Card, rows []string) []string {
	var out []string
	for _, r := range rows {
		if t.model(c, r) != "" {
			out = append(out, r)
		}
	}
	return out
}

// noModel opens the no-model judgement for c's tier: serving rows exist,
// none can be named a model.
func (t *tick) noModel(c store.Card, rows []string) {
	var bench, kin []string
	for _, r := range rows {
		if t.kind(r) == roster.KindKin {
			kin = append(kin, r)
		} else {
			bench = append(bench, r)
		}
	}
	var why []string
	if len(bench) > 0 {
		why = append(why, fmt.Sprintf("policy v%d names no bench model for tier %s (bench rows %s; give its catalog's bench a %s model: nova-sprint policy set <file> --reason <why>)",
			t.o.a.Version, c.Tier, strings.Join(bench, " "), c.Tier))
	}
	if len(kin) > 0 {
		why = append(why, "friend rows "+strings.Join(kin, " ")+" have no model in nova-config (nova-config friend set <name> --model <provider/model>)")
	}
	t.judge("no-model", c.Tier, "ready "+c.ID+" is not dealt: no open row serving tier "+c.Tier+" can be named a model: "+strings.Join(why, "; "))
}

// modelServed says whether an open row serving tier can be named a model.
func (t *tick) modelServed(tier string) bool {
	c := store.Card{Tier: tier}
	return len(t.named(c, t.serving(c))) > 0
}

// gated says whether the tick deals nothing (Answers.NoDeal), opening the
// no-policy judgement on the coordinator's row when it does.
func (t *tick) gated() bool {
	if t.o.a.NoDeal == "" {
		return false
	}
	t.judge("no-policy", t.coordinatorRow(), "the run deals nothing: "+t.o.a.NoDeal)
	return true
}

// coordinatorRow is the first row hooked in as coordinator, by name;
// "sprint" when none is.
func (t *tick) coordinatorRow() string {
	for _, n := range t.rowNames {
		if r := t.v.Rows[n]; Hooked(r) && r.Role == "coordinator" {
			return n
		}
	}
	return "sprint"
}

package sprint

import (
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/swarm"
)

// A card that writes a new TLA+ model is tiered heavy (the owner, 2026-10-17: "I think
// models at heavy is fine, and touching models can be pro now", over 2026-10-04's frontier;
// docs/SPEC-SPRINT.md, the card decides its model). A new model is read off the card's
// NEW line, never its prose: a .tla module (swarm.CardNewModels, the lint's
// new-model-is-heavy-or-above rule). Editing what tla/ holds (the models, .cfg files, the
// TLC run records) is any tier.

// ModelTier is the brief add admits for a card whose NEW names a new model
// (swarm.CardNewModels): with tier: heavy on its line 1 when it names no tier, and said
// the words the add's unit carries; the brief as given, said "", when it names heavy or
// frontier or writes no new model; why the refusal when line 1 names a lower tier. A
// brief whose model lines do not read is the model-lines check's to refuse, and is given
// back as it is.
func ModelTier(brief string) (out, said, why string) {
	model := swarm.CardNewModels([]byte(brief))
	if len(model) == 0 {
		return brief, "", ""
	}
	m, bad := cardhdr.ReadModel(brief)
	work := "NEW names a new TLA+ model (" + strings.Join(model, ",") + ")"
	switch {
	case bad != "" || m.Tier == cardhdr.RouteFrontier || m.Tier == cardhdr.RouteHeavy:
		return brief, "", ""
	case m.Tier != "":
		return brief, "", work + "; a card that writes a model is tiered heavy or above (docs/SPEC-SPRINT.md, the card decides its model), and line 1 names tier " + m.Tier +
			": write tier: heavy on line 1 (or frontier), or give the new model to its own card (editing the models, .cfg files and run records tla/ holds is any tier)"
	}
	first, rest, nl := strings.Cut(brief, "\n")
	out = strings.TrimRight(first, " \t") + " tier: " + cardhdr.RouteHeavy
	if nl {
		out += "\n" + rest
	}
	return out, "tiered heavy: " + work, ""
}

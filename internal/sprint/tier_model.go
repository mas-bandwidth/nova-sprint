package sprint

import (
	"path"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A card that writes a new TLA+ model is tiered heavy (the owner, 2026-10-17: "I think
// models at heavy is fine, and touching models can be pro now", over 2026-10-04's frontier;
// docs/SPEC-SPRINT.md, the card decides its model). A new model is read off the card's
// NEW line, never its prose: a .tla module (swarm.CardNewModels, the lint's
// new-model-is-heavy-or-above rule). Editing what tla/ holds (the models, .cfg files, the
// TLC run records) is any tier.

// modelProbes are the file names a PATHS entry's last part is matched against: a module
// and a model-checking config.
var modelProbes = []string{"Model.tla", "MCModel.cfg"}

// ModelPaths is the entries of paths that can name a model: one whose last part is a .tla
// module or a glob of them (*.tla), anywhere; one under a tla/ directory whose last part
// is a config or matches a module or a config (*, **, MC*.cfg); and the tla/ directory
// itself. A bare glob outside tla/ (security/**) names no model.
func ModelPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimPrefix(strings.TrimSpace(p), "./")
		dirs := strings.Split(strings.TrimSuffix(p, "/"), "/")
		base := dirs[len(dirs)-1]
		inTLA := slices.Contains(dirs[:len(dirs)-1], "tla")
		module, _ := path.Match(base, modelProbes[0])
		config, _ := path.Match(base, modelProbes[1])
		if base == "tla" || strings.HasSuffix(base, ".tla") || inTLA && (module || config || strings.HasSuffix(base, ".cfg")) {
			out = append(out, p)
		}
	}
	return out
}

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

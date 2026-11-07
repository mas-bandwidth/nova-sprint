package verbs

import (
	"context"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// RosterSource reads one authoritative nova-config roster snapshot.
type RosterSource interface {
	ReadRoster(context.Context) (roster.Roster, error)
}

func (e *Engine) takeRoster(ctx context.Context, snap store.Snapshot, c store.Card, actor string) (int, []string, Result, bool) {
	if e.Roster == nil {
		return 0, nil, unavailable("take"), false
	}
	roster, err := e.Roster.ReadRoster(ctx)
	if err != nil {
		return 0, nil, operationalFailure("take", err), false
	}
	_, width, tiers, ok := roster.Row(actor)
	if !ok {
		return 0, nil, refused("take", "row "+actor+" is not in the roster", "set it in nova-config"), false
	}
	if !slices.Contains(tiers, c.Tier) {
		return 0, nil, refused("take", "row "+actor+" does not serve tier "+c.Tier, "set it in nova-config"), false
	}

	working := 0
	for _, item := range snap.Cards {
		if item.Holder == actor && item.State == "working" && (item.Kind == "work" || item.Kind == "read") {
			working++
		}
	}
	if working >= width {
		return 0, nil, refused("take", fmt.Sprintf("row %s is at width %d", actor, width), "finish a card first"), false
	}
	return width, tiers, Result{}, true
}

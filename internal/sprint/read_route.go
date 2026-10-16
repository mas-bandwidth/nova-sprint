package sprint

import (
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
)

// A read needs a reader, not a route (docs/SPEC-SPRINT.md section 6; the card
// pr-wire-server-restart, 2026-10-05, could not be read: the fleet's table held flash
// and pro routes only, and the friends and buds that read heavy were up). The owner
// chose this over a heavy fleet route, which would let the fleet be dealt heavy cards
// it cannot run. A fleet reader runs the route its read draws, so it serves a tier
// only while an enabled route of the tier is in the tier's array (or the store holds
// no route, where every reader runs its own). A friend's or a bud's reader,
// reader-<name> beaten by her daemon, brings her own harness model by tier: it serves a
// tier its tiers cell names that her seat's tiers reach, route or none, and its verdict's
// usage line says the model and the harness its read ran on (readUsageFields).

// ownModelReader says the reader brings its own model: it is a friend's reader,
// reader-<name> for a friend (or a bud, a friend on a Claude account) whose seat the
// snapshot holds or whose friend row the fleet table has (nova-tools pkg/friend ReaderOf).
// Any other reader is the fleet's and runs its read's route. A held friend's reader
// brings no model: a friend and her reader row are one member to the server, and a
// held member serves nothing (docs/SPEC-SPRINT.md section 11, hold).
func (s *Snapshot) ownModelReader(reader string) bool {
	name, ok := strings.CutPrefix(reader, ReaderPrefix)
	if !ok || name == "" {
		return false
	}
	if s.heldFriendReader(reader) {
		return false
	}
	if slices.ContainsFunc(s.Friends, func(f FriendSeat) bool { return f.Name == name }) {
		return true
	}
	return s.Fleet != nil && s.Fleet.HasRow(FriendRow(name))
}

// heldFriendReader says reader is a held friend's reader row: reader-<name> names a
// friend the snapshot holds with status held (her seat, FriendStates: the coordinator's
// hold is the one held). A held friend and her reader row are one
// member to the server, and a held member serves nothing, new reads and its route
// judgment alike (docs/SPEC-SPRINT.md section 11, hold).
func (s *Snapshot) heldFriendReader(reader string) bool {
	name, ok := strings.CutPrefix(reader, ReaderPrefix)
	if !ok || name == "" {
		return false
	}
	for _, f := range s.Friends {
		if f.Name == name {
			return f.Held || f.Status == Held
		}
	}
	return false
}

// tierRouted says a fleet reader can draw a read of tier: an enabled route of the tier is
// in its array, or the store holds no route at all (its reader runs its own --model). The
// second is the store with no routes the help documents (a twin, the selftest install
// gate's mem twin, a one-machine try): it is live, so it stays until the owner rules on it;
// in a store with routes a tier with none is never served.
func (s *Snapshot) tierRouted(tier string) bool {
	if len(s.Routes) == 0 {
		return true
	}
	for _, name := range s.tierArray(tier) {
		for _, r := range s.Routes {
			if r.Name == name && r.Tier == tier && r.Enabled {
				return true
			}
		}
	}
	return false
}

// readerServesTier says the reader may be asked a read of tier. Tier is respected (the
// owner, 2026-10-16: "Tier must be respected. If no configured tier exists then that is a
// configuration issue, not something to work around."): a read runs on a model configured
// for its tier, never on none and never on another tier's.
//
//   - A held friend's reader serves nothing (docs/SPEC-SPRINT.md section 11, hold).
//   - A friend's reader (ownModelReader) runs her own harness model by tier, as her row
//     does, and draws no fleet route: it serves a tier its tiers cell names (an empty
//     cell names every tier) that her seat's tiers reach (friendAtOrAbove, her row's own
//     read rule), so a flash friend's reader is never asked a heavy read.
//   - A fleet reader runs the route its read draws, so it serves a tier only while an
//     enabled route of the tier is in its array (tierRouted), whatever its tiers cell
//     names: the cell narrows what it reads (reader set --tiers), it is never a route.
//     Dealt on a tier no route serves, a read carried no route, model or deadline and its
//     member refused it at launch (2026-10-16, frontier reads re-asked of the same
//     readers); f4a0d6a1's "regardless of routes" is reversed.
//
// A read whose tier no reader serves waits under its tier's one judgment (NNoRoute,
// readRouteMissing), closed once a route of the tier is enabled.
//
// tla/ReaderTiers.tla Reads, NoReadOnUnroutedTier
func (s *Snapshot) readerServesTier(reader, tier string) bool {
	if s.heldFriendReader(reader) {
		return false
	}
	if s.ownModelReader(reader) {
		if !s.readerReadsTier(reader, tier) {
			return false
		}
		if f, ok := s.friendSeatOf(reader); ok {
			return friendAtOrAbove(s, f, tier)
		}
		return true
	}
	if !s.tierRouted(tier) {
		return false
	}
	stored := strings.TrimSpace(s.readerTiersStored(reader))
	return stored == "" || slices.Contains(Split(stored), tier)
}

// friendSeatOf is the seat of the friend whose reader row reader is (reader-<name>), when
// the snapshot holds it.
func (s *Snapshot) friendSeatOf(reader string) (FriendSeat, bool) {
	name, ok := strings.CutPrefix(reader, ReaderPrefix)
	if !ok || name == "" {
		return FriendSeat{}, false
	}
	for _, f := range s.Friends {
		if f.Name == name {
			return f, true
		}
	}
	return FriendSeat{}, false
}

// ownModelReaderUp says a reader up that brings its own model serves tier: the reads of
// the tier need no route.
func (s *Snapshot) ownModelReaderUp(tier string) bool {
	if s.Readers == nil {
		return false
	}
	for _, rd := range s.Readers.Rows() {
		if s.ReaderIsUp(rd) && s.ownModelReader(rd) && s.readerServesTier(rd, tier) {
			return true
		}
	}
	return false
}

// readUsageFields is what a verdict on the read card c records of the run beside its
// usage when the read drew no route: the model (provider/model) and the harness its
// reader's usage line names (model=, harness=), each only where the card names none.
// A routed read keeps its route's.
func readUsageFields(c *Card, usage string) map[string]string {
	out := map[string]string{}
	if c.F(FieldRoute) != "" || strings.TrimSpace(usage) == "" {
		return out
	}
	u := cardcost.ParseUsage(usage)
	if m := u.Model; m != "" && m != "-" && c.F(FieldModel) == "" {
		out[FieldModel] = m
	}
	for _, w := range strings.Fields(usage) {
		if h, ok := strings.CutPrefix(w, "harness="); ok && h != "" && h != "-" && c.F(FieldHarness) == "" {
			out[FieldHarness] = h
		}
	}
	return out
}

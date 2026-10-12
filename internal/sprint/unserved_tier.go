package sprint

import (
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// docs/SPEC-SPRINT.md section 5, "An unserved tier falls to the strongest served tier".
//
// The setting nova-sprint set --unserved-tier-falls-to (PropUnservedTierFallsTo) is the
// coordinator's one replacement for the hand pinning the seat did on 2026-10-12: with only
// a frontier friend held, no route and no up friend served tier frontier, and the frontier
// cards sat in ready while the seat pinned every one of them by hand.
//
// While the setting names a tier (heavy, pro or flash; off, the default, is today's
// behaviour), the tick's part below walks every unlanded primary not pinned by the seat and
// whose brief tier (cardhdr.Model.Tier, flash when the brief names none) no route serves and
// no up friend serves: the one tier check is routeTier's (tierServed, read through
// tierHasServer). It records a fallback tier on the card, a field of its own
// (FieldFallbackTier, never the brief's line 1 and never a pin the seat made): the
// strongest tier at or below the setting that is served. The deal and DealtTier read the
// fallback tier, so the card runs where something can take it, with a happened note naming
// the unserved tier and why.
//
// When a server for the brief tier comes up (a route or an up friend serves it), the next
// tick clears the fallback on every card not yet taken: a ready card's field is unset, a
// dealt-but-untaken card's work card is withdrawn and the primary returns to ready, so the
// deal gives it its own tier again. A taken card keeps its run.

// PartUnservedFallback is the tick's fallback part, before the deal (steps_tick.go).
const PartUnservedFallback = "unserved fallback"

// NUnservedFallback is the happened note the fallback part writes: the brief tier is
// unserved, so the card falls to the strongest served tier at or below the setting.
const NUnservedFallback = "an unserved tier falls to the strongest served tier"

// fallbackTier is the fallback tier the deal of c draws from (FieldFallbackTier), "" when
// none is set or the seat pinned the card's tier or model (a seat pin is never changed).
func fallbackTier(c *Card, m cardhdr.Model) string {
	if c == nil || m.Pin != "" || c.F(FieldTier) != "" {
		return ""
	}
	return c.F(FieldFallbackTier)
}

// briefTier is the tier c's brief's line 1 names, flash when it names none: the card's own
// tier, what the fallback part checks for a server.
func cardBriefTier(c *Card, m cardhdr.Model) string {
	if m.Tier == "" {
		return cardhdr.RouteFlash
	}
	return m.Tier
}

// CardTierShown is the tier the card and where views print for the primary c: its own brief
// tier with "(falls to <t>)" when the sprint gave it a fallback tier, else the tier it is on
// (docs/SPEC-SPRINT.md section 5, "An unserved tier falls to the strongest served tier").
func CardTierShown(c *Card) string {
	if c == nil {
		return ""
	}
	m, _ := modelOf(c)
	if fb := fallbackTier(c, m); fb != "" {
		return fmt.Sprintf("%s (falls to %s)", cardBriefTier(c, m), fb)
	}
	now, _ := CardTiers(c)
	return now
}

// tierHasServer says the tier is served: an enabled route of it is in its array, or the
// store holds no route at all (the member runs its own model), or a friend up serves it
// (tierServed, the one tier check).
func (s *Snapshot) tierHasServer(tier string) bool {
	if s.tierRouted(tier) {
		return true
	}
	up, _ := s.tierServed(tier, nil)
	return len(up) > 0
}

// strongestServedAtOrBelow is the strongest tier at or below the setting that is served, ""
// when none of them is: the fallback tier an unserved card is given.
func (s *Snapshot) strongestServedAtOrBelow(setting string) string {
	i := slices.Index(tierLadder, setting)
	for j := i; j >= 0; j-- {
		if s.tierHasServer(tierLadder[j]) {
			return tierLadder[j]
		}
	}
	return ""
}

// fallbackUntaken says the primary c is not yet taken: it holds no work card, or its work
// card is not in a member's Working column (a taken card keeps its run).
func (s *Snapshot) fallbackUntaken(c *Card) bool {
	wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt")))
	return wc == nil || wc.Col != Working
}

// TickUnservedFallback is the tick's part (steps_tick.go, before the deal): it sets the
// fallback tier on every ready primary whose brief tier no route and no up friend serves,
// and clears it on every card not yet taken whose brief tier is served again
// (unserved_tier.go; docs/SPEC-SPRINT.md section 5).
func TickUnservedFallback(s *Snapshot, r TickReq) (Plan, int) {
	setting := s.UnservedTierFallsTo()
	if setting == "" {
		return Plan{}, 0
	}
	if s.Friends == nil && len(r.Friends) > 0 {
		n := *s
		n.Friends = r.Friends
		s = &n
	}
	var p Plan
	p.on(s)
	// a card the seat pinned (tier --set, brief --tier) or whose brief pins a model is
	// never changed
	for _, c := range s.Work.Column(Ready) {
		if IsSentinel(c) {
			continue
		}
		m, bad := modelOf(c)
		if bad != "" || m.Pin != "" || c.F(FieldTier) != "" {
			continue
		}
		brief := cardBriefTier(c, m)
		if s.tierHasServer(brief) {
			continue
		}
		if c.F(FieldFallbackTier) != "" {
			continue
		}
		fb := s.strongestServedAtOrBelow(setting)
		if fb == "" {
			continue
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row,
			Changes: []Change{change(Work, setEntry(c, map[string]string{FieldFallbackTier: fb}))},
			Moved:   fmt.Sprintf("%s tier %s is unserved: falls to %s", c.ID, brief, fb)})
		n := happened(NUnservedFallback, c.Row, s.Now, c.ID)
		n.Who = r.who()
		n.What = fmt.Sprintf("%s tier %s is unserved (no route and no up friend serves it): falls to %s, the strongest served tier at or below %s", c.ID, brief, fb, setting)
		p.Notes = append(p.Notes, n)
	}
	// the clear: a card not yet taken whose brief tier is served again goes back to it
	seen := map[string]bool{}
	for _, col := range []string{Ready, Working} {
		for _, c := range s.Work.Column(col) {
			if seen[c.ID] || IsSentinel(c) {
				continue
			}
			seen[c.ID] = true
			if c.F(FieldFallbackTier) == "" {
				continue
			}
			m, bad := modelOf(c)
			if bad != "" || m.Pin != "" || c.F(FieldTier) != "" {
				continue
			}
			if !s.tierHasServer(cardBriefTier(c, m)) || !s.fallbackUntaken(c) {
				continue
			}
			if wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt"))); wc != nil && wc.Col != Working {
				u := withdrawUnit(s, wc, nil, []string{FieldTier}, NUnservedFallback, r.who(), fmt.Sprintf("%s tier %s is served again: its fallback %s is cleared", c.ID, cardBriefTier(c, m), c.F(FieldFallbackTier)))
				u.Changes[len(u.Changes)-1] = change(Work, moveEntry(c, c.Row, Ready, nil, "work", FieldFallbackTier, FieldTierNow))
				p.Units = append(p.Units, u)
				continue
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row,
				Changes: []Change{change(Work, setEntry(c, nil, FieldFallbackTier, FieldTierNow))},
				Moved:   fmt.Sprintf("%s tier %s is served again: its fallback is cleared", c.ID, cardBriefTier(c, m))})
		}
	}
	return p, 0
}

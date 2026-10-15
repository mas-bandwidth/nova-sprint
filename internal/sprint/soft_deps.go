package sprint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/swarm"
)

const (
	// FieldLandAfter is the comma-separated soft dependencies (LAND-AFTER:) of a primary.
	FieldLandAfter = "land_after"
	// FieldLandAfterSet marks that soft_deps was explicitly set or cleared on the record.
	FieldLandAfterSet = "land_after_set"
)

// BriefLandAfterRaw reads the LAND-AFTER: line of a brief's typed header block
// without deduplicating, so duplicate entries can be diagnosed at admission.
func BriefLandAfterRaw(brief string) []string {
	v, ok := swarm.CardHeaderValue([]byte(brief), "LAND-AFTER")
	if !ok {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		part, _, _ = strings.Cut(part, "(")
		id := strings.TrimSpace(part)
		if id == "" || id == "-" || strings.EqualFold(id, "none") || strings.Contains(id, "#") {
			continue
		}
		out = append(out, id)
	}
	return out
}

// BriefLandAfter reads the LAND-AFTER: line of a brief's typed header block:
// comma-separated card IDs, ignoring "-" or "none" or issue references (#).
func BriefLandAfter(brief string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range BriefLandAfterRaw(brief) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// HasBriefLandAfter reports whether any card in rs declares a LAND-AFTER: line.
func HasBriefLandAfter(rs []AddReq) bool {
	for _, r := range rs {
		if len(BriefLandAfterRaw(r.Brief)) > 0 {
			return true
		}
		for _, cd := range r.Cards {
			if len(BriefLandAfterRaw(cd.Brief)) > 0 {
				return true
			}
		}
	}
	return false
}

// AddSoftDepsExtras returns the IDs named in LAND-AFTER: across rs so store.Step.Extras
// loads any off-table records for refusal reporting.
func AddSoftDepsExtras(rs []AddReq) []string {
	var out []string
	for _, r := range rs {
		out = append(out, BriefLandAfter(r.Brief)...)
		for _, cd := range r.Cards {
			out = append(out, BriefLandAfter(cd.Brief)...)
		}
	}
	return out
}

// SoftDepsRaw returns the stored soft dependencies of c without table resolution.
func SoftDepsRaw(c *Card) []string {
	if c == nil {
		return nil
	}
	if c.F(FieldLandAfterSet) == "true" {
		return Split(c.F(FieldLandAfter))
	}
	if v := c.F(FieldLandAfter); v != "" {
		return Split(v)
	}
	return BriefLandAfter(c.F("brief"))
}

// SoftDepsOf returns the active soft dependencies of c against s:
// a dropped soft dependency is released (if replaced by a twin via replaces /
// cost_carried_to / reason "replaced by <id>", rewritten to the twin), so a
// dropped predecessor never leaves a card stranded.
//
// tla/SoftDependency.tla SoftDepsOf, DropReleasesSoft, DropWithTwinRewritesSoft, DropReleasesOrRelinks
func SoftDepsOf(s *Snapshot, c *Card) []string {
	raw := SoftDepsRaw(c)
	if len(raw) == 0 {
		return nil
	}
	if s == nil || s.Work == nil {
		return raw
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range raw {
		target := resolveSoftTarget(s, id)
		if target == "" || target == c.ID || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out
}

func resolveSoftTarget(s *Snapshot, id string) string {
	cur := id
	for range 16 {
		t := s.Work.Card(cur)
		if t == nil {
			return ""
		}
		if t.Placed() && !IsSentinel(t) {
			return cur
		}
		if t.Placed() {
			return ""
		}
		next := strings.TrimSpace(t.F(FieldCostCarriedTo))
		if next == "" {
			if after, ok := strings.CutPrefix(strings.TrimSpace(t.F("reason")), "replaced by "); ok {
				next = strings.TrimSpace(after)
			}
		}
		if next == "" {
			for _, cand := range s.Work.Cards() {
				if cand.Placed() && !IsSentinel(cand) && contains(Split(cand.F(FieldReplaces)), cur) {
					next = cand.ID
					break
				}
			}
		}
		if next == "" || next == cur {
			return ""
		}
		cur = next
	}
	return ""
}

// LandOrderBlocked returns the soft dependencies of c that have neither landed
// nor been included ahead of c in landing.
//
// tla/SoftDependency.tla LandOrderBlocked, SoftLandsInOrder
func LandOrderBlocked(s *Snapshot, c *Card, landing map[string]bool) []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, dep := range SoftDepsOf(s, c) {
		if s.StateOf(dep) != Landed && !landing[dep] {
			out = append(out, dep)
		}
	}
	return out
}

// MergeSoftOrder filters and orders queued cards so that every card's hard needs
// (WaitsFor) and soft dependencies (LandOrderBlocked) are either already landed
// or precede it in the returned slice. A card whose soft dependency has not
// landed and is not queued ahead of it is held out of the batch without blocking
// cards behind it that do not depend on it. When every queued card is blocked by
// an unlanded soft dependency, softWhy names the first card and its unlanded
// soft dependency.
//
// tla/SoftDependency.tla MergeSoftOrder, SoftLandsInOrder, SoftNeverDeadlocksOnUnmergedPredecessor
func MergeSoftOrder(s *Snapshot, cards []*Card) (ordered []*Card, softWhy string) {
	if len(cards) == 0 || s == nil || s.Work == nil {
		return cards, ""
	}
	pending := slices.Clone(cards)
	landed := map[string]bool{}
	out := make([]*Card, 0, len(cards))
	for len(pending) > 0 {
		best := -1
		for i, c := range pending {
			pr := s.Work.Card(c.ID)
			if len(WaitsFor(s, pr, landed)) == 0 && len(LandOrderBlocked(s, pr, landed)) == 0 {
				best = i
				break
			}
		}
		if best < 0 {
			break
		}
		c := pending[best]
		out = append(out, c)
		landed[c.ID] = true
		pending = slices.Delete(pending, best, best+1)
	}
	if len(out) == 0 && len(pending) > 0 {
		for _, c := range pending {
			pr := s.Work.Card(c.ID)
			if blocked := LandOrderBlocked(s, pr, landed); len(blocked) > 0 {
				return out, fmt.Sprintf("%s: soft dependency on %s not landed; nothing was changed", c.ID, strings.Join(blocked, ", "))
			}
		}
	}
	return out, ""
}

// SoftDepsSetReq is the request for needs set <id> --land-after <a,b>.
type SoftDepsSetReq struct {
	ID      string   `json:"id"`
	Raw     string   `json:"land_after"`
	Answers []string `json:"answers,omitempty"`
	Who     string   `json:"who,omitempty"`
}

// SetSoftDeps replaces soft_deps on a placed, unlanded primary.
//
// tla/SoftDependency.tla SetSoftDeps, SetSoftDepsOnReady, ClearSoftDeps
func SetSoftDeps(s *Snapshot, r SoftDepsSetReq) Plan {
	var p Plan
	if why := notCoordinator(s, r.Who, "needs set"); why != "" {
		p.refuse(r.ID, why)
		return p
	}
	c := s.Work.Card(r.ID)
	switch {
	case c == nil:
		p.refuse(r.ID, "no primary "+r.ID+" on the table")
		return p
	case IsSentinel(c):
		p.refuse(r.ID, r.ID+" is a sentinel, not a primary")
		return p
	case !c.Placed():
		p.refuse(r.ID, r.ID+" is not on the table ("+orDash(c.F("outcome"))+"); soft_deps is set on an unlanded primary on the table")
		return p
	case c.Col == Landed:
		p.refuse(r.ID, r.ID+" has already landed; soft_deps is set on an unlanded primary")
		return p
	}
	trimmed := strings.TrimSpace(r.Raw)
	var wants []string
	if trimmed != "" && trimmed != "-" && !strings.EqualFold(trimmed, "none") {
		wants = Split(r.Raw)
	}
	if why := validateSoftDeps(s, c.ID, cardRepoOf(c), cardBaseOf(c), Split(c.F("needs")), wants, nil); why != "" {
		p.refuse(r.ID, why)
		return p
	}
	if cycle := softCycleWith(s, c.ID, map[string][]string{c.ID: wants}, nil); len(cycle) > 0 {
		p.refuse(r.ID, "cycle in dependencies: "+strings.Join(cycle, " -> "))
		return p
	}
	set := map[string]string{FieldLandAfterSet: "true"}
	var unset []string
	if len(wants) > 0 {
		set[FieldLandAfter] = strings.Join(wants, ",")
	} else {
		unset = append(unset, FieldLandAfter)
	}
	p.Units = append(p.Units, Unit{
		Key:     c.ID,
		Stream:  c.Row,
		Changes: []Change{change(Work, setEntry(c, set, unset...))},
		Moved:   fmt.Sprintf("%s land_after %s stream=%s %s", c.ID, orDash(set[FieldLandAfter]), c.Row, c.Col),
	})
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

func cardRepoOf(c *Card) string {
	if c == nil {
		return ""
	}
	cb := swarm.ReadCardBase([]byte(c.F("brief")))
	if cb.Named != "" {
		return cb.Named
	}
	if cb.Repo != "" {
		return cb.Repo
	}
	return c.F(FieldRepo)
}

func cardBaseOf(c *Card) string {
	if c == nil {
		return ""
	}
	cb := swarm.ReadCardBase([]byte(c.F("brief")))
	if cb.Ref != "" {
		return cb.Ref
	}
	return c.F(FieldBase)
}

type addingCardMeta struct {
	id          string
	repo        string
	base        string
	needs       []string
	softDepsRaw []string
	softDeps    []string
}

func validateSoftDeps(s *Snapshot, id, repo, base string, hardNeeds, wants []string, adding map[string]addingCardMeta) string {
	seen := map[string]bool{}
	for _, n := range wants {
		if n == id {
			return fmt.Sprintf("%s cannot have a soft dependency on itself", id)
		}
		if seen[n] {
			return fmt.Sprintf("%s is named twice in --land-after", n)
		}
		seen[n] = true
		if contains(hardNeeds, n) {
			return fmt.Sprintf("%s is already in needs of %s: one relation, not two (DEPENDS-ON or LAND-AFTER, not both)", n, id)
		}
		if meta, ok := adding[n]; ok {
			if repo != "" && meta.repo != "" && repo != meta.repo {
				return fmt.Sprintf("%s is in repository %s, not %s: the order would mean nothing", n, meta.repo, repo)
			}
			if base != meta.base {
				return fmt.Sprintf("%s is on base %s, not %s: the order would mean nothing", n, orDash(meta.base), orDash(base))
			}
			continue
		}
		t := s.Work.Card(n)
		switch {
		case t == nil:
			return fmt.Sprintf("soft dependency %s is not on the table", n)
		case IsSentinel(t):
			return fmt.Sprintf("soft dependency %s is a sentinel, not a primary", n)
		case !t.Placed():
			return fmt.Sprintf("soft dependency %s is not on the table (%s)", n, orDash(t.F("outcome")))
		}
		tRepo := cardRepoOf(t)
		tBase := cardBaseOf(t)
		if repo != "" && tRepo != "" && repo != tRepo {
			return fmt.Sprintf("%s is in repository %s, not %s: the order would mean nothing", n, tRepo, repo)
		}
		if base != tBase {
			return fmt.Sprintf("%s is on base %s, not %s: the order would mean nothing", n, orDash(tBase), orDash(base))
		}
	}
	return ""
}

// softCycleWith checks whether start participates in a cycle in the combined
// (needs union soft_deps) graph when softOverrides and adding are applied.
func softCycleWith(s *Snapshot, start string, softOverrides map[string][]string, adding map[string]addingCardMeta) []string {
	edgesOf := func(id string) []string {
		var hard, soft []string
		if meta, ok := adding[id]; ok {
			hard = meta.needs
			soft = meta.softDeps
		} else if s != nil && s.Work != nil {
			if c := s.Work.Placed(id); c != nil && !IsSentinel(c) {
				hard = Split(c.F("needs"))
				soft = SoftDepsOf(s, c)
			}
		}
		if ov, ok := softOverrides[id]; ok {
			soft = ov
		}
		out := make([]string, 0, len(hard)+len(soft))
		seen := map[string]bool{}
		for _, d := range append(slices.Clone(hard), soft...) {
			if d != "" && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
		return out
	}
	var path []string
	state := map[string]int{} // 0 unvisited, 1 visiting, 2 visited
	var dfs func(u string) []string
	dfs = func(u string) []string {
		state[u] = 1
		path = append(path, u)
		for _, v := range edgesOf(u) {
			if state[v] == 1 {
				idx := slices.Index(path, v)
				if idx >= 0 {
					return append(slices.Clone(path[idx:]), v)
				}
				return []string{u, v, u}
			}
			if state[v] == 0 {
				if cyc := dfs(v); len(cyc) > 0 {
					return cyc
				}
			}
		}
		path = path[:len(path)-1]
		state[u] = 2
		return nil
	}
	return dfs(start)
}

// WithAddSoftDeps validates LAND-AFTER: headers on cards being admitted by rs
// and records FieldLandAfter on their created work table entries without moving
// ready cards to waiting.
//
// tla/SoftDependency.tla AddCardWithSoftDeps, SoftNeverBlocksDeal, SoftDoesNotBlockDeal
func WithAddSoftDeps(s *Snapshot, p Plan, rs []AddReq) Plan {
	if len(p.Refused) > 0 {
		return p
	}
	adding := map[string]addingCardMeta{}
	var order []string
	for _, r := range rs {
		if r.Sentinel {
			continue
		}
		cards := r.Cards
		if len(cards) == 0 && len(r.IDs) > 0 {
			for _, id := range r.IDs {
				cards = append(cards, CardAdd{ID: id, Brief: r.Brief, Needs: r.Needs, Base: r.Base, Repo: r.Repo})
			}
		}
		for _, cd := range cards {
			cb := swarm.ReadCardBase([]byte(cd.Brief))
			repo := cd.Repo
			if repo == "" {
				repo = cb.Named
			}
			if repo == "" {
				repo = cb.Repo
			}
			base := cd.Base
			if base == "" {
				base = cb.Ref
			}
			raw := BriefLandAfterRaw(cd.Brief)
			soft := BriefLandAfter(cd.Brief)
			adding[cd.ID] = addingCardMeta{
				id:          cd.ID,
				repo:        repo,
				base:        base,
				needs:       cd.Needs,
				softDepsRaw: raw,
				softDeps:    soft,
			}
			order = append(order, cd.ID)
		}
	}
	for _, id := range order {
		meta := adding[id]
		if len(meta.softDepsRaw) == 0 {
			continue
		}
		if why := validateSoftDeps(s, id, meta.repo, meta.base, meta.needs, meta.softDepsRaw, adding); why != "" {
			return Plan{Refused: []Refusal{{Key: id, Why: why}}}
		}
		if cyc := softCycleWith(s, id, nil, adding); len(cyc) > 0 {
			return Plan{Refused: []Refusal{{Key: id, Why: "cycle in dependencies: " + strings.Join(cyc, " -> ")}}}
		}
	}
	for ui := range p.Units {
		for ci := range p.Units[ui].Changes {
			ch := &p.Units[ui].Changes[ci]
			if ch.Table != Work || ch.Entry.Remove {
				continue
			}
			meta, ok := adding[ch.Entry.ID]
			if !ok || len(meta.softDeps) == 0 {
				continue
			}
			val := strings.Join(meta.softDeps, ",")
			if ch.Entry.Set == nil {
				ch.Entry.Set = map[string]string{}
			}
			ch.Entry.Set[FieldLandAfter] = val
			ch.Entry.Set[FieldLandAfterSet] = "true"
		}
	}
	return p
}

// WithDropSoftDeps releases or rewrites soft_deps on placed primaries when a
// card they name in soft_deps is dropped (with --relink rewriting to r.Relink,
// and a plain drop releasing the soft dependency without requiring --cascade,
// --waive, or --relink).
//
// tla/SoftDependency.tla DropReleasesSoft, DropWithTwinRewritesSoft, DropReleasesOrRelinks
func WithDropSoftDeps(s *Snapshot, p Plan, r DropReq) Plan {
	if len(p.Refused) > 0 || s == nil || s.Work == nil {
		return p
	}
	dropped := map[string]bool{}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == Work && ch.Entry.Remove {
				dropped[ch.Entry.ID] = true
			}
		}
	}
	if len(dropped) == 0 {
		return p
	}
	for _, c := range s.Work.Cards() {
		if !c.Placed() || IsSentinel(c) || dropped[c.ID] {
			continue
		}
		raw := SoftDepsRaw(c)
		if !slices.ContainsFunc(raw, func(d string) bool { return dropped[d] }) {
			continue
		}
		var next []string
		seen := map[string]bool{}
		for _, d := range raw {
			target := d
			if dropped[d] {
				if r.Relink != "" && r.Relink != c.ID {
					target = r.Relink
				} else {
					continue
				}
			}
			if target != "" && target != c.ID && !seen[target] {
				seen[target] = true
				next = append(next, target)
			}
		}
		set := map[string]string{FieldLandAfterSet: "true"}
		var unset []string
		if len(next) > 0 {
			set[FieldLandAfter] = strings.Join(next, ",")
		} else {
			unset = append(unset, FieldLandAfter)
		}
		if len(p.Units) > 0 {
			p.Units[0].Changes = append(p.Units[0].Changes, change(Work, setEntry(c, set, unset...)))
		}
	}
	return p
}

package sprint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// TriageCheck is one named row of the plan review (docs/SPEC-SPRINT.md,
// plan and check columns). Details explain failures rather than only scoring them.
type TriageCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// TriageRow is one card's complete review in the selected set.
type TriageRow struct {
	ID            string        `json:"id"`
	Passed        bool          `json:"passed"`
	Checks        []TriageCheck `json:"checks"`
	Bytes         int           `json:"bytes"`
	RepeatedBytes int           `json:"repeated_bytes"`
	Precedents    []string      `json:"precedents"`
}

// TriageSet reviews one snapshot without changing it (docs/SPEC-SPRINT.md,
// plan and check columns). Every card is compared with all admitted cards,
// including unselected siblings, so selecting one cannot hide a collision.
func TriageSet(s *Snapshot, ids []string) []TriageRow {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	out := make([]TriageRow, 0, len(ids))
	for _, id := range ids {
		row := TriageRow{ID: id, Passed: true, Checks: []TriageCheck{}, Precedents: []string{}}
		add := func(name, why string) {
			row.Checks = append(row.Checks, TriageCheck{Name: name, Passed: why == "", Detail: why})
			row.Passed = row.Passed && why == ""
		}
		c := s.Work.Placed(id)
		if c == nil {
			add("admitted", "no admitted card")
			out = append(out, row)
			continue
		}
		brief := c.F("brief")
		row.Bytes = len(brief)
		lines := map[string]bool{}
		for _, line := range strings.Split(brief, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				if lines[line] {
					row.RepeatedBytes += len(line) + 1
				}
				lines[line] = true
			}
		}
		add("shared-paths", triageShared(s, c))
		add("dependencies", triageDependencies(s, c))
		add("bench", triageBench(brief))
		_, _, tierWhy := ModelTier(brief)
		add("tier", tierWhy)
		sizeWhy := ""
		if row.Bytes > 16*1024 {
			sizeWhy = fmt.Sprintf("brief is %d bytes, limit 16384; %d repeated line bytes", row.Bytes, row.RepeatedBytes)
		}
		add("size", sizeWhy)
		duplicate := ""
		if done := triageDone(brief); done != "" {
			for _, old := range s.Work.Column(Landed) {
				if old.ID != id && triageRepo(old) == triageRepo(c) && triageDone(old.F("brief")) == done {
					duplicate = "Done is matches landed card " + old.ID
					break
				}
			}
		}
		add("landed", duplicate)
		class := triageHeader(brief, "CLASS")
		if class != "" {
			for _, prior := range sortedCards(s.Work) {
				if triageHeader(prior.F("brief"), "CLASS") == class {
					for _, line := range strings.Split(prior.F(FieldFindings), "\n") {
						if line = strings.TrimSpace(line); line != "" {
							row.Precedents = append(row.Precedents, prior.ID+": "+line)
						}
					}
				}
			}
			for _, old := range sortedCards(s.Fleet) {
				pr := s.Work.Card(old.F("primary"))
				if pr == nil || triageHeader(pr.F("brief"), "CLASS") != class || (old.Col != "failed" && old.Col != "fail" && old.F("outcome") != "failed" && old.F("outcome") != "fail" && old.F("ok") != "no") {
					continue
				}
				report := strings.TrimSpace(old.F("report"))
				if report == "" {
					report = old.F("reason")
				}
				first, _, _ := strings.Cut(report, "\n")
				if first != "" {
					row.Precedents = append(row.Precedents, old.ID+": "+first)
				}
			}
		}
		add("precedent", "")
		out = append(out, row)
	}
	return out
}

func triageHeader(brief, key string) string {
	v, _ := swarm.CardHeaderValue([]byte(brief), key)
	return strings.TrimSpace(v)
}
func triageRepo(c *Card) string {
	if r := c.F(FieldRepo); r != "" {
		return r
	}
	return triageHeader(c.F("brief"), "REPO")
}

// triageShared composes the established path/ledger helpers over the set
// (docs/SPEC-SPRINT.md, plan and check columns); ledger sharing is permitted.
func triageShared(s *Snapshot, c *Card) string {
	paths := append(headerGlobs(c.F("brief"), "PATHS"), headerGlobs(c.F("brief"), "NEW")...)
	var bad []string
	for _, p := range headerGlobs(c.F("brief"), "SHARED") {
		if !SharedLedger(p) {
			bad = append(bad, p+" is a shared non-ledger path")
		}
	}
	if len(paths) == 0 {
		return strings.Join(bad, "; ")
	}
	for _, other := range sortedCards(s.Work) {
		if other.ID == c.ID || !other.Placed() || other.Col == Landed || IsSentinel(other) || triageRepo(other) != triageRepo(c) {
			continue
		}
		theirs := append(headerGlobs(other.F("brief"), "PATHS"), headerGlobs(other.F("brief"), "NEW")...)
		for _, p := range paths {
			if SharedLedger(p) {
				continue
			}
			for _, q := range theirs {
				if !SharedLedger(q) && overlaps(p, q) {
					bad = append(bad, p+" overlaps "+other.ID+":"+q)
				}
			}
		}
	}
	return strings.Join(bad, "; ")
}

func triageNeeds(c *Card) []string {
	needs := append(Split(c.F("needs")), headerGlobs(c.F("brief"), "DEPENDS-ON")...)
	slices.Sort(needs)
	return slices.Compact(needs)
}

// triageDependencies verifies named needs and a directed cycle over the whole
// admitted graph (docs/SPEC-SPRINT.md, plan and check columns).
func triageDependencies(s *Snapshot, c *Card) string {
	var bad []string
	for _, id := range triageNeeds(c) {
		if s.Work.Placed(id) == nil {
			bad = append(bad, "dependency "+id+" is not admitted or landed")
		}
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if done[id] {
			return false
		}
		card := s.Work.Placed(id)
		if card == nil || card.Col == Landed {
			return false
		}
		visiting[id] = true
		for _, need := range triageNeeds(card) {
			if visit(need) {
				return true
			}
		}
		delete(visiting, id)
		done[id] = true
		return false
	}
	if visit(c.ID) {
		bad = append(bad, "dependency cycle reachable from "+c.ID)
	}
	if strings.Contains(strings.ToLower(triageHeader(c.F("brief"), "CLASS")), "conformance") {
		emitter := triageHeader(c.F("brief"), "EMITTER")
		if emitter == "" || !slices.Contains(triageNeeds(c), emitter) {
			bad = append(bad, "conformance names EMITTER and depends on it")
		}
	}
	return strings.Join(bad, "; ")
}

// triageBench requires explicit bench headers when the brief names the work
// they describe (docs/SPEC-SPRINT.md, plan and check columns).
func triageBench(brief string) string {
	var bad []string
	text := strings.ToLower(brief)
	paths := headerGlobs(brief, "PATHS")
	model := len(ModelPaths(paths)) > 0
	goWork := slices.ContainsFunc(paths, func(p string) bool { return strings.HasSuffix(p, ".go") })
	if (model || goWork || strings.Contains(text, "go test") || strings.Contains(text, "cargo test") || strings.Contains(text, "cmake")) && triageHeader(brief, "TOOLCHAINS") == "" {
		bad = append(bad, "TOOLCHAINS header required")
	}
	if (model || strings.Contains(text, "make tlc")) && triageHeader(brief, "TLC") == "" {
		bad = append(bad, "TLC header required")
	}
	if (strings.Contains(text, "../") || strings.Contains(text, "sibling repository") || strings.Contains(text, "sibling repo")) && triageHeader(brief, "SIBLINGS") == "" {
		bad = append(bad, "SIBLINGS header required")
	}
	return strings.Join(bad, "; ")
}

// triageDone takes the acceptance paragraph, excluding following card boilerplate
// (docs/SPEC-SPRINT.md, plan and check columns). An empty acceptance is no match.
func triageDone(brief string) string {
	var lines []string
	found := false
	for _, line := range strings.Split(brief, "\n") {
		trim := strings.TrimSpace(line)
		if !found {
			if at := strings.Index(strings.ToLower(trim), "done is:"); at >= 0 {
				found = true
				trim = trim[at+len("done is:"):]
			} else {
				continue
			}
		} else if trim == "" || strings.HasPrefix(trim, "STEP ") || strings.HasPrefix(trim, "RULES") || strings.HasPrefix(trim, "LEDGER") {
			break
		}
		lines = append(lines, trim)
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

// TriageReq selects one coordinator set (docs/SPEC-SPRINT.md, plan and check columns).
type TriageReq struct {
	IDs     []string
	Stream  string
	AllPlan bool
	Who     string
}

func TriageIDs(s *Snapshot, r TriageReq) []string {
	if len(r.IDs) > 0 {
		return slices.Clone(r.IDs)
	}
	var ids []string
	for _, c := range s.Work.Column(Planned, Checked) {
		if (r.Stream == "" || c.Row == r.Stream) && (!r.AllPlan || c.Col == Planned) {
			ids = append(ids, c.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// ReviewPlan moves only passing cards from plan to check; the full review is
// recomputed from this commit's snapshot (docs/SPEC-SPRINT.md, plan and check columns).
func ReviewPlan(s *Snapshot, r TriageReq) (Plan, []TriageRow) {
	p := Plan{}
	p.on(s)
	rows := TriageSet(s, TriageIDs(s, r))
	if s.Actor != s.Coordinator || s.Actor == "" {
		p.refuse("check", "only the coordinator checks a plan")
		return p, rows
	}
	for _, row := range rows {
		c := s.Work.Placed(row.ID)
		if c == nil || c.Col != Planned {
			p.refuse(row.ID, "check requires plan")
			continue
		}
		if !row.Passed {
			var why []string
			for _, v := range row.Checks {
				if !v.Passed {
					why = append(why, v.Name+": "+v.Detail)
				}
			}
			p.refuse(row.ID, strings.Join(why, "; "))
			continue
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, moveEntry(c, c.Row, Checked, map[string]string{"checked": stamp(s.Now)}))}, Moved: c.ID + " -> check"})
	}
	return p, rows
}

// PromotePlan rechecks the set at promotion, preventing an intervening new
// sibling or edit from using stale approval (docs/SPEC-SPRINT.md, plan and check columns).
func PromotePlan(s *Snapshot, r TriageReq) Plan {
	p := Plan{}
	p.on(s)
	if s.Actor != s.Coordinator || s.Actor == "" {
		p.refuse("promote", "only the coordinator promotes a plan")
		return p
	}
	for _, row := range TriageSet(s, TriageIDs(s, r)) {
		c := s.Work.Placed(row.ID)
		if c == nil || c.Col != Checked {
			p.refuse(row.ID, "promote requires check; run check first")
			continue
		}
		if !row.Passed {
			p.refuse(row.ID, "set triage changed; repair the plan and check it again")
			continue
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, moveEntry(c, c.Row, Waiting, map[string]string{"promoted": stamp(s.Now)}))}, Moved: c.ID + " -> waiting"})
	}
	return p
}

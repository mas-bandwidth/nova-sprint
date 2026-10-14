// Package cardgen.
package cardgen

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// CardPlan is one card of a plan under review: its id and its brief text. The
// shared-path rule (docs/SPEC-CARD-CONTRACT.md) reads only the brief's typed
// header lines, so a plan is a text-only description of the cards, pure and
// store-free like the rest of cardgen.
type CardPlan struct {
	ID    string
	Brief string
}

// SharedPathRow is one path two or more cards of a plan name together in their
// PATHS lines. Ledger says a lander-owned ledger covers the path (LedgerInPaths),
// so the lander merges it and the cards do not collide. Edge says a needs edge
// chains the pair, so the later card lands at the earlier's tip. A row that is
// neither ledger nor edge is a refusal: the two cards would edit the file at once.
type SharedPathRow struct {
	Path   string
	Cards  []string
	Ledger bool
	Edge   bool
}

// SharedPathRows is the overlap table of a plan: one row per repository and path
// named in PATHS by two or more cards, in first-named path order then repository
// order within each path, each with the cards
// that name it, whether a lander-owned ledger covers it, and whether a needs edge
// chains the cards (docs/SPEC-CARD-CONTRACT.md, the shared-path rule).
func SharedPathRows(cards []CardPlan) []SharedPathRow {
	type pathKey struct{ repo, path string }
	byPath := map[pathKey][]string{}
	var order []pathKey
	pathRank := map[string]int{}
	// Unknown identities overlap every repository naming that path. They are
	// never evidence that two cards stage into separate repositories.
	unknown := map[string][]string{}
	needs := map[string][]string{}
	for _, c := range cards {
		repo := planRepo(c.Brief)
		needs[c.ID] = briefNeeds(c.Brief)
		for _, p := range headerPaths(c.Brief, "PATHS") {
			if _, seen := pathRank[p]; !seen {
				pathRank[p] = len(pathRank)
			}
			key := pathKey{repo, p}
			if !slices.Contains(byPath[key], c.ID) {
				if len(byPath[key]) == 0 {
					order = append(order, key)
				}
				byPath[key] = append(byPath[key], c.ID)
			}
			if repo == "" && !slices.Contains(unknown[p], c.ID) {
				unknown[p] = append(unknown[p], c.ID)
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		return pathRank[order[i].path] < pathRank[order[j].path]
	})
	var rows []SharedPathRow
	for _, key := range order {
		ids := append([]string(nil), byPath[key]...)
		if key.repo != "" && len(unknown[key.path]) > 0 {
			// Keep plan order when an unresolved card preceded the known repository.
			ids = nil
			for _, c := range cards {
				if (slices.Contains(byPath[key], c.ID) || slices.Contains(unknown[key.path], c.ID)) && !slices.Contains(ids, c.ID) {
					ids = append(ids, c.ID)
				}
			}
		}
		if len(ids) < 2 {
			continue
		}
		rows = append(rows, SharedPathRow{
			Path: key.path, Cards: ids,
			Ledger: LedgerInPaths(key.path), Edge: anyEdge(needs, ids),
		})
	}
	return rows
}

// planRepo uses the effective staging repository, including base-repo precedence.
// An unreadable identity stays unknown. Normalization follows cmd/nova-sprint's
// normRepo/sameRepo: scheme and user do not distinguish a repository, host case
// does not matter, path case does, and a trailing slash and .git are ignored.
// Keep this small local copy aligned until a shared repository helper is needed.
func planRepo(brief string) string {
	u := swarm.ReadCardBase([]byte(brief)).Repo
	if swarm.CardRepoURL(u) == "" {
		return ""
	}
	u = strings.TrimSpace(u)
	host, path := "", u
	if i := strings.Index(u, "://"); i >= 0 {
		host, path, _ = strings.Cut(u[i+3:], "/")
	} else if colon := strings.Index(u, ":"); colon > 0 && !strings.Contains(u[:colon], "/") {
		host, path = u[:colon], u[colon+1:]
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	if host == "" {
		return path
	}
	return strings.ToLower(host) + "/" + strings.TrimPrefix(path, "/")
}

// anyEdge says a needs edge chains two of the cards: one reaches the other
// through the needs of the plan's cards.
func anyEdge(needs map[string][]string, ids []string) bool {
	var reaches func(a, b string, seen map[string]bool) bool
	reaches = func(a, b string, seen map[string]bool) bool {
		if seen[a] {
			return false
		}
		seen[a] = true
		for _, n := range needs[a] {
			if n == b || reaches(n, b, seen) {
				return true
			}
		}
		return false
	}
	for i := range ids {
		for _, b := range ids[i+1:] {
			if reaches(ids[i], b, map[string]bool{}) || reaches(b, ids[i], map[string]bool{}) {
				return true
			}
		}
	}
	return false
}

// Review is a plan review of a set of cards, for a seat reading the plan before it
// admits a stream (docs/SPRINT-COORDINATOR.md, the plan review; docs/SPEC-CARD-CONTRACT.md,
// the shared-path rule): the overlap table (Table) and the first non-ledger overlap
// without an edge (RefuseOverlaps), "" when the plan is clean. The table prints every
// overlap; the refusal names both cards and the path and is what the add refuses on.
func Review(cards []CardPlan) (table, refusal string) {
	rows := SharedPathRows(cards)
	return Table(rows), RefuseOverlaps(rows)
}

// RefuseOverlaps is the first non-ledger overlap without an edge, "" when there
// is none: the brief the add refuses on, naming both cards and the path.
func RefuseOverlaps(rows []SharedPathRow) string {
	for _, r := range rows {
		if r.Ledger || r.Edge {
			continue
		}
		return fmt.Sprintf("%s is named in PATHS by %s and %s, and neither needs the other", r.Path, r.Cards[0], r.Cards[1])
	}
	return ""
}

// Table renders the overlap table for a plan review: one line per overlap with
// the path, the cards, ledger or not, and the edge if any.
func Table(rows []SharedPathRow) string {
	var b strings.Builder
	for _, r := range rows {
		ledger := "ledger"
		if !r.Ledger {
			ledger = "not-ledger"
		}
		edge := "no-edge"
		if r.Edge {
			edge = "edge"
		}
		fmt.Fprintf(&b, "%s  %s  %s  %s\n", r.Path, strings.Join(r.Cards, ","), ledger, edge)
	}
	return b.String()
}

// briefNeeds is the needs a brief names: the first `Needs:` header line, else
// the DEPENDS-ON: line, its ids comma separated, each cut at an opening
// parenthesis. "none" or "-" (also after the cut), an owner/repo#n reference,
// or no such line is no needs.
func briefNeeds(brief string) []string {
	value, found := "", false
	for _, line := range strings.Split(brief, "\n") {
		if key, v, ok := cardhdr.KeyValue(line); ok && key == "Needs" {
			value, found = v, true
			break
		}
	}
	if !found {
		value, _ = swarm.CardHeaderValue([]byte(brief), "DEPENDS-ON")
	}
	var out []string
	for _, id := range strings.Split(value, ",") {
		if cut, _, ok := strings.Cut(id, "("); ok {
			id = cut
		}
		switch id = strings.TrimSpace(id); {
		case id == "", id == "-", id == "none", strings.Contains(id, "#"):
			continue
		}
		out = append(out, id)
	}
	return out
}

// headerPaths is the files a brief's typed header line names (PATHS:, SHARED:),
// commas or blanks between them; none and - name no file.
func headerPaths(brief, key string) []string {
	value, _ := swarm.CardHeaderValue([]byte(brief), key)
	var out []string
	for _, p := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if p != "none" && p != "-" {
			out = append(out, p)
		}
	}
	return out
}

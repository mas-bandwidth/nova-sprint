package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// briefCheck is one brief of an add held to the card checks, under the id it is admitted as.
type briefCheck struct {
	id, brief string
	// stream is the stream the brief is added to, "" when it is not one stream: a stream
	// that names a default tier (stream set --default-tier) stands for a line 1 that names
	// none, so its tier-line finding is not held (the card is stored with the default)
	stream string
	// siblings are the other briefs the call holds beside this one, by card id (brief check over a
	// directory): a need is looked for among them before the table (dependsFindings)
	siblings map[string]string
}

// holdCardChecks holds each brief of an add to the card checks nova-card generate holds
// it to before it leaves (card.Checks) and to the sibling-path rule (cardgen.LintSiblingPath,
// docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header): a card brief names a tier on line 1
// and a TEST whose package PATHS names; no brief carries the name of the sprint's
// coordinator, its owner or a friend outside double-quoted words, nor names a card dropped
// off the table, nor names a sibling path its SIBLINGS header does not cover. One red
// brief refuses the whole call, exit 2, nothing written, every finding on its own LINT
// DRIFT line.
func (a *app) holdCardChecks(verbName string, st *store.Store, stderr io.Writer, briefs ...briefCheck) int {
	var tokens []string
	given := false
	for _, b := range briefs {
		given = given || b.brief != ""
		tokens = append(tokens, idTokenRE.FindAllString(b.brief, -1)...)
		tokens = append(tokens, swarm.DependsOnIDs(b.brief)...) // an id with no hyphen is no token above
	}
	if !given {
		return 0
	}
	ctx := context.Background()
	names, err := personalNames(ctx, st)
	if err != nil {
		return a.readFailed(verbName, err, stderr)
	}
	// a dropped card is kept off the table, so it is read by id: the briefs' words that
	// could be a card's id, and no more
	s, err := st.Load(ctx, []string{sprint.Work}, func(*sprint.Snapshot) map[string][]string {
		return map[string][]string{sprint.Work: uniquify(tokens)}
	})
	if err != nil {
		return a.readFailed(verbName, err, stderr)
	}
	opts := card.Options{Names: personal(names), Dropped: droppedIDs(s)}
	var red []cardgen.LintFinding
	defaults := map[string]string{}
	for _, b := range briefs {
		for _, f := range card.Checks(b.id, b.brief, opts) {
			if f.Check == "tier-line" && b.stream != "" {
				t, ok := defaults[b.stream]
				if !ok {
					if t, err = streamDefaultTier(ctx, st, b.stream); err != nil {
						return a.readFailed(verbName, err, stderr)
					}
					defaults[b.stream] = t
				}
				if t != "" {
					continue // stored with the stream's default tier (sprint.FieldDefaultTier)
				}
			}
			red = append(red, f)
		}
		red = append(red, cardgen.LintSiblingPath(b.id, b.brief)...)
	}
	red = append(red, dependsFindings(s, briefs)...)
	if len(red) == 0 {
		return 0
	}
	for _, f := range red {
		fmt.Fprintln(stderr, oneline.Escape(f.String()))
	}
	return refuse(stderr, verbName, fmt.Sprintf("%d brief finding(s), the first %s: %s; nothing was written, every finding is a LINT DRIFT line above", len(red), red[0].Check, red[0].Excerpt))
}

// idTokenRE is a word of a brief that could be a card's id: letters and digits joined by
// at least one hyphen (s1-1, fix-x.w2, the ids --count and the twins make); a dropped card
// whose id has no hyphen is not looked for.
var idTokenRE = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._~]*(?:-[A-Za-z0-9._~]*[A-Za-z0-9])+`)

// droppedIDs is every card of the snapshot kept off the table with the outcome dropped.
func droppedIDs(s *sprint.Snapshot) []string {
	var out []string
	for _, c := range s.Work.Cards() {
		if !c.Placed() && c.F("outcome") == "dropped" {
			out = append(out, c.ID)
		}
	}
	return out
}

// roleWords are the words a brief names a role by; a sprint whose coordinator or owner is
// recorded under one (an actor left at its default) has no name there to keep out.
var roleWords = []string{"coordinator", "owner", "friend", "member", "reader", "child", "worker"}

// personal is names without the role words.
func personal(names []string) []string {
	var out []string
	for _, n := range names {
		if !slices.Contains(roleWords, strings.ToLower(n)) {
			out = append(out, n)
		}
	}
	return out
}

// streamDefaultTier is the tier the stream names for a card whose line 1 names none
// (sprint.FieldDefaultTier, stream set --default-tier); "" when it names none.
func streamDefaultTier(ctx context.Context, st *store.Store, stream string) (string, error) {
	s, err := st.Load(ctx, []string{sprint.Merge}, nil)
	if err != nil {
		return "", err
	}
	if ctl := s.StreamCtl(stream); ctl != nil {
		return ctl.F(sprint.FieldDefaultTier), nil
	}
	return "", nil
}

// dependsFindings holds each brief's needs line to the DEPENDS-ON grammar (swarm.LintDependsOn;
// docs/SPEC-CARD-CONTRACT.md, "The DEPENDS-ON grammar"): every need names what it consumes from
// the needed card, or an overlap kept with its reason. The needed card's brief is looked for among
// the briefs of the call first (the same add, or the directory brief check reads), then on the
// table the snapshot s holds. A nil s is no table (brief check with the store out of reach): an id
// the call's briefs do not hold is then not judged, and its depends-unknown finding is left out;
// the store line of brief check says the table was out of reach. A brief carrying a CARRY: head=
// line (what --widen writes) is the card's own text with its needs line unchanged, and is not
// held again.
func dependsFindings(s *sprint.Snapshot, briefs []briefCheck) []cardgen.LintFinding {
	inCall := map[string]string{}
	for _, b := range briefs {
		if b.id != "" {
			inCall[b.id] = b.brief
		}
		for id, text := range b.siblings {
			if _, ok := inCall[id]; !ok {
				inCall[id] = text
			}
		}
	}
	needed := func(id string) ([]byte, bool) {
		if text, ok := inCall[id]; ok {
			return []byte(text), true
		}
		if s != nil {
			if c := s.Work.Card(id); c != nil {
				return []byte(c.F("brief")), true
			}
		}
		return nil, false
	}
	var out []cardgen.LintFinding
	for _, b := range briefs {
		if v, ok := swarm.CardHeaderValue([]byte(b.brief), "CARRY"); ok && strings.HasPrefix(v, "head=") {
			continue
		}
		for _, f := range swarm.LintDependsOn([]byte(b.brief), needed) {
			if f.Check == swarm.DependsUnknownCheck && s == nil {
				continue // no table was read: the id may be a card on it
			}
			out = append(out, cardgen.LintFinding{ID: b.id, Check: f.Check, Line: f.Line, Excerpt: f.Excerpt})
		}
	}
	return out
}

// holdDependsOn is the DEPENDS-ON grammar alone, for a verb that does not run the other card
// checks (brief, which holds a replacement text to it): the findings print as LINT DRIFT lines
// and one refusal follows, exit 2, nothing written. The table is read for the needed briefs.
func (a *app) holdDependsOn(verbName string, st *store.Store, stderr io.Writer, briefs ...briefCheck) int {
	var ids []string
	for _, b := range briefs {
		ids = append(ids, swarm.DependsOnIDs(b.brief)...)
	}
	if len(ids) == 0 {
		return 0
	}
	s, err := st.Load(context.Background(), []string{sprint.Work}, func(*sprint.Snapshot) map[string][]string {
		return map[string][]string{sprint.Work: uniquify(ids)}
	})
	if err != nil {
		return a.readFailed(verbName, err, stderr)
	}
	red := dependsFindings(s, briefs)
	if len(red) == 0 {
		return 0
	}
	for _, f := range red {
		fmt.Fprintln(stderr, oneline.Escape(f.String()))
	}
	return refuse(stderr, verbName, fmt.Sprintf("%d DEPENDS-ON finding(s), the first %s: %s; nothing was written, every finding is a LINT DRIFT line above", len(red), red[0].Check, red[0].Excerpt))
}

// overlapNeedsSay is the OVERLAP NEEDS line of a call whose cards keep overlap needs: the entries
// written `(overlap: ...)` across the cards' briefs (each brief once per card it is given to);
// "" when the call keeps none. docs/SPEC-CARD-CONTRACT.md section 2 holds the grammar.
func overlapNeedsSay(cards map[string]int) string {
	n := 0
	for brief, times := range cards {
		n += swarm.DependsOverlapCount([]byte(brief)) * times
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("OVERLAP NEEDS %d", n)
}

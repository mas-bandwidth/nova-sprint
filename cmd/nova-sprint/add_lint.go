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
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// briefCheck is one brief of an add held to the card checks, under the id it is admitted as.
type briefCheck struct {
	id, brief string
	// stream is the stream the brief is added to, "" when it is not one stream: a stream
	// that names a default tier (stream set --default-tier) stands for a line 1 that names
	// none, so its tier-line finding is not held (the card is stored with the default)
	stream string
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
	if len(red) == 0 {
		return 0
	}
	var ruleTokens []string
	var ids []string
	for _, f := range red {
		ruleTokens = append(ruleTokens, f.Check)
		if f.ID != "" && !slices.Contains(ids, f.ID) {
			ids = append(ids, f.ID)
		}
	}
	a.recordLintRefusal(st, verbName, ids, ruleTokens)
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

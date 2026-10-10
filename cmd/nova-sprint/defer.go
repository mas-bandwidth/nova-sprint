package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// defer defers waiting cards to the roadmap with their whole briefs: each named
// waiting card (or every waiting card of --stream, or of the streams recording
// --repo) is written into the roadmap data as one item whose text is its whole
// brief, then dropped from the store. --expect checks the count before anything
// is written. The roadmap data is the checkout's docs/roadmap.sexp (--repo-dir).
func (a *app) cmdDefer(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("defer")
	var stream string
	var repo listFlag
	var expect int
	var repoDir, reason string
	fs.StringVar(&stream, "stream", "", "the waiting cards of this stream")
	fs.Var(&repo, "repo", "the waiting cards of the streams recording this repository (owner/name), comma separated or repeated")
	fs.IntVar(&expect, "expect", 0, "the number of waiting cards the call defers; a set of another size now is refused and nothing changes")
	fs.StringVar(&repoDir, "repo-dir", "", "the checkout whose docs/roadmap.sexp each whole brief is written into")
	fs.StringVar(&reason, "reason", "", "why each card leaves the table; kept with its record")
	ids, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "defer", err.Error())
	}
	switch {
	case strings.TrimSpace(reason) == "":
		return refuse(stderr, "defer", "wants --reason <text>")
	case repoDir == "":
		return refuse(stderr, "defer", "wants --repo-dir <dir>, the checkout whose docs/roadmap.sexp it writes")
	case len(ids) > 0 && (stream != "" || len(repo) > 0):
		return refuse(stderr, "defer", "name the cards, or give --stream or --repo, not both")
	case stream != "" && len(repo) > 0:
		return refuse(stderr, "defer", "--stream and --repo select different sets: give one")
	case len(ids) == 0 && stream == "" && len(repo) == 0:
		return refuse(stderr, "defer", "wants ids, --stream <s> or --repo <owner/name>")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "defer", err.Error())
	}
	ctx := context.Background()
	var streams []string
	if stream != "" {
		streams = []string{stream}
	}
	if len(repo) > 0 {
		streams, err = a.repoStreams(ctx, st, repo)
		if err != nil {
			return a.readFailed("defer", err, stderr)
		}
		if len(streams) == 0 {
			return refuse(stderr, "defer", "no stream records "+strings.Join(repo, ",")+": nothing to defer; run: nova-sprint streams")
		}
	}
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	if err != nil {
		return a.readFailed("defer", err, stderr)
	}
	named := map[string]bool{}
	for _, id := range ids {
		named[id] = true
	}
	inStream := map[string]bool{}
	for _, s := range streams {
		inStream[s] = true
	}
	var chosen []*sprint.Card
	for _, c := range snap.Work.Column(sprint.Waiting) {
		if sprint.IsSentinel(c) {
			continue
		}
		if len(ids) > 0 {
			if !named[c.ID] {
				continue
			}
		} else if !inStream[c.Row] {
			continue
		}
		chosen = append(chosen, c)
	}
	// a named card that is no waiting primary is refused before anything is
	// written, naming it; the step's plan re-checks it on its own read.
	if len(ids) > 0 {
		for _, id := range ids {
			if !picked(chosen, id) {
				c := snap.Work.Card(id)
				switch {
				case c == nil:
					return refuse(stderr, "defer", id+": no such card on the table")
				case sprint.IsSentinel(c):
					return refuse(stderr, "defer", id+" is a sentinel; a sentinel is released, not deferred")
				default:
					return refuse(stderr, "defer", id+" is not waiting (it is "+c.Col+"); only a waiting card defers")
				}
			}
		}
	}
	if len(chosen) == 0 {
		return refuse(stderr, "defer", "no waiting card to defer")
	}
	if expect != 0 && len(chosen) != expect {
		return refuse(stderr, "defer", fmt.Sprintf("--expect %d was printed for another set: %d waiting card(s) now (%s)", expect, len(chosen), strings.Join(cardIDs(chosen), ",")))
	}
	// the roadmap write first: a write that fails defers nothing
	if err := writeRoadmap(filepath.Join(repoDir, "docs", "roadmap.sexp"), chosen, a.now().UTC().Format("2006-01-02")); err != nil {
		return refuse(stderr, "defer", "the roadmap write failed and nothing was deferred: "+err.Error())
	}
	step := store.DeferStep(sprint.DeferReq{Sel: sprint.Sel{IDs: cardIDs(chosen)}, Reason: reason, Who: c.actor})
	return a.runStep("defer", *c, st, step, stdout, stderr)
}

// picked says id is one of the chosen cards.
func picked(cards []*sprint.Card, id string) bool {
	for _, c := range cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

// cardIDs is the ids of the cards, in order.
func cardIDs(cards []*sprint.Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

// writeRoadmap appends one item per card to the roadmap data, its text the
// card's whole brief, refusing a card whose id the roadmap already holds. It
// decodes the file before and after the append, so a write never corrupts the
// data.
func writeRoadmap(path string, cards []*sprint.Card, date string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	decoded, err := roadmapdoc.Decode(path, raw)
	if err != nil {
		return fmt.Errorf("%s does not decode: %w", path, err)
	}
	have := map[string]bool{}
	for _, it := range decoded.Items {
		have[it.ID] = true
	}
	var block strings.Builder
	for _, c := range cards {
		id, brief := c.ID, c.F("brief")
		if have[id] {
			return fmt.Errorf("the roadmap already holds an item %q; defer it under another name", id)
		}
		have[id] = true
		title := sprint.StreamTitle(brief)
		if title == "" {
			title = id
		}
		block.WriteString(renderRoadmapItem(id, title, brief, date))
	}
	next, err := insertRoadmapItems(string(raw), block.String())
	if err != nil {
		return err
	}
	if _, err := roadmapdoc.Decode(path, []byte(next)); err != nil {
		return fmt.Errorf("the appended roadmap data does not decode: %w", err)
	}
	if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// renderRoadmapItem is one item record: the whole brief as its text, the THE
// TASK sentence as its title, aimed after the fixes-only ladder.
func renderRoadmapItem(id, title, text, date string) string {
	return fmt.Sprintf("  (item %s :group %s\n   :title %s\n   :text %s\n   :date %s\n   :release %s\n   :origin %s)\n",
		sexp(id), sexp("sprint"), sexp(title), sexp(text), sexp(date), sexp("after v1.4"), sexp("card moved out of the sprint (work record, "+date+")"))
}

// sexp is a string as a roadmap-data string literal.
func sexp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// insertRoadmapItems inserts the item blocks before the roadmap form's closing
// of its :items list (the file's trailing "))").
func insertRoadmapItems(src, block string) (string, error) {
	s := strings.TrimRight(src, "\r\n")
	if !strings.HasSuffix(s, "))") {
		return "", errors.New("the roadmap data does not end with its items list")
	}
	return s[:len(s)-2] + "\n" + block + "))" + "\n", nil
}

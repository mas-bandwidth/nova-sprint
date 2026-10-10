package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmapdoc"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/internal/worklang"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// defer moves waiting cards out of the sprint and into the roadmap, each with its whole
// brief (docs/fixes.sexp, the fix defer-cards-to-the-roadmap). A roadmap item replaces
// the cards it stands for, so a card that will not be worked in this sprint is kept as a
// record in the roadmap data and taken off the table, both in one verb: the named cards,
// or the waiting cards of a stream or of the streams recording a repository.
//
// The verb is a drop (store.DropStep, sprint.Drop: all of the moves are the drop's, the
// state machine is the drop's, and so is its model) plus one file write, so it adds no
// state of its own. The order is the file first and the drop second: a brief is in the
// roadmap file before its card leaves the table, never after. Then the file is read back
// against the store: it holds exactly the cards the drop took off the table, so a card the
// drop refused (another waiting card still needs it) or a drop that did not confirm
// leaves no item for a card still on the table. A card taken off the table keeps its own
// record in the store with its brief too (sprint.Drop), so a brief is never lost.

// deferReason is the reason a deferred card is dropped with: kept with its record.
func deferReason(file string) string { return "deferred to the roadmap: " + filepath.Base(file) }

// deferItem is one roadmap item a card becomes.
type deferItem struct {
	ID, Group, Title, Text, Why, Date, Origin, Release string
}

var sexpQuote = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// sexp is the item as roadmapdoc's shape reads it: the text is the brief exactly, a
// string with its backslashes and quotes escaped and its line ends as they are.
func (it deferItem) sexp() string {
	q := func(s string) string { return `"` + sexpQuote.Replace(s) + `"` }
	var b strings.Builder
	fmt.Fprintf(&b, "(item %s :group %s :title %s", q(it.ID), q(it.Group), q(it.Title))
	fmt.Fprintf(&b, "\n   :text %s", q(it.Text))
	fmt.Fprintf(&b, "\n   :why %s :date %s :origin %s", q(it.Why), q(it.Date), q(it.Origin))
	if it.Release != "" {
		fmt.Fprintf(&b, " :release %s", q(it.Release))
	}
	b.WriteString(")")
	return b.String()
}

// insertRoadmapItems is the roadmap file with the items added at the end of its :items
// list, every other byte (the comments, the wrapping) as it was. The result is read back
// whole by roadmapdoc.Decode, so a file that is not the roadmap shape, an id already in
// use, a group that does not exist, and a file past roadmapdoc.Limits are all refused,
// and each new item must read back with the brief it was given, exactly.
func insertRoadmapItems(file string, data []byte, items []deferItem) ([]byte, error) {
	top, err := worklang.Read(file, data, roadmapdoc.Limits)
	if err != nil {
		return nil, err
	}
	var list *worklang.Form
	for i := 2; i+1 < len(top.List); i += 2 {
		if k := top.List[i]; k.Kind == worklang.Keyword && k.Value == "items" {
			list = &top.List[i+1]
			break
		}
	}
	if list == nil || list.Kind != worklang.List || list.End < 1 || data[list.End-1] != ')' {
		return nil, fmt.Errorf("%s has no :items list to add to", file)
	}
	var out bytes.Buffer
	out.Write(data[:list.End-1])
	for _, it := range items {
		out.WriteString("\n  ")
		out.WriteString(it.sexp())
	}
	out.Write(data[list.End-1:])
	doc, err := roadmapdoc.Decode(file, out.Bytes())
	if err != nil {
		return nil, err
	}
	if len(doc.Items) < len(items) {
		return nil, fmt.Errorf("%s: the items added did not read back", file)
	}
	for i, it := range items {
		got := doc.Items[len(doc.Items)-len(items)+i]
		if got.ID != it.ID || got.Group != it.Group || got.Text != it.Text {
			return nil, fmt.Errorf("%s: item %s did not read back with its brief as it was", file, it.ID)
		}
	}
	return out.Bytes(), nil
}

// writeKeptMode writes data to path through a temporary file in its directory and a
// rename, keeping the file's mode, so a reader never sees half a roadmap.
func writeKeptMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".defer-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		os.Remove(name)
	}
	return err
}

// refused is a refusal of the call as the sprint and the file stand, exit 1 and nothing
// changed; a usage refusal (the flags do not make a call) is refuse's exit 2.
func refused(stderr io.Writer, what string) int {
	refuse(stderr, "defer", what)
	return 1
}

// cmdDefer is the verb: see the comment above.
func (a *app) cmdDefer(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("defer")
	file := fs.String("file", "docs/roadmap.sexp", "the roadmap data the items are added to (the server's file when the verb is sent to it: the path is made absolute); run make roadmap after, to regenerate ROADMAP.md")
	into := fs.String("into", "", "the roadmap group the items go under (a (group ...) record of the file)")
	release := fs.String("release", "", "the release the items are aimed at, when one is (the items' :release)")
	why := fs.String("why", "", "why the cards leave the sprint (the items' :why; default: that they were deferred out of the sprint)")
	stream := fs.String("stream", "", "the waiting cards of this stream")
	var repo listFlag
	fs.Var(&repo, "repo", "the waiting cards of the streams recording this repository (owner/name), comma separated or repeated; needs --expect <n>, the number of cards it selects")
	expect := fs.Int("expect", -1, "the number of cards the call selects, as a --dry-run printed it; another number refuses the call and nothing changes")
	dry := fs.Bool("dry-run", false, "list the cards and the items they would become, and write nothing")
	ids, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, "defer", err.Error())
	}
	modes := 0
	for _, given := range []bool{len(ids) > 0, *stream != "", len(repo) > 0} {
		if given {
			modes++
		}
	}
	switch {
	case modes != 1:
		return refuse(stderr, "defer", "wants the cards to defer: ids, or --stream <s>, or --repo <owner/name> --expect <n> (one of the three)")
	case *into == "":
		return refuse(stderr, "defer", "wants --into <roadmap group>: the group of the roadmap the items go under")
	case len(repo) > 0 && *expect < 0:
		return refuse(stderr, "defer", "--repo selects the waiting cards of whole streams: give --expect <n> (--dry-run prints n), so what it takes is read before it runs")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "defer", err.Error())
	}
	ctx := context.Background()
	cards, why2 := a.deferCards(ctx, st, ids, *stream, repo)
	if why2 != "" {
		return refused(stderr, why2)
	}
	if *expect >= 0 && *expect != len(cards) {
		return refused(stderr, fmt.Sprintf("--expect %d was printed for another set: the call selects %d waiting card(s); nothing was changed", *expect, len(cards)))
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return refused(stderr, "--file: "+err.Error())
	}
	doc, err := roadmapdoc.Decode(*file, data)
	if err != nil {
		return refused(stderr, "--file is not a roadmap: "+err.Error())
	}
	var groups []string
	for _, g := range doc.Groups {
		groups = append(groups, g.ID)
	}
	if !slices.Contains(groups, *into) {
		return refused(stderr, fmt.Sprintf("--into %s names no group of %s; its groups are %s", *into, *file, strings.Join(groups, ", ")))
	}
	reasonText := *why
	if reasonText == "" {
		reasonText = "Deferred out of the sprint to the roadmap; the text is the card's whole brief."
	}
	date := a.now().UTC().Format("2006-01-02")
	var items []deferItem
	var named []string
	for _, card := range cards {
		items = append(items, deferItem{ID: card.ID, Group: *into, Title: "Deferred card " + card.ID, Text: card.F("brief"), Why: reasonText,
			Date: date, Origin: "card " + card.ID + " of stream " + card.Row + ", deferred from the sprint", Release: *release})
		named = append(named, card.ID)
	}
	next, err := insertRoadmapItems(*file, data, items)
	if err != nil {
		return refused(stderr, err.Error())
	}
	if *dry {
		for _, it := range items {
			fmt.Fprintf(stdout, "DEFER %s waiting -> item %s under %s\n", oneline.Escape(it.ID), oneline.Escape(it.ID), oneline.Escape(it.Group))
		}
		fmt.Fprintf(stdout, "DEFER dry-run: %d waiting card(s) would go to %s; --expect %d; nothing written\n", len(items), oneline.Escape(*file), len(items))
		return 0
	}
	if err := writeKeptMode(*file, next); err != nil {
		return refuse(stderr, "defer", "--file could not be written: "+err.Error()+"; nothing was changed")
	}
	reason := deferReason(*file)
	c.after = func(ctx context.Context, st *store.Store, res store.Result) []string {
		return a.droppedBriefs(ctx, st, res, reason)
	}
	code := a.runStep("drop", *c, st, store.DropStep(sprint.DropReq{Sel: sprint.Sel{IDs: named}, Reason: reason, Who: c.actor}), stdout, stderr)
	// The file holds exactly the cards that left the table.
	left, err := a.deferred(ctx, st, named, reason)
	switch {
	case err != nil:
		fmt.Fprintf(stderr, "%s defer: the store could not be read back (%v): %s holds an item for each of the %d card(s) named, and some may still be on the table; run: nova-sprint card <id>\n", prog, err, oneline.Escape(*file), len(named))
		return max(code, 2)
	case len(left) == len(named):
		fmt.Fprintf(stderr, "%s defer: %d card(s) are items of %s under %s; run: make roadmap\n", prog, len(left), oneline.Escape(*file), oneline.Escape(*into))
		return code
	}
	var keep []deferItem
	for _, it := range items {
		if slices.Contains(left, it.ID) {
			keep = append(keep, it)
		}
	}
	final := data
	if len(keep) > 0 {
		if final, err = insertRoadmapItems(*file, data, keep); err != nil {
			fmt.Fprintf(stderr, "%s defer: %s keeps an item for every card named but only %d left the table: %v\n", prog, oneline.Escape(*file), len(left), err)
			return max(code, 2)
		}
	}
	if err := writeKeptMode(*file, final); err != nil {
		fmt.Fprintf(stderr, "%s defer: %s keeps an item for every card named but only %d left the table: %v\n", prog, oneline.Escape(*file), len(left), err)
		return max(code, 2)
	}
	fmt.Fprintf(stderr, "%s defer: %d of %d card(s) left the table (the rest stay in the sprint); %s holds the %d item(s) of those that did\n", prog, len(left), len(named), oneline.Escape(*file), len(left))
	return max(code, 1)
}

// deferCards is the waiting cards the call names, in order; why is non-empty when the
// call is refused. Every card named by id must be waiting; a stream (or the streams of a
// repository) gives its waiting cards. A card without a brief cannot be deferred.
func (a *app) deferCards(ctx context.Context, st *store.Store, ids []string, stream string, repo []string) ([]*sprint.Card, string) {
	snap, err := st.Load(ctx, []string{sprint.Work}, nil)
	if err != nil {
		return nil, "the work table could not be read: " + err.Error()
	}
	var cards []*sprint.Card
	var bad []string
	switch {
	case len(ids) > 0:
		for _, id := range ids {
			switch card := snap.Work.Placed(id); {
			case card == nil:
				bad = append(bad, id+" is no card on the table")
			case card.Col != sprint.Waiting:
				bad = append(bad, id+" is "+card.Col+", not waiting")
			default:
				cards = append(cards, card)
			}
		}
	default:
		streams := []string{stream}
		if len(repo) > 0 {
			if streams, err = a.repoStreams(ctx, st, repo); err != nil {
				return nil, "the streams could not be read: " + err.Error()
			}
			if len(streams) == 0 {
				return nil, "no stream records " + strings.Join(repo, ",") + ": nothing to select; run: nova-sprint streams"
			}
		}
		for _, s := range streams {
			cards = append(cards, snap.Work.Cell(s, sprint.Waiting)...)
		}
		if len(cards) == 0 {
			return nil, "no waiting card in " + strings.Join(streams, ",") + ": nothing to defer; nothing was changed"
		}
	}
	seen := map[string]bool{}
	for _, card := range cards {
		if seen[card.ID] {
			bad = append(bad, card.ID+" is named twice")
		}
		seen[card.ID] = true
		if strings.TrimSpace(card.F("brief")) == "" {
			bad = append(bad, card.ID+" has no brief to carry")
		}
	}
	if len(bad) > 0 {
		return nil, strings.Join(bad, "; ") + "; nothing was changed"
	}
	return cards, ""
}

// deferred is which of the cards named are off the table with the defer's reason: the
// ones whose drop took.
func (a *app) deferred(ctx context.Context, st *store.Store, named []string, reason string) ([]string, error) {
	var left []string
	for _, id := range named {
		info, err := st.CardOf(ctx, id)
		if err != nil {
			return nil, err
		}
		if p := info.Primary; p != nil && !p.Placed() && p.F("outcome") == "dropped" && p.F("reason") == reason {
			left = append(left, id)
		}
	}
	return left, nil
}

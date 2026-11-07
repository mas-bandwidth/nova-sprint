package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runJudge is `judge`: with no argument the open judgements, with one the
// judgement (or group) and its choices, with two the answer. A choice that
// moves a card (unblock, drop) rides in the answer's op; the others are the
// answer alone, which the next tick reads. A group is answered member by
// member, one op each, after --expect <n> confirms how many.
func runJudge(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	if len(in.args) == 0 {
		return judgementList(snap), nil
	}
	members, err := judgementsOf(snap, in.arg(0))
	if err != nil {
		return "", err
	}
	if len(in.args) == 1 {
		return judgementShow(members), nil
	}
	choice, reason := in.arg(1), in.str("reason")
	if reason == "" {
		return "", refuse("a judgement's answer needs its reason", "add --reason \"<why>\"")
	}
	if len(members) > 1 {
		if n, f := in.num("expect"); f != nil || n != len(members) {
			return "", refuse(fmt.Sprintf("group %s has %d open judgements", in.arg(0), len(members)), fmt.Sprintf("confirm with --expect %d", len(members)))
		}
	}
	var lines []string
	for i, j := range members {
		if snap, err = e.again(ctx, snap, i); err != nil {
			return "", err
		}
		r, err := answerRequest(j, choice, reason)
		if err != nil {
			return "", err
		}
		line, err := e.move(ctx, in, snap, r, j.About)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return done("nova-sprint judge --as "+in.actor, lines...), nil
}

// judgementsOf is the open judgement named, or every open member of the
// group named.
func judgementsOf(snap store.Snapshot, name string) ([]store.Judgement, error) {
	if j, ok := snap.Judgements[name]; ok {
		if j.State != "open" {
			return nil, refuse(fmt.Sprintf("judgement %s was %s by %s", j.ID, j.State, j.Actor), "nova-sprint judge")
		}
		return []store.Judgement{j}, nil
	}
	var out []store.Judgement
	for _, j := range snap.Judgements {
		if j.Group == name && j.State == "open" {
			out = append(out, j)
		}
	}
	if len(out) == 0 {
		return nil, refuse("no open judgement or group "+name, "nova-sprint judge")
	}
	slices.SortFunc(out, func(a, b store.Judgement) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// choicesOf is a judgement's choices, as its command lists them.
func choicesOf(j store.Judgement) []string {
	_, list, ok := strings.Cut(j.Command, "choices:")
	if !ok {
		return []string{"retry"}
	}
	var out []string
	for _, c := range strings.Split(list, "|") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// answerRequest is the answer as a request: unblock and drop move the card
// the judgement is about; every other choice is the answer alone.
func answerRequest(j store.Judgement, choice, reason string) (steps.Request, error) {
	cs := choicesOf(j)
	if !slices.Contains(cs, choice) {
		return steps.Request{}, misuse("judge", "judgement "+j.ID+" takes one of: "+strings.Join(cs, " "))
	}
	r := steps.Request{Judgement: j.ID, Answer: choice}
	switch choice {
	case "unblock":
		r.Card, r.Event = j.About, card.Unblock{Reason: reason}
	case "drop":
		r.Card, r.Event = j.About, card.Cancel{Reason: reason}
	}
	return r, nil
}

func judgementList(snap store.Snapshot) string {
	var open []store.Judgement
	for _, j := range snap.Judgements {
		if j.State == "open" {
			open = append(open, j)
		}
	}
	if len(open) == 0 {
		return done(waitNext, "no open judgements")
	}
	slices.SortFunc(open, func(a, b store.Judgement) int { return a.OpenedAt.Compare(b.OpenedAt) })
	return done("nova-sprint judge <id> to see one", judgementLines(open)...)
}

func judgementShow(members []store.Judgement) string {
	lines := judgementLines(members)
	if len(members) > 1 {
		return done(fmt.Sprintf("nova-sprint judge %s <choice> --expect %d --reason \"<why>\"", members[0].Group, len(members)), lines...)
	}
	return done(members[0].Command, append(lines, "question: "+oneLine(members[0].Question))...)
}

func judgementLines(js []store.Judgement) []string {
	out := make([]string, len(js))
	for i, j := range js {
		out[i] = fmt.Sprintf("JUDGEMENT %s kind=%s about=%s group=%s choices=%s", j.ID, j.Kind, j.About, j.Group, strings.Join(choicesOf(j), "|"))
	}
	return out
}

// runDrop is `drop` and `replace`: a producer cancelled (or replaced by
// another), every dependant failed with its need in the same op unless
// --waive <dependant> or --relink <dependant>=<card> chose otherwise. A
// dry run prints what fails.
func runDrop(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	reason := in.str("reason")
	if reason == "" {
		return "", refuse("a "+in.verb+" needs its reason", "add --reason \"<why>\"")
	}
	var ev card.Event = card.Cancel{Reason: reason}
	if in.verb == "replace" {
		by := in.str("by")
		if by == "" {
			return "", misuse("replace", "replace names the card that replaces it: --by <card>")
		}
		ev = card.Replace{By: by, Reason: reason}
	}
	chosen, err := dropChoices(in, reason)
	if err != nil {
		return "", err
	}
	var lines []string
	for i, id := range in.args {
		if snap, err = e.again(ctx, snap, i); err != nil {
			return "", err
		}
		if c, ok := snap.Cards[id]; ok && c.Of != "" {
			return "", refuse(id+" is a consumer; a drop is of its producer", "nova-sprint "+in.verb+" "+c.Of+" --reason \"<why>\"")
		}
		line, err := e.move(ctx, in, snap, steps.Request{Card: id, Event: ev, Drop: chosen}, id)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return done(nextAfter(in), lines...), nil
}

// dropChoices is --waive <dependant> and --relink <dependant>=<card>.
func dropChoices(in *input, reason string) (map[string]card.Event, error) {
	out := map[string]card.Event{}
	for _, d := range in.list("waive") {
		out[d] = card.Waive{Reason: reason}
	}
	for _, x := range in.list("relink") {
		d, to, ok := strings.Cut(x, "=")
		if !ok || d == "" || to == "" {
			return nil, misuse(in.verb, "--relink is <dependant>=<card>")
		}
		if _, dup := out[d]; dup {
			return nil, misuse(in.verb, d+" is both waived and relinked")
		}
		out[d] = card.Relink{To: to, Reason: reason}
	}
	return out, nil
}

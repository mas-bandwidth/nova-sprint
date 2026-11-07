package verbs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// nextView is next's answer: the one thing to do and its command, and, for
// a row, its inventory (nova-worker reads `next --json`). The lists are
// never null; width 0 is an answer (no room, or no roster).
type nextView struct {
	Message    string              `json:"message"`
	Next       string              `json:"next"`
	Epoch      int                 `json:"epoch"`
	Seq        int64               `json:"seq"`
	Row        string              `json:"row"`
	State      string              `json:"state"`
	Role       string              `json:"role"`
	Width      int                 `json:"width"`
	Held       bool                `json:"held"`
	Model      string              `json:"model"`
	TierModels map[string][]string `json:"tier_models"`
	Harness    string              `json:"harness"`
	RosterErr  string              `json:"roster_error,omitempty"`
	Ready      []consumerLine      `json:"ready"`
	Working    []consumerLine      `json:"working"`
	Final      []consumerLine      `json:"final"`
	Judgements []judgementLine     `json:"judgements"`
}

type consumerLine struct {
	ID       string `json:"id"`
	Producer string `json:"producer"`
	Kind     string `json:"kind"`
	Stream   string `json:"stream"`
	State    string `json:"state"`
	Priority string `json:"priority"`
	Head     string `json:"head"`
	Gen      int    `json:"gen"`
	Attempt  int    `json:"attempt"`
	Score    int    `json:"score"`
	Command  string `json:"command"`
}

type judgementLine struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	About    string `json:"about"`
	Question string `json:"question"`
	Command  string `json:"command"`
}

// runNext is `next` (CORRECT-USE, "What next looks at, in order"). A
// missing prerequisite is its answer, exit 0; it exits 2 only when it
// cannot look.
func runNext(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	v := nextView{Row: in.actor, TierModels: map[string][]string{}, Ready: []consumerLine{}, Working: []consumerLine{}, Final: []consumerLine{}, Judgements: []judgementLine{}}
	if err := e.look(ctx, &v, in.has("json")); err != nil {
		return "", err
	}
	if in.has("json") {
		b, err := json.Marshal(v)
		return string(b) + "\n", err
	}
	return done(v.Next, "NEXT "+v.Message), nil
}

// look fills v: the store and its schema, the name, the bus, the hook, then
// the row's cards, its judgements and the streams.
func (e *Engine) look(ctx context.Context, v *nextView, inventory bool) error {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return e.unready(ctx, v, err)
	}
	v.Epoch, v.Seq = snap.Epoch, snap.Seq
	if v.Row == "" {
		v.Message, v.Next = "choose the name you use in the sprint", "nova-sprint next --as <name>"
		return nil
	}
	if err := e.onBus(ctx, v.Row); err != nil {
		return guidance(v, err)
	}
	row := snap.Rows[v.Row]
	v.State, v.Role, v.Held = row.State, row.Role, row.Held
	if row.Session == "" {
		v.Message, v.Next = v.Row+" is not hooked in to the sprint", "nova-sprint hook --as "+v.Row+" --role worker"
		return nil
	}
	fillInventory(v, snap)
	if inventory {
		e.fillRoster(ctx, v, snap)
	}
	answer(v, snap)
	return nil
}

// unready is next's answer when the store cannot be read: the fleet down,
// the store not set up or a refusal (no sprint credential in this process)
// is guidance; anything else is a failure to look.
func (e *Engine) unready(ctx context.Context, v *nextView, cause error) error {
	if errors.Is(cause, ErrNotSetUp) {
		v.Message, v.Next = "the sprint is not set up", "nova-sprint up --as "+cmp.Or(v.Row, "<coordinator>")
		return nil
	}
	var op *OperationError
	if errors.As(cause, &op) && op.Exit != exitFailed {
		return guidance(v, op)
	}
	if e.Doctor == nil {
		return cause
	}
	if err := e.Doctor.Fleet(ctx); err != nil {
		return guidance(v, err)
	}
	if ok, err := e.Doctor.Schema(ctx); err == nil && !ok {
		v.Message, v.Next = "nova-tools is up; the sprint is not set up", "nova-sprint up --as "+cmp.Or(v.Row, "<coordinator>")
		return nil
	}
	return cause
}

// guidance turns a refusal into next's answer; a failure stays one.
func guidance(v *nextView, err error) error {
	f := classify(err)
	if f.Exit != exitRefused {
		return f
	}
	v.Message, v.Next = f.What, f.Fix
	return nil
}

// fillInventory is the row's consumers by state, in deal order.
func fillInventory(v *nextView, snap store.Snapshot) {
	for _, c := range snap.Cards {
		if c.Holder != v.Row || (c.Kind != "work" && c.Kind != "read") {
			continue
		}
		p := snap.Cards[c.Of]
		line := consumerLine{ID: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, State: c.State, Priority: p.Priority,
			Head: c.Head, Gen: c.Gen, Attempt: c.Attempt, Score: p.Score, Command: stepOf(c, v.Row, snap)}
		switch c.State {
		case card.Ready:
			v.Ready = append(v.Ready, line)
		case card.Working:
			v.Working = append(v.Working, line)
		case card.Done, card.Fail:
			v.Final = append(v.Final, line)
		}
	}
	for _, l := range [][]consumerLine{v.Ready, v.Working, v.Final} {
		slices.SortFunc(l, dealOrder)
	}
	if v.Role == "coordinator" {
		v.Judgements = openJudgements(snap)
	}
}

// fillRoster is the row's width, model and harness from nova-config, and
// the models it may run by tier (tierModels); an unreadable roster is width
// 0 with the reason, never a refusal.
func (e *Engine) fillRoster(ctx context.Context, v *nextView, snap store.Snapshot) {
	a, err := e.answers(ctx, snap)
	if err == nil {
		ro, rerr := e.roster(ctx, a.Limits)
		if err = rerr; err == nil {
			_, v.Width, _, _ = ro.Row(v.Row)
			v.Model, v.Harness = ro.Kin[v.Row].Model, cmp.Or(ro.Kin[v.Row].Harness, ro.Workers[v.Row].Harness)
			v.TierModels = tierModels(ro, v.Row, a.Catalog)
		}
	}
	if err != nil {
		v.RosterErr = classify(err).What
	}
}

// answer is the first of: a card in hand, a judgement owed, a stream's
// state.
func answer(v *nextView, snap store.Snapshot) {
	if len(v.Working)+len(v.Ready) > 0 {
		first := append(slices.Clone(v.Working), v.Ready...)[0]
		v.Message, v.Next = fmt.Sprintf("%s %s is %s with %s", first.Kind, first.ID, inHand(first.State), v.Row), first.Command
	} else if len(v.Judgements) > 0 {
		v.Message, v.Next = "judgement "+v.Judgements[0].ID+" needs you", v.Judgements[0].Command
	} else {
		v.Message, v.Next = streamsAnswer(snap)
		if snap.Stopped && (v.Next == waitNext || v.Role != "coordinator") {
			v.Message, v.Next = stoppedAnswer(snap, v.Row, v.Role)
		}
	}
	if more := len(v.Working) + len(v.Ready) + len(v.Judgements) - 1; more > 0 {
		v.Message += fmt.Sprintf(" (%d more: nova-sprint next --as %s --json lists them)", more, v.Row)
	}
}

func inHand(state string) string {
	if state == card.Ready {
		return "dealt before the deal became the take (the tick deals it again)"
	}
	return "in hand"
}

// stepOf is the command a consumer held by row is at: in hand and not yet
// acknowledged, take (which prints the brief); acknowledged, finish or read.
// A ready one predates the deal being the take: the tick withdraws it and
// deals it again, so there is nothing to run.
func stepOf(c store.Card, row string, snap store.Snapshot) string {
	ref, epoch := fmtCard(c.ID, c.Gen), snap.Epoch
	_, acked := snap.Cursors[steps.AckCursor(epoch, c.ID)]
	switch {
	case c.State != card.Working:
		return waitNext
	case !acked:
		return fmt.Sprintf("nova-sprint take %s --as %s --epoch %d", ref, row, epoch)
	case c.Kind == "read":
		return fmt.Sprintf("nova-sprint read %s yes|fix --as %s --epoch %d", ref, row, epoch)
	}
	return fmt.Sprintf("nova-sprint finish %s --head <sha> --as %s --epoch %d", ref, row, epoch)
}

// stoppedAnswer is a stopped sprint's answer when nothing else is owed: a
// fresh store is stopped, never started, until the coordinator starts it.
// The coordinator is told to start once the streams need nothing first; a
// worker waits, whatever the streams need.
func stoppedAnswer(snap store.Snapshot, row, role string) (string, string) {
	msg := "the sprint is stopped (" + orWord(snap.StopReason, "no reason given") + "): nothing is dealt, read or landed until it starts"
	if role == "coordinator" {
		return msg, "nova-sprint start --as " + row + " --reason \"<why>\""
	}
	return msg, waitNext
}

// streamsAnswer is the sprint's state when nothing is owed: no stream, an
// empty stream, everything landed, or waiting.
func streamsAnswer(s store.Snapshot) (string, string) {
	if len(s.Streams) == 0 {
		return "no stream exists", "nova-sprint stream add <name> --repo <org/repo>"
	}
	count, open := map[string]int{}, 0
	for _, c := range s.Cards {
		if c.Kind == "producer" || c.Kind == "sentinel" {
			count[c.Stream]++
			if c.State != card.LandedState && c.State != card.Fail {
				open++
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(s.Streams)) {
		if count[name] == 0 {
			return "stream " + name + " has no cards", "nova-sprint add --stream " + name + " --brief-file <file>"
		}
	}
	if open == 0 {
		return "every card is final", waitNext
	}
	return "nothing needs you", waitNext
}

// openJudgements are the open judgements, oldest first.
func openJudgements(s store.Snapshot) []judgementLine {
	var js []store.Judgement
	for _, j := range s.Judgements {
		if j.State == "open" {
			js = append(js, j)
		}
	}
	slices.SortFunc(js, func(a, b store.Judgement) int {
		return cmp.Or(a.OpenedAt.Compare(b.OpenedAt), strings.Compare(a.ID, b.ID))
	})
	out := []judgementLine{}
	for _, j := range js {
		out = append(out, judgementLine{ID: j.ID, Kind: j.Kind, About: j.About, Question: j.Question, Command: strings.TrimSpace(j.Command)})
	}
	return out
}

// dealOrder is the deal's order: level, then score, then id.
func dealOrder(a, b consumerLine) int {
	return cmp.Or(slices.Index(card.Levels, b.Priority)-slices.Index(card.Levels, a.Priority), b.Score-a.Score, strings.Compare(a.ID, b.ID))
}

package verbs

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type machineConsumer struct {
	ID, Producer, Kind, Stream, State, Priority, Head string
	Gen, Attempt, Score                               int
}
type machineNext struct {
	nextAnswer
	Epoch, Width               int
	Seq                        int64
	Row, State, Model, Harness string
	Held                       bool
	RosterPresent              bool `json:"roster_present"`
	Ready, Working, Final      []machineConsumer
	Version                    string `json:"version"`
}

func (e *Engine) nextJSON(ctx context.Context, actor string) Result {
	if e.Store == nil || e.Roster == nil || !validBuildVersion(e.Version) {
		return unavailable("next")
	}
	snap, err := e.Store.Snapshot(ctx)
	if err != nil {
		return operationalFailure("next", err)
	}
	if !roleMatchesSnapshot(ctx, actor, snap) {
		return refused("next", "hook changed", "nova-sprint hook --as "+actor+" --role worker")
	}
	row, ok := snap.Rows[actor]
	if !ok {
		return refused("next", "unknown row "+actor, "nova-sprint hook --as "+actor+" --role worker")
	}
	roster, err := e.Roster.ReadRoster(ctx)
	if err != nil {
		return operationalFailure("next", err)
	}
	_, width, _, rosterPresent := roster.Row(actor)
	out := machineNext{nextAnswer: nextFromSnapshot(snap, actor), Epoch: snap.Epoch, Seq: snap.Seq, Row: actor, State: row.State, Width: width, Held: row.Held, RosterPresent: rosterPresent, Version: e.Version, Ready: []machineConsumer{}, Working: []machineConsumer{}, Final: []machineConsumer{}}
	if k, found := roster.Kin[actor]; found {
		out.Model, out.Harness = k.Model, k.Harness
	}
	if w, found := roster.Workers[actor]; found {
		out.Harness = w.Harness
	}
	fillMachineConsumers(&out, snap, actor)
	data, err := json.Marshal(out)
	if err != nil {
		return unavailable("next")
	}
	return Result{string(data) + "\n", 0}
}
func fillMachineConsumers(out *machineNext, snap store.Snapshot, actor string) {
	for _, c := range snap.Cards {
		if c.Holder != actor || (c.Kind != "work" && c.Kind != "read") {
			continue
		}
		p := snap.Cards[c.Of]
		item := machineConsumer{ID: c.ID, Producer: c.Of, Kind: c.Kind, Stream: c.Stream, State: c.State, Gen: c.Gen, Attempt: c.Attempt, Priority: p.Priority, Score: p.Score, Head: c.Head}
		switch c.State {
		case "ready":
			if out.RosterPresent {
				out.Ready = append(out.Ready, item)
			}
		case "working":
			out.Working = append(out.Working, item)
		case "done", "fail":
			out.Final = append(out.Final, item)
		}
	}
	for _, items := range [][]machineConsumer{out.Ready, out.Working, out.Final} {
		slices.SortFunc(items, compareMachineConsumers)
	}
}
func compareMachineConsumers(a, b machineConsumer) int {
	ai, bi := slices.Index(card.Levels, a.Priority), slices.Index(card.Levels, b.Priority)
	if ai < 0 {
		ai = 0
	}
	if bi < 0 {
		bi = 0
	}
	if ai != bi {
		return bi - ai
	}
	if a.Score != b.Score {
		if a.Score > b.Score {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}

package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// laneTake is take --as <member> --lane <n>: a lane of a worker asking the server what it
// holds, and taking its next card when it holds none (sprint.LaneTake; the model is nova-tools
// tla/FriendLane.tla). The lane names its member and its lane and nothing else: the server
// picks the epoch (the one it holds now; a clear between that read and the step refuses the
// step as stale, and the lane asks again). After the step the server's own record is read
// and said on one line, whatever the step moved:
//
//	LANE <member> lane=<n> epoch=<e> holds=<card>@<gen> kind=<work|read> job=<job>
//	LANE <member> lane=<n> epoch=<e> holds=-
//
// job is the card's job as a friend's daemon names it (<id>~<epoch>, .g<gen> from the second
// generation). A lane runs exactly the card the line names, and reports on it by those ids.
func (a *app) laneTake(c common, st *store.Store, as string, lane int, stdout, stderr io.Writer) int {
	ctx := context.Background()
	if c.epoch < 0 {
		es, err := st.EpochNow(ctx)
		if err != nil {
			return refuse(stderr, "take", "the sprint's epoch did not read: "+err.Error())
		}
		c.epoch = int64(es.N)
	}
	epoch := uint64(c.epoch)
	c.packets = func(ctx context.Context, st *store.Store, res store.Result) []sprint.Packet {
		cs, err := laneCells(ctx, st, as)
		if err != nil {
			return nil
		}
		for _, x := range cs {
			if x.F(sprint.FieldLane) == strconv.Itoa(lane) {
				ps, _ := st.Packets(ctx, []*sprint.Card{x})
				return ps
			}
		}
		return nil
	}
	c.after = func(ctx context.Context, st *store.Store, res store.Result) []string {
		return []string{laneLine(ctx, st, as, lane, epoch)}
	}
	return a.runStep("take", c, st, store.TakeStep(sprint.TakeReq{As: as, Lane: lane, Who: as}), stdout, stderr)
}

// laneLine is the server's record of what lane n of as holds at epoch, read now.
func laneLine(ctx context.Context, st *store.Store, as string, lane int, epoch uint64) string {
	head := fmt.Sprintf("LANE %s lane=%d epoch=%d", as, lane, epoch)
	cs, err := laneCells(ctx, st, as)
	if err != nil {
		return head + " holds=? (the table did not read: " + err.Error() + ")"
	}
	var held []*sprint.Card
	for _, x := range cs {
		if x.F(sprint.FieldLane) == strconv.Itoa(lane) {
			held = append(held, x)
		}
	}
	if len(held) == 0 {
		return head + " holds=-"
	}
	sprint.SortCards(held)
	x := held[0]
	gen := max(x.Int("gen"), 1)
	job := sprint.StoredID(x.ID, epoch)
	if gen > 1 {
		job += ".g" + strconv.Itoa(gen)
	}
	kind := cmp.Or(x.F("kind"), "work")
	if _, ok := sprint.ReaderMachine(as); ok {
		kind = "read"
	}
	return fmt.Sprintf("%s holds=%s@%d kind=%s job=%s", head, x.ID, gen, kind, job)
}

// laneCells is where a lane of as holds its card: a reader's reading cell on the readers
// table (reader-<name>), else a worker's working cell on the fleet table.
func laneCells(ctx context.Context, st *store.Store, as string) ([]*sprint.Card, error) {
	if _, ok := sprint.ReaderMachine(as); ok {
		return st.ReadCells(ctx, sprint.Readers, as, sprint.Reading)
	}
	return st.ReadCells(ctx, sprint.Fleet, as, sprint.Working)
}

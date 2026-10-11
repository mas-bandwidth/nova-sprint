package store

import (
	"context"
	"encoding/json"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// DoctorRead is one read of the sprint for `nova-sprint doctor` (internal/sprint/doctor.go): the
// four tables as the tick's deal plans on them (the routes and tiers, the readers' states and
// who starts no card, the friends' seats), the tick's request (the members' beats, the friends,
// the machine's STOPPED spans), every reader's and friend's beat, and the stops record counted
// now. It writes nothing.
type DoctorRead struct {
	Snap        *sprint.Snapshot
	Req         sprint.TickReq
	ReaderBeats map[string]sprint.Beat
	FriendBeats map[string]sprint.Beat
	Stops       StopsRecord
}

// DoctorRead reads the sprint once for the doctor, as StopsNow reads it, and gives the snapshot
// what a dealing step's plan reads beside its tables (engine.go: the routes, the readers' states,
// NoRoom, the friends), so the doctor asks the deal itself why a card waits.
func (st *Store) DoctorRead(ctx context.Context) (DoctorRead, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return DoctorRead{}, err
	}
	_, shapes, err := pinned.look(ctx)
	if err != nil {
		return DoctorRead{}, err
	}
	_, beats, err := pinned.fleetBeats(ctx, shapes)
	if err != nil {
		return DoctorRead{}, err
	}
	snap, _, err := pinned.Fenced(withBudget(ctx), All, tickExtras, nil)
	if err != nil {
		return DoctorRead{}, err
	}
	now := pinned.now()
	m, _, err := pinned.Machine(ctx)
	if err != nil {
		return DoctorRead{}, err
	}
	req := sprint.TickReq{Who: sprint.MachineActor, Stopped: m.StoppedBetween, Beats: beats}
	if req.Friends, err = pinned.friendSeats(ctx, snap, now); err != nil {
		return DoctorRead{}, err
	}
	out := DoctorRead{Req: req, Stops: stopsOf(snap, req, now)}
	set, err := pinned.routes(ctx)
	if err != nil {
		return DoctorRead{}, err
	}
	set.into(snap)
	if err := pinned.readerStatesInto(ctx, snap); err != nil {
		return DoctorRead{}, err
	}
	snap.Friends = req.Friends
	out.Snap = snap
	if out.ReaderBeats, err = pinned.readerBeats(ctx, snap.Readers); err != nil {
		return DoctorRead{}, err
	}
	if out.FriendBeats, err = pinned.FriendBeats(ctx); err != nil {
		return DoctorRead{}, err
	}
	return out, nil
}

// readerBeats is the last beat of every reader row, in one exchange where the store can; a
// reader that never beat has none. A store that keeps no beats has none at all.
func (st *Store) readerBeats(ctx context.Context, readers *sprint.Table) (map[string]sprint.Beat, error) {
	out := map[string]sprint.Beat{}
	if readers == nil || len(readers.Rows()) == 0 {
		return out, nil
	}
	kv, err := st.rootKV()
	if err != nil {
		return out, nil // ignored: a store with no machine records keeps no beats
	}
	rows := readers.Rows()
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = readerBeatKey(r)
	}
	vals, oks, err := getKeys(ctx, kv, names)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		var b sprint.Beat
		if i < len(oks) && oks[i] && json.Unmarshal([]byte(vals[i]), &b) == nil {
			out[r] = b
		}
	}
	return out, nil
}

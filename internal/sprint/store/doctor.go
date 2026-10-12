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
	// Refused is the server's refused-verb record of the sprint's current epoch: every verb
	// the server answered with a non-zero code, with what it sent and the refusal. The doctor
	// reads it, never a member's log (docs/SPEC-SPRINT.md, "The doctor's refused-verb and
	// beat-form checks").
	Refused []sprint.RefusedVerb
	Stops   StopsRecord
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
	// The beats are the tick's own read (fleetBeats above): the doctor's readers check
	// and ReadsOut mark a read card working on a row a ghost from the row's own beat, so
	// the snapshot the doctor reads must carry them (readCardGhost, live.go;
	// docs/SPEC-SPRINT.md section 6, the reads). nil beats leave every ghost live.
	snap.Beats = req.Beats
	out.Snap = snap
	if out.ReaderBeats, err = pinned.readerBeats(ctx, snap.Readers); err != nil {
		return DoctorRead{}, err
	}
	if out.FriendBeats, err = pinned.FriendBeats(ctx); err != nil {
		return DoctorRead{}, err
	}
	if out.Refused, err = pinned.refusedVerbs(ctx); err != nil {
		return DoctorRead{}, err
	}
	return out, nil
}

// keyRefusedRecord is the sprint's refused-verb record, keyed by epoch (the pinned backend's
// own epoch scope): the server appends each refused verb, the doctor reads the window.
const keyRefusedRecord = "refusedrecord"

// RecordRefused appends one refused verb to the epoch's refused-verb record, pruning the
// window (sprint.PruneRefused). It is the server's write, best effort: a record that does not
// read or write is not written, and the refusal's answer is never held back by it. The record
// is keyed by the sprint's current epoch, so a refusal of an old epoch is not counted again.
func (st *Store) RecordRefused(ctx context.Context, r sprint.RefusedVerb) error {
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	kv, err := pinned.kv()
	if err != nil {
		return err
	}
	var rec sprint.RefusedRecord
	if raw, ok, err := kv.GetKey(ctx, keyRefusedRecord); err != nil {
		return err
	} else if ok {
		// ignored: an unreadable record starts again, as a first refusal does
		_ = json.Unmarshal([]byte(raw), &rec)
	}
	if rec.Epoch != pinned.epoch {
		rec = sprint.RefusedRecord{Epoch: pinned.epoch}
	}
	r.Epoch = pinned.epoch
	rec.Verbs = append(rec.Verbs, r)
	rec.Verbs = sprint.PruneRefused(rec.Verbs, pinned.now())
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyRefusedRecord, string(b))
}

// refusedVerbs is the epoch's refused-verb record with its window pruned; nil when there is
// none, it does not read, or it is another epoch's.
func (st *Store) refusedVerbs(ctx context.Context) ([]sprint.RefusedVerb, error) {
	kv, err := st.kv()
	if err != nil {
		return nil, nil // ignored: a store that keeps no machine records keeps no refusals
	}
	raw, ok, err := kv.GetKey(ctx, keyRefusedRecord)
	if err != nil || !ok {
		return nil, err
	}
	var rec sprint.RefusedRecord
	if json.Unmarshal([]byte(raw), &rec) != nil || rec.Epoch != st.epoch {
		return nil, nil
	}
	return sprint.PruneRefused(rec.Verbs, st.now()), nil
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

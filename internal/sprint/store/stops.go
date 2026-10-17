package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/viewrowswire"
)

// The stops record (internal/sprint/stops.go; the owner, 2026-10-10: no silent stops and no
// silent waits): every automatic stop that holds, with when it began, and what waits on the
// seat (the open judgments, those past their deadline, the oldest, the escalation level),
// counted by the tick from the snapshot it reads anyway and kept in one key, so where (the
// dashboard) reads it in the exchange it makes already, and `nova-sprint doctor` lists it.

// keyStops is the stops record's key, under the deployment's prefix.
const keyStops = "stops" // STRING, the stops record (JSON)

// StopsRefresh is how long the tick keeps a record that has not changed before it writes
// it again, so its At says the machine still counts it.
const StopsRefresh = time.Minute

// StopsRecord is the stops record: the epoch and the time it was counted at, what waits on
// the seat, and every automatic stop that holds.
type StopsRecord struct {
	Epoch uint64           `json:"epoch"`
	At    time.Time        `json:"at"`
	Seat  sprint.SeatWaits `json:"seat"`
	Stops []sprint.Stop    `json:"stops"`
}

// stopsOf is the record of a snapshot at now, for the tick's request (its beats, friends
// and STOPPED spans).
func stopsOf(s *sprint.Snapshot, r sprint.TickReq, now time.Time) StopsRecord {
	stops := sprint.LiveStops(s, r)
	if stops == nil {
		stops = []sprint.Stop{}
	}
	return StopsRecord{Epoch: s.Epoch, At: now.UTC(), Seat: sprint.SeatWaitsOf(s, r), Stops: stops}
}

// keepStops writes the record when it differs from the last this process wrote, or when
// that one is StopsRefresh old: an idle tick writes nothing. A store that keeps no records
// keeps none.
func (st *Store) keepStops(ctx context.Context, tw *Twin, rec StopsRecord) error {
	kv, err := st.kv()
	if err != nil {
		return nil
	}
	body := rec
	body.At = time.Time{}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	tw.stopsMu.Lock()
	same := tw.stopsKept == string(b) && rec.At.Sub(tw.stopsAt) < StopsRefresh
	tw.stopsMu.Unlock()
	if same {
		return nil
	}
	full, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := kv.SetKey(ctx, keyStops, string(full)); err != nil {
		return err
	}
	tw.stopsMu.Lock()
	tw.stopsKept, tw.stopsAt = string(b), rec.At
	tw.stopsMu.Unlock()
	return nil
}

// stopsPage is one stops record decoded by the stops row reader (S1, S2).
type stopsPage struct {
	rec StopsRecord
}

// stopSchema is one automatic stop's schema JSON (S4).
type stopSchema struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Since   int64  `json:"since,omitempty"`
	What    string `json:"what"`
	Effect  string `json:"effect"`
	Undo    string `json:"undo"`
	Signal  string `json:"signal"`
}

// seatWaitsSchema is the seat's waits in schema JSON.
type seatWaitsSchema struct {
	Judgments  int64  `json:"judgments"`
	Overdue    int64  `json:"overdue"`
	OldestID   string `json:"oldest_id,omitempty"`
	OldestType string `json:"oldest_type,omitempty"`
	OldestAt   int64  `json:"oldest_at,omitempty"`
	Level      int64  `json:"level"`
}

// stopsSchema is the schema JSON of the stops record: times in Unix nanoseconds.
type stopsSchema struct {
	Epoch uint64          `json:"epoch"`
	At    int64           `json:"at"`
	Seat  seatWaitsSchema `json:"seat"`
	Stops []stopSchema    `json:"stops"`
}

func stopsToSchema(r StopsRecord) stopsSchema {
	return stopsSchema{Epoch: r.Epoch, At: rowTimeNanos(r.At),
		Seat: seatWaitsSchema{Judgments: int64(r.Seat.Judgments), Overdue: int64(r.Seat.Overdue),
			OldestID: r.Seat.OldestID, OldestType: r.Seat.OldestType,
			OldestAt: rowTimeNanos(r.Seat.OldestAt), Level: int64(r.Seat.Level)},
		Stops: stopsToSchemaStops(r.Stops)}
}

func stopsToSchemaStops(ss []sprint.Stop) []stopSchema {
	if ss == nil {
		return nil
	}
	out := make([]stopSchema, len(ss))
	for i, s := range ss {
		out[i] = stopSchema{Kind: s.Kind, Subject: s.Subject, Since: rowTimeNanos(s.Since),
			What: s.What, Effect: s.Effect, Undo: s.Undo, Signal: s.Signal}
	}
	return out
}

func stopsFromSchema(z stopsSchema) StopsRecord {
	return StopsRecord{Epoch: z.Epoch, At: doneTimeOf(z.At),
		Seat: sprint.SeatWaits{Judgments: int(z.Seat.Judgments), Overdue: int(z.Seat.Overdue),
			OldestID: z.Seat.OldestID, OldestType: z.Seat.OldestType,
			OldestAt: doneTimeOf(z.Seat.OldestAt), Level: int(z.Seat.Level)},
		Stops: stopsFromSchemaStops(z.Stops)}
}

func stopsFromSchemaStops(ss []stopSchema) []sprint.Stop {
	if ss == nil {
		return nil
	}
	out := make([]sprint.Stop, len(ss))
	for i, s := range ss {
		out[i] = sprint.Stop{Kind: s.Kind, Subject: s.Subject, Since: doneTimeOf(s.Since),
			What: s.What, Effect: s.Effect, Undo: s.Undo, Signal: s.Signal}
	}
	return out
}

// stopsRowFns adapts the variable table and today's legacy JSON reader to the store row codec (S1, S2).
func stopsRowFns() RowFns[stopsPage] {
	return RowFns[stopsPage]{
		Name: "STOPSRECORD", Forms: variableWireForms(),
		Reset:    func(v *stopsPage) { *v = stopsPage{} },
		LoadWire: stopsLoadWire, LoadJSON: stopsLoadJSON,
		Legacy:     legacyObject,
		LoadLegacy: stopsLoadLegacy,
		SaveWire:   func(v *stopsPage) []byte { return stopsSaveWireRec(v.rec) },
		SaveJSON:   func(v *stopsPage) []byte { return stopsSaveJSONRec(v.rec) },
	}
}

// stopsRows makes and registers the one stops reader on this Store (S4).
func (st *Store) stopsRows() *RowCodec[stopsPage] {
	if st == nil {
		return nil
	}
	lazyMu.Lock()
	defer lazyMu.Unlock()
	b := st.viewCodecsLocked()
	if b.stops == nil {
		b.stops = NewRowCodec(stopsRowFns())
	}
	b.stops.Register(st)
	return b.stops
}

// stopsLoadWire adapts the generated variable table to the stops record (S1, S2).
func stopsLoadWire(v *stopsPage, wire []byte) RowReport {
	size, err := viewrowswire.StopsRecordLoadMeasureReason(wire)
	if err != nil {
		return RowReport{Reason: err.Error()}
	}
	var rep viewrowswire.TableReport
	out := viewrowswire.StopsRecordLoad(make([]byte, size), wire, &rep)
	if out == nil {
		return tableReportRow(rep)
	}
	buf := make([]byte, viewrowswire.StopsRecordToJsonMeasure(out))
	n := viewrowswire.StopsRecordToJson(out, buf)
	var z stopsSchema
	if err := json.Unmarshal(buf[:n], &z); err != nil {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	v.rec = stopsFromSchema(z)
	return RowReport{}
}

// stopsLoadJSON adapts the generated schema JSON form to the stops record (S2).
func stopsLoadJSON(v *stopsPage, text []byte) RowReport {
	var builder viewrowswire.StopsRecordBuilder
	if !builder.Init() {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.StopsRecordFromJson(&builder, text, &rep) {
		return tableReportRow(rep)
	}
	root := builder.GetRoot()
	buf := make([]byte, viewrowswire.StopsRecordMeasure(root, &builder.Arena))
	n := viewrowswire.StopsRecordSave(root, buf, &builder.Arena)
	if n < 0 {
		return RowReport{Malformed: true, Reason: "malformed"}
	}
	return stopsLoadWire(v, buf[:n])
}

// stopsSaveWire writes the stops record as the variable table wire (S2).
func stopsSaveWireRec(r StopsRecord) []byte {
	b, err := json.Marshal(stopsToSchema(r))
	if err != nil {
		return nil
	}
	var builder viewrowswire.StopsRecordBuilder
	if !builder.Init() {
		return nil
	}
	var rep viewrowswire.TableReport
	if !viewrowswire.StopsRecordFromJson(&builder, b, &rep) {
		return nil
	}
	root := builder.GetRoot()
	size := viewrowswire.StopsRecordMeasure(root, &builder.Arena)
	if size < 0 {
		return nil
	}
	buf := make([]byte, size)
	n := viewrowswire.StopsRecordSave(root, buf, &builder.Arena)
	if n < 0 {
		return nil
	}
	return buf[:n]
}

// stopsSaveJSON writes the stops record as the schema JSON text (S2).
func stopsSaveJSONRec(r StopsRecord) []byte {
	b, err := json.Marshal(stopsToSchema(r))
	if err != nil {
		return nil
	}
	return b
}

// stopsLoadLegacy keeps today's JSON stops record decoder (S2, S4).
func stopsLoadLegacy(v *stopsPage, raw []byte) RowReport {
	if err := json.Unmarshal(raw, &v.rec); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	return RowReport{}
}

// readStops is the stops record as stored; false when there is none or it does not read.
func readStops(raw string, ok bool) (StopsRecord, bool) {
	var r StopsRecord
	if !ok || json.Unmarshal([]byte(raw), &r) != nil {
		return StopsRecord{}, false
	}
	return r, true
}

// StopsKept is the stops record the tick last wrote, when it is of the pinned epoch; false
// when there is none (before the first tick, after a clear, or a store that keeps no records).
func (st *Store) StopsKept(ctx context.Context) (StopsRecord, bool, error) {
	kv, err := st.kv()
	if err != nil {
		return StopsRecord{}, false, nil
	}
	v, ok, err := kv.GetKey(ctx, keyStops)
	if err != nil {
		return StopsRecord{}, false, err
	}
	if !ok {
		return StopsRecord{}, false, nil
	}
	page, _, err := st.stopsRows().Read([]byte(v))
	if err != nil {
		return StopsRecord{}, false, nil
	}
	r := page.rec
	return r, r.Epoch == st.epoch, nil
}

// StopsNow is the stops record counted now from a fresh read of the sprint, as the tick
// counts it (the members' beats, the friends' seats, the machine's STOPPED spans): what
// `nova-sprint doctor` lists, whether or not the machine ticks.
func (st *Store) StopsNow(ctx context.Context) (StopsRecord, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return StopsRecord{}, err
	}
	_, shapes, err := pinned.look(ctx)
	if err != nil {
		return StopsRecord{}, err
	}
	_, beats, err := pinned.fleetBeats(ctx, shapes)
	if err != nil {
		return StopsRecord{}, err
	}
	snap, _, err := pinned.Fenced(withBudget(ctx), All, tickExtras, nil)
	if err != nil {
		return StopsRecord{}, err
	}
	now := pinned.now()
	m, _, err := pinned.Machine(ctx)
	if err != nil {
		return StopsRecord{}, err
	}
	req := sprint.TickReq{Who: sprint.MachineActor, Stopped: m.StoppedBetween, Beats: beats}
	if req.Friends, err = pinned.friendSeats(ctx, snap, now); err != nil {
		return StopsRecord{}, err
	}
	return stopsOf(snap, req, now), nil
}

// Tell writes one happened note addressed to the coordinator, in a notes-only step of the
// machine's, so the server sends it the coordinator on the bus: what a verb that runs outside the
// tick (promote) found and the seat must hear of. typ is the note's type, what its text and
// hint the verb to run.
func (st *Store) Tell(ctx context.Context, verb, typ, what, hint string) error {
	return st.tellTick(ctx, verb, typ, what, hint)
}

package store

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
)

// The coordinator's seat (docs/SPEC-SPRINT.md, "Handing over the seat"): the
// coordinator key names the holder; keySeat records the last change of it,
// written by the seat step's commit with the coordinator key, its log line and
// its note (Redis commit, Mem Release), so the seat never moves unrecorded,
// and carries the seat's generation (sprint.FirstSeatGeneration while there
// is no record). keyOwner names the sprint's owner, whose name a take
// carries. All are the sprint's, kept by a clear, and teardown removes them.
const (
	keySeat  = "seat"
	keyOwner = "owner"
)

// Seat is the last change of the seat, ok false when the seat has not moved
// since init.
func (st *Store) Seat(ctx context.Context) (sprint.SeatChange, bool, error) {
	var s sprint.SeatChange
	if _, ok := st.B.(KV); !ok {
		return s, false, nil // a backend that keeps no keys has no seat record
	}
	if err := st.getJSON(ctx, keySeat, &s); err != nil {
		return s, false, err
	}
	return s, s.Holder != "", nil
}

// SeatState is the seat as the friends' daemons read it every second
// (nova-sprint seat): the holder, the sprint's epoch and the seat's
// generation, from three keys and no table.
type SeatState struct {
	Holder     string `json:"holder"`
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
}

// SeatState reads the seat: the coordinator key, the seat record and the epoch.
func (st *Store) SeatState(ctx context.Context) (SeatState, error) {
	var s SeatState
	var err error
	if s.Holder, err = st.B.Coordinator(ctx); err != nil {
		return s, err
	}
	if s.Generation, err = st.seatGeneration(ctx); err != nil {
		return s, err
	}
	es, err := st.EpochNow(ctx)
	s.Epoch = es.N
	return s, err
}

// SeatCheck is the seat state with what the key must agree with
// (seat-key-follows-record.w2): Record is the holder the seat's record names
// ("" while the seat has not moved since init, init's key being its record);
// Server is the actor the run loop runs as ("" when no server's record is
// fresh); Drift is how the key (Holder), the record and the server disagree
// (sprint.SeatDrift), "" when they do not.
type SeatCheck struct {
	SeatState
	Record string `json:"record"`
	Server string `json:"server"`
	Drift  string `json:"drift,omitempty"`
}

// SeatCheck reads the seat state, the seat's record and the server's record.
func (st *Store) SeatCheck(ctx context.Context) (SeatCheck, error) {
	var s SeatCheck
	var err error
	if s.SeatState, err = st.SeatState(ctx); err != nil {
		return s, err
	}
	rec, ok, err := st.Seat(ctx)
	if err != nil {
		return s, err
	}
	if ok {
		s.Record = rec.Holder
	}
	if s.Server, err = st.ServerActor(ctx); err != nil {
		return s, err
	}
	s.Drift = sprint.SeatDrift(s.Holder, s.Record, s.Server)
	return s, nil
}

// InitSeat is init's write of the coordinator key (docs/SPEC-SPRINT.md,
// "Handing over the seat", seat-key-follows-record.w5): the seat record's
// holder when the seat has a record; else, over a key that already holds the
// coordinator, that holder (init does not change the coordinator); else first,
// the name init was given (--coordinator, else its actor). A fresh init writes
// no seat record. Friend liveness and its hook record do not affect this choice.
// The run loop never writes the key; init and the seat's steps do.
func (st *Store) InitSeat(ctx context.Context, first string) (string, error) {
	rec, ok, err := st.Seat(ctx)
	if err != nil {
		return "", err
	}
	if ok {
		return rec.Holder, st.B.SetCoordinator(ctx, rec.Holder)
	}
	coord, err := st.B.Coordinator(ctx)
	if err != nil {
		return "", err
	}
	if coord != "" {
		return coord, st.B.SetCoordinator(ctx, coord)
	}
	return first, st.B.SetCoordinator(ctx, first)
}

// SeatRepairStep writes the coordinator key from the seat's record, read now
// (sprint.RepairSeat): the record's holder or the owner repairs it, with a
// reason, logged with who and why. A seat with no record, or whose key says
// what its record does, is refused with nothing written.
func (st *Store) SeatRepairStep(ctx context.Context, r sprint.SeatRepairReq) (Step, string, error) {
	key, err := st.B.Coordinator(ctx)
	if err != nil {
		return Step{}, "", err
	}
	rec, ok, err := st.Seat(ctx)
	if err != nil {
		return Step{}, "", err
	}
	if why := sprint.NotSeatRepair(key, rec, ok, r); why != "" {
		return Step{}, why, nil
	}
	return Step{Named: true, Args: ArgsOf(r), Verb: "seat",
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.RepairSeat(s, rec, r) }}, "", nil
}

// keyServer is the server's record: the actor the run loop runs as, when it
// last said so, the version it runs (its line's second field), its source
// commit, when it started, its landing mode and its process (ServerRecord).
// The run loop writes it at its start and every ServerEvery, for
// ServerTTL on a store that expires keys, so a torn-down sprint keeps none past
// it; it is read as the server's while it is no older than ServerTTL. The
// record is the server's heartbeat: the deployment's one key (keyServer's name
// has no epoch part), not the sprint's, so a clear leaves it to its TTL and the
// server writes it again within ServerEvery either way, and a stopped server's
// version is stale within ServerTTL and so is neither read nor shown.
const keyServer = "server"

// ServerEvery is how often the run loop writes its record; ServerTTL is how
// long a record stands for a server. The record is the server's heartbeat: one
// SET with an expiry a second (ServerEvery), and the version where --json
// shows is absent once that record is older than ServerTTL.
const (
	ServerEvery = time.Second
	ServerTTL   = 3 * time.Second
)

// ServerRecord is the server's record as keyServer holds it: the actor the run
// loop runs as, when it last said so, the version it runs and its source commit
// (the same string nova-sprint version prints and the vcs commit it was built
// from, so the dashboard states the server's own version), when the server
// started, whether it lands (run --land), and the process that wrote it, its
// pid on its host, so a land by hand can tell the server from itself
// (docs/fixes.sexp, land-fenced-while-server-lands: the server-record fence,
// the clone lock's other half of one land at a time; the fence's reader is
// cmd/nova-sprint's serverLanding). A record written before the server said its
// landing mode reads as not landing, and its version and start are absent.
type ServerRecord struct {
	Actor   string    `json:"actor"`
	At      time.Time `json:"at"`
	Version string    `json:"version,omitempty"`
	Commit  string    `json:"commit,omitempty"`
	Started time.Time `json:"started,omitempty"`
	Land    bool      `json:"land,omitempty"`
	PID     int       `json:"pid,omitempty"`
	Host    string    `json:"host,omitempty"`
}

// expiringKV is a backend whose keys can expire (Redis): the server's record is
// written for ServerTTL. A KV without it keeps the record until it is written
// again, and the reader judges it by its time.
type expiringKV interface {
	SetKeyFor(ctx context.Context, name, value string, ttl time.Duration) error
}

// SetServerActor records the actor the run loop runs as, at the clock's
// reading, and nothing of its landing or its process.
func (st *Store) SetServerActor(ctx context.Context, actor string) error {
	return st.SetServer(ctx, ServerRecord{Actor: actor})
}

// SetServer writes the server's record r, its At the clock's reading: the
// server's report of itself, which seat, handover and land read. It writes
// nothing else, and never the coordinator key.
func (st *Store) SetServer(ctx context.Context, r ServerRecord) error {
	kv, ok := st.B.(KV)
	if !ok {
		return nil // a backend that keeps no keys has no server's record
	}
	r.At = st.now()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if x, ok := st.B.(expiringKV); ok {
		return x.SetKeyFor(ctx, keyServer, string(b), ServerTTL)
	}
	return kv.SetKey(ctx, keyServer, string(b))
}

// Server is the server's record, ok false with no record, or one older than
// ServerTTL.
func (st *Store) Server(ctx context.Context) (ServerRecord, bool, error) {
	var r ServerRecord
	kv, ok := st.B.(KV)
	if !ok {
		return r, false, nil
	}
	raw, ok, err := kv.GetKey(ctx, keyServer)
	if err != nil || !ok {
		return r, false, err
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return r, false, err
	}
	if st.now().Sub(r.At) > ServerTTL {
		return ServerRecord{}, false, nil
	}
	return r, true, nil
}

// SeatAndServer reads the seat's record and the server's heartbeat in one
// exchange where the store reads many keys in one (getKeys): where --json
// carries both under a fixed round-trip budget, so the server's record must not
// cost a trip of its own. Its answers are exactly Seat's (the seat moved) and
// Server's (the server is fresh, no older than ServerTTL).
func (st *Store) SeatAndServer(ctx context.Context) (sprint.SeatChange, bool, ServerRecord, bool, error) {
	seat, moved, server, fresh, _, err := st.SeatServerCoordinator(ctx)
	return seat, moved, server, fresh, err
}

// SeatServerCoordinator reads the seat, server and coordinator keys in one
// MGET on Redis. This funds where's decision-index read without another serial
// coordinator GET; no backend call count is hidden or rewritten.
func (st *Store) SeatServerCoordinator(ctx context.Context) (sprint.SeatChange, bool, ServerRecord, bool, string, error) {
	var s sprint.SeatChange
	var r ServerRecord
	kv, ok := st.B.(KV)
	if !ok {
		coordinator, err := st.B.Coordinator(ctx)
		return s, false, r, false, coordinator, err // a backend without keys keeps no seat/server records
	}
	vals, oks, err := getKeys(ctx, kv, []string{keySeat, keyServer, keyCoordinator})
	if err != nil {
		return s, false, r, false, "", err
	}
	if oks[0] {
		seat, _, err := st.seatRows().seat.Read([]byte(vals[0]))
		if err != nil {
			return s, false, r, false, "", err // the refusal names the seat record by name (S2)
		}
		s = seat
	}
	fresh := false
	if oks[1] {
		if err := json.Unmarshal([]byte(vals[1]), &r); err != nil {
			return s, false, r, false, "", err
		}
		if fresh = st.now().Sub(r.At) <= ServerTTL; !fresh {
			r = ServerRecord{}
		}
	}
	return s, s.Holder != "", r, fresh, vals[2], nil
}

// ServerActor is the actor the server runs as, by its record: "" with no
// record, or one older than ServerTTL.
func (st *Store) ServerActor(ctx context.Context) (string, error) {
	r, ok, err := st.Server(ctx)
	if err != nil || !ok {
		return "", err
	}
	return r.Actor, nil
}

// seatGeneration is the seat's generation as the seat record holds it:
// FirstSeatGeneration with no record (the seat has not moved since init, or
// the backend keeps no keys).
func (st *Store) seatGeneration(ctx context.Context) (uint64, error) {
	kv, ok := st.B.(KV)
	if !ok {
		return sprint.FirstSeatGeneration, nil
	}
	raw, ok, err := kv.GetKey(ctx, keySeat)
	if err != nil {
		return 0, err
	}
	return seatGenerationOf(raw, ok)
}

// seatGenerationOf is the generation the seat record's bytes carry, raw read
// with ok as the key read: FirstSeatGeneration for no record, and for a record
// written before the seat carried one.
func seatGenerationOf(raw string, ok bool) (uint64, error) {
	if !ok {
		return sprint.FirstSeatGeneration, nil
	}
	var s sprint.SeatChange
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return 0, err
	}
	return max(s.Generation, sprint.FirstSeatGeneration), nil
}

// The seat change record reads through the store row codec (S1, S2, S4) from
// the generated variable table of friendrowswire (schemas/friendrows/friendrows.schema):
// the table wire (form byte 1; a unit that holds a variable table emits no fixed form),
// the padded standard base64 of that wire and the schema JSON text, and today's legacy
// JSON. A damaged value is refused by name, and every read is counted under the form it
// was read in. Writers still write the legacy form; the writer switch is a later card.

// seatRowSets is each store's seat reader, keyed by the store's done reader: every pinned
// copy of a store shares that one reader (Store.clone), so a store and its copies count
// into one set (S4).
var seatRowSets sync.Map

// seatRowCodecs is one store's reader of the seat change record.
type seatRowCodecs struct {
	seat *RowCodec[sprint.SeatChange]
}

// seatRows is this store's reader of the seat change record, registered on it so its Check
// reports the reader's per-form counts (S4).
func (st *Store) seatRows() *seatRowCodecs {
	key := st.doneRows()
	v, ok := seatRowSets.Load(key)
	if !ok {
		v, _ = seatRowSets.LoadOrStore(key, &seatRowCodecs{
			seat: NewRowCodec(seatRowFns()),
		})
	}
	c := v.(*seatRowCodecs)
	c.seat.Register(st)
	return c
}

// seatSchema is the SeatChange table's schema JSON text (friendrows.schema SeatChange).
type seatSchema struct {
	Holder     string  `json:"holder"`
	Generation uint64  `json:"generation"`
	From       string  `json:"from"`
	At         *string `json:"at"`
	By         string  `json:"by"`
	Taken      bool    `json:"taken"`
	ApprovedBy string  `json:"approved_by"`
	Reason     string  `json:"reason"`
}

// seatTable is the generated SeatChange table (S1, S2).
func seatTable() friendTable[friendrowswire.SeatChange] {
	return friendTable[friendrowswire.SeatChange]{
		measure: friendrowswire.SeatChangeLoadMeasureReason,
		load:    friendrowswire.SeatChangeLoad,
		fromJSON: func(text []byte, rep *friendrowswire.TableReport) (*friendrowswire.SeatChange, bool) {
			var b friendrowswire.SeatChangeBuilder
			if !b.Init() {
				return nil, false
			}
			if !friendrowswire.SeatChangeFromJson(&b, text, rep) || !b.Lock() {
				return nil, false
			}
			return b.AsConst(), true
		},
		toJSON: func(w *friendrowswire.SeatChange) []byte {
			buf := make([]byte, max(friendrowswire.SeatChangeToJsonMeasure(w), 0))
			return buf[:max(friendrowswire.SeatChangeToJson(w, buf), 0)]
		},
		save: func(w *friendrowswire.SeatChange) []byte {
			buf := make([]byte, max(friendrowswire.SeatChangeMeasure(w), 0))
			return buf[:max(friendrowswire.SeatChangeSave(w, buf), 0)]
		},
	}
}

// seatRowFns is the seat change record (sprint.SeatChange) as the row reader reads it:
// SEATCHANGE (S1, S2, S4).
func seatRowFns() RowFns[sprint.SeatChange] {
	return presenceRowFns("SEATCHANGE", seatTable(), seatToSchema, seatOfSchema)
}

func seatToSchema(s *sprint.SeatChange) seatSchema {
	return seatSchema{
		Holder: s.Holder, Generation: s.Generation, From: s.From,
		At: rowTime(s.At), By: s.By, Taken: s.Taken, ApprovedBy: s.ApprovedBy, Reason: s.Reason,
	}
}

func seatOfSchema(m *seatSchema) (sprint.SeatChange, bool) {
	at, ok := rowTimeOf(m.At)
	return sprint.SeatChange{
		Holder: m.Holder, Generation: m.Generation, From: m.From,
		At: at, By: m.By, Taken: m.Taken, ApprovedBy: m.ApprovedBy, Reason: m.Reason,
	}, ok
}

// Owner is the sprint's owner as init recorded it, "" when none was.
func (st *Store) Owner(ctx context.Context) (string, error) {
	kv, ok := st.B.(KV)
	if !ok {
		return "", nil
	}
	v, _, err := kv.GetKey(ctx, keyOwner)
	return v, err
}

// SetOwner records the sprint's owner. It is refused when the backend keeps no
// keys.
func (st *Store) SetOwner(ctx context.Context, name string) error {
	kv, ok := st.B.(KV)
	if !ok {
		return errNoKeys
	}
	return kv.SetKey(ctx, keyOwner, name)
}

// SeatStep moves the seat (sprint.MoveSeat): its commit writes the coordinator
// key and the seat's record with the step's note and log line.
func SeatStep(r sprint.SeatReq) Step {
	return Step{Named: true, Args: ArgsOf(r), Verb: "coordinator",
		Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.MoveSeat(s, r) }}
}

// seatRecord is the seat change as the seat key holds it.
func seatRecord(c *sprint.SeatChange) (string, error) {
	b, err := json.Marshal(c)
	return string(b), err
}

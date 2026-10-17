package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/oprowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// Redis is the Backend of a Redis store holding the table layer's function
// library: the tables through pkg/ntable, in process; the operation
// records and the notifications under the deployment's prefix.
type Redis struct {
	C     redis.UniversalClient
	Names sprint.Names
	Now   func() time.Time
	// Pinned is the sprint epoch the backend is pinned to: the sprint keys it
	// names and the epoch its writes carry. Old reads the tables at that epoch
	// as it was, not the active one.
	Pinned uint64
	Old    bool
	// trips counts the client's round trips, once CountTrips is called
	// (stats.go); the backend's pinned copies share it.
	trips *atomic.Int64
	// beforeExec, set by a functional test only, runs inside a conditional
	// delete's transaction after its read and before its EXEC: the seam a test
	// writes another writer's change through (RowsDelIf).
	beforeExec func()
}

func (r *Redis) key(name string) string { return r.Names.KeyAt(name, r.Pinned) }

// AtEpoch is the backend pinned to an epoch.
func (r *Redis) AtEpoch(epoch uint64, old bool) Backend {
	c := *r
	c.Pinned, c.Old = epoch, old
	return &c
}

func (r *Redis) writeOpts() ntable.WriteOptions { return ntable.WriteOptions{Epoch: r.Pinned} }

// Epoch reads the sprint's epoch hash in one exchange.
func (r *Redis) Epoch(ctx context.Context) (EpochState, error) {
	vals, err := r.C.HMGet(ctx, r.Names.EpochKey(), "n", "cleared", "restore", "defaults").Result()
	if err != nil {
		return EpochState{}, err
	}
	var es EpochState
	if v, ok := vals[0].(string); ok {
		if es.N, err = strconv.ParseUint(v, 10, 64); err != nil {
			return es, fmt.Errorf("the sprint's epoch %q is not a number", v)
		}
	}
	if v, ok := vals[1].(string); ok {
		es.Cleared, _ = time.Parse(time.RFC3339Nano, v)
	}
	if v, ok := vals[2].(string); ok && v != "" {
		es.Owed = true
	}
	// the set-defaults owed flag names the epoch that owes it: one naming another epoch
	// (an older binary advanced past it without writing its own) is not this epoch's
	if v, ok := vals[3].(string); ok && es.N > 0 && v == strconv.FormatUint(es.N, 10) {
		es.DefaultsOwed = true
	}
	return es, nil
}

// AdvanceEpoch is WATCH on the epoch hash, then one MULTI/EXEC; the hash's
// restore field names the epoch whose shape the new one owes, and its defaults
// field the new epoch, owed its set-defaults step (EpochState.DefaultsOwed), in
// the same write.
func (r *Redis) AdvanceEpoch(ctx context.Context, from uint64, at time.Time, owner ...string) (bool, error) {
	key := r.Names.EpochKey()
	fence := r.Names.KeyAt(keyFence, from)
	id := ""
	if len(owner) > 0 {
		id = owner[0]
	}
	moved := false
	err := r.C.Watch(ctx, func(tx *redis.Tx) error {
		v, err := tx.HGet(ctx, key, "n").Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		n := uint64(0)
		if v != "" {
			n, _ = strconv.ParseUint(v, 10, 64)
		}
		if n != from {
			moved = true
			return nil
		}
		body, err := tx.Get(ctx, fence).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		// the holder's id, read through HELDLOCK in any form; a damaged
		// value is refused by name, never read as an empty fence (S2, C13)
		held := ""
		if body != "" {
			if held, err = opRows.heldID(body); err != nil {
				return err
			}
		}
		if held != id {
			moved = true
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.HSet(ctx, key, "n", strconv.FormatUint(from+1, 10), "cleared", at.UTC().Format(time.RFC3339Nano), "restore", strconv.FormatUint(from, 10),
				"defaults", strconv.FormatUint(from+1, 10))
			return nil
		})
		return err
	}, key, fence)
	if errors.Is(err, redis.TxFailedErr) || moved {
		return false, nil
	}
	return err == nil, err
}

// SettleEpoch removes the restore owed at epoch n: WATCH on the epoch hash,
// then one MULTI/EXEC, only while the sprint is at n.
func (r *Redis) SettleEpoch(ctx context.Context, n uint64) error {
	key := r.Names.EpochKey()
	err := r.C.Watch(ctx, func(tx *redis.Tx) error {
		v, err := tx.HGet(ctx, key, "n").Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		if v != strconv.FormatUint(n, 10) {
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.HDel(ctx, key, "restore")
			return nil
		})
		return err
	}, key)
	if errors.Is(err, redis.TxFailedErr) {
		// the epoch hash moved: another clear advanced it, or settled it
		return nil
	}
	return err
}

// SettleDefaults removes the set-defaults step owed at epoch n: WATCH on the
// epoch hash, then one MULTI/EXEC, only while the sprint is at n and the flag
// names n.
func (r *Redis) SettleDefaults(ctx context.Context, n uint64) error {
	key := r.Names.EpochKey()
	at := strconv.FormatUint(n, 10)
	err := r.C.Watch(ctx, func(tx *redis.Tx) error {
		vals, err := tx.HMGet(ctx, key, "n", "defaults").Result()
		if err != nil {
			return err
		}
		if v, _ := vals[0].(string); v != at {
			return nil
		}
		if v, _ := vals[1].(string); v != at {
			return nil
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.HDel(ctx, key, "defaults")
			return nil
		})
		return err
	}, key)
	if errors.Is(err, redis.TxFailedErr) {
		return fmt.Errorf("the sprint's epoch moved as the set-defaults step of epoch %d was settled; run: nova-sprint clear again", n)
	}
	return err
}

// Shapes reads every table in one pipeline; pinned to an old epoch, each at
// that epoch.
func (r *Redis) Shapes(ctx context.Context, tables []string) ([]ntable.Table, error) {
	if r.Old {
		out := make([]ntable.Table, len(tables))
		for i, t := range tables {
			s, err := ntable.ReadAt(ctx, r.C, t, r.Pinned)
			if err != nil {
				return nil, err
			}
			out[i] = s
		}
		return out, nil
	}
	pipe := r.C.Pipeline()
	cmds := make([]*ntable.ReadCmd, len(tables))
	for i, t := range tables {
		cmds[i] = ntable.NewReader(t).Queue(ctx, pipe)
	}
	if _, err := pipe.Exec(ctx); err != nil && !isReply(err) {
		return nil, err
	}
	out := make([]ntable.Table, len(tables))
	for i, cmd := range cmds {
		t, _, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

func isReply(err error) bool {
	var re redis.Error
	return errors.As(err, &re)
}

// CellIDs reads every owned set cell's members in one pipeline, through the
// table layer's cell reader: each set column is read as a members column.
func (r *Redis) CellIDs(ctx context.Context, shapes []ntable.Table) (map[string][]string, error) {
	pipe := r.C.Pipeline()
	views := make([]ntable.Table, len(shapes))
	cmds := make([]*ntable.CellsCmd, len(shapes))
	for i, t := range shapes {
		v := t
		v.Columns = append([]ntable.Column(nil), t.Columns...)
		for j := range v.Columns {
			if v.Columns[j].HasSet() {
				v.Columns[j].Projection = ntable.Members
			}
		}
		v.Rows = make([]ntable.Row, len(t.Rows))
		for k, row := range t.Rows {
			row.Cells = append([]ntable.Cell(nil), row.Cells...)
			v.Rows[k] = row
		}
		views[i] = v
		cmds[i] = ntable.QueueCells(ctx, pipe, &views[i])
	}
	if _, err := pipe.Exec(ctx); err != nil && !isReply(err) {
		return nil, err
	}
	out := map[string][]string{}
	for i, cmd := range cmds {
		cmd.Result()
		t := views[i]
		for _, row := range t.Rows {
			for j, c := range t.Columns {
				if !c.HasSet() || j >= len(row.Cells) {
					continue
				}
				cell := row.Cells[j]
				if cell.Unread {
					return nil, fmt.Errorf("table %s row %s column %s did not come back: %s", t.Name, row.Key, c.Name, cell.UnreadWhy)
				}
				for _, m := range cell.Members {
					out[t.Name] = append(out[t.Name], m.Member)
				}
			}
		}
	}
	return out, nil
}

func (r *Redis) ReadSet(ctx context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	if r.Old {
		return ntable.ReadSetMembers(ctx, r.C, table, ids, r.Pinned)
	}
	return ntable.ReadSetMembers(ctx, r.C, table, ids)
}

func (r *Redis) Apply(ctx context.Context, m ntable.BatchManifest) (ntable.Receipt, error) {
	return ntable.ApplyBatch(ctx, r.C, m)
}

// ApplyAll applies manifests of one table in one round trip (one
// MULTI/EXEC): each its own batch, as Apply applies it.
func (r *Redis) ApplyAll(ctx context.Context, ms []ntable.BatchManifest) ([]ntable.Receipt, []error) {
	return ntable.ApplyBatches(ctx, r.C, ms)
}

var _ BatchApplier = (*Redis)(nil)

func (r *Redis) Create(ctx context.Context, t ntable.Table) error {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	return ntable.Create(ctx, r.C, t, now())
}

func (r *Redis) RowsAdd(ctx context.Context, table string, rows []string) error {
	_, err := ntable.RowsAdd(ctx, r.C, table, rows, r.writeOpts())
	return err
}

func (r *Redis) RowsHide(ctx context.Context, table string, rows []string) error {
	_, err := ntable.RowsHide(ctx, r.C, table, true, rows, r.writeOpts())
	return err
}

func (r *Redis) RowsShow(ctx context.Context, table string, rows []string) error {
	_, err := ntable.RowsHide(ctx, r.C, table, false, rows, r.writeOpts())
	return err
}

// RowsDel removes each row through the table layer's row delete, one write
// each; the caller checked that none holds a card but a stream's control
// card.
func (r *Redis) RowsDel(ctx context.Context, table string, rows []string) error {
	for _, row := range rows {
		if _, err := ntable.RowDel(ctx, r.C, table, row, r.writeOpts()); err != nil {
			return err
		}
	}
	return nil
}

// RowsDelIf removes the rows of the guards that hold, in one optimistic
// transaction for every guard: WATCH on each guard's record (its Key, where the
// table layer writes it) and on the table's rows, the records read (on no cell,
// at the guard's revision), and every passing row's delete sent through the
// table layer's row delete in one MULTI/EXEC. Every write of a record (a batch,
// a cell add) and every row added changes a watched key, so a member placed
// again after the read aborts the EXEC, and the read is made again (up to
// condTries times; past that nothing is deleted and the caller's next run
// tries again).
func (r *Redis) RowsDelIf(ctx context.Context, table string, guards []RowGuard) ([]string, error) {
	// the restore state (the epoch hash and the table's properties) is watched and read in
	// the same optimistic transaction: no member row is deleted while the restore is pending
	g := verbGuard(table)
	rows, o, err := r.delIf(ctx, table, guards, true, &g, func(p redis.Pipeliner, rg RowGuard, body string) {
		p.FCall(ctx, ntable.FnRowDel, []string{ntable.DefKey(table)}, table, rg.Row, body)
	})
	if err == nil && o != GuardApplied {
		return nil, &GuardError{Outcome: o}
	}
	return rows, err
}

// restoreOutcome reads the restore state inside a WATCH: the epoch hash's n and defaults
// fields and the marker on the marker table's properties at the pinned epoch.
func (r *Redis) restoreOutcome(ctx context.Context, tx *redis.Tx, g RestoreGuard) (GuardOutcome, error) {
	vals, err := tx.HMGet(ctx, r.Names.EpochKey(), "n", "defaults").Result()
	if err != nil {
		return GuardApplied, err
	}
	at := strconv.FormatUint(r.Pinned, 10)
	n, _ := vals[0].(string)
	if n == "" {
		n = "0"
	}
	flag, _ := vals[1].(string)
	marked, err := tx.HExists(ctx, ntable.PropsKeyAt(g.MarkerTable, r.Pinned), g.Marker).Result()
	if err != nil {
		return GuardApplied, err
	}
	owed := r.Pinned > 0 && flag == at
	return guardOutcome(n == at, owed && !marked, g), nil
}

// GuardedRows is WATCH on the epoch hash and the marker table's properties, the restore
// state read, then every write sent through the table layer's own row verb in one
// MULTI/EXEC; a change of a watched key aborts the EXEC and the state is read again.
func (r *Redis) GuardedRows(ctx context.Context, g RestoreGuard, writes []RowWrite) (GuardOutcome, error) {
	options, err := r.optionsBody()
	if err != nil {
		return GuardApplied, err
	}
	watch := []string{r.Names.EpochKey(), ntable.PropsKeyAt(g.MarkerTable, r.Pinned)}
	for try := 0; try < condTries; try++ {
		out := GuardApplied
		var cmds []redis.Cmder
		err := r.C.Watch(ctx, func(tx *redis.Tx) error {
			o, err := r.restoreOutcome(ctx, tx, g)
			if err != nil {
				return err
			}
			if o != GuardApplied {
				out = o
				return nil
			}
			if r.beforeExec != nil {
				r.beforeExec()
			}
			cmds, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				for _, w := range writes {
					if err := queueRowWrite(ctx, p, w, options); err != nil {
						return err
					}
				}
				return nil
			})
			return err
		}, watch...)
		if errors.Is(err, redis.TxFailedErr) {
			continue // the epoch, its flag or the marker changed: read again
		}
		if err != nil {
			return GuardApplied, err
		}
		for _, c := range cmds {
			if fc, ok := c.(*redis.Cmd); ok {
				if reply, err := fc.Slice(); err == nil && len(reply) > 0 && fmt.Sprint(reply[0]) == "REFUSED" {
					return GuardApplied, fmt.Errorf("the table layer refused a guarded row write: %v", reply)
				}
			}
		}
		return out, nil
	}
	return GuardApplied, fmt.Errorf("the sprint's epoch or its restore's marker kept changing under a guarded row write (%d tries); nothing was changed; run it again", condTries)
}

// queueRowWrite queues one row write through the table layer's row verb, as ntable's own
// RowsAdd, RowSet and RowDel send it.
func queueRowWrite(ctx context.Context, p redis.Pipeliner, w RowWrite, options string) error {
	keys := []string{ntable.DefKey(w.Table)}
	switch w.Op {
	case "add":
		body, err := json.Marshal(struct {
			Rows []string       `json:"rows"`
			Spec ntable.RowSpec `json:"spec"`
		}{w.Rows, ntable.RowSpec{}})
		if err != nil {
			return err
		}
		p.FCall(ctx, ntable.FnRowsAdd, keys, w.Table, string(body), options)
	case "set":
		body, err := json.Marshal(w.Texts)
		if err != nil {
			return err
		}
		p.FCall(ctx, ntable.FnRowSet, keys, w.Table, w.Rows[0], string(body), options)
	case "del":
		for _, row := range w.Rows {
			p.FCall(ctx, ntable.FnRowDel, keys, w.Table, row, options)
		}
	default:
		return fmt.Errorf("a guarded row write %q is no row verb", w.Op)
	}
	return nil
}

// optionsBody is the write options every table-layer write carries: the pinned epoch.
func (r *Redis) optionsBody() (string, error) {
	body, err := json.Marshal(struct {
		Epoch string `json:"epoch"`
		Actor string `json:"actor"`
		Fence string `json:"fence"`
		Idem  string `json:"idem"`
	}{Epoch: strconv.FormatUint(r.Pinned, 10)})
	return string(body), err
}

// KeysDelIf deletes the keys of the guards that hold, whose rows are not in the
// table, in one optimistic transaction as RowsDelIf: a row added or a record
// placed again after the read aborts it.
func (r *Redis) KeysDelIf(ctx context.Context, table string, guards []RowGuard) ([]string, error) {
	rows, _, err := r.delIf(ctx, table, guards, false, nil, func(p redis.Pipeliner, g RowGuard, _ string) {
		if len(g.Keys) > 0 {
			p.Del(ctx, g.Keys...)
		}
	})
	return rows, err
}

// condTries is how many times a conditional delete reads again after another
// writer changed a watched key.
const condTries = 3

// delIf is the one transaction of RowsDelIf and KeysDelIf: rowPresent says the
// guard's row must be in the table (a row delete) or must not be (a key delete). With rg,
// the restore state is watched and read in the same transaction, and nothing is deleted
// unless it is the state rg wants.
func (r *Redis) delIf(ctx context.Context, table string, guards []RowGuard, rowPresent bool, rg *RestoreGuard, op func(redis.Pipeliner, RowGuard, string)) ([]string, GuardOutcome, error) {
	if len(guards) == 0 {
		return nil, GuardApplied, nil
	}
	body, err := json.Marshal(struct {
		Epoch string `json:"epoch"`
		Actor string `json:"actor"`
		Fence string `json:"fence"`
		Idem  string `json:"idem"`
	}{Epoch: strconv.FormatUint(r.Pinned, 10)})
	if err != nil {
		return nil, GuardApplied, err
	}
	watch := []string{ntable.RowsKeyAt(table, r.Pinned)}
	if rg != nil {
		watch = append(watch, r.Names.EpochKey(), ntable.PropsKeyAt(rg.MarkerTable, r.Pinned))
	}
	ids := make([]string, len(guards))
	for i, g := range guards {
		watch = append(watch, g.Key)
		ids[i] = g.ID
	}
	for try := 0; try < condTries; try++ {
		var pass []RowGuard
		var cmds []redis.Cmder
		out := GuardApplied
		err := r.C.Watch(ctx, func(tx *redis.Tx) error {
			pass = nil
			if rg != nil {
				o, err := r.restoreOutcome(ctx, tx, *rg)
				if err != nil {
					return err
				}
				if o != GuardApplied {
					out = o
					return nil
				}
			}
			shape, err := ntable.Shape(ctx, tx, table)
			if err != nil {
				return err
			}
			rows := map[string]bool{}
			for _, row := range shape.Rows {
				rows[row.Key] = true
			}
			recs := map[string]ntable.ReadSetMember{}
			for start := 0; start < len(ids); start += ntable.LimitReadSetMembers {
				rs, err := ntable.ReadSetMembers(ctx, tx, table, ids[start:min(start+ntable.LimitReadSetMembers, len(ids))])
				if err != nil {
					return err
				}
				for _, m := range rs.Members {
					recs[m.ID] = m
				}
			}
			for _, g := range guards {
				if m, ok := recs[g.ID]; ok && !m.Placed && m.Revision == g.Rev && rows[g.Row] == rowPresent {
					pass = append(pass, g)
				}
			}
			if len(pass) == 0 {
				return nil
			}
			if r.beforeExec != nil {
				r.beforeExec()
			}
			cmds, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				for _, g := range pass {
					op(p, g, string(body))
				}
				return nil
			})
			return err
		}, watch...)
		if errors.Is(err, redis.TxFailedErr) {
			continue // another writer changed a watched key: read again
		}
		if err != nil {
			return nil, GuardApplied, err
		}
		if out != GuardApplied {
			return nil, out, nil
		}
		for _, c := range cmds {
			if fc, ok := c.(*redis.Cmd); ok {
				if reply, err := fc.Slice(); err == nil && len(reply) > 0 && fmt.Sprint(reply[0]) == "REFUSED" {
					return nil, GuardApplied, fmt.Errorf("the table layer refused a conditional delete on %s: %v", table, reply)
				}
			}
		}
		rows := make([]string, len(pass))
		for i, g := range pass {
			rows[i] = g.Row
		}
		return rows, GuardApplied, nil
	}
	return nil, GuardApplied, nil
}

// Place puts a record that is on no cell back into the cell, through the table
// layer's cell add, one write.
func (r *Redis) Place(ctx context.Context, table, row, col, id string, score float64) error {
	_, err := ntable.CellAdd(ctx, r.C, table, row, col, id, score, r.writeOpts())
	return err
}

func (r *Redis) RowSet(ctx context.Context, table, row string, texts map[string]string) error {
	_, err := ntable.RowSet(ctx, r.C, table, row, texts, r.writeOpts())
	return err
}

func (r *Redis) ViewSet(ctx context.Context, v ntable.View) error { return ntable.ViewSet(ctx, r.C, v) }

func (r *Redis) ViewDelete(ctx context.Context, name string) error {
	_, err := ntable.ViewDelete(ctx, r.C, name)
	return err
}

func (r *Redis) DropTable(ctx context.Context, table string) error {
	_, err := ntable.DropDefinition(ctx, r.C, table, r.writeOpts())
	return err
}

func (r *Redis) CheckTable(ctx context.Context, table string) error {
	_, err := ntable.Check(ctx, r.C, table)
	return err
}

// The log's keys, under the deployment's prefix.
const (
	keyFence    = "fence"    // STRING, the pending operation record
	keyGen      = "fencegen" // STRING, the fence's generation
	keyInbox    = "inbox"    // STREAM of notifications, field "note"
	keyLog      = "log"      // STREAM of the log's lines, field "line"
	keyNotes    = "notes"    // HASH note id -> judgment note
	keyAliases  = "aliases"  // HASH alias (j<n>) -> note id, and its field "n", the count (sprint.Alias)
	keyAnswered = "answered" // HASH judgment id -> who answered it (the step's actor), written as it closes
	keyOpen     = "open"     // HASH <note id>|<subject> -> note id, one per open subject
	keyCursor   = "cursor"   // STRING, the coordinator's last read stream id
	keyProgress = "progress" // HASH stream -> RFC3339 time of its last progress
	keyDone     = "done"     // HASH caller operation id -> result
	keyQueue    = "queue"    // LIST of the work table's queued changes (sprint.QueuedChange, JSON), oldest first
)

func (r *Redis) ReadFence(ctx context.Context) (Fence, error) {
	p := r.C.Pipeline()
	mget, llen := r.queueFence(ctx, p)
	if _, err := p.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return Fence{}, err
	}
	return fenceOf(mget, llen)
}

// queueFence queues the fence's read on a pipeline: the fence, its
// generation and the machine's state, and the queue's length.
func (r *Redis) queueFence(ctx context.Context, p redis.Pipeliner) (*redis.SliceCmd, *redis.IntCmd) {
	return p.MGet(ctx, r.key(keyFence), r.key(keyGen), r.Names.Key(keyMachine), r.Names.Key(keyStuck)), p.LLen(ctx, r.key(keyQueue))
}

// fenceOf is the fence a pipeline read.
func fenceOf(mget *redis.SliceCmd, llen *redis.IntCmd) (Fence, error) {
	vals, err := mget.Result()
	if err != nil {
		return Fence{}, err
	}
	var f Fence
	f.Queued = int(llen.Val())
	if s, ok := vals[2].(string); ok {
		var m Machine
		if json.Unmarshal([]byte(s), &m) == nil {
			f.Running = m.Running()
			f.RunSeq = m.RunSeq
			f.StopRevoked = m.stopRevoked()
			f.StopIssued = m.StopIssued
			f.StopDebt = m.StopDebt
		}
	}
	if s, ok := vals[1].(string); ok {
		f.Gen, _ = strconv.ParseUint(s, 10, 64)
	}
	if len(vals) > 3 {
		f.Stuck, _ = vals[3].(string)
	}
	if s, ok := vals[0].(string); ok {
		// the whole record, read through OPRECORD in any form; a damaged
		// record is refused by name (S2, C13)
		op, err := opRows.fenceRecord(s)
		if err != nil {
			return f, fmt.Errorf("the fence holds an unreadable operation record: %w", err)
		}
		f.Pending = op
	}
	return f, nil
}

var errFenceMoved = errors.New("the fence moved")

// Acquire is WATCH on the fence and its generation, then MULTI/EXEC.
func (r *Redis) Acquire(ctx context.Context, gen uint64, op OpRecord) (bool, error) {
	body, err := json.Marshal(op)
	if err != nil {
		return false, err
	}
	fence, genKey, epochKey := r.key(keyFence), r.key(keyGen), r.Names.EpochKey()
	err = r.C.Watch(ctx, func(tx *redis.Tx) error {
		vals, err := tx.MGet(ctx, fence, genKey).Result()
		if err != nil {
			return err
		}
		var g uint64
		if s, ok := vals[1].(string); ok {
			g, _ = strconv.ParseUint(s, 10, 64)
		}
		if vals[0] != nil || g != gen {
			return errFenceMoved
		}
		e, err := tx.HGet(ctx, epochKey, "n").Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		if n, _ := strconv.ParseUint(e, 10, 64); n != r.Pinned {
			return errFenceMoved // the sprint was cleared since the step read it
		}
		_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.Set(ctx, fence, body, 0)
			p.Set(ctx, genKey, gen+1, 0)
			return nil
		})
		return err
	}, fence, genKey, epochKey)
	if errors.Is(err, errFenceMoved) || errors.Is(err, redis.TxFailedErr) {
		return false, nil
	}
	return err == nil, err
}

// Release is WATCH on the fence, then one MULTI/EXEC: the notifications
// appended, the judgments opened on every subject, the answered ones closed,
// the streams' progress, the caller's result, and the fence emptied.
func (r *Redis) Release(ctx context.Context, op OpRecord, commit bool) error {
	fence := r.key(keyFence)
	// The fence holds this operation when its record begins with this
	// operation's id (json.Marshal writes OpRecord's id first): read that
	// much of it, not the whole record, which carries every manifest. Every
	// writer of the fence still writes the legacy record (S4: readers first),
	// so the prefix holds; a writer that switches the fence to a table form
	// switches this check with it (TestOpRowsDualRead pins the prefix).
	id, err := json.Marshal(op.ID)
	if err != nil {
		return err
	}
	prefix := `{"id":` + string(id) + `,`
	for i := 0; i < 8; i++ {
		err = r.C.Watch(ctx, func(tx *redis.Tx) error {
			cur, err := tx.GetRange(ctx, fence, 0, int64(len(prefix)-1)).Result()
			if err != nil {
				return err
			}
			if cur != prefix {
				return nil // empty (released) or another operation's
			}
			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				if commit {
					if err := r.commit(ctx, p, op); err != nil {
						return err
					}
				}
				p.Del(ctx, fence)
				return nil
			})
			return err
		}, fence)
		// The fence moved while this release was prepared: another writer
		// finishing the same operation released it, or took the fence after;
		// read it again, and release only if it still holds this operation.
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	// The fence kept moving through every attempt, as Acquire takes it: read
	// it once more; an operation it no longer holds was released by the
	// writer that finished it, which is success. Only a true store error, or
	// the fence still holding it, is unknown.
	held, herr := r.heldOp(ctx)
	if herr != nil {
		return herr
	}
	recorded := false
	if commit && op.CallerOp != "" {
		_, ok, derr := r.Done(ctx, op.CallerOp)
		if derr != nil {
			return derr
		}
		recorded = ok
	}
	if released(op, commit, held, recorded) {
		return nil
	}
	return err
}

// heldOp is the id of the operation the fence holds, "" when it is empty.
func (r *Redis) heldOp(ctx context.Context) (string, error) {
	cur, err := r.C.Get(ctx, r.key(keyFence)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// read through HELDLOCK in any form, refused by name when damaged (S2, C13)
	id, err := opRows.heldID(cur)
	if err != nil {
		return "", fmt.Errorf("the fence holds an unreadable operation record: %w", err)
	}
	return id, nil
}

// released says a release whose transaction kept failing is done: the fence
// no longer holds the operation (held is the id it holds, "" when empty) and,
// for a commit with a caller's operation id, its result is recorded, so the
// writer that released it finished it.
func released(op OpRecord, commit bool, held string, recorded bool) bool {
	if held == op.ID {
		return false
	}
	return !commit || op.CallerOp == "" || recorded
}

func (r *Redis) commit(ctx context.Context, p redis.Pipeliner, op OpRecord) error {
	if op.Drain > 0 {
		p.LTrim(ctx, r.key(keyQueue), int64(op.Drain), -1)
	}
	for _, x := range op.Queue {
		body, err := json.Marshal(x)
		if err != nil {
			return err
		}
		p.RPush(ctx, r.key(keyQueue), string(body))
	}
	for _, line := range op.Log {
		body, err := json.Marshal(line)
		if err != nil {
			return err
		}
		p.XAdd(ctx, &redis.XAddArgs{Stream: r.key(keyLog), Values: []any{"line", string(body)}})
	}
	notes := append(append([]sprint.Note{}, op.Notes...), op.Decided...)
	// each judgment's and acknowledgement's alias is its place among the epoch's
	// (sprint.Alias): the count is read here, under the fence this commit holds, and
	// written back with the notes; one read, only when the commit writes one
	counted := 0
	for _, n := range notes {
		if n.Kind == sprint.Judgment || n.Kind == sprint.Acknowledged {
			counted++
		}
	}
	if counted > 0 {
		n, err := r.C.HGet(ctx, r.key(keyAliases), "n").Int()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		for i := range notes {
			if notes[i].Kind == sprint.Judgment || notes[i].Kind == sprint.Acknowledged {
				n++
				notes[i].Alias = sprint.Alias(n)
				p.HSet(ctx, r.key(keyAliases), notes[i].Alias, notes[i].ID)
			}
		}
		p.HSet(ctx, r.key(keyAliases), "n", n)
	}
	for _, n := range notes {
		body, err := json.Marshal(n.Bound())
		if err != nil {
			return err
		}
		lb, err := json.Marshal(sprint.NoteLine(n.Bound(), op.ID))
		if err != nil {
			return err
		}
		p.XAdd(ctx, &redis.XAddArgs{Stream: r.key(keyLog), Values: []any{"line", string(lb)}})
		p.XAdd(ctx, &redis.XAddArgs{Stream: r.key(keyInbox), Values: []any{"note", string(body)}})
		if n.Kind == sprint.Judgment || n.Kind == sprint.Acknowledged {
			p.HSet(ctx, r.key(keyNotes), n.ID, string(body))
			for _, s := range n.Subjects() {
				p.HSet(ctx, r.key(keyOpen), sprint.OpenKey(n.ID, s), n.ID)
			}
		}
	}
	for _, n := range op.Updates {
		body, err := json.Marshal(n.Bound())
		if err != nil {
			return err
		}
		line := sprint.NoteLine(n.Bound(), op.ID)
		line.Verb = "updated"
		lb, err := json.Marshal(line)
		if err != nil {
			return err
		}
		p.HSet(ctx, r.key(keyNotes), n.ID, string(body))
		p.XAdd(ctx, &redis.XAddArgs{Stream: r.key(keyLog), Values: []any{"line", string(lb)}})
	}
	if len(op.Closes) > 0 {
		p.HDel(ctx, r.key(keyOpen), op.Closes...)
		for id, who := range answeredBy(op) {
			p.HSet(ctx, r.key(keyAnswered), id, who)
		}
	}
	if op.Stuck != "" {
		p.Del(ctx, r.Names.Key(keyStuck))
	}
	for _, s := range op.Streams {
		p.HSet(ctx, r.key(keyProgress), s, op.At.UTC().Format(time.RFC3339))
	}
	if op.CallerOp != "" {
		p.HSet(ctx, r.key(keyDone), op.CallerOp, op.Result)
	}
	if op.Seat != nil {
		rec, err := seatRecord(op.Seat)
		if err != nil {
			return err
		}
		p.Set(ctx, r.Names.Key(keyCoordinator), op.Seat.Holder, 0)
		p.Set(ctx, r.Names.Key(keySeat), rec, 0)
	}
	if len(op.CloseTimers) > 0 {
		raw, err := r.C.Get(ctx, r.Names.Key(keyTimers)).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		if raw != "" {
			// read through TIMERS in any form, refused by name when damaged
			// (S2, C13); the record is written back legacy (S4)
			ts, _, err := opRows.timers.Read([]byte(raw))
			if err != nil {
				return err
			}
			closing := map[string]bool{}
			for _, id := range op.CloseTimers {
				closing[id] = true
			}
			var rem []sprint.Timer
			for _, tm := range ts.Open {
				if !closing[tm.ID] {
					rem = append(rem, tm)
				}
			}
			ts.Open = rem
			rec, err := json.Marshal(ts)
			if err != nil {
				return err
			}
			p.Set(ctx, r.Names.Key(keyTimers), string(rec), 0)
		}
	} else if op.Timers != nil {
		rec, err := json.Marshal(op.Timers)
		if err != nil {
			return err
		}
		p.Set(ctx, r.Names.Key(keyTimers), string(rec), 0)
	}
	return nil
}

// QueueRead is LRANGE over the whole queue: one exchange.
func (r *Redis) QueueRead(ctx context.Context) ([]sprint.QueuedChange, error) {
	raw, err := r.C.LRange(ctx, r.key(keyQueue), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	return opRows.queueEntries(raw)
}

func (r *Redis) Done(ctx context.Context, callerOp string) (string, bool, error) {
	v, err := r.C.HGet(ctx, r.key(keyDone), callerOp).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	return v, err == nil, err
}

// DoneBefore asks every earlier epoch's results for the caller's operation id,
// in one pipeline.
func (r *Redis) DoneBefore(ctx context.Context, callerOp string, before uint64) (uint64, bool, error) {
	if before == 0 {
		return 0, false, nil
	}
	pipe := r.C.Pipeline()
	cmds := make([]*redis.BoolCmd, before)
	for e := uint64(0); e < before; e++ {
		cmds[e] = pipe.HExists(ctx, r.Names.KeyAt(keyDone, e), callerOp)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, false, err
	}
	for e := before; e > 0; e-- {
		if cmds[e-1].Val() {
			return e - 1, true, nil
		}
	}
	return 0, false, nil
}

func (r *Redis) SetReview(ctx context.Context, noteID string, at, set time.Time) error {
	raw, err := r.C.HGet(ctx, r.key(keyNotes), noteID).Result()
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("no judgment %s; run: nova-sprint inbox", noteID)
	}
	if err != nil {
		return err
	}
	// read through NOTES in any form, refused by name when damaged (S2,
	// C13); written back legacy (S4)
	n, _, err := opRows.notes.Read([]byte(raw))
	if err != nil {
		return err
	}
	n.Review, n.ReviewSet = at, set
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return r.C.HSet(ctx, r.key(keyNotes), noteID, string(body)).Err()
}

func (r *Redis) Progress(ctx context.Context) (map[string]time.Time, error) {
	h, err := r.C.HGetAll(ctx, r.key(keyProgress)).Result()
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for k, v := range h {
		out[k], _ = time.Parse(time.RFC3339, v)
	}
	return out, nil
}

func (r *Redis) Answered(ctx context.Context, ids []string) (map[string]string, error) {
	return r.hmget(ctx, keyAnswered, ids)
}

func (r *Redis) Aliases(ctx context.Context, aliases []string) (map[string]string, error) {
	return r.hmget(ctx, keyAliases, aliases)
}

// hmget is the fields of one of the log's hashes that are set, by name: one read,
// none for no names.
func (r *Redis) hmget(ctx context.Context, key string, fields []string) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	if len(fields) == 0 {
		return out, nil
	}
	vals, err := r.C.HMGet(ctx, r.key(key), fields...).Result()
	if err != nil {
		return nil, err
	}
	for i, v := range vals {
		if s, ok := v.(string); ok {
			out[fields[i]] = s
		}
	}
	return out, nil
}

func (r *Redis) OpenNotes(ctx context.Context) ([]sprint.Open, error) {
	open, err := r.C.HGetAll(ctx, r.key(keyOpen)).Result()
	if err != nil {
		return nil, err
	}
	return r.openOf(ctx, open)
}

// openOf is the open judgments of the open index read: the notes it names,
// read in one exchange.
func (r *Redis) openOf(ctx context.Context, open map[string]string) ([]sprint.Open, error) {
	if len(open) == 0 {
		return nil, nil
	}
	var ids []string
	seen := map[string]bool{}
	for _, nid := range open {
		if !seen[nid] {
			seen[nid] = true
			ids = append(ids, nid)
		}
	}
	vals, err := r.C.HMGet(ctx, r.key(keyNotes), ids...).Result()
	if err != nil {
		return nil, err
	}
	notes := map[string]sprint.Note{}
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		// read through NOTES in any form, refused by name when damaged (S2, C13)
		n, _, err := opRows.notes.Read([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("notification %s: %w", ids[i], err)
		}
		notes[ids[i]] = n
	}
	var out []sprint.Open
	for k, nid := range open {
		out = append(out, sprint.Open{Key: k, Note: notes[nid]})
	}
	sortOpen(out)
	return out, nil
}

// rangeSince is the NotesSince and LogSince read: the stream's entries after
// a cursor (all when after is empty), up to max, in one XRANGE, returning the
// entries whose decode succeeds and their stream ids. decode returns an error
// for an entry the caller does not count, and that entry is skipped whole.
func rangeSince[T any](ctx context.Context, c redis.UniversalClient, key, after string, max int, decode func(map[string]any) (T, error)) ([]T, []string, error) {
	start := "-"
	if after != "" {
		start = "(" + after
	}
	msgs, err := c.XRangeN(ctx, key, start, "+", int64(max)).Result()
	if err != nil {
		return nil, nil, err
	}
	var out []T
	var ids []string
	for _, m := range msgs {
		t, err := decode(m.Values)
		if err == nil {
			out = append(out, t)
			ids = append(ids, m.ID)
		}
	}
	return out, ids, nil
}

func (r *Redis) NotesSince(ctx context.Context, after string, max int) ([]sprint.Note, []string, error) {
	return rangeSince(ctx, r.C, r.key(keyInbox), after, max, opRows.inboxNote)
}

func (r *Redis) Tails(ctx context.Context) (string, string, error) {
	p := r.C.Pipeline()
	lg := p.XRevRangeN(ctx, r.key(keyLog), "+", "-", 1)
	in := p.XRevRangeN(ctx, r.key(keyInbox), "+", "-", 1)
	if _, err := p.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return "", "", err
	}
	last := func(c *redis.XMessageSliceCmd) string {
		if ms, err := c.Result(); err == nil && len(ms) > 0 {
			return ms[0].ID
		}
		return ""
	}
	return last(lg), last(in), nil
}

func (r *Redis) LogSince(ctx context.Context, after string, max int) ([]sprint.Line, []string, error) {
	return rangeSince(ctx, r.C, r.key(keyLog), after, max, opRows.logLine)
}

func (r *Redis) Cursor(ctx context.Context) (string, error) {
	v, err := r.C.Get(ctx, r.key(keyCursor)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

func (r *Redis) SetCursor(ctx context.Context, id string) error {
	return r.C.Set(ctx, r.key(keyCursor), id, 0).Err()
}

// keyCoordinator holds the coordinator's name: one for the sprint, as the
// machine's records, so a clear keeps it.
const keyCoordinator = "coordinator"

func (r *Redis) Coordinator(ctx context.Context) (string, error) {
	v, err := r.C.Get(ctx, r.Names.Key(keyCoordinator)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return v, err
}

// SetCoordinator writes the coordinator key: init's alone (Store.InitSeat, from
// the seat record when there is one); the seat's steps write it in their commit
// with the record, and the run loop never does (seat-key-follows-record.w2).
func (r *Redis) SetCoordinator(ctx context.Context, name string) error {
	return r.C.Set(ctx, r.Names.Key(keyCoordinator), name, 0).Err()
}

// SetKeyFor writes a machine record that expires after ttl: the server's
// record (keyServer), so a sprint torn down keeps none past it.
func (r *Redis) SetKeyFor(ctx context.Context, name, value string, ttl time.Duration) error {
	return r.C.Set(ctx, r.Names.Key(name), value, ttl).Err()
}

// changeEvent is the part of a table change stream's event the twin reads
// (twin.go): the revisions it moved the table between, the epoch, the verb,
// and the records it names.
type changeEvent struct {
	epoch, verb         string
	before, after       uint64
	members, batchDelta string
	args                string // the write's arguments, a JSON list (the table, then its own)
}

// changePage is how many events one read of a change stream takes.
const changePage = 64

// twinVerbs are the table writes whose events name every record they changed
// (a batch's account names each of its entries; a row's texts and rows name
// none): any other write in the span makes the twin read the table whole.
var twinVerbs = map[string]bool{"apply": true, "row_set": true, "rows_add": true, "row_add": true}

// rowFlagVerbs are the table writes that change rows' flags and no record: a row
// hide or show (stream archive) names none because it changed none, and the shape
// read beside the catch-up carries the rows (as the Mem store's stream answers it).
var rowFlagVerbs = map[string]bool{"rows_hide": true}

// TableChanges reads the table's change stream from its newest event back to
// the one that left revision from, and says the records the writes between
// from and to named (twin.go). ok is false when the events do not chain from
// from to to at the pinned epoch, or one is a write that does not name its
// records.
func (r *Redis) TableChanges(ctx context.Context, table string, from, to uint64) ([]string, bool, error) {
	ids, _, _, ok, err := r.tableChanges(ctx, table, from, to, false)
	return ids, ok, err
}

// TableChangesMarked is TableChanges with the stream ids of the events that left
// revisions from and to (the load cache's marks, loadcache.go): a store that lost its
// last writes and wrote others since gives the same revision a new event, and a new
// id. Revision 0 is the epoch's empty table, left by no event: its mark is "".
func (r *Redis) TableChangesMarked(ctx context.Context, table string, from, to uint64) ([]string, string, string, bool, error) {
	return r.tableChanges(ctx, table, from, to, true)
}

// tableChanges is TableChanges, and with marks the ids of the events that left from
// and to.
func (r *Redis) tableChanges(ctx context.Context, table string, from, to uint64, marks bool) ([]string, string, string, bool, error) {
	gap := func(why string) ([]string, string, string, bool, error) {
		return nil, "", "", false, &GapError{Table: table, From: from, To: to, Why: why}
	}
	if to < from {
		return gap("the twin is ahead of the table")
	}
	if to == from && (!marks || to == 0) {
		return nil, "", "", true, nil
	}
	key := ntable.DefKey(table) + ":changes"
	epoch := strconv.FormatUint(r.Pinned, 10)
	need := to
	var ids []string
	toMark := ""
	// chained: the events from to back to from are read; with marks the one that
	// left from is looked for next
	chained := to == from
	end := "+"
	for page := 0; page < 64; page++ {
		evs, err := r.C.XRevRangeN(ctx, key, end, "-", changePage).Result()
		if err != nil && strings.Contains(err.Error(), "NOPERM") {
			// a user not granted the stream's read: said, and the twin reads
			// the table whole
			return nil, "", "", false, &GrantError{Command: "XREVRANGE", Key: key, Cause: err}
		}
		if err != nil {
			return nil, "", "", false, err
		}
		if len(evs) == 0 {
			return gap(fmt.Sprintf("the stream ends before revision %d", need))
		}
		for _, x := range evs {
			ev := readChange(x.Values)
			if ev.epoch != epoch || ev.after > to {
				// another epoch's, or written after the shape was read (the
				// read of the records sees it, and refuses the revision)
				continue
			}
			if ev.after != need {
				return gap(fmt.Sprintf("the event before revision %d leaves revision %d", need, ev.after))
			}
			if ev.after == to {
				toMark = x.ID
			}
			if chained {
				// the event that left from: its id is from's mark
				return ids, x.ID, toMark, true, nil
			}
			if (ev.verb == "set" && orderOnly(ev.args)) || rowFlagVerbs[ev.verb] {
				// the rows' order or their hidden flags alone: no record changed, and the
				// shape read beside the catch-up carries the rows
			} else {
				if !twinVerbs[ev.verb] {
					return gap(fmt.Sprintf("a write %q at revision %d names no records", ev.verb, ev.after))
				}
				named, err := changeIDs(ev)
				if err != nil {
					return gap(fmt.Sprintf("the event at revision %d is unreadable: %v", ev.after, err))
				}
				ids = append(ids, named...)
			}
			need = ev.before
			if need < from {
				return gap(fmt.Sprintf("the events skip revision %d", from))
			}
			if need == from {
				if !marks || from == 0 {
					return ids, "", toMark, true, nil
				}
				chained = true
			}
		}
		end = "(" + evs[len(evs)-1].ID
	}
	return gap("more than 64 pages of events")
}

func readChange(v map[string]any) changeEvent {
	str := func(k string) string { s, _ := v[k].(string); return s }
	ev := changeEvent{epoch: str("epoch"), verb: str("verb"), members: str("members"), batchDelta: str("batch_delta"), args: str("args")}
	ev.before, _ = strconv.ParseUint(str("rev_before"), 10, 64)
	ev.after, _ = strconv.ParseUint(str("rev_after"), 10, 64)
	return ev
}

// changeIDs is the records an event names: the members it moved and every
// entry of its batch's account.
func changeIDs(ev changeEvent) ([]string, error) {
	return opRows.changeIDs(ev)
}

// changeIDs is changeIDs read through these readers: the moved members and the
// members of the batch's account through MEMBERS in any form, refused by name
// when damaged (S2, C13). The account's envelope is nova-tables' own protocol
// and stays its JSON.
func (c *opRowCodecs) changeIDs(ev changeEvent) ([]string, error) {
	out, err := c.memberIDs(ev.members)
	if err != nil {
		return nil, err
	}
	if ev.batchDelta != "" {
		var d struct {
			Members json.RawMessage `json:"members"`
		}
		if err := json.Unmarshal([]byte(ev.batchDelta), &d); err != nil {
			return nil, err
		}
		// a batch that changed no record (a properties-only apply, such as promoted) is
		// encoded by the store's cjson with its empty members as {}: it names none
		// a table form inside the account is a JSON string holding it (S2)
		raw := string(d.Members)
		if len(raw) > 0 && raw[0] == '"' {
			var text string
			if err := json.Unmarshal(d.Members, &text); err == nil {
				raw = text
			}
		}
		members, err := c.memberIDs(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, members...)
	} else if ev.verb == "apply" {
		return nil, fmt.Errorf("a batch event without its account")
	}
	return out, nil
}

var _ TableChanger = (*Redis)(nil)

// RowsSet writes the display cells of many rows in one round trip.
func (r *Redis) RowsSet(ctx context.Context, table string, rows map[string]map[string]string) error {
	return ntable.RowSetMany(ctx, r.C, table, rows, r.writeOpts())
}

var _ RowsSetter = (*Redis)(nil)

// ReadView reads, in one exchange, what a read of the twin reads first (the
// fence, the tables' shapes, the open judgments' index and the coordinator),
// then the notes the index names in a second: two round trips where one each
// took five (twin.go).
func (r *Redis) ReadView(ctx context.Context, tables []string) (View, error) {
	if r.Old {
		return View{}, errors.New("a read of an earlier epoch reads no view")
	}
	p := r.C.Pipeline()
	mget, llen := r.queueFence(ctx, p)
	shapes := make([]*ntable.ReadCmd, len(tables))
	for i, t := range tables {
		shapes[i] = ntable.NewReader(t).Queue(ctx, p)
	}
	open := p.HGetAll(ctx, r.key(keyOpen))
	coord := p.Get(ctx, r.Names.Key(keyCoordinator))
	seat := p.Get(ctx, r.Names.Key(keySeat))
	if _, err := p.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) && !isReply(err) {
		return View{}, err
	}
	var v View
	var err error
	if v.Fence, err = fenceOf(mget, llen); err != nil {
		return View{}, err
	}
	v.Shapes = make([]ntable.Table, len(tables))
	for i, cmd := range shapes {
		if v.Shapes[i], _, err = cmd.Result(); err != nil {
			return View{}, err
		}
	}
	idx, err := open.Result()
	if err != nil {
		return View{}, err
	}
	if v.Coordinator, err = coord.Result(); err != nil && !errors.Is(err, redis.Nil) {
		return View{}, err
	}
	raw, err := seat.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return View{}, err
	}
	if v.SeatGeneration, err = seatGenerationOf(raw, err == nil); err != nil {
		return View{}, err
	}
	if v.Open, err = r.openOf(ctx, idx); err != nil {
		return View{}, err
	}
	return v, nil
}

var _ ViewReader = (*Redis)(nil)

// RowsOrder puts the named rows first, in that order, through the table
// layer's row order, ending any standing sort in the same call (the table
// layer refuses a row order under one: SORTED).
func (r *Redis) RowsOrder(ctx context.Context, table string, rows []string) error {
	_, err := ntable.Set(ctx, r.C, table, ntable.SetOpts{RowSort: &ntable.Sort{Manual: true}, RowOrder: rows}, r.writeOpts())
	return err
}

var _ RowsOrderer = (*Redis)(nil)

// orderOnly says a set's arguments (its event's args: the table, then the change) change
// the rows' order and nothing else: row_order, row_sort and row_move alone. Such a write
// names no record because it changes none: the fleet's order by status (orderFleet)
// moves rows, and the twin, which takes each table's rows from the shape it reads beside
// its catch-up, read the fleet table whole after every such write (34 times on
// 2026-10-04, each ~2,800 records). Any other key, or arguments it cannot read, is a
// write that may change records, and the table is read whole as before.
func orderOnly(args string) bool {
	var list []string
	if json.Unmarshal([]byte(args), &list) != nil || len(list) != 2 {
		return false
	}
	var change map[string]json.RawMessage
	if json.Unmarshal([]byte(list[1]), &change) != nil || len(change) == 0 {
		return false
	}
	for k := range change {
		switch k {
		case "row_order", "row_sort", "row_move":
		default:
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The operation rows' readers (the reader-first half of the schema-table
// adoption of the op journal, the held lock, the notes and note lines, the
// timers, the queued changes, the members a table change names and the step
// result: S4 and C13, readers first, then writers). Each record is read through
// the store row reader (rowcodec.go, pkg/schemaread) from the generated tables
// of oprowswire (schemas/oprows/oprows.schema): the table wire (form byte 1),
// the padded standard base64 of that wire, the schema JSON text of a variable
// table, and the legacy JSON every writer still writes (S2). A damaged value is
// refused by name, `<RECORD> REFUSED: <reason>; run: nova-sprint check`, and
// every read is counted under its record and its form (S4). These reads run in
// the Redis backend, which has no Store, so the readers are registered
// process-wide and show on every store's check (rowcodec.go RegisterProcess).
// Writers stay legacy: the writer switch is a later card, which reads these
// counts first. The fence's release and relock still find the operation they
// hold by the legacy record's `{"id":` prefix (Release, Relock); a writer that
// switches the fence to a table form switches that check with it.

// opTable is one generated table of oprowswire as the row reader uses it, W
// its generated record: the wire's load, the schema JSON text's load and the
// table's JSON text and wire of a loaded record (S1, S2). measure is nil for a
// fixed table, whose load decides alone, and fromJSON reads only for SaveWire:
// a fixed table has no JSON text form (S1, oprows.schema).
type opTable[W any] struct {
	measure  func(wire []byte) (int64, error)
	load     func(wire []byte, report *oprowswire.TableReport) *W
	fromJSON func(text []byte, report *oprowswire.TableReport) (*W, bool)
	toJSON   func(*W) []byte
	save     func(*W) []byte
	fixed    bool
}

// opVariable is a variable table's opTable from its generated functions (S1).
func opVariable[W any, B any](
	measure func([]byte) (int64, error),
	load func(region, wire []byte, report *oprowswire.TableReport) *W,
	init func(*B) bool, from func(*B, []byte, *oprowswire.TableReport) bool, lock func(*B) bool, root func(*B) *W,
	jsonMeasure func(*W) int64, toJSON func(*W, []byte) int64,
	wireMeasure func(*W) int64, save func(*W, []byte) int64,
) opTable[W] {
	return opTable[W]{
		measure: measure,
		load: func(wire []byte, rep *oprowswire.TableReport) *W {
			n, err := measure(wire)
			if err != nil || n < 0 {
				return nil
			}
			return load(presenceRegion(n), wire, rep)
		},
		fromJSON: func(text []byte, rep *oprowswire.TableReport) (*W, bool) {
			b := new(B)
			if !init(b) {
				return nil, false
			}
			if !from(b, text, rep) || !lock(b) {
				return nil, false
			}
			return root(b), true
		},
		toJSON: func(w *W) []byte {
			buf := make([]byte, max(jsonMeasure(w), 0))
			return buf[:max(toJSON(w, buf), 0)]
		},
		save: func(w *W) []byte {
			m := wireMeasure(w)
			if m < 0 {
				return nil
			}
			buf := make([]byte, m)
			n := save(w, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
	}
}

// opReport is the generated report as the strict read decides on it. S2: a key
// given twice is malformed, never last-wins.
func opReport(r *oprowswire.TableReport) RowReport {
	return RowReport{
		Unknown: int(r.Unknown), KindMismatch: int(r.KindMismatch), Clamped: int(r.Clamped),
		Malformed: r.Malformed, Duplicate: int(r.Duplicate), Reason: r.Reason,
	}
}

// opRowFns adapts one record to the row reader (S1, S2): T is the store's Go
// record, M its schema JSON shape (the generated text's keys) and W the
// generated record. A loaded table is read into T through its schema JSON text,
// so the wire, its base64 and the JSON text decode to one value. legacy says a
// value is today's legacy text and loadLegacy reads it; a nil legacy is
// opLegacyObject and a nil loadLegacy DecodeLegacy (S2).
func opRowFns[T, M, W any](name string, tab opTable[W], to func(*T) (M, bool), from func(*M) (T, bool), legacy func([]byte) bool, loadLegacy func(*T, []byte) RowReport) RowFns[T] {
	if legacy == nil {
		legacy = opLegacyObject
	}
	if loadLegacy == nil {
		loadLegacy = DecodeLegacy[T]
	}
	value := func(v *T, w *W) RowReport {
		dec := json.NewDecoder(bytes.NewReader(tab.toJSON(w)))
		dec.DisallowUnknownFields()
		var m M
		if err := dec.Decode(&m); err != nil {
			return RowReport{Malformed: true}
		}
		t, ok := from(&m)
		if !ok {
			return RowReport{Malformed: true}
		}
		*v = t
		return RowReport{}
	}
	table := func(v *T) *W {
		m, ok := to(v)
		if !ok {
			return nil
		}
		text, err := json.Marshal(m)
		if err != nil {
			return nil
		}
		var rep oprowswire.TableReport
		w, ok := tab.fromJSON(text, &rep)
		if !ok || w == nil || opReport(&rep).refuses() {
			return nil
		}
		return w
	}
	fns := RowFns[T]{
		Name:  name,
		Forms: []byte{1}, // the table wire of oprows.schema, fixed and variable alike (S1)
		Reset: func(v *T) { var zero T; *v = zero },
		LoadWire: func(v *T, wire []byte) RowReport {
			var rep oprowswire.TableReport
			w := tab.load(wire, &rep)
			if w == nil {
				return RowReport{Malformed: true, Reason: rep.Reason}
			}
			if rr := opReport(&rep); rr.refuses() {
				return rr
			}
			rr := value(v, w)
			rr.Unknown = int(rep.Unknown)
			return rr
		},
		Legacy:     legacy,
		LoadLegacy: loadLegacy,
		SaveWire: func(v *T) []byte {
			if w := table(v); w != nil {
				return tab.save(w)
			}
			return nil
		},
	}
	if tab.measure != nil {
		fns.Measure = func(wire []byte) error {
			_, err := tab.measure(wire)
			return err
		}
	}
	if !tab.fixed {
		// S1: a variable table also travels as its schema JSON text.
		fns.LoadJSON = func(v *T, text []byte) RowReport {
			var rep oprowswire.TableReport
			w, ok := tab.fromJSON(text, &rep)
			rr := opReport(&rep)
			if !ok || w == nil {
				rr.Malformed = true
				return rr
			}
			if rr.refuses() {
				return rr
			}
			out := value(v, w)
			out.Unknown = rr.Unknown
			return out
		}
		fns.SaveJSON = func(v *T) []byte {
			if w := table(v); w != nil {
				return tab.toJSON(w)
			}
			return nil
		}
	}
	return fns
}

// opLegacyObject reports today's legacy JSON: an object as encoding/json writes
// it, on one line. The generated writer's schema JSON text spans lines, so a
// schema text that lost its closing newline is still read as schema text, and
// is never read as a legacy record whose keys it does not share (S2).
func opLegacyObject(in []byte) bool {
	t := bytes.TrimSpace(in)
	return len(t) > 0 && t[0] == '{' && bytes.IndexByte(t, '\n') < 0
}

// opValue is one element of a wrapped list of words (Name128, Name256, Name400,
// Text512: oprows.schema, an array of string(N) wraps its element).
type opValue struct {
	Value string `json:"value"`
}

// opValues is a list of words as the schema's wrapped list.
func opValues(in []string) []opValue {
	out := make([]opValue, len(in))
	for i, s := range in {
		out[i] = opValue{Value: s}
	}
	return out
}

// opStrings is opValues read back; an empty list is nil, as encoding/json
// reads an omitted list.
func opStrings(in []opValue) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.Value
	}
	return out
}

// opNanos is a time as oprows.schema holds it: Unix nanoseconds UTC, 0 for the
// zero time.
func opNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

// opTimeOf is opNanos read back, in UTC.
func opTimeOf(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// opInt32 is a count the schema holds as int32; ok is false past its range.
func opInt32(n int) (int64, bool) {
	return int64(n), n >= -1<<31 && n < 1<<31
}

// ---------------------------------------------------------------------------
// The held lock (HELDLOCK): the identity of the operation the fence holds, the
// part of it a read that only asks whose the fence is takes (AdvanceEpoch,
// heldOp). The generated HeldLock fixed table reads the id, the verb and the
// time of a lock's image, and of an operation record's image too: the table
// wire skips the fields it does not name as newer data.

// heldLock is the fence's holder as HeldLock reads it; Lock is the legacy
// record's flag, which the fixed table does not hold.
type heldLock struct {
	ID   string    `json:"id"`
	Lock bool      `json:"lock,omitempty"`
	Verb string    `json:"verb"`
	At   time.Time `json:"at"`
}

type heldLockSchema struct {
	ID   string `json:"id"`
	Verb string `json:"verb"`
	At   int64  `json:"at"`
}

func heldLockTable() opTable[oprowswire.HeldLock] {
	return opTable[oprowswire.HeldLock]{
		fixed: true,
		load: func(wire []byte, rep *oprowswire.TableReport) *oprowswire.HeldLock {
			var w oprowswire.HeldLock
			oprowswire.HeldLockReset(&w)
			if !oprowswire.HeldLockLoad(&w, wire, rep) {
				return nil
			}
			return &w
		},
		fromJSON: func(text []byte, rep *oprowswire.TableReport) (*oprowswire.HeldLock, bool) {
			var w oprowswire.HeldLock
			oprowswire.HeldLockReset(&w)
			return &w, oprowswire.HeldLockFromJson(&w, text, rep)
		},
		toJSON: func(w *oprowswire.HeldLock) []byte {
			buf := make([]byte, max(oprowswire.HeldLockToJsonMeasure(w), 0))
			return buf[:max(oprowswire.HeldLockToJson(w, buf), 0)]
		},
		save: func(w *oprowswire.HeldLock) []byte {
			buf := make([]byte, max(oprowswire.HeldLockMeasure(w), 0))
			n := oprowswire.HeldLockSave(w, buf)
			if n < 0 {
				return nil
			}
			return buf[:n]
		},
	}
}

// heldLockRowFns is HELDLOCK (S1, S2, S4). The fixed table has no JSON text
// form (S1); a fence value that is JSON text is the operation record's, read
// whole through its table and its holder taken from it.
func heldLockRowFns() RowFns[heldLock] {
	fns := opRowFns("HELDLOCK", heldLockTable(), func(h *heldLock) (heldLockSchema, bool) {
		return heldLockSchema{ID: h.ID, Verb: h.Verb, At: opNanos(h.At)}, true
	}, func(m *heldLockSchema) (heldLock, bool) {
		return heldLock{ID: m.ID, Verb: m.Verb, At: opTimeOf(m.At)}, true
	}, nil, nil)
	record := opRecordRowFns().LoadJSON
	fns.LoadJSON = func(v *heldLock, text []byte) RowReport {
		var op OpRecord
		rr := record(&op, text)
		if !rr.refuses() {
			*v = heldLock{ID: op.ID, Lock: op.Lock, Verb: op.Verb, At: op.At}
		}
		return rr
	}
	return fns
}

// ---------------------------------------------------------------------------
// The timers (TIMERS, sprint.Timers).

type opTimerSchema struct {
	ID    string `json:"id"`
	Wakes string `json:"wakes"`
	Due   int64  `json:"due"`
	Note  string `json:"note"`
	By    string `json:"by"`
	Set   int64  `json:"set"`
}

type opTimersSchema struct {
	OpenTimers []opTimerSchema `json:"open_timers"`
}

func opTimersTo(ts *sprint.Timers) opTimersSchema {
	s := opTimersSchema{OpenTimers: make([]opTimerSchema, len(ts.Open))}
	for i, t := range ts.Open {
		s.OpenTimers[i] = opTimerSchema{ID: t.ID, Wakes: t.For, Due: opNanos(t.Due), Note: t.Note, By: t.By, Set: opNanos(t.Set)}
	}
	return s
}

func opTimersFrom(m *opTimersSchema) sprint.Timers {
	var ts sprint.Timers
	for _, t := range m.OpenTimers {
		ts.Open = append(ts.Open, sprint.Timer{ID: t.ID, For: t.Wakes, Due: opTimeOf(t.Due), Note: t.Note, By: t.By, Set: opTimeOf(t.Set)})
	}
	return ts
}

func timersTable() opTable[oprowswire.Timers] {
	return opVariable(oprowswire.TimersLoadMeasureReason, oprowswire.TimersLoad,
		func(b *oprowswire.TimersBuilder) bool { return b.Init() }, oprowswire.TimersFromJson,
		func(b *oprowswire.TimersBuilder) bool { return b.Lock() }, func(b *oprowswire.TimersBuilder) *oprowswire.Timers { return b.AsConst() },
		func(w *oprowswire.Timers) int64 { return oprowswire.TimersToJsonMeasure(w) },
		func(w *oprowswire.Timers, buf []byte) int64 { return oprowswire.TimersToJson(w, buf) },
		func(w *oprowswire.Timers) int64 { return oprowswire.TimersMeasure(w) },
		func(w *oprowswire.Timers, buf []byte) int64 { return oprowswire.TimersSave(w, buf) })
}

// timersRowFns is TIMERS (S1, S2, S4).
func timersRowFns() RowFns[sprint.Timers] {
	return opRowFns("TIMERS", timersTable(), func(ts *sprint.Timers) (opTimersSchema, bool) {
		return opTimersTo(ts), true
	}, func(m *opTimersSchema) (sprint.Timers, bool) {
		return opTimersFrom(m), true
	}, nil, nil)
}

// ---------------------------------------------------------------------------
// The members a table change names (MEMBERS): the members list of a change
// event, the members of its batch's account and the teardown member list. The
// legacy form is the list the table layer writes, `[{"id":...},...]`, whose
// entries carry more than the id; the schema's MemberList is an object.

type opMemberSchema struct {
	ID string `json:"id"`
}

type opMemberListSchema struct {
	Members []opMemberSchema `json:"members"`
}

func memberListTable() opTable[oprowswire.MemberList] {
	return opVariable(oprowswire.MemberListLoadMeasureReason, oprowswire.MemberListLoad,
		func(b *oprowswire.MemberListBuilder) bool { return b.Init() }, oprowswire.MemberListFromJson,
		func(b *oprowswire.MemberListBuilder) bool { return b.Lock() }, func(b *oprowswire.MemberListBuilder) *oprowswire.MemberList { return b.AsConst() },
		func(w *oprowswire.MemberList) int64 { return oprowswire.MemberListToJsonMeasure(w) },
		func(w *oprowswire.MemberList, buf []byte) int64 { return oprowswire.MemberListToJson(w, buf) },
		func(w *oprowswire.MemberList) int64 { return oprowswire.MemberListMeasure(w) },
		func(w *oprowswire.MemberList, buf []byte) int64 { return oprowswire.MemberListSave(w, buf) })
}

// legacyMemberList says a value is the table layer's list, today's legacy form.
func legacyMemberList(in []byte) bool {
	t := bytes.TrimLeft(in, " \t\r\n")
	return len(t) > 0 && t[0] == '['
}

// loadLegacyMemberList reads the table layer's list: each entry's id, the rest
// of the entry being the table layer's own (S2).
func loadLegacyMemberList(v *[]string, in []byte) RowReport {
	var ms []opMemberSchema
	if err := json.Unmarshal(in, &ms); err != nil {
		return RowReport{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	*v = nil
	for _, m := range ms {
		*v = append(*v, m.ID)
	}
	return RowReport{}
}

// membersRowFns is MEMBERS (S1, S2, S4).
func membersRowFns() RowFns[[]string] {
	return opRowFns("MEMBERS", memberListTable(), func(ids *[]string) (opMemberListSchema, bool) {
		s := opMemberListSchema{Members: make([]opMemberSchema, len(*ids))}
		for i, id := range *ids {
			s.Members[i] = opMemberSchema{ID: id}
		}
		return s, true
	}, func(m *opMemberListSchema) ([]string, bool) {
		var out []string
		for _, e := range m.Members {
			out = append(out, e.ID)
		}
		return out, true
	}, legacyMemberList, loadLegacyMemberList)
}

// ---------------------------------------------------------------------------
// The notes and note lines (NOTES, sprint.Note; LOGLINES, sprint.Line).

type opTransitionSchema struct {
	State   string    `json:"state"`
	Cause   string    `json:"cause"`
	Waiting int64     `json:"waiting"`
	Owners  []opValue `json:"owners"`
}

type opDecisionSchema struct {
	Lane            string    `json:"lane"`
	To              string    `json:"to"`
	From            string    `json:"from"`
	Set             string    `json:"set"`
	Body            string    `json:"body"`
	Token           string    `json:"token"`
	PreparedAt      int64     `json:"prepared_at"`
	RetryBefore     int64     `json:"retry_before"`
	SentAt          int64     `json:"sent_at"`
	Fault           string    `json:"fault"`
	Reports         []opValue `json:"reports"`
	UnknownReceipts int64     `json:"unknown_receipts"`
}

type opCoordinatorSchema struct {
	CoveredBy   string `json:"covered_by"`
	From        string `json:"from"`
	To          string `json:"to"`
	Token       string `json:"token"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	Episode     string `json:"episode"`
	Sent        bool   `json:"sent"`
	RetryBefore int64  `json:"retry_before"`
}

type opNoteSchema struct {
	ID                     string               `json:"id"`
	SafeDefault            string               `json:"safe_default"`
	DefaultCoordinatorTold bool                 `json:"default_coordinator_told"`
	DefaultOwnerTold       bool                 `json:"default_owner_told"`
	Alias                  string               `json:"alias"`
	Kind                   string               `json:"kind"`
	NoteType               string               `json:"note_type"`
	Stream                 string               `json:"stream"`
	Primaries              []opValue            `json:"primaries"`
	Count                  int64                `json:"count"`
	What                   string               `json:"what"`
	Who                    string               `json:"who"`
	Attempt                int64                `json:"attempt"`
	Before                 int64                `json:"before"`
	At                     int64                `json:"at"`
	Decisions              []opValue            `json:"decisions"`
	Marked                 bool                 `json:"marked"`
	Answers                string               `json:"answers"`
	Suspects               []opValue            `json:"suspects"`
	Card                   string               `json:"card"`
	Other                  string               `json:"other"`
	Tier                   string               `json:"tier"`
	OtherStream            string               `json:"other_stream"`
	StreamLevel            bool                 `json:"stream_level"`
	SprintLevel            bool                 `json:"sprint_level"`
	Needs                  []opValue            `json:"needs"`
	Review                 int64                `json:"review"`
	ReviewSet              int64                `json:"review_set"`
	To                     string               `json:"to"`
	Hint                   string               `json:"hint"`
	StreamTransition       *opTransitionSchema  `json:"stream_transition,omitempty"`
	DecisionDelivery       *opDecisionSchema    `json:"decision_delivery,omitempty"`
	CoordinatorDelivery    *opCoordinatorSchema `json:"coordinator_delivery,omitempty"`
}

func opNoteTo(n *sprint.Note) (opNoteSchema, bool) {
	count, ok1 := opInt32(n.Count)
	attempt, ok2 := opInt32(n.Attempt)
	before, ok3 := opInt32(n.Before)
	s := opNoteSchema{
		ID: n.ID, SafeDefault: n.SafeDefault, DefaultCoordinatorTold: n.DefaultCoordinatorTold, DefaultOwnerTold: n.DefaultOwnerTold,
		Alias: n.Alias, Kind: n.Kind, NoteType: n.Type, Stream: n.Stream, Primaries: opValues(n.Primaries), Count: count,
		What: n.What, Who: n.Who, Attempt: attempt, Before: before, At: opNanos(n.At), Decisions: opValues(n.Decisions),
		Marked: n.Marked, Answers: n.Answers, Suspects: opValues(n.Suspects), Card: n.Card, Other: n.Other, Tier: n.Tier,
		OtherStream: n.OtherStream, StreamLevel: n.StreamLevel, SprintLevel: n.SprintLevel, Needs: opValues(n.Needs),
		Review: opNanos(n.Review), ReviewSet: opNanos(n.ReviewSet), To: n.To, Hint: n.Hint,
	}
	ok4 := true
	if t := n.StreamTransition; t != nil {
		w, ok := opInt32(t.Waiting)
		ok4 = ok
		s.StreamTransition = &opTransitionSchema{State: t.State, Cause: t.Cause, Waiting: w, Owners: opValues(t.Owners)}
	}
	ok5 := true
	if d := n.DecisionDelivery; d != nil {
		u, ok := opInt32(d.UnknownReceipts)
		ok5 = ok
		s.DecisionDelivery = &opDecisionSchema{
			Lane: d.Lane, To: d.To, From: d.From, Set: d.Set, Body: d.Body, Token: d.Token,
			PreparedAt: opNanos(d.PreparedAt), RetryBefore: opNanos(d.RetryBefore), SentAt: opNanos(d.SentAt),
			Fault: d.Fault, Reports: opValues(d.Reports), UnknownReceipts: u,
		}
	}
	if c := n.CoordinatorDelivery; c != nil {
		s.CoordinatorDelivery = &opCoordinatorSchema{
			CoveredBy: c.CoveredBy, From: c.From, To: c.To, Token: c.Token, Subject: c.Subject, Body: c.Body,
			Episode: c.Episode, Sent: c.Sent, RetryBefore: opNanos(c.RetryBefore),
		}
	}
	return s, ok1 && ok2 && ok3 && ok4 && ok5
}

func opNoteFrom(m *opNoteSchema) sprint.Note {
	n := sprint.Note{
		ID: m.ID, SafeDefault: m.SafeDefault, DefaultCoordinatorTold: m.DefaultCoordinatorTold, DefaultOwnerTold: m.DefaultOwnerTold,
		Alias: m.Alias, Kind: m.Kind, Type: m.NoteType, Stream: m.Stream, Primaries: opStrings(m.Primaries), Count: int(m.Count),
		What: m.What, Who: m.Who, Attempt: int(m.Attempt), Before: int(m.Before), At: opTimeOf(m.At), Decisions: opStrings(m.Decisions),
		Marked: m.Marked, Answers: m.Answers, Suspects: opStrings(m.Suspects), Card: m.Card, Other: m.Other, Tier: m.Tier,
		OtherStream: m.OtherStream, StreamLevel: m.StreamLevel, SprintLevel: m.SprintLevel, Needs: opStrings(m.Needs),
		Review: opTimeOf(m.Review), ReviewSet: opTimeOf(m.ReviewSet), To: m.To, Hint: m.Hint,
	}
	if t := m.StreamTransition; t != nil {
		n.StreamTransition = &sprint.StreamTransition{State: t.State, Cause: t.Cause, Waiting: int(t.Waiting), Owners: opStrings(t.Owners)}
	}
	if d := m.DecisionDelivery; d != nil {
		n.DecisionDelivery = &sprint.DecisionDelivery{
			Lane: d.Lane, To: d.To, From: d.From, Set: d.Set, Body: d.Body, Token: d.Token,
			PreparedAt: opTimeOf(d.PreparedAt), RetryBefore: opTimeOf(d.RetryBefore), SentAt: opTimeOf(d.SentAt),
			Fault: d.Fault, Reports: opStrings(d.Reports), UnknownReceipts: int(d.UnknownReceipts),
		}
	}
	if c := m.CoordinatorDelivery; c != nil {
		n.CoordinatorDelivery = &sprint.CoordinatorDelivery{
			CoveredBy: c.CoveredBy, From: c.From, To: c.To, Token: c.Token, Subject: c.Subject, Body: c.Body,
			Episode: c.Episode, Sent: c.Sent, RetryBefore: opTimeOf(c.RetryBefore),
		}
	}
	return n
}

func noteTable() opTable[oprowswire.Note] {
	return opVariable(oprowswire.NoteLoadMeasureReason, oprowswire.NoteLoad,
		func(b *oprowswire.NoteBuilder) bool { return b.Init() }, oprowswire.NoteFromJson,
		func(b *oprowswire.NoteBuilder) bool { return b.Lock() }, func(b *oprowswire.NoteBuilder) *oprowswire.Note { return b.AsConst() },
		func(w *oprowswire.Note) int64 { return oprowswire.NoteToJsonMeasure(w) },
		func(w *oprowswire.Note, buf []byte) int64 { return oprowswire.NoteToJson(w, buf) },
		func(w *oprowswire.Note) int64 { return oprowswire.NoteMeasure(w) },
		func(w *oprowswire.Note, buf []byte) int64 { return oprowswire.NoteSave(w, buf) })
}

// notesRowFns is NOTES (S1, S2, S4).
func notesRowFns() RowFns[sprint.Note] {
	return opRowFns("NOTES", noteTable(), opNoteTo, func(m *opNoteSchema) (sprint.Note, bool) {
		return opNoteFrom(m), true
	}, nil, nil)
}

type opCardTextSchema struct {
	Name string  `json:"name"`
	Text *string `json:"text"`
}

type opPairSchema struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type opLineSchema struct {
	Kind      string             `json:"kind"`
	At        int64              `json:"at"`
	Epoch     uint64             `json:"epoch"`
	Op        string             `json:"op"`
	Card      string             `json:"card"`
	Cards     []opValue          `json:"cards"`
	Primary   string             `json:"primary"`
	Stream    string             `json:"stream"`
	TableName string             `json:"table_name"`
	From      string             `json:"from"`
	To        string             `json:"to"`
	Removed   bool               `json:"removed"`
	Gen       int64              `json:"gen"`
	Actor     string             `json:"actor"`
	Verb      string             `json:"verb"`
	Cause     string             `json:"cause"`
	Answers   []opValue          `json:"answers"`
	Text      []opCardTextSchema `json:"text"`
	Set       []opPairSchema     `json:"set"`
	Note      *opNoteSchema      `json:"note,omitempty"`
}

// opKeys is a map's keys in order: oprows.schema holds a map as a list of
// pairs in key order.
func opKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func opLineTo(l *sprint.Line) (opLineSchema, bool) {
	gen, ok := opInt32(l.Gen)
	s := opLineSchema{
		Kind: l.Kind, At: opNanos(l.At), Epoch: l.Epoch, Op: l.Op, Card: l.Card, Cards: opValues(l.Cards),
		Primary: l.Primary, Stream: l.Stream, TableName: l.Table, From: l.From, To: l.To, Removed: l.Removed,
		Gen: gen, Actor: l.Actor, Verb: l.Verb, Cause: l.Cause, Answers: opValues(l.Answers),
		Text: []opCardTextSchema{}, Set: []opPairSchema{},
	}
	for _, k := range opKeys(l.Text) {
		t := l.Text[k]
		ok = ok && len(t) <= sprint.MaxCardTextBytes // the blob's bound (oprows.schema CardText)
		s.Text = append(s.Text, opCardTextSchema{Name: k, Text: &t})
	}
	for _, k := range opKeys(l.Set) {
		s.Set = append(s.Set, opPairSchema{Name: k, Value: l.Set[k]})
	}
	if l.Note != nil {
		n, nok := opNoteTo(l.Note)
		ok = ok && nok
		s.Note = &n
	}
	return s, ok
}

// opLineFrom reads a line back. A card text is a blob, which declares no bound:
// the reader holds it to sprint.MaxCardTextBytes (oprows.schema CardText).
func opLineFrom(m *opLineSchema) (sprint.Line, bool) {
	l := sprint.Line{
		Kind: m.Kind, At: opTimeOf(m.At), Epoch: m.Epoch, Op: m.Op, Card: m.Card, Cards: opStrings(m.Cards),
		Primary: m.Primary, Stream: m.Stream, Table: m.TableName, From: m.From, To: m.To, Removed: m.Removed,
		Gen: int(m.Gen), Actor: m.Actor, Verb: m.Verb, Cause: m.Cause, Answers: opStrings(m.Answers),
	}
	for _, t := range m.Text {
		if t.Text != nil && len(*t.Text) > sprint.MaxCardTextBytes {
			return sprint.Line{}, false
		}
		if l.Text == nil {
			l.Text = map[string]string{}
		}
		if t.Text != nil {
			l.Text[t.Name] = *t.Text
		} else {
			l.Text[t.Name] = ""
		}
	}
	for _, p := range m.Set {
		if l.Set == nil {
			l.Set = map[string]string{}
		}
		l.Set[p.Name] = p.Value
	}
	if m.Note != nil {
		n := opNoteFrom(m.Note)
		l.Note = &n
	}
	return l, true
}

func logLineTable() opTable[oprowswire.LogLine] {
	return opVariable(oprowswire.LogLineLoadMeasureReason, oprowswire.LogLineLoad,
		func(b *oprowswire.LogLineBuilder) bool { return b.Init() }, oprowswire.LogLineFromJson,
		func(b *oprowswire.LogLineBuilder) bool { return b.Lock() }, func(b *oprowswire.LogLineBuilder) *oprowswire.LogLine { return b.AsConst() },
		func(w *oprowswire.LogLine) int64 { return oprowswire.LogLineToJsonMeasure(w) },
		func(w *oprowswire.LogLine, buf []byte) int64 { return oprowswire.LogLineToJson(w, buf) },
		func(w *oprowswire.LogLine) int64 { return oprowswire.LogLineMeasure(w) },
		func(w *oprowswire.LogLine, buf []byte) int64 { return oprowswire.LogLineSave(w, buf) })
}

// logLinesRowFns is LOGLINES (S1, S2, S4).
func logLinesRowFns() RowFns[sprint.Line] {
	return opRowFns("LOGLINES", logLineTable(), opLineTo, opLineFrom, nil, nil)
}

// ---------------------------------------------------------------------------
// The queued changes (QUEUE, sprint.QueuedChange): the work table's queue and
// an operation's own. The entry is a nova-tables batch member entry, its own
// protocol, held whole as a blob: its legacy JSON bytes.

type opPropSchema struct {
	TableName string `json:"table_name"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	Was       string `json:"was"`
	WasAbsent bool   `json:"was_absent"`
}

type opQueuedSchema struct {
	Entry *[]byte       `json:"entry"`
	Prop  *opPropSchema `json:"prop,omitempty"`
	Verb  string        `json:"verb"`
	Actor string        `json:"actor"`
	Moved string        `json:"moved"`
}

func opQueuedTo(q *sprint.QueuedChange) (opQueuedSchema, bool) {
	s := opQueuedSchema{Verb: q.Verb, Actor: q.Actor, Moved: q.Moved}
	if q.Entry != nil {
		b, err := json.Marshal(q.Entry)
		if err != nil {
			return s, false
		}
		s.Entry = &b
	}
	if p := q.Prop; p != nil {
		s.Prop = &opPropSchema{TableName: p.Table, Name: p.Name, Value: p.Value, Was: p.Was, WasAbsent: p.WasAbsent}
	}
	return s, true
}

func opQueuedFrom(m *opQueuedSchema) (sprint.QueuedChange, bool) {
	q := sprint.QueuedChange{Verb: m.Verb, Actor: m.Actor, Moved: m.Moved}
	if m.Entry != nil {
		var e ntable.BatchMemberEntry
		if err := json.Unmarshal(*m.Entry, &e); err != nil {
			return sprint.QueuedChange{}, false
		}
		q.Entry = &e
	}
	if p := m.Prop; p != nil {
		q.Prop = &sprint.PropWrite{Table: p.TableName, Name: p.Name, Value: p.Value, Was: p.Was, WasAbsent: p.WasAbsent}
	}
	return q, true
}

func queuedTable() opTable[oprowswire.QueuedChange] {
	return opVariable(oprowswire.QueuedChangeLoadMeasureReason, oprowswire.QueuedChangeLoad,
		func(b *oprowswire.QueuedChangeBuilder) bool { return b.Init() }, oprowswire.QueuedChangeFromJson,
		func(b *oprowswire.QueuedChangeBuilder) bool { return b.Lock() }, func(b *oprowswire.QueuedChangeBuilder) *oprowswire.QueuedChange { return b.AsConst() },
		func(w *oprowswire.QueuedChange) int64 { return oprowswire.QueuedChangeToJsonMeasure(w) },
		func(w *oprowswire.QueuedChange, buf []byte) int64 { return oprowswire.QueuedChangeToJson(w, buf) },
		func(w *oprowswire.QueuedChange) int64 { return oprowswire.QueuedChangeMeasure(w) },
		func(w *oprowswire.QueuedChange, buf []byte) int64 { return oprowswire.QueuedChangeSave(w, buf) })
}

// queueRowFns is QUEUE (S1, S2, S4).
func queueRowFns() RowFns[sprint.QueuedChange] {
	return opRowFns("QUEUE", queuedTable(), opQueuedTo, opQueuedFrom, nil, nil)
}

// ---------------------------------------------------------------------------
// The step result (STEPRESULT, Result): what a step did, recorded under the
// caller's operation id and replayed by it.

type opRefusalSchema struct {
	ID  string `json:"id"`
	Why string `json:"why"`
}

type opTableCountSchema struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type opResultSchema struct {
	Verb     string               `json:"verb"`
	Op       string               `json:"op"`
	Moved    []opValue            `json:"moved"`
	Refused  []opRefusalSchema    `json:"refused"`
	Notes    int64                `json:"notes"`
	Raised   []opValue            `json:"raised"`
	Said     []opValue            `json:"said"`
	Attempts int64                `json:"attempts"`
	Replay   bool                 `json:"replay"`
	Repaired []opValue            `json:"repaired"`
	Pending  string               `json:"pending"`
	Skipped  []opValue            `json:"skipped"`
	Args     string               `json:"args"`
	Lost     bool                 `json:"lost"`
	Tables   []opTableCountSchema `json:"tables"`
	Drained  []*opResultSchema    `json:"drained"`
}

func opResultTo(r *Result) (opResultSchema, bool) {
	notes, ok1 := opInt32(r.Notes)
	attempts, ok2 := opInt32(r.Attempts)
	ok := ok1 && ok2
	s := opResultSchema{
		Verb: r.Verb, Op: r.Op, Moved: opValues(r.Moved), Refused: make([]opRefusalSchema, len(r.Refused)), Notes: notes,
		Raised: opValues(r.Raised), Said: opValues(r.Said), Attempts: attempts, Replay: r.Replay, Repaired: opValues(r.Repaired),
		Pending: r.Pending, Skipped: opValues(r.Skipped), Args: r.Args, Lost: r.Lost,
		Tables: []opTableCountSchema{}, Drained: []*opResultSchema{},
	}
	for i, f := range r.Refused {
		s.Refused[i] = opRefusalSchema{ID: f.Key, Why: f.Why}
	}
	keys := make([]string, 0, len(r.Tables))
	for k := range r.Tables {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n, nok := opInt32(r.Tables[k])
		ok = ok && nok
		s.Tables = append(s.Tables, opTableCountSchema{Name: k, Count: n})
	}
	for i := range r.Drained {
		d, dok := opResultTo(&r.Drained[i])
		ok = ok && dok
		s.Drained = append(s.Drained, &d)
	}
	return s, ok
}

func opResultFrom(m *opResultSchema) Result {
	r := Result{
		Verb: m.Verb, Op: m.Op, Moved: opStrings(m.Moved), Notes: int(m.Notes), Raised: opStrings(m.Raised),
		Said: opStrings(m.Said), Attempts: int(m.Attempts), Replay: m.Replay, Repaired: opStrings(m.Repaired),
		Pending: m.Pending, Skipped: opStrings(m.Skipped), Args: m.Args, Lost: m.Lost,
	}
	for _, f := range m.Refused {
		r.Refused = append(r.Refused, sprint.Refusal{Key: f.ID, Why: f.Why})
	}
	for _, t := range m.Tables {
		if r.Tables == nil {
			r.Tables = map[string]int{}
		}
		r.Tables[t.Name] = int(t.Count)
	}
	for _, d := range m.Drained {
		if d != nil {
			r.Drained = append(r.Drained, opResultFrom(d))
		}
	}
	return r
}

func stepResultTable() opTable[oprowswire.StepResult] {
	return opVariable(oprowswire.StepResultLoadMeasureReason, oprowswire.StepResultLoad,
		func(b *oprowswire.StepResultBuilder) bool { return b.Init() }, oprowswire.StepResultFromJson,
		func(b *oprowswire.StepResultBuilder) bool { return b.Lock() }, func(b *oprowswire.StepResultBuilder) *oprowswire.StepResult { return b.AsConst() },
		func(w *oprowswire.StepResult) int64 { return oprowswire.StepResultToJsonMeasure(w) },
		func(w *oprowswire.StepResult, buf []byte) int64 { return oprowswire.StepResultToJson(w, buf) },
		func(w *oprowswire.StepResult) int64 { return oprowswire.StepResultMeasure(w) },
		func(w *oprowswire.StepResult, buf []byte) int64 { return oprowswire.StepResultSave(w, buf) })
}

// stepResultRowFns is STEPRESULT (S1, S2, S4). The request digest a result
// carries (args, store.ArgsOf) is read with it: the table holds it to its 24
// characters (oprows.schema RequestDigest, StepResult.args).
func stepResultRowFns() RowFns[Result] {
	return opRowFns("STEPRESULT", stepResultTable(), opResultTo, func(m *opResultSchema) (Result, bool) {
		return opResultFrom(m), true
	}, nil, nil)
}

// ---------------------------------------------------------------------------
// The operation journal record (OPRECORD, OpRecord): the record the fence
// holds, whole. Its manifests and its queued changes' entries are nova-tables'
// own protocol, held whole as blobs of their legacy JSON bytes; its result is
// the step result's record (the legacy record holds it as JSON text).

type opManifestSchema struct {
	TableName string  `json:"table_name"`
	Data      *[]byte `json:"data"`
}

type opSeatSchema struct {
	Holder     string `json:"holder"`
	Generation uint64 `json:"generation"`
	From       string `json:"from"`
	At         int64  `json:"at"`
	By         string `json:"by"`
	Taken      bool   `json:"taken"`
	ApprovedBy string `json:"approved_by"`
	Reason     string `json:"reason"`
}

type opRecordSchema struct {
	ID          string             `json:"id"`
	IsLock      bool               `json:"is_lock"`
	Verb        string             `json:"verb"`
	At          int64              `json:"at"`
	Manifests   []opManifestSchema `json:"manifests"`
	Notes       []opNoteSchema     `json:"notes"`
	Decided     []opNoteSchema     `json:"decided"`
	Log         []opLineSchema     `json:"log"`
	Updates     []opNoteSchema     `json:"updates"`
	Closes      []opValue          `json:"closes"`
	Streams     []opValue          `json:"streams"`
	CallerOp    string             `json:"caller_op"`
	Result      *opResultSchema    `json:"result"`
	Stuck       string             `json:"stuck"`
	Queue       []opQueuedSchema   `json:"queue"`
	Drain       int64              `json:"drain"`
	Seat        *opSeatSchema      `json:"seat,omitempty"`
	CloseTimers []opValue          `json:"close_timers"`
	Timers      *opTimersSchema    `json:"timers"`
}

func opNotesTo(ns []sprint.Note) ([]opNoteSchema, bool) {
	out := make([]opNoteSchema, len(ns))
	ok := true
	for i := range ns {
		s, sok := opNoteTo(&ns[i])
		ok = ok && sok
		out[i] = s
	}
	return out, ok
}

func opNotesFrom(ms []opNoteSchema) []sprint.Note {
	if len(ms) == 0 {
		return nil
	}
	out := make([]sprint.Note, len(ms))
	for i := range ms {
		out[i] = opNoteFrom(&ms[i])
	}
	return out
}

func opRecordTo(o *OpRecord) (opRecordSchema, bool) {
	drain, ok := opInt32(o.Drain)
	s := opRecordSchema{
		ID: o.ID, IsLock: o.Lock, Verb: o.Verb, At: opNanos(o.At), Manifests: make([]opManifestSchema, len(o.Manifests)),
		Log: make([]opLineSchema, len(o.Log)), Closes: opValues(o.Closes), Streams: opValues(o.Streams), CallerOp: o.CallerOp,
		Stuck: o.Stuck, Queue: make([]opQueuedSchema, len(o.Queue)), Drain: drain, CloseTimers: opValues(o.CloseTimers),
	}
	for i, m := range o.Manifests {
		b, err := json.Marshal(m)
		if err != nil {
			return s, false
		}
		s.Manifests[i] = opManifestSchema{TableName: m.Table, Data: &b}
	}
	var nok, dok, uok bool
	s.Notes, nok = opNotesTo(o.Notes)
	s.Decided, dok = opNotesTo(o.Decided)
	s.Updates, uok = opNotesTo(o.Updates)
	ok = ok && nok && dok && uok
	for i := range o.Log {
		l, lok := opLineTo(&o.Log[i])
		ok = ok && lok
		s.Log[i] = l
	}
	if o.Result != "" {
		var r Result
		if err := json.Unmarshal([]byte(o.Result), &r); err != nil {
			return s, false
		}
		rs, rok := opResultTo(&r)
		ok = ok && rok
		s.Result = &rs
	}
	for i := range o.Queue {
		q, qok := opQueuedTo(&o.Queue[i])
		ok = ok && qok
		s.Queue[i] = q
	}
	if c := o.Seat; c != nil {
		s.Seat = &opSeatSchema{Holder: c.Holder, Generation: c.Generation, From: c.From, At: opNanos(c.At), By: c.By, Taken: c.Taken, ApprovedBy: c.ApprovedBy, Reason: c.Reason}
	}
	if o.Timers != nil {
		ts := opTimersTo(o.Timers)
		s.Timers = &ts
	}
	return s, ok
}

// opRecordFrom reads an operation record back. A manifest is a blob, which
// declares no bound: the reader holds it to ntable.LimitManifestBytes
// (oprows.schema Manifest), and its bytes are the manifest's own, never
// decoded past what OpRecord holds.
func opRecordFrom(m *opRecordSchema) (OpRecord, bool) {
	o := OpRecord{
		ID: m.ID, Lock: m.IsLock, Verb: m.Verb, At: opTimeOf(m.At), Notes: opNotesFrom(m.Notes), Decided: opNotesFrom(m.Decided),
		Updates: opNotesFrom(m.Updates), Closes: opStrings(m.Closes), Streams: opStrings(m.Streams), CallerOp: m.CallerOp,
		Stuck: m.Stuck, Drain: int(m.Drain), CloseTimers: opStrings(m.CloseTimers),
	}
	for _, mf := range m.Manifests {
		var bm ntable.BatchManifest
		if mf.Data == nil || len(*mf.Data) > ntable.LimitManifestBytes || json.Unmarshal(*mf.Data, &bm) != nil || bm.Table != mf.TableName {
			return OpRecord{}, false
		}
		o.Manifests = append(o.Manifests, bm)
	}
	for i := range m.Log {
		l, ok := opLineFrom(&m.Log[i])
		if !ok {
			return OpRecord{}, false
		}
		o.Log = append(o.Log, l)
	}
	if m.Result != nil {
		body, err := json.Marshal(opResultFrom(m.Result))
		if err != nil {
			return OpRecord{}, false
		}
		o.Result = string(body)
	}
	for i := range m.Queue {
		q, ok := opQueuedFrom(&m.Queue[i])
		if !ok {
			return OpRecord{}, false
		}
		o.Queue = append(o.Queue, q)
	}
	if c := m.Seat; c != nil {
		o.Seat = &sprint.SeatChange{Holder: c.Holder, Generation: c.Generation, From: c.From, At: opTimeOf(c.At), By: c.By, Taken: c.Taken, ApprovedBy: c.ApprovedBy, Reason: c.Reason}
	}
	if m.Timers != nil {
		ts := opTimersFrom(m.Timers)
		o.Timers = &ts
	}
	return o, true
}

func opRecordTable() opTable[oprowswire.OpRecord] {
	return opVariable(oprowswire.OpRecordLoadMeasureReason, oprowswire.OpRecordLoad,
		func(b *oprowswire.OpRecordBuilder) bool { return b.Init() }, oprowswire.OpRecordFromJson,
		func(b *oprowswire.OpRecordBuilder) bool { return b.Lock() }, func(b *oprowswire.OpRecordBuilder) *oprowswire.OpRecord { return b.AsConst() },
		func(w *oprowswire.OpRecord) int64 { return oprowswire.OpRecordToJsonMeasure(w) },
		func(w *oprowswire.OpRecord, buf []byte) int64 { return oprowswire.OpRecordToJson(w, buf) },
		func(w *oprowswire.OpRecord) int64 { return oprowswire.OpRecordMeasure(w) },
		func(w *oprowswire.OpRecord, buf []byte) int64 { return oprowswire.OpRecordSave(w, buf) })
}

// opRecordRowFns is OPRECORD (S1, S2, S4).
func opRecordRowFns() RowFns[OpRecord] {
	return opRowFns("OPRECORD", opRecordTable(), opRecordTo, opRecordFrom, nil, nil)
}

// ---------------------------------------------------------------------------
// The readers and the read helpers the read sites call.

// opRowCodecs is the operation rows' readers.
type opRowCodecs struct {
	op      *RowCodec[OpRecord]
	lock    *RowCodec[heldLock]
	notes   *RowCodec[sprint.Note]
	lines   *RowCodec[sprint.Line]
	timers  *RowCodec[sprint.Timers]
	queue   *RowCodec[sprint.QueuedChange]
	members *RowCodec[[]string]
	result  *RowCodec[Result]
}

func newOpRowCodecs() *opRowCodecs {
	return &opRowCodecs{
		op:      NewRowCodec(opRecordRowFns()),
		lock:    NewRowCodec(heldLockRowFns()),
		notes:   NewRowCodec(notesRowFns()),
		lines:   NewRowCodec(logLinesRowFns()),
		timers:  NewRowCodec(timersRowFns()),
		queue:   NewRowCodec(queueRowFns()),
		members: NewRowCodec(membersRowFns()),
		result:  NewRowCodec(stepResultRowFns()),
	}
}

// opRows is the process's operation rows' readers, registered process-wide so
// every store's check lists them, at zero before their first read (S4).
var opRows = func() *opRowCodecs {
	c := newOpRowCodecs()
	c.op.RegisterProcess()
	c.lock.RegisterProcess()
	c.notes.RegisterProcess()
	c.lines.RegisterProcess()
	c.timers.RegisterProcess()
	c.queue.RegisterProcess()
	c.members.RegisterProcess()
	c.result.RegisterProcess()
	return c
}()

// heldID is the id of the operation a fence value holds, read through HELDLOCK;
// a value it cannot read is refused by name, never read as an empty fence (S2).
func (c *opRowCodecs) heldID(raw string) (string, error) {
	h, _, err := c.lock.Read([]byte(raw))
	if err != nil {
		return "", err
	}
	return h.ID, nil
}

// fenceRecord is the operation record a fence value holds, read through
// OPRECORD and refused by name when damaged (S2).
func (c *opRowCodecs) fenceRecord(raw string) (*OpRecord, error) {
	op, _, err := c.op.Read([]byte(raw))
	if err != nil {
		return nil, err
	}
	return &op, nil
}

// memberIDs is the members a change names, read through MEMBERS: the table
// layer's list, the table wire, its base64 or the schema JSON text. An empty
// value or the table layer's empty list names none (S2).
func (c *opRowCodecs) memberIDs(raw string) ([]string, error) {
	if m := strings.TrimSpace(raw); m == "" || m == "[]" || m == "{}" || m == "null" {
		return nil, nil
	}
	ids, _, err := c.members.Read([]byte(raw))
	return ids, err
}

// stepResult is a recorded step result read through STEPRESULT (S2).
func (c *opRowCodecs) stepResult(raw string) (Result, error) {
	r, _, err := c.result.Read([]byte(raw))
	return r, err
}

// queueEntries is the work table's queue read through QUEUE in any form; a
// damaged entry is refused by name (S2, C13).
func (c *opRowCodecs) queueEntries(raw []string) ([]sprint.QueuedChange, error) {
	out := make([]sprint.QueuedChange, len(raw))
	for i, b := range raw {
		q, _, err := c.queue.Read([]byte(b))
		if err != nil {
			return nil, fmt.Errorf("the work table's queue holds an unreadable entry: %w", err)
		}
		out[i] = q
	}
	return out, nil
}

// errNoEntry is a stream entry that holds no value of its record.
var errNoEntry = errors.New("no entry")

// inboxNote is one inbox entry's note read through NOTES in any form. The
// inbox read skips an entry it cannot read on purpose (rangeSince: an entry
// whose decode fails is skipped whole, so one damaged note does not hide the
// rest); a damaged note is still refused by name and counted under NOTES as a
// refusal (S2, S4, C13), never read as an empty note.
func (c *opRowCodecs) inboxNote(v map[string]any) (sprint.Note, error) {
	s, ok := v["note"].(string)
	if !ok {
		return sprint.Note{}, errNoEntry
	}
	n, _, err := c.notes.Read([]byte(s))
	return n, err
}

// logLine is one log entry's line read through LOGLINES in any form. The log
// read skips an entry it cannot read on purpose (rangeSince, as inboxNote); a
// damaged line is refused by name and counted under LOGLINES as a refusal (S2,
// S4, C13), never read as an empty line.
func (c *opRowCodecs) logLine(v map[string]any) (sprint.Line, error) {
	s, ok := v["line"].(string)
	if !ok {
		return sprint.Line{}, errNoEntry
	}
	l, _, err := c.lines.Read([]byte(s))
	return l, err
}

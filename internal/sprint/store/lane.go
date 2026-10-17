package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
)

// The lanes (sprint/lane.go; docs/SPEC-SPRINT.md section 18): one record per lane
// kind under the deployment's prefix, lanes:<kind>, outside the tables and the fence,
// holding every machine's holders and queue. A take or a give is one read of the
// width (the work table's go_lanes property), one read of the record and one write
// of it; the server runs the workers' verbs one at a time (run --listen), so no two
// steps of the record interleave. It works while the machine is RUNNING or STOPPED.

// laneKey is a lane kind's record.
func laneKey(kind string) string { return "lanes:" + kind }

// laneWhy is why the kind, the machine or the worker is refused, every problem at
// once; "" when none is.
func laneWhy(kind, machine, who string) string {
	var why []string
	if !slices.Contains(sprint.LaneKinds, kind) {
		why = append(why, "a lane kind is one of "+strings.Join(sprint.LaneKinds, ", ")+", found "+kind)
	}
	if !sprint.ValidID(machine) {
		why = append(why, "a machine name wants letters, digits, _ and -: "+machine)
	}
	if !sprint.ValidLaneWho(who) {
		why = append(why, "a worker name wants letters, digits, _ and -, or two such joined by one / (lander/<stream>): "+who)
	}
	return strings.Join(why, "; ")
}

// LaneWidth is the lanes of a kind each machine has: the work table's go_lanes, else
// sprint.LaneWidthDefault (no sprint, or no setting).
func (st *Store) LaneWidth(ctx context.Context) (int, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return 0, err
	}
	shapes, err := pinned.B.Shapes(ctx, []string{pinned.Names.Table(sprint.Work)})
	if refusalCode(err) == "NOTABLE" {
		return sprint.LaneWidthDefault, nil // no sprint yet: the default
	}
	if err != nil {
		return 0, err
	}
	var v string
	var set bool
	if len(shapes) > 0 {
		v, set = shapes[0].Props[sprint.PropGoLanes]
	}
	return sprint.LaneWidth(v, set), nil
}

// laneHoldSchema is the LaneHold table's schema JSON text (friendrows.schema
// LaneHold): the open text and the nullable stamps.
type laneHoldSchema struct {
	Who     *string `json:"who"`
	Claimed bool    `json:"claimed"`
	Since   *string `json:"since"`
	Seen    *string `json:"seen"`
}

// laneWaitSchema is the LaneWait table's schema JSON text (friendrows.schema
// LaneWait).
type laneWaitSchema struct {
	Who   *string `json:"who"`
	Since *string `json:"since"`
	Seen  *string `json:"seen"`
}

// laneSchema is the Lane table's schema JSON text (friendrows.schema Lane): one
// machine's holders and queue.
type laneSchema struct {
	Holders []laneHoldSchema `json:"holders"`
	Queue   []laneWaitSchema `json:"queue"`
}

// laneTable is the generated Lane variable table (S1, S2).
func laneTable() friendTable[friendrowswire.Lane] {
	return friendTable[friendrowswire.Lane]{
		measure: friendrowswire.LaneLoadMeasureReason,
		load:    friendrowswire.LaneLoad,
		fromJSON: func(text []byte, rep *friendrowswire.TableReport) (*friendrowswire.Lane, bool) {
			var b friendrowswire.LaneBuilder
			if !b.Init() {
				return nil, false
			}
			if !friendrowswire.LaneFromJson(&b, text, rep) || !b.Lock() {
				return nil, false
			}
			return b.AsConst(), true
		},
		toJSON: func(w *friendrowswire.Lane) []byte {
			buf := make([]byte, max(friendrowswire.LaneToJsonMeasure(w), 0))
			return buf[:max(friendrowswire.LaneToJson(w, buf), 0)]
		},
		save: func(w *friendrowswire.Lane) []byte {
			buf := make([]byte, max(friendrowswire.LaneMeasure(w), 0))
			return buf[:max(friendrowswire.LaneSave(w, buf), 0)]
		},
	}
}

// laneRowFns adapts the Lane table to the store row reader: LANES (S1, S2, S4).
// The record is a map of machines, so the map envelope stays today's JSON object
// and each machine's value reads through the generated Lane table (as the friend
// roster reads each entry through FriendEntry, presence.go presenceRowMap).
func laneRowFns() RowFns[sprint.Lane] {
	return presenceRowFns("LANES", laneTable(), func(l *sprint.Lane) laneSchema {
		s := laneSchema{
			Holders: make([]laneHoldSchema, len(l.Holders)),
			Queue:   make([]laneWaitSchema, len(l.Queue)),
		}
		for i, h := range l.Holders {
			s.Holders[i] = laneHoldSchema{Who: rowText(h.Who), Claimed: h.Claimed, Since: rowTime(h.Since), Seen: rowTime(h.Seen)}
		}
		for i, w := range l.Queue {
			s.Queue[i] = laneWaitSchema{Who: rowText(w.Who), Since: rowTime(w.Since), Seen: rowTime(w.Seen)}
		}
		return s
	}, func(m *laneSchema) (sprint.Lane, bool) {
		l := sprint.Lane{
			Holders: make([]sprint.LaneHold, len(m.Holders)),
			Queue:   make([]sprint.LaneWait, len(m.Queue)),
		}
		for i, h := range m.Holders {
			since, ok := rowTimeOf(h.Since)
			if !ok {
				return sprint.Lane{}, false
			}
			seen, ok := rowTimeOf(h.Seen)
			if !ok {
				return sprint.Lane{}, false
			}
			l.Holders[i] = sprint.LaneHold{Who: rowTextOf(h.Who), Claimed: h.Claimed, Since: since, Seen: seen}
		}
		for i, w := range m.Queue {
			since, ok := rowTimeOf(w.Since)
			if !ok {
				return sprint.Lane{}, false
			}
			seen, ok := rowTimeOf(w.Seen)
			if !ok {
				return sprint.Lane{}, false
			}
			l.Queue[i] = sprint.LaneWait{Who: rowTextOf(w.Who), Since: since, Seen: seen}
		}
		return l, true
	})
}

// laneRowSets is each store's lanes reader, keyed by the store's done reader so a
// pinned copy counts into the store's one reader (S4).
var laneRowSets sync.Map

// lanesRows is this store's reader of one machine's lanes, registered on it so
// Check reports its per-form counts (S4).
func (st *Store) lanesRows() *RowCodec[sprint.Lane] {
	key := st.doneRows()
	v, ok := laneRowSets.Load(key)
	if !ok {
		v, _ = laneRowSets.LoadOrStore(key, NewRowCodec(laneRowFns()))
	}
	c := v.(*RowCodec[sprint.Lane])
	c.Register(st)
	return c
}

// lanes reads a kind's record; none is no lane held. Each machine's value is read
// alike from today's legacy JSON, its table wire and the padded standard base64 of
// that wire, and a damaged value is refused by name (S2, C13), never read as no
// lane held. Writers still write the legacy form (S4).
func (st *Store) lanes(ctx context.Context, kind string) (sprint.Lanes, KV, error) {
	kv, err := st.rootKV()
	if err != nil {
		return nil, nil, err
	}
	out := sprint.Lanes{}
	raw, ok, err := kv.GetKey(ctx, laneKey(kind))
	if err != nil || !ok {
		return out, kv, err
	}
	read, err := presenceRowMap(st.lanesRows(), []byte(raw))
	if err != nil {
		return nil, kv, err
	}
	if read != nil {
		out = read
	}
	return out, kv, nil
}

// LaneStep is a take (give false) or a give of who on machine's lanes of kind at the
// store's clock: the answer, with the record written.
func (st *Store) LaneStep(ctx context.Context, kind, machine, who string, give bool) (sprint.LaneAnswer, error) {
	if why := laneWhy(kind, machine, who); why != "" {
		return sprint.LaneAnswer{}, fmt.Errorf("%s", why)
	}
	if sprint.LanesDisabled {
		// disabled until v1.3 (sprint.LanesDisabled): a take is granted at once, a give
		// gives nothing back, and the record is neither read nor written
		return sprint.LaneAnswer{Granted: !give}, nil
	}
	width, err := st.LaneWidth(ctx)
	if err != nil {
		return sprint.LaneAnswer{}, err
	}
	ls, kv, err := st.lanes(ctx, kind)
	if err != nil {
		return sprint.LaneAnswer{}, err
	}
	now := st.now().UTC()
	var ans sprint.LaneAnswer
	if give {
		ls, ans = ls.Give(machine, who, width, now)
	} else {
		ls, ans = ls.Take(machine, who, width, now)
	}
	b, err := json.Marshal(ls)
	if err != nil {
		return ans, err
	}
	return ans, kv.SetKey(ctx, laneKey(kind), string(b))
}

// LaneRows is every machine's lanes of every kind at the store's clock, the timeouts
// applied as a reader sees them (nothing is written): lane list, where --json --cards. A
// store that keeps no records has no lanes.
func (st *Store) LaneRows(ctx context.Context) ([]sprint.LaneRow, error) {
	if _, err := st.rootKV(); err != nil {
		return nil, nil // ignored: a store that keeps no records has no lanes to show
	}
	width, err := st.LaneWidth(ctx)
	if err != nil {
		return nil, err
	}
	out := []sprint.LaneRow{}
	for _, kind := range sprint.LaneKinds {
		ls, _, err := st.lanes(ctx, kind)
		if err != nil {
			return nil, err
		}
		out = append(out, ls.Rows(kind, width, st.now().UTC())...)
	}
	return out, nil
}

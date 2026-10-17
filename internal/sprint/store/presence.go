package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"
	"unsafe"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// A fleet member's beats (sprint/presence.go): one record per member under
// the deployment's prefix, beat:<member>, outside the tables and the fence,
// written only by the member's own beat. A beat is one read of the record and
// one write of it, whatever the machine's state; the tick reads every
// member's record in one exchange.

// keyStrangers is the machines that beat and are no member of the sprint:
// when each was first seen, and whether the coordinator was told.
const keyStrangers = "strangers"

// stranger is one unknown machine that beats.
type stranger struct {
	Seen time.Time `json:"seen"`
	Told bool      `json:"told,omitempty"`
}

func (st *Store) strangers(ctx context.Context) (map[string]stranger, error) {
	out := map[string]stranger{}
	kv, err := st.rootKV()
	if err != nil {
		return out, err
	}
	raw, ok, err := kv.GetKey(ctx, keyStrangers)
	if err != nil || !ok {
		return out, err
	}
	// An unreadable record is read as none on purpose: the next stranger's note writes it
	// whole again. Each entry is read through the Stranger table (S2, S4).
	if s, err := presenceRowMap(st.presenceRows().stranger, []byte(raw)); err == nil && s != nil {
		out = s
	}
	return out, nil
}

func (st *Store) putStrangers(ctx context.Context, s map[string]stranger) error {
	kv, err := st.rootKV()
	if err != nil {
		return err
	}
	// S4: writers still write the legacy form here; the writer switch to the Stranger table
	// form is a later card, which reads the per-form counts first.
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, keyStrangers, string(b))
}

// noteStranger records a machine that beats and is no member of the sprint,
// once.
func (st *Store) noteStranger(ctx context.Context, member string) error {
	pinned, err := st.pin(ctx)
	if err != nil {
		return err
	}
	shapes, err := pinned.B.Shapes(ctx, []string{pinned.Names.Table(sprint.Fleet)})
	if refusalCode(err) == "NOTABLE" {
		return nil // no sprint yet: nothing to be a stranger to
	}
	if err != nil || len(shapes) == 0 {
		return err
	}
	for _, r := range shapes[0].Rows {
		if r.Key == member {
			return nil
		}
	}
	s, err := st.strangers(ctx)
	if err != nil {
		return err
	}
	if _, known := s[member]; known {
		return nil
	}
	s[member] = stranger{Seen: st.now()}
	return st.putStrangers(ctx, s)
}

// tellStrangers writes one happened notification for each unknown machine
// that beats and was not told of yet: add it with fleet up.
func (st *Store) tellStrangers(ctx context.Context) (Result, error) {
	var res Result
	s, err := st.strangers(ctx)
	if err != nil || len(s) == 0 {
		return res, err
	}
	var names []string
	for m, x := range s {
		if !x.Told {
			names = append(names, m)
		}
	}
	if len(names) == 0 {
		return res, nil
	}
	sort.Strings(names)
	res, err = st.Run(ctx, Step{Verb: "tick strangers", Actor: sprint.MachineActor, Load: tables(sprint.Fleet), Plan: func(snap *sprint.Snapshot) sprint.Plan {
		return sprint.StrangerNotes(snap, names)
	}})
	if err != nil {
		return res, err
	}
	for _, m := range names {
		x := s[m]
		x.Told = true
		s[m] = x
	}
	return res, st.putStrangers(ctx, s)
}

// beatKey is a member's beat record.
func beatKey(member string) string { return "beat:" + member }

// KeysGetter is a store that reads many records in one exchange.
type KeysGetter interface {
	GetKeys(ctx context.Context, names []string) ([]string, []bool, error)
}

// rootKV is the machine records' store: the backend before pinning, as the
// records are the deployment's and not an epoch's.
func (st *Store) rootKV() (KV, error) {
	b := st.root
	if b == nil {
		b = st.B
	}
	kv, ok := b.(KV)
	if !ok {
		return nil, fmt.Errorf("this store keeps no beats")
	}
	return kv, nil
}

// Beat writes one beat of member at the store's clock: the load given, or
// else measured from src; the record's measuring state carries from beat to
// beat. It works while the machine is RUNNING or STOPPED and touches no table.
func (st *Store) Beat(ctx context.Context, member string, given *float64, src hostload.Source) (sprint.Beat, error) {
	return st.BeatOwing(ctx, member, given, src, nil, "")
}

// BeatOwing is Beat with the stop-returns the member's lanes still owe after the machine's
// stop (fleet beat --stop-returns; section 14): kept on the record, which start reads; and
// the member's word that it starts no card (fleet beat --no-room; sprint.Beat.NoRoom), each
// beat's own: a beat without it clears it.
func (st *Store) BeatOwing(ctx context.Context, member string, given *float64, src hostload.Source, stopReturns *int, noRoom string) (sprint.Beat, error) {
	return st.BeatLive(ctx, member, given, src, stopReturns, noRoom, nil)
}

// BeatLive is BeatOwing with the member's live set (fleet beat --live; sprint.ParseLive): nil
// when the beat carries none, which leaves its row unreconciled (sprint.LiveReturns); its
// entries' sightings carried from the record before (sprint.NextLive).
func (st *Store) BeatLive(ctx context.Context, member string, given *float64, src hostload.Source, stopReturns *int, noRoom string, live []string) (sprint.Beat, error) {
	return st.BeatDraining(ctx, member, given, src, stopReturns, noRoom, live, false)
}

// BeatDraining is BeatLive with the member's word that it drains (fleet beat --draining: its
// binary was replaced; sprint.Beat.Draining), stamped from its first such beat
// (sprint.DrainingFrom): a beat without it clears it.
func (st *Store) BeatDraining(ctx context.Context, member string, given *float64, src hostload.Source, stopReturns *int, noRoom string, live []string, draining bool) (sprint.Beat, error) {
	return st.BeatFacts(ctx, member, given, src, stopReturns, noRoom, live, draining, nil)
}

// BeatFacts is BeatDraining with the member's gate facts (fleet beat --facts; sprint.Beat.Facts),
// measured by the member on its own machine: nil keeps the record's facts as they were, as a
// machine's facts do not go away between two beats. The tick writes them onto the member's
// control card (sprint factsPlan).
func (st *Store) BeatFacts(ctx context.Context, member string, given *float64, src hostload.Source, stopReturns *int, noRoom string, live []string, draining bool, facts []string) (sprint.Beat, error) {
	kv, err := st.rootKV()
	if err != nil {
		return sprint.Beat{}, err
	}
	if err := st.beatable(ctx, member); err != nil {
		return sprint.Beat{}, err
	}
	var prev sprint.Beat
	raw, ok, err := kv.GetKey(ctx, beatKey(member))
	if err != nil {
		return prev, err
	}
	if ok {
		// An unreadable record is a first beat on purpose: the next write replaces it. The
		// refusal is named and counted under BEAT (S2, S4).
		if b, err := presenceRow(st.presenceRows().beat, []byte(raw)); err == nil {
			prev = b
		}
	}
	now := st.now()
	pct, how, meter := 0.0, sprint.HowGiven, prev.Meter
	if given != nil {
		if *given < 0 || *given > hostload.MaxPercent {
			return prev, fmt.Errorf("a load is a percent from 0 to %v, found %v", hostload.MaxPercent, *given)
		}
		pct = *given
	} else {
		var measured bool
		pct, how, meter, measured = hostload.Measure(src, prev.Meter, now)
		if !measured {
			return prev, fmt.Errorf("this machine's load cannot be measured here: give it with --load <percent>")
		}
	}
	b := sprint.NextBeat(prev, now, pct, how, meter)
	// the machine's logical cores: the source's when it names them (a meter's,
	// or fleet beat --cores), else this process's machine
	if b.Cores = src.NCPU; b.Cores <= 0 {
		b.Cores = runtime.NumCPU()
	}
	if stopReturns != nil {
		b.StopReturns = *stopReturns
	} else {
		b.StopReturns = prev.StopReturns
	}
	b.NoRoom = noRoom
	b.Live, b.LiveKnown, b.LiveSince, b.LiveSeen = sprint.NextLive(prev, now, live)
	b.Draining, b.DrainingSince = sprint.DrainingFrom(prev, draining, now)
	if b.Facts = prev.Facts; facts != nil {
		b.Facts = facts
	}
	out, err := json.Marshal(b)
	if err != nil {
		return b, err
	}
	if err := kv.SetKey(ctx, beatKey(member), string(out)); err != nil {
		return b, err
	}
	if sprint.IsFriendRow(member) {
		return b, nil // a one-shot friend's row is the roster's (beatable), never a stranger
	}
	return b, st.noteStranger(ctx, member)
}

// beatable says whether member may beat: a machine's name, or a one-shot friend's row
// (friend.<name>, nova-config friend --session one-shot), whose member lane beats as any
// member does and whose status is that beat (friendRows). A live friend's row is not a
// member and never beats here: a friend lane, not a member, serves it.
func (st *Store) beatable(ctx context.Context, member string) error {
	friend, isFriend := sprint.FriendOfRow(member)
	if !isFriend {
		if !sprint.ValidID(member) {
			return fmt.Errorf("a member name wants letters, digits, _ and -: %s", member)
		}
		return nil
	}
	r, _, err := st.roster(ctx)
	if err != nil {
		return err
	}
	e, ok := r[friend]
	switch {
	case !ok || !sprint.ValidID(friend):
		return fmt.Errorf("no friend %s on the roster: a friend row's member beats only for a friend nova-config names (nova-sprint friend sync)", friend)
	case e.Session != sprint.FriendOneShot:
		return fmt.Errorf("friend %s is live: a live friend's row is served by a friend lane, never a member; a one-shot friend's member beats here (nova-config friend set %s --session one-shot)", friend, friend)
	}
	return nil
}

// TouchBeat is the beat that releasing a hold gives a member that has beaten
// (docs/SPEC-SPRINT.md section 5; the coordinator's fleet up counts as a
// beat): its last beat becomes the store's clock now, its load and samples as
// they were, so the tick after the release, within a beat window, finds it up
// and takes none of its cards back. A member that never beat has no record to
// touch: it stays down until it beats. It says whether it wrote.
func (st *Store) TouchBeat(ctx context.Context, member string) (bool, error) {
	kv, err := st.rootKV()
	if err != nil {
		return false, err
	}
	raw, ok, err := kv.GetKey(ctx, beatKey(member))
	if err != nil || !ok {
		return false, err
	}
	// An unreadable record is no beat on purpose: the next beat replaces it. The refusal is
	// named and counted under BEAT (S2, S4).
	b, err := presenceRow(st.presenceRows().beat, []byte(raw))
	if err != nil || !b.Beaten() {
		return false, nil
	}
	b.At = st.now().UTC().Truncate(time.Second)
	out, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	return true, kv.SetKey(ctx, beatKey(member), string(out))
}

// Beats is the beat records of the members, in one exchange where the store
// can; a member that has never beaten has none.
func (st *Store) Beats(ctx context.Context, members []string) (map[string]sprint.Beat, error) {
	out := map[string]sprint.Beat{}
	if len(members) == 0 {
		return out, nil
	}
	kv, err := st.rootKV()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = beatKey(m)
	}
	vals, oks, err := getKeys(ctx, kv, names)
	if err != nil {
		return nil, err
	}
	rows := st.presenceRows().beat
	for i, m := range members {
		if i >= len(oks) || !oks[i] {
			continue
		}
		// An unreadable record is no beat on purpose, as for a member that never beat: the
		// member reads down until its next beat replaces it. The refusal is named and counted
		// under BEAT (S2, S4).
		if b, err := presenceRow(rows, []byte(vals[i])); err == nil {
			out[m] = b
		}
	}
	return out, nil
}

// The friend roster and presence rows' readers (the reader-first half of the schema-table
// adoption of these rows: S4, readers first, then writers). Each record is read through the
// store row reader (rowcodec.go) from the generated variable tables of friendrowswire
// (schemas/friendrows/friendrows.schema): the table wire (form byte 1; a unit that holds a
// variable table emits no fixed form, friendrows.schema), the padded standard base64 of that
// wire and the schema JSON text, and today's legacy JSON (S2). A damaged value is refused by
// name, `<RECORD> REFUSED: <reason>; run: nova-sprint check`, and every read is counted under
// its record and its form (S4). Writers still write the legacy form: the writer switch is a
// later card, which reads these counts first.

// presenceRowSets is each store's friend roster and presence readers, keyed by the store's
// done reader: every pinned copy of a store shares that one reader (Store.clone), so a store
// and its copies count into one set (S4).
var presenceRowSets sync.Map

// presenceRowCodecs is one store's readers of the friend roster and presence rows.
type presenceRowCodecs struct {
	entry    *RowCodec[friendEntry]
	status   *RowCodec[friendStatus]
	beat     *RowCodec[sprint.Beat]
	stranger *RowCodec[stranger]
}

// presenceRows is this store's readers of the friend roster and presence rows, registered on
// it so its Check reports their per-form counts (S4).
func (st *Store) presenceRows() *presenceRowCodecs {
	key := st.doneRows()
	v, ok := presenceRowSets.Load(key)
	if !ok {
		v, _ = presenceRowSets.LoadOrStore(key, &presenceRowCodecs{
			entry:    NewRowCodec(friendEntryRowFns()),
			status:   NewRowCodec(friendStatusRowFns()),
			beat:     NewRowCodec(beatRowFns()),
			stranger: NewRowCodec(strangerRowFns()),
		})
	}
	c := v.(*presenceRowCodecs)
	c.entry.Register(st)
	c.status.Register(st)
	c.beat.Register(st)
	c.stranger.Register(st)
	return c
}

// friendTable is one generated variable table of friendrowswire as the row reader uses it, W
// its generated record: the wire's measure and load, the schema JSON text's load, and the
// table's JSON text and wire of a loaded record (S1, S2).
type friendTable[W any] struct {
	measure  func(wire []byte) (int64, error)
	load     func(region, wire []byte, report *friendrowswire.TableReport) *W
	fromJSON func(text []byte, report *friendrowswire.TableReport) (*W, bool)
	toJSON   func(*W) []byte
	save     func(*W) []byte
}

// presenceRegion is an aligned buffer of n bytes for a region load.
func presenceRegion(n int64) []byte {
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

// presenceReport is the generated report as the strict read decides on it. S2: a key given
// twice is malformed, never last-wins.
func presenceReport(r *friendrowswire.TableReport) RowReport {
	return RowReport{
		Unknown: int(r.Unknown), KindMismatch: int(r.KindMismatch), Clamped: int(r.Clamped),
		Malformed: r.Malformed, Duplicate: int(r.Duplicate), Reason: r.Reason,
	}
}

// refuses says the strict read refuses a load with this report (schemaread.Reader.Read).
func (r RowReport) refuses() bool {
	return r.Reason != "" || r.Malformed || r.Duplicate > 0 || r.KindMismatch > 0 || r.Clamped > 0
}

// presenceRowFns adapts one record to the row reader (S1, S2): T is the store's Go record, M
// its schema JSON shape (the generated text's keys), W the generated record. A loaded table is
// read into T through its schema JSON text, so the wire, its base64 and the JSON text decode
// to one value; legacy is today's encoding/json text of T.
func presenceRowFns[T, M, W any](name string, tab friendTable[W], to func(*T) M, from func(*M) (T, bool)) RowFns[T] {
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
		text, err := json.Marshal(to(v))
		if err != nil {
			return nil
		}
		var rep friendrowswire.TableReport
		w, ok := tab.fromJSON(text, &rep)
		if !ok || presenceReport(&rep).refuses() {
			return nil
		}
		return w
	}
	return RowFns[T]{
		Name:  name,
		Forms: []byte{1}, // the table wire: friendrows.schema emits no fixed form (S1)
		Reset: func(v *T) { var zero T; *v = zero },
		Measure: func(wire []byte) error {
			_, err := tab.measure(wire)
			return err
		},
		LoadWire: func(v *T, wire []byte) RowReport {
			n, err := tab.measure(wire)
			if err != nil || n < 0 {
				return RowReport{Malformed: true}
			}
			var rep friendrowswire.TableReport
			w := tab.load(presenceRegion(n), wire, &rep)
			if w == nil {
				return RowReport{Malformed: true, Reason: rep.Reason}
			}
			if rr := presenceReport(&rep); rr.refuses() {
				return rr
			}
			rr := value(v, w)
			rr.Unknown = int(rep.Unknown)
			return rr
		},
		LoadJSON: func(v *T, text []byte) RowReport {
			var rep friendrowswire.TableReport
			w, ok := tab.fromJSON(text, &rep)
			rr := presenceReport(&rep)
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
		},
		Legacy:     LegacyObject,
		LoadLegacy: DecodeLegacy[T],
		SaveWire: func(v *T) []byte {
			if w := table(v); w != nil {
				return tab.save(w)
			}
			return nil
		},
		SaveJSON: func(v *T) []byte {
			if w := table(v); w != nil {
				return tab.toJSON(w)
			}
			return nil
		},
	}
}

// presenceRow reads one stored value of a record. A value a JSON object holds as a JSON
// string (the base64 of a table's wire, inside a roster object) is read as that string's
// text. S2.
func presenceRow[T any](c *RowCodec[T], raw []byte) (T, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			raw = []byte(s)
		}
	}
	v, _, err := c.Read(raw)
	return v, err
}

// rowTime is a time.Time as the schema's open text: RFC3339Nano, as encoding/json writes it,
// and null for the zero time (friendrows.schema, "Source time.Time JSON").
func rowTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(time.RFC3339Nano)
	return &s
}

// rowTimeOf is rowTime read back, as encoding/json reads a time.Time; ok is false for a text
// that is not one.
func rowTimeOf(s *string) (time.Time, bool) {
	var t time.Time
	if s == nil || *s == "" {
		return t, true
	}
	if err := t.UnmarshalText([]byte(*s)); err != nil {
		return time.Time{}, false
	}
	return t, true
}

// rowText is a string as the schema's open text: null when empty (friendrows.schema).
func rowText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// rowTextOf is rowText read back.
func rowTextOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// rowInt is an optional integer as the schema's ?int64.
func rowInt(p *int) *int64 {
	if p == nil {
		return nil
	}
	v := int64(*p)
	return &v
}

// rowIntOf is rowInt read back.
func rowIntOf(p *int64) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

// rowWord is one word of an open word list (RunWord, LiveWord, FactWord).
type rowWord struct {
	Text *string `json:"text"`
}

func rowWords(ws []string) []rowWord {
	var out []rowWord
	for _, w := range ws {
		out = append(out, rowWord{Text: rowText(w)})
	}
	return out
}

func rowWordsOf(ws []rowWord) []string {
	var out []string
	for _, w := range ws {
		out = append(out, rowTextOf(w.Text))
	}
	return out
}

// beatSchema is the Beat table's schema JSON text (friendrows.schema Beat). An empty list is
// left out: the table reader reads a null list as a kind mismatch.
type beatSchema struct {
	At            *string             `json:"at"`
	Load          float64             `json:"load"`
	Cores         int64               `json:"cores"`
	How           *string             `json:"how"`
	Samples       []loadSampleSchema  `json:"samples,omitempty"`
	Meter         meterSchema         `json:"meter"`
	Friend        *friendReportSchema `json:"friend"`
	StopReturns   int64               `json:"stop_returns"`
	NoRoom        *string             `json:"no_room"`
	Live          []rowWord           `json:"live,omitempty"`
	LiveKnown     bool                `json:"live_known"`
	LiveSince     *string             `json:"live_since"`
	LiveSeen      []liveSightSchema   `json:"live_seen,omitempty"`
	Draining      bool                `json:"draining"`
	DrainingSince *string             `json:"draining_since"`
	Facts         []rowWord           `json:"facts,omitempty"`
}

type loadSampleSchema struct {
	Pct float64 `json:"pct"`
	At  *string `json:"at"`
}

type meterSchema struct {
	Ticks  *ticksSchema `json:"ticks"`
	TopAt  *string      `json:"top_at"`
	TopPct float64      `json:"top_pct"`
	Files  *filesSchema `json:"files"`
}

type ticksSchema struct {
	Busy  uint64 `json:"busy"`
	Total uint64 `json:"total"`
}

type filesSchema struct {
	At     *string        `json:"at"`
	Open   int64          `json:"open"`
	Max    int64          `json:"max"`
	Warn   int64          `json:"warn"`
	Alarm  int64          `json:"alarm"`
	Top    []holderSchema `json:"top,omitempty"`
	TopAt  *string        `json:"top_at"`
	TopErr *string        `json:"top_err"`
}

type holderSchema struct {
	PID     int64   `json:"pid"`
	Command *string `json:"cmd"`
	User    *string `json:"user"`
	Open    int64   `json:"open"`
}

type friendReportSchema struct {
	Running     []rowWord `json:"running,omitempty"`
	Working     *int64    `json:"working"`
	Queue       *int64    `json:"queue"`
	Width       *int64    `json:"width"`
	StopReturns *int64    `json:"stop_returns"`
	Paced       *int64    `json:"paced"`
	Active      *string   `json:"active"`
	Window      *string   `json:"window"`
	Until       *string   `json:"until"`
	Reason      *string   `json:"reason"`
	Build       *string   `json:"build"`
	Started     *string   `json:"started"`
	Present     *string   `json:"present"`
}

type liveSightSchema struct {
	Entry *string `json:"entry"`
	At    *string `json:"at"`
}

// beatTable is the generated Beat table (S1, S2).
func beatTable() friendTable[friendrowswire.Beat] {
	return friendTable[friendrowswire.Beat]{
		measure: friendrowswire.BeatLoadMeasureReason,
		load:    friendrowswire.BeatLoad,
		fromJSON: func(text []byte, rep *friendrowswire.TableReport) (*friendrowswire.Beat, bool) {
			var b friendrowswire.BeatBuilder
			if !b.Init() {
				return nil, false
			}
			if !friendrowswire.BeatFromJson(&b, text, rep) || !b.Lock() {
				return nil, false
			}
			return b.AsConst(), true
		},
		toJSON: func(w *friendrowswire.Beat) []byte {
			buf := make([]byte, max(friendrowswire.BeatToJsonMeasure(w), 0))
			return buf[:max(friendrowswire.BeatToJson(w, buf), 0)]
		},
		save: func(w *friendrowswire.Beat) []byte {
			buf := make([]byte, max(friendrowswire.BeatMeasure(w), 0))
			return buf[:max(friendrowswire.BeatSave(w, buf), 0)]
		},
	}
}

// beatRowFns is the member, friend and reader beat (sprint.Beat) as the row reader reads it:
// BEAT (S1, S2, S4).
func beatRowFns() RowFns[sprint.Beat] {
	return presenceRowFns("BEAT", beatTable(), beatToSchema, beatOfSchema)
}

func beatToSchema(b *sprint.Beat) beatSchema {
	m := beatSchema{
		At: rowTime(b.At), Load: b.Load, Cores: int64(b.Cores), How: rowText(b.How),
		StopReturns: int64(b.StopReturns), NoRoom: rowText(b.NoRoom), Live: rowWords(b.Live),
		LiveKnown: b.LiveKnown, LiveSince: rowTime(b.LiveSince), Draining: b.Draining,
		DrainingSince: rowTime(b.DrainingSince), Facts: rowWords(b.Facts),
		Meter: meterSchema{TopAt: rowTime(b.Meter.TopAt), TopPct: b.Meter.TopPct},
	}
	for _, s := range b.Samples {
		m.Samples = append(m.Samples, loadSampleSchema{Pct: s.Pct, At: rowTime(s.At)})
	}
	if t := b.Meter.Ticks; t != nil {
		m.Meter.Ticks = &ticksSchema{Busy: t.Busy, Total: t.Total}
	}
	if f := b.Meter.Files; f != nil {
		fs := &filesSchema{At: rowTime(f.At), Open: int64(f.Open), Max: int64(f.Max), Warn: int64(f.Warn), Alarm: int64(f.Alarm), TopAt: rowTime(f.TopAt), TopErr: rowText(f.TopErr)}
		for _, h := range f.Top {
			fs.Top = append(fs.Top, holderSchema{PID: int64(h.PID), Command: rowText(h.Command), User: rowText(h.User), Open: int64(h.Open)})
		}
		m.Meter.Files = fs
	}
	if r := b.Friend; r != nil {
		m.Friend = &friendReportSchema{
			Running: rowWords(r.Running), Working: rowInt(r.Working), Queue: rowInt(r.Queue), Width: rowInt(r.Width),
			StopReturns: rowInt(r.StopReturns), Paced: rowInt(r.Paced), Active: rowTime(r.Active), Window: rowText(r.Window),
			Until: rowTime(r.Until), Reason: rowText(r.Reason), Build: rowText(r.Build), Started: rowTime(r.Started), Present: rowTime(r.Present),
		}
	}
	for _, e := range slices.Sorted(maps.Keys(b.LiveSeen)) {
		m.LiveSeen = append(m.LiveSeen, liveSightSchema{Entry: rowText(e), At: rowTime(b.LiveSeen[e])})
	}
	return m
}

func beatOfSchema(m *beatSchema) (sprint.Beat, bool) {
	ok := true
	at := func(s *string) time.Time {
		t, good := rowTimeOf(s)
		ok = ok && good
		return t
	}
	b := sprint.Beat{
		At: at(m.At), Load: m.Load, Cores: int(m.Cores), How: rowTextOf(m.How),
		StopReturns: int(m.StopReturns), NoRoom: rowTextOf(m.NoRoom), Live: rowWordsOf(m.Live),
		LiveKnown: m.LiveKnown, LiveSince: at(m.LiveSince), Draining: m.Draining,
		DrainingSince: at(m.DrainingSince), Facts: rowWordsOf(m.Facts),
		Meter: hostload.State{TopAt: at(m.Meter.TopAt), TopPct: m.Meter.TopPct},
	}
	for _, s := range m.Samples {
		b.Samples = append(b.Samples, sprint.LoadSample{Pct: s.Pct, At: at(s.At)})
	}
	if t := m.Meter.Ticks; t != nil {
		b.Meter.Ticks = &hostload.Ticks{Busy: t.Busy, Total: t.Total}
	}
	if f := m.Meter.Files; f != nil {
		fs := &hostload.Files{At: at(f.At), Open: int(f.Open), Max: int(f.Max), Warn: int(f.Warn), Alarm: int(f.Alarm), TopAt: at(f.TopAt), TopErr: rowTextOf(f.TopErr)}
		for _, h := range f.Top {
			fs.Top = append(fs.Top, hostload.Holder{PID: int(h.PID), Command: rowTextOf(h.Command), User: rowTextOf(h.User), Open: int(h.Open)})
		}
		b.Meter.Files = fs
	}
	if r := m.Friend; r != nil {
		b.Friend = &sprint.FriendReport{
			Running: rowWordsOf(r.Running), Working: rowIntOf(r.Working), Queue: rowIntOf(r.Queue), Width: rowIntOf(r.Width),
			StopReturns: rowIntOf(r.StopReturns), Paced: rowIntOf(r.Paced), Active: at(r.Active), Window: rowTextOf(r.Window),
			Until: at(r.Until), Reason: rowTextOf(r.Reason), Build: rowTextOf(r.Build), Started: at(r.Started), Present: at(r.Present),
		}
	}
	for _, s := range m.LiveSeen {
		if b.LiveSeen == nil {
			b.LiveSeen = map[string]time.Time{}
		}
		b.LiveSeen[rowTextOf(s.Entry)] = at(s.At)
	}
	return b, ok
}

// strangerRowFns is one stranger entry as the row reader reads it: STRANGERS (S1, S2,
// S4). The strangers record is a JSON object keyed by the machine's name under keyStrangers,
// and each entry is read through this codec, as the roster reads each entry (presenceRowMap).
// Stranger is a fixed table (friendrows.schema): the sprint server owns the record and is
// its only writer and reader, so by S1 it is fixed, not variable. Its wire rides the table
// wire (form byte 1), as every table of this unit does; the schema JSON text holds seen as
// Unix nanoseconds UTC, 0 for the zero time.
func strangerRowFns() RowFns[stranger] {
	load := func(v *stranger, w *friendrowswire.Stranger) {
		v.Seen = doneTimeOf(w.Seen)
		v.Told = w.Told
	}
	save := func(v *stranger) *friendrowswire.Stranger {
		var w friendrowswire.Stranger
		if !v.Seen.IsZero() {
			w.Seen = v.Seen.UnixNano()
		}
		w.Told = v.Told
		return &w
	}
	return RowFns[stranger]{
		Name:  "STRANGERS",
		Forms: []byte{1},
		Reset: func(v *stranger) { *v = stranger{} },
		LoadWire: func(v *stranger, wire []byte) RowReport {
			var w friendrowswire.Stranger
			var rep friendrowswire.TableReport
			if !friendrowswire.StrangerLoad(&w, wire, &rep) || presenceReport(&rep).refuses() {
				rr := presenceReport(&rep)
				if rr.Reason == "" && !rr.Malformed && rr.Duplicate == 0 && rr.Clamped == 0 &&
					rr.Unknown == 0 && rr.KindMismatch == 0 {
					rr.Malformed = true
				}
				return rr
			}
			load(v, &w)
			return RowReport{Unknown: int(rep.Unknown)}
		},
		LoadJSON: func(v *stranger, text []byte) RowReport {
			var w friendrowswire.Stranger
			var rep friendrowswire.TableReport
			if !friendrowswire.StrangerFromJson(&w, text, &rep) || presenceReport(&rep).refuses() {
				rr := presenceReport(&rep)
				if rr.Reason == "" && !rr.Malformed && rr.Duplicate == 0 && rr.Clamped == 0 &&
					rr.Unknown == 0 && rr.KindMismatch == 0 {
					rr.Malformed = true
				}
				return rr
			}
			load(v, &w)
			return RowReport{Unknown: int(rep.Unknown)}
		},
		Legacy:     LegacyObject,
		LoadLegacy: DecodeLegacy[stranger],
		SaveWire: func(v *stranger) []byte {
			w := save(v)
			buf := make([]byte, max(friendrowswire.StrangerMeasure(w), 0))
			return buf[:max(friendrowswire.StrangerSave(w, buf), 0)]
		},
		SaveJSON: func(v *stranger) []byte {
			w := save(v)
			buf := make([]byte, max(friendrowswire.StrangerToJsonMeasure(w), 0))
			return buf[:max(friendrowswire.StrangerToJson(w, buf), 0)]
		},
	}
}

// getKeys reads machine records, in one exchange where the store can.
func getKeys(ctx context.Context, kv KV, names []string) ([]string, []bool, error) {
	if kg, ok := kv.(KeysGetter); ok {
		return kg.GetKeys(ctx, names)
	}
	var vals []string
	var oks []bool
	for _, n := range names {
		v, ok, err := kv.GetKey(ctx, n)
		if err != nil {
			return nil, nil, err
		}
		vals, oks = append(vals, v), append(oks, ok)
	}
	return vals, oks, nil
}

// SyncFleet brings every display cell of the fleet up to date, reading the
// control cards: each member's status (adopting while the tick holds it to adopt, else held, up or down; sprint.FleetRowStatus), width, load (the
// highest measured load of the last LoadWindow while its beat is fresh, with its open
// file descriptors beside it over the warn bound, sprint.LoadText); done
// and ok% are the table's own formulas. A store that keeps no beats shows the
// control card's status and no load. It is what a step's mirrors run. It says whether it wrote.
func (st *Store) SyncFleet(ctx context.Context) (bool, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return false, err
	}
	shapes, err := pinned.B.Shapes(ctx, []string{pinned.Names.Table(sprint.Fleet)})
	if err != nil || len(shapes) == 0 {
		return false, err
	}
	shape := shapes[0]
	var ids, members []string
	for _, r := range shape.Rows {
		ids = append(ids, pinned.sid(sprint.CtlID(r.Key)))
		members = append(members, r.Key)
	}
	if len(ids) == 0 {
		return false, nil
	}
	rs, err := pinned.readSet(ctx, shape.Name, ids)
	if err != nil {
		return false, err
	}
	var beats map[string]sprint.Beat
	if _, kerr := st.rootKV(); kerr == nil {
		if beats, err = st.Beats(ctx, members); err != nil {
			return false, err
		}
	}
	now := st.now()
	status := map[string]string{}
	diffs := map[string]map[string]string{}
	for _, row := range shape.Rows {
		m, _ := rs.Member(pinned.sid(sprint.CtlID(row.Key)))
		ctl := &sprint.Card{Fields: m.Fields}
		want := map[string]string{sprint.Status: dash(ctl.F("status")), sprint.Load: "", sprint.FieldWidth: sprint.WidthText(ctl)}
		if beats != nil {
			b := beats[row.Key]
			want[sprint.Status], want[sprint.Load] = sprint.FleetRowStatus(ctl, b, now), sprint.LoadText(b, now)
		}
		status[row.Key] = want[sprint.Status]
		if d := rowDiff(row, want); len(d) > 0 {
			diffs[row.Key] = d
		}
	}
	if err := pinned.setRows(ctx, shape.Name, diffs); err != nil {
		return false, err
	}
	ordered, err := st.orderFleet(ctx, shape, status)
	return len(diffs) > 0 || ordered, err
}

// fleetBeats is the fleet's shape, from the shapes a tick read or else read
// now, and the beats of its members.
func (st *Store) fleetBeats(ctx context.Context, shapes []ntable.Table) (ntable.Table, map[string]sprint.Beat, error) {
	name := st.Names.Table(sprint.Fleet)
	var shape ntable.Table
	found := false
	for _, sh := range shapes {
		if sh.Name == name {
			shape, found = sh, true
		}
	}
	if !found {
		read, err := st.B.Shapes(ctx, []string{name})
		if err != nil || len(read) == 0 {
			return shape, nil, err
		}
		shape = read[0]
	}
	var members []string
	for _, r := range shape.Rows {
		members = append(members, r.Key)
	}
	beats, err := st.Beats(ctx, members)
	if err != nil {
		return shape, beats, err
	}
	// each friend's own beat under her name, beside the machines' (her live set and work
	// report, read as facts of her cards, never her state): a friend's beat is kept by her
	// name, not her row
	for _, row := range members {
		f, ok := sprint.FriendOfRow(row)
		if !ok {
			continue
		}
		b, err := st.FriendBeatOf(ctx, f)
		if err != nil {
			return shape, beats, err
		}
		if b.Friend != nil || b.LiveKnown {
			// her live set is her row's evidence for working (sprint.LiveReturns), with or
			// without a report beside it
			if beats == nil {
				beats = map[string]sprint.Beat{}
			}
			beats[f] = b
		}
	}
	return shape, beats, nil
}

// freshOf is the members that are alive at now (fewer than MissedBeatsDown
// beat windows missed), in row order: the set whose change is a member
// coming or going.
func freshOf(shape ntable.Table, beats map[string]sprint.Beat, now time.Time) []string {
	var out []string
	for _, r := range shape.Rows {
		if beats[r.Key].Alive(now) {
			out = append(out, r.Key)
		}
	}
	return out
}

// showFleet is the tick's own pass over the fleet's display cells, from the
// row texts and the beats alone, reading no control card: the load of each
// member (with its open files over the warn bound, sprint.LoadText), and its status up or down by its beat. A member shown held stays
// held: a hold is set and released only by a verb, whose step brings every
// cell up to date (SyncFleet). It says whether it wrote.
func (st *Store) showFleet(ctx context.Context, shape ntable.Table, beats map[string]sprint.Beat, now time.Time) (bool, error) {
	diffs, status := map[string]map[string]string{}, map[string]string{}
	for _, row := range shape.Rows {
		b := beats[row.Key]
		want := map[string]string{sprint.Load: sprint.LoadText(b, now)}
		if row.Texts[sprint.Status] != sprint.Held {
			want[sprint.Status] = sprint.Down
			if b.Alive(now) {
				want[sprint.Status] = sprint.Up
			}
		}
		status[row.Key] = row.Texts[sprint.Status]
		if s, ok := want[sprint.Status]; ok {
			status[row.Key] = s
		}
		if d := rowDiff(row, want); len(d) > 0 {
			diffs[row.Key] = d
		}
	}
	if len(diffs) > 0 {
		pinned, err := st.pin(ctx)
		if err != nil {
			return false, err
		}
		if err := pinned.setRows(ctx, shape.Name, diffs); err != nil {
			return false, err
		}
	}
	ordered, err := st.orderFleet(ctx, shape, status)
	return len(diffs) > 0 || ordered, err
}

// rowDiff is the display cells of a row that differ from want.
func rowDiff(row ntable.Row, want map[string]string) map[string]string {
	diff := map[string]string{}
	for k, v := range want {
		if row.Texts[k] != v {
			diff[k] = v
		}
	}
	return diff
}

// RowsSetter is a store that writes the display cells of many rows of a
// table in one exchange.
type RowsSetter interface {
	RowsSet(ctx context.Context, table string, rows map[string]map[string]string) error
}

// setRows writes the display cells of every row that differs, in one exchange
// where the store can (the owner's rule: never a row at a time).
func (st *Store) setRows(ctx context.Context, table string, rows map[string]map[string]string) error {
	if len(rows) == 0 {
		return nil
	}
	if rs, ok := st.B.(RowsSetter); ok {
		return rs.RowsSet(ctx, table, rows)
	}
	for _, k := range slices.Sorted(maps.Keys(rows)) {
		if err := st.B.RowSet(ctx, table, k, rows[k]); err != nil {
			return err
		}
	}
	return nil
}

// RowsOrderer is a store that puts a table's rows in an order (the table
// layer's row order: the named rows first, in that order).
type RowsOrderer interface {
	RowsOrder(ctx context.Context, table string, rows []string) error
}

// statusRank is a fleet row's place by its status cell: up first, then held,
// then down (the owner, 2026-10-01: "Please sort the fleet table such that we
// sort first alphabetically by machine name (as is current), then stable sort
// by status, such that "up" is first, then "held" then "down"").
func statusRank(status string) int {
	switch status {
	case sprint.Up:
		return 0
	case sprint.Held, sprint.Adopting:
		return 1
	case sprint.Down:
		return 2
	}
	return 3
}

// FleetOrder is the fleet's rows by name, then stably by status: up, held
// (adopting with it), down, anything else last. status is each row's status
// cell. The friends table is ordered by it too (FriendRows): one order for both.
func FleetOrder(rows []string, status map[string]string) []string {
	out := slices.Clone(rows)
	slices.Sort(out)
	slices.SortStableFunc(out, func(a, b string) int { return statusRank(status[a]) - statusRank(status[b]) })
	return out
}

// orderFleet puts the fleet's rows in FleetOrder when they are not, where the
// store can order rows; status is each row's status cell as it now stands. It
// says whether it wrote.
func (st *Store) orderFleet(ctx context.Context, shape ntable.Table, status map[string]string) (bool, error) {
	if _, ok := st.B.(RowsOrderer); !ok {
		return false, nil
	}
	var rows []string
	for _, r := range shape.Rows {
		rows = append(rows, r.Key)
	}
	want := FleetOrder(rows, status)
	if slices.Equal(rows, want) {
		return false, nil
	}
	pinned, err := st.pin(ctx)
	if err != nil {
		return false, err
	}
	ro, ok := pinned.B.(RowsOrderer)
	if !ok {
		return false, nil
	}
	return true, ro.RowsOrder(ctx, shape.Name, want)
}

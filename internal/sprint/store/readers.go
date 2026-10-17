package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// A reader's presence (sprint/readers.go; docs/SPEC-SPRINT.md section 6): two
// records per reader under the deployment's prefix, outside the tables and the
// fence as a member's beat is: reader-beat:<reader>, written by the reader's
// own queue, and reader-away:<reader>, the coordinator's hold, written by
// reader away and emptied by reader up.

// ReadCardIDs returns both identities (the plain identity and the second identity with generation suffix)
// for a reader at a primary's attempt, so store loads can enumerate both without extra round trips.
func ReadCardIDs(primary string, attempt int, reader string) []string {
	return sprint.ReadCardIDs(primary, attempt, reader)
}

func readerBeatKey(reader string) string { return "reader-beat:" + reader }
func readerAwayKey(reader string) string { return "reader-away:" + reader }

// readerHold is the coordinator's hold of a reader away: empty while released.
// Held marks a hold made by hold <reader> (hold.go), whose state reads held, with
// the coordinator's Reason and whether it took the reads begun back (Return).
type readerHold struct {
	Away    bool      `json:"away,omitempty"`
	Retired bool      `json:"retired,omitempty"` // reader retire: held away for good, the row off the view
	At      time.Time `json:"at,omitempty"`
	By      string    `json:"by,omitempty"`
	Held    bool      `json:"held,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Return  bool      `json:"return,omitempty"`
}

// ReaderRows is the readers table's rows, in table order.
func (st *Store) ReaderRows(ctx context.Context) ([]string, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return nil, err
	}
	shapes, err := pinned.B.Shapes(ctx, []string{pinned.Names.Table(sprint.Readers)})
	if refusalCode(err) == "NOTABLE" {
		return nil, nil // no sprint yet: no reader
	}
	if err != nil || len(shapes) == 0 {
		return nil, err
	}
	var rows []string
	for _, r := range shapes[0].Rows {
		rows = append(rows, r.Key)
	}
	return rows, nil
}

// ReaderBeat writes one beat of reader at the store's clock, to the second,
// when the readers table has its row; it says whether it wrote. It touches no
// table: the reader's own queue is its beat. noRoom is the reader's word that it
// starts no read (queue --no-room; sprint.Beat.NoRoom), "" for none.
func (st *Store) ReaderBeat(ctx context.Context, reader, noRoom string) (bool, error) {
	return st.ReaderBeatDraining(ctx, reader, noRoom, false)
}

// ReaderBeatDraining is ReaderBeat with the reader's word that it drains (queue --draining:
// its binary was replaced; sprint.Beat.Draining), stamped from its first such beat
// (sprint.DrainingFrom): a beat without it clears it.
func (st *Store) ReaderBeatDraining(ctx context.Context, reader, noRoom string, draining bool) (bool, error) {
	rows, err := st.ReaderRows(ctx)
	if err != nil || !contains(rows, reader) {
		return false, err
	}
	// a retired reader is no reader: its beat writes none, and its loop is told so
	states, err := st.ReaderStates(ctx, []string{reader}, st.now())
	if err != nil || states[reader] == sprint.ReaderRetired {
		return false, err
	}
	if !draining {
		return true, st.beatReaders(ctx, noRoom, reader)
	}
	kv, err := st.rootKV()
	if err != nil {
		return true, nil // a store that keeps no beats holds every reader up
	}
	var prev sprint.Beat
	raw, ok, err := kv.GetKey(ctx, readerBeatKey(reader))
	if err != nil {
		return true, err
	}
	if ok {
		// ignored: an unreadable record is a first beat, which this write replaces
		_ = json.Unmarshal([]byte(raw), &prev)
	}
	now := st.now()
	b := sprint.Beat{At: now.UTC().Truncate(time.Second), NoRoom: noRoom}
	b.Draining, b.DrainingSince = sprint.DrainingFrom(prev, true, now)
	out, err := json.Marshal(b)
	if err != nil {
		return true, err
	}
	return true, kv.SetKey(ctx, readerBeatKey(reader), string(out))
}

// BeatReaders is a beat of every reader of the readers table: the readers of
// a twin are this one process's.
func (st *Store) BeatReaders(ctx context.Context) error {
	rows, err := st.ReaderRows(ctx)
	if err != nil {
		return err
	}
	return st.beatReaders(ctx, "", rows...)
}

func (st *Store) beatReaders(ctx context.Context, noRoom string, readers ...string) error {
	kv, err := st.rootKV()
	if err != nil {
		return nil // a store that keeps no beats holds every reader up
	}
	out, err := json.Marshal(sprint.Beat{At: st.now().UTC().Truncate(time.Second), NoRoom: noRoom})
	if err != nil {
		return err
	}
	for _, r := range readers {
		if err := kv.SetKey(ctx, readerBeatKey(r), string(out)); err != nil {
			return err
		}
	}
	return nil
}

// SetReaderAway holds reader away (reader away) or releases the hold (reader
// up), by the coordinator who. The reader is a row of the readers table.
func (st *Store) SetReaderAway(ctx context.Context, reader string, away bool, who string) error {
	return st.setReaderHold(ctx, reader, readerHold{Away: away}, who)
}

// SetReaderRetired retires reader (reader retire): held away for good, its beat
// writing none, its row and read cards kept; reader up brings it back. The reader is a row of the readers table.
func (st *Store) SetReaderRetired(ctx context.Context, reader, who string) error {
	return st.setReaderHold(ctx, reader, readerHold{Away: true, Retired: true}, who)
}

// setReaderHold writes the coordinator's hold of a reader, stamped and signed
// when it holds anything, empty when it releases.
func (st *Store) setReaderHold(ctx context.Context, reader string, hold readerHold, who string) error {
	kv, err := st.rootKV()
	if err != nil {
		return err
	}
	rows, err := st.ReaderRows(ctx)
	if err != nil {
		return err
	}
	if !contains(rows, reader) {
		return fmt.Errorf("no reader %s on the readers table; run: nova-sprint reader add %s", reader, reader)
	}
	if hold.Away {
		hold.At, hold.By = st.now().UTC().Truncate(time.Second), who
	}
	out, err := json.Marshal(hold)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, readerAwayKey(reader), string(out))
}

// ForgetReaders deletes the beat and the hold of readers taken off the readers
// table (reader remove): a reader added again under the name starts clean, and
// teardown has no key of a reader it can no longer find.
func (st *Store) ForgetReaders(ctx context.Context, readers []string) error {
	var keys []string
	for _, r := range readers {
		keys = append(keys, st.Names.Key(readerBeatKey(r)), st.Names.Key(readerAwayKey(r)))
	}
	_, err := st.B.DeleteKeys(ctx, keys)
	return err
}

// ReaderStates is each of readers' state at now: held away, up, away or down
// (sprint.ReaderState). A store that keeps no records says none (nil), and
// every reader is held up.
func (st *Store) ReaderStates(ctx context.Context, readers []string, now time.Time) (map[string]string, error) {
	out, _, err := st.readerStates(ctx, readers, now)
	return out, err
}

// readerStates is ReaderStates and, beside it, each reader whose fresh beat says it starts
// no read, with its word (sprint.NoRoomNow).
func (st *Store) readerStates(ctx context.Context, readers []string, now time.Time) (map[string]string, map[string]string, error) {
	kv, err := st.rootKV()
	if err != nil {
		return nil, nil, nil
	}
	out, noRoom := map[string]string{}, map[string]string{}
	if len(readers) == 0 {
		return out, noRoom, nil
	}
	names := make([]string, 0, 2*len(readers))
	for _, r := range readers {
		names = append(names, readerBeatKey(r), readerAwayKey(r))
	}
	vals, oks, err := getKeys(ctx, kv, names)
	if err != nil {
		return nil, nil, err
	}
	for i, r := range readers {
		var b sprint.Beat
		var hold readerHold
		if oks[2*i] {
			// ignored (S4): an unreadable record is no beat, which the next beat replaces;
			// the read is counted under BEAT and the refusal is counted, by name (S2)
			var err error
			b, _, err = st.presenceRows().beat.Read([]byte(vals[2*i]))
			if err != nil {
				b = sprint.Beat{}
			}
		}
		if oks[2*i+1] {
			// ignored: an unreadable record is no hold, which the next reader away or reader up replaces
			_ = json.Unmarshal([]byte(vals[2*i+1]), &hold)
		}
		out[r] = sprint.ReaderState(hold.Away, b, now)
		if why := sprint.NoRoomNow(b, now); why != "" {
			noRoom[r] = why
		}
		if hold.Retired {
			out[r] = sprint.ReaderRetired
		}
		if hold.Held {
			out[r] = sprint.ReaderHeld // hold <reader> (hold.go): held, whatever it beats
		}
	}
	return out, noRoom, nil
}

// readerStatesInto gives a snapshot its readers' states: the readers table's
// rows as the snapshot holds them; and its NoRoom, every reader and fleet member
// whose fresh beat says it starts no card (sprint.Beat.NoRoom), so the deal and
// the ask pass them by.
func (st *Store) readerStatesInto(ctx context.Context, s *sprint.Snapshot) error {
	noRoom := map[string]string{}
	if s.Readers != nil {
		m, nr, err := st.readerStates(ctx, s.Readers.Rows(), s.Now)
		if err != nil {
			return err
		}
		s.ReaderStates = m
		maps.Copy(noRoom, nr)
	}
	if err := st.memberNoRoomInto(ctx, s, noRoom); err != nil {
		return err
	}
	s.NoRoom = noRoom
	return nil
}

// memberNoRoomInto adds to into every fleet member of s whose fresh beat says it starts no
// card, with its word (sprint.NoRoomNow); a store that keeps no beats adds none.
func (st *Store) memberNoRoomInto(ctx context.Context, s *sprint.Snapshot, into map[string]string) error {
	if s.Fleet == nil {
		return nil
	}
	if _, err := st.rootKV(); err != nil {
		return nil
	}
	beats, err := st.Beats(ctx, s.Members())
	if err != nil {
		return err
	}
	for m, b := range beats {
		if why := sprint.NoRoomNow(b, s.Now); why != "" {
			into[m] = why
		}
	}
	return nil
}

// ReadReaderTiers reads all readers and their tiers from the readers table.
// It returns a map from reader name to tiers cell.
func ReadReaderTiers(ctx context.Context, st *Store) (map[string]string, error) {
	if err := st.EnsureReaderTiers(ctx); err != nil {
		return nil, err
	}
	rows, err := st.ReaderRows(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]string{}, nil
	}
	snap, _, _, err := st.fencedRead(ctx, []string{st.Names.Table(sprint.Readers)}, nil, nil)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	if snap.Readers != nil && snap.Readers.Texts != nil {
		for _, r := range rows {
			if t, ok := snap.Readers.Texts[r]; ok {
				result[r] = t[sprint.ReaderTiers]
			}
		}
	}
	return result, nil
}

// commonTierResult holds the result of computing a common fleet tier for a new reader.
type commonTierResult struct {
	Tier     string
	HasOther bool
	Differ   bool
}

// CommonFleetTierForAdd returns the common fleet tier for new fleet readers being
// added. It returns the common tier if all other fleet readers share the same
// non-empty tiers; otherwise returns empty with reason info.
func CommonFleetTierForAdd(ctx context.Context, st *Store, names []string) commonTierResult {
	// Check if any of the named readers are fleet readers.
	anyFleet := false
	for _, n := range names {
		if _, ok := sprint.ReaderMachine(n); ok {
			anyFleet = true
			break
		}
	}
	if !anyFleet {
		return commonTierResult{}
	}
	// Load all readers to get their tiers.
	readerTiers, err := ReadReaderTiers(ctx, st)
	if err != nil {
		return commonTierResult{}
	}
	// Collect tiers of other fleet readers (not being added now).
	otherTiers := []string{}
	for r, t := range readerTiers {
		if _, ok := sprint.ReaderMachine(r); !ok {
			continue // skip non-fleet readers
		}
		// Skip readers being added now.
		skip := false
		for _, n := range names {
			if r == n {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		otherTiers = append(otherTiers, t)
	}
	if len(otherTiers) == 0 {
		return commonTierResult{HasOther: false}
	}
	tier := sprint.CommonFleetTier(otherTiers)
	if tier == "" {
		return commonTierResult{HasOther: true, Differ: true}
	}
	return commonTierResult{Tier: tier, HasOther: true}
}

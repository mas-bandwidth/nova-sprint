package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpRowsDualRead pins the reader-first dual read of the operation rows
// (S4, C13): the op journal record (OPRECORD), the held lock (HELDLOCK), the
// notes and note lines (NOTES, LOGLINES), the timers (TIMERS), the work table's
// queued changes (QUEUE), the members a table change names and the teardown
// member list (MEMBERS) and the step result with its request digest
// (STEPRESULT). Each is read alike from its table wire, the padded standard
// base64 of that wire, the schema JSON text of a variable table and the legacy
// JSON every writer still writes (S2); every malformed value of each form is
// refused by name (`<RECORD> REFUSED: <reason>; run: nova-sprint check`) and
// never read as absent, empty or zero; a value past a declared bound is refused
// clamped; reads are counted per record and per form (S4). The read sites are
// driven through the helpers they call (fenceOf, heldID, changeIDs,
// queueEntries, inboxNote, logLine, replay, withSkips) and the counts are read
// on a store's check. It runs on the in-memory backend (store.NewMem), with
// the clock at 1000; it uses no Redis, no socket and no real time.
func TestOpRowsDualRead(t *testing.T) {
	t.Parallel()

	at := func(s int64) time.Time { return time.Unix(1000+s, 0).UTC() }
	c := newOpRowCodecs()

	timers := sprint.Timers{Open: []sprint.Timer{
		{ID: "t1", For: "friend.alpha", Due: at(600), Note: "check the landing", By: "coordinator", Set: at(0)},
		{ID: "t2", For: "beta", Due: at(900), By: "gamma", Set: at(1)},
	}}
	note := sprint.Note{
		ID: "n12", SafeDefault: "hold", DefaultCoordinatorTold: true, Alias: "j12", Kind: sprint.Judgment, Type: "card.stuck",
		Stream: "v15", Primaries: []string{"card-a", "card-b"}, Count: 2, What: "the card is stuck", Who: "coordinator",
		Attempt: 2, Before: 1, At: at(3), Decisions: []string{"land", "rework"}, Suspects: []string{"card-c"}, Card: "card-a",
		Tier: "heavy", Needs: []string{"card-b"}, Review: at(60), ReviewSet: at(4), To: "coordinator", Hint: "read the gate",
		StreamTransition: &sprint.StreamTransition{State: "stopped", Cause: "a red gate", Waiting: 3, Owners: []string{"beta"}},
		DecisionDelivery: &sprint.DecisionDelivery{Lane: "friend.beta", To: "friend.beta", From: "coordinator", Set: "j12",
			Body: "decided: land", Token: "tok-1", PreparedAt: at(5), RetryBefore: at(300), Reports: []string{"card-a"}, UnknownReceipts: 1},
		CoordinatorDelivery: &sprint.CoordinatorDelivery{From: "coordinator", To: "friend.gamma", Token: "tok-2", Subject: "a judgment",
			Body: "see j12", Episode: "e1", Sent: true, RetryBefore: at(301)},
	}
	plain := sprint.Note{ID: "n13", Kind: sprint.Happened, Type: "card.landed", Count: 1, At: at(7), Primaries: []string{"card-a"}}
	line := sprint.Line{
		Kind: "move", At: at(8), Epoch: 21, Op: "finish-ab12", Card: "card-a", Cards: []string{"card-a"}, Primary: "card-a",
		Stream: "v15", Table: "work", From: "m1:working", To: "m1:done", Gen: 2, Actor: "beta", Verb: "finish", Cause: "landed",
		Answers: []string{"j12"}, Text: map[string]string{"fix": "move the journal", "report": "gate green"},
		Set: map[string]string{"head": "4d9941fbc7fd"}, Note: &plain,
	}
	queued := sprint.QueuedChange{
		Entry: &ntable.BatchMemberEntry{ID: "card-a", Set: map[string]string{"status": "done"}},
		Prop:  &sprint.PropWrite{Table: "work", Name: "promoted", Value: "a8", Was: "a7"}, Verb: "finish", Actor: "beta", Moved: "card-a",
	}
	result := Result{
		Verb: "finish", Op: "finish-ab12", Moved: []string{"card-a"}, Refused: []sprint.Refusal{{Key: "card-b", Why: "held"}},
		Notes: 1, Raised: []string{"n12 card.stuck"}, Said: []string{"NOTE landed"}, Attempts: 1, Skipped: []string{"a stale entry"},
		Args: ArgsOf(map[string]string{"card": "card-a"}), Tables: map[string]int{"work": 3},
		Drained: []Result{{Verb: "drain", Moved: []string{"card-c"}, Tables: map[string]int{"work": 1}}},
	}
	resultText, err := json.Marshal(result)
	require.NoError(t, err)
	op := OpRecord{
		ID: "finish-ab12", Verb: "finish", At: at(9),
		Manifests: []ntable.BatchManifest{{Schema: 1, Table: "sprint:work", Epoch: "21", OperationID: "finish-ab12",
			Members: []ntable.BatchMemberEntry{{ID: "card-a", Set: map[string]string{"status": "done"}}}}},
		Notes: []sprint.Note{note}, Decided: []sprint.Note{plain}, Log: []sprint.Line{line}, Updates: []sprint.Note{note},
		Closes: []string{"n11|card-a"}, Streams: []string{"v15"}, CallerOp: "cli-77", Result: string(resultText),
		Queue: []sprint.QueuedChange{queued}, Drain: 1,
		Seat:        &sprint.SeatChange{Holder: "coordinator", Generation: 4, From: "gamma", At: at(10), By: "owner", Reason: "the seat returns"},
		CloseTimers: []string{"t0"}, Timers: &timers,
	}
	lock := heldLock{ID: "tick-ab-lock", Verb: "tick lock", At: at(11)}
	lockOp := OpRecord{ID: lock.ID, Lock: true, Verb: lock.Verb, At: lock.At}
	members := []string{"card-a", "card-b"}
	// the table layer's list carries more than the id
	membersLegacy := []byte(`[{"id":"card-a","rev":"3"},{"id":"card-b","rev":"4"}]`)

	t.Run("every form reads alike and is counted", func(t *testing.T) {
		opF := opFormsOf(t, c.op, op, true)
		lockF := opFormsOf(t, c.lock, lock, false)
		noteF := opFormsOf(t, c.notes, note, true)
		lineF := opFormsOf(t, c.lines, line, true)
		timersF := opFormsOf(t, c.timers, timers, true)
		queueF := opFormsOf(t, c.queue, queued, true)
		resultF := opFormsOf(t, c.result, result, true)
		membersF := opFormsOf(t, c.members, members, true, membersLegacy)
		got, _, err := c.members.Read(membersLegacy)
		require.NoError(t, err)
		assert.Equal(t, members, got, "the table layer's list reads its ids")
		for _, f := range []opForms{opF, lockF, noteF, lineF, timersF, queueF, resultF, membersF} {
			require.NotEmpty(t, f.legacy, f.name)
		}

		// The legacy record of a lock reads through HELDLOCK; an operation
		// record's wire reads through it too: the held id is all it takes.
		lockLegacy, err := json.Marshal(lockOp)
		require.NoError(t, err)
		h, _, err := c.lock.Read(lockLegacy)
		require.NoError(t, err)
		assert.Equal(t, heldLock{ID: lock.ID, Lock: true, Verb: lock.Verb, At: lock.At}, h)
		for _, in := range [][]byte{opF.legacy, opF.bin, opF.b64, opF.js} {
			id, err := c.heldID(string(in))
			require.NoError(t, err)
			assert.Equal(t, op.ID, id)
		}

		counts := func(cs *schemaread.Counts) [4]uint64 {
			return [4]uint64{cs.Reads(schemaread.FormBinary), cs.Reads(schemaread.FormBase64), cs.Reads(schemaread.FormJSON), cs.Reads(schemaread.FormLegacy)}
		}
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.op.Counts()), "OPRECORD")
		assert.Equal(t, [4]uint64{2, 2, 1, 3}, counts(c.lock.Counts()), "HELDLOCK: its forms, the lock and the record")
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.notes.Counts()), "NOTES")
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.lines.Counts()), "LOGLINES")
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.timers.Counts()), "TIMERS")
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.queue.Counts()), "QUEUE")
		assert.Equal(t, [4]uint64{1, 1, 1, 1}, counts(c.result.Counts()), "STEPRESULT")
		assert.Equal(t, [4]uint64{1, 1, 1, 2}, counts(c.members.Counts()), "MEMBERS")
		_ = lockF
	})

	t.Run("the read sites read every form", func(t *testing.T) {
		rc := newOpRowCodecs()
		opF := opFormsOf(t, rc.op, op, true)
		for _, in := range [][]byte{opF.legacy, opF.bin, opF.b64, opF.js} {
			f, err := fenceOf(opFenceCmd(rc, string(in)), opQueueLen(2))
			require.NoError(t, err)
			require.NotNil(t, f.Pending)
			assert.Equal(t, op, *f.Pending, "fenceOf reads the whole record")
			assert.EqualValues(t, 4, f.Gen)
		}
		qF := opFormsOf(t, rc.queue, queued, true)
		q, err := rc.queueEntries([]string{string(qF.legacy), string(qF.bin), string(qF.b64), string(qF.js)})
		require.NoError(t, err)
		assert.Equal(t, []sprint.QueuedChange{queued, queued, queued, queued}, q, "QueueRead reads every entry")

		nF := opFormsOf(t, rc.notes, note, true)
		lF := opFormsOf(t, rc.lines, line, true)
		for _, in := range [][]byte{nF.legacy, nF.bin, nF.b64, nF.js} {
			n, err := rc.inboxNote(map[string]any{"note": string(in)})
			require.NoError(t, err)
			assert.Equal(t, note, n, "NotesSince reads the note")
		}
		for _, in := range [][]byte{lF.legacy, lF.bin, lF.b64, lF.js} {
			l, err := rc.logLine(map[string]any{"line": string(in)})
			require.NoError(t, err)
			assert.Equal(t, line, l, "LogSince reads the line")
		}
		_, err = rc.inboxNote(map[string]any{})
		assert.ErrorIs(t, err, errNoEntry)

		mF := opFormsOf(t, rc.members, members, true, membersLegacy)
		for i, in := range [][]byte{membersLegacy, mF.bin, mF.b64, mF.js} {
			// a JSON account carries a table wire as its base64 (S2)
			account := in
			if i == 1 {
				account = mF.b64
			}
			delta, err := json.Marshal(map[string]json.RawMessage{"members": json.RawMessage(strings.TrimSpace(string(opJSONValue(account))))})
			require.NoError(t, err)
			ids, err := rc.changeIDs(changeEvent{verb: "apply", members: string(in), batchDelta: string(delta)})
			require.NoError(t, err)
			assert.Equal(t, []string{"card-a", "card-b", "card-a", "card-b"}, ids, "changeIDs reads the moved and the batch's members")
		}
		ids, err := rc.changeIDs(changeEvent{verb: "apply", members: "{}", batchDelta: `{"members":{}}`})
		require.NoError(t, err)
		assert.Empty(t, ids, "the table layer's empty list names none")

		rF := opFormsOf(t, rc.result, result, true)
		step := Step{Verb: "finish", CallerOp: "cli-77", Args: result.Args}
		for _, in := range [][]byte{rF.legacy, rF.bin, rF.b64, rF.js} {
			got, err := replay(rc, step, string(in))
			require.NoError(t, err)
			want := result
			want.Replay = true
			assert.Equal(t, want, got, "replay reads the recorded result")
		}
		_, err = replay(rc, Step{Verb: "finish", CallerOp: "cli-77", Args: ArgsOf("other")}, string(rF.bin))
		var conflict *OpConflictError
		require.ErrorAs(t, err, &conflict, "the digest read from the wire is compared")
		assert.True(t, conflict.OtherArgs)

		st := &Store{}
		skipped := st.withSkips(OpRecord{ID: "op-1", Verb: "finish", Result: string(rF.b64)}, []Skip{{Card: "card-a", Primary: "card-a", Table: "work"}})
		var res Result
		require.NoError(t, json.Unmarshal([]byte(skipped.Result), &res), "the skip writes the result back legacy")
		assert.Equal(t, result.Notes+1, res.Notes)
		assert.NotEmpty(t, res.Skipped)

		// The writers still write the legacy form: the fence's release and
		// relock find the record by its `{"id":` prefix (redis.go Release,
		// lock.go Relock), and json.Marshal writes OpRecord's id first.
		for _, o := range []OpRecord{op, lockOp} {
			body, err := json.Marshal(o)
			require.NoError(t, err)
			id, err := json.Marshal(o.ID)
			require.NoError(t, err)
			assert.True(t, bytes.HasPrefix(body, []byte(`{"id":`+string(id)+`,`)), "the legacy record opens with its id")
			assert.Equal(t, len(body), recordSize(o), "recordSize measures the legacy record")
		}
	})

	t.Run("every malformed value is refused by name", func(t *testing.T) {
		rc := newOpRowCodecs()
		total := 0
		check := func(name string, read func([]byte) (any, error), good []byte, kind string) {
			t.Helper()
			for _, in := range opDamages(good, kind) {
				v, err := read(in)
				total++
				if !opAssertRefused(t, name, err) {
					t.Logf("%s %s read %q", name, kind, in)
					return
				}
				assert.True(t, reflect.ValueOf(v).IsZero(), "%s: a refused read returns nothing", name)
			}
		}
		type rec struct {
			name  string
			read  func([]byte) (any, error)
			forms opForms
			fixed bool
		}
		recs := []rec{
			{"OPRECORD", opReadAny(rc.op), opFormsOf(t, rc.op, op, true), false},
			{"HELDLOCK", opReadAny(rc.lock), opFormsOf(t, rc.lock, lock, false), true},
			{"NOTES", opReadAny(rc.notes), opFormsOf(t, rc.notes, note, true), false},
			{"LOGLINES", opReadAny(rc.lines), opFormsOf(t, rc.lines, line, true), false},
			{"TIMERS", opReadAny(rc.timers), opFormsOf(t, rc.timers, timers, true), false},
			{"QUEUE", opReadAny(rc.queue), opFormsOf(t, rc.queue, queued, true), false},
			{"STEPRESULT", opReadAny(rc.result), opFormsOf(t, rc.result, result, true), false},
			{"MEMBERS", opReadAny(rc.members), opFormsOf(t, rc.members, members, true, membersLegacy), false},
		}
		for _, r := range recs {
			check(r.name, r.read, r.forms.bin, "binary")
			check(r.name, r.read, r.forms.b64, "base64")
			check(r.name, r.read, r.forms.legacy, "legacy")
			if !r.fixed {
				check(r.name, r.read, r.forms.js, "json")
			}
		}
		check("MEMBERS", opReadAny(rc.members), membersLegacy, "legacy")
		t.Logf("%d malformed values refused by name", total)

		// Every byte of every form of every record, flipped by each mask: the
		// read is refused by name or reads a value that is not zero, never a
		// zero value with no error (S2). A flip inside a value's own text is
		// another well-formed value; a flip of a key or a field id is refused
		// or leaves the record's other fields, and a single-list record
		// (TIMERS, MEMBERS) whose list it hides is refused no_known_field.
		// OPRECORD is swept on a smaller record: its generated loads give each
		// list a first chunk of 64 elements, so one read of the full record,
		// whose notes are large, clears megabytes (the pull request's 'Schema
		// improvements wanted'). The small record still holds a field of each
		// kind: words, a time, a number, a list and a nested record. Each
		// record is swept in its own parallel subtest, on its own readers.
		small := OpRecord{ID: "op-s", Verb: "finish", At: at(9), CallerOp: "cli-1", Drain: 1,
			Closes: []string{"n1|c"}, Notes: []sprint.Note{{ID: "n1", Kind: sprint.Happened, Type: "t", At: at(1)}}}
		sweep := newOpRowCodecs()
		recs[0].forms = opFormsOf(t, sweep.op, small, true)
		t.Run("every byte flipped", func(t *testing.T) {
			for _, r := range recs {
				t.Run(r.name, func(t *testing.T) {
					t.Parallel()
					forms := map[string][]byte{"binary": r.forms.bin, "base64": r.forms.b64, "legacy": r.forms.legacy}
					if !r.fixed {
						forms["json"] = r.forms.js
					}
					if r.name == "MEMBERS" {
						forms["table layer's list"] = membersLegacy
					}
					flips := 0
					for kind, good := range forms {
						for _, in := range opFlips(good, kind) {
							flips++
							v, err := r.read(in)
							if err != nil {
								if !opAssertRefused(t, r.name, err) {
									t.Logf("%s %s flip read %q", r.name, kind, in)
									return
								}
								continue
							}
							if !assert.False(t, reflect.ValueOf(v).IsZero(), "%s %s: a flipped byte read as zero with no error: %q", r.name, kind, in) {
								return
							}
						}
					}
					t.Logf("%s: %d flipped values refused by name or read as a value", r.name, flips)
				})
			}
		})

		// The flips the cold read found: the 8 bytes after the timers' list in
		// the TIMERS wire, those after the members in a one-id MEMBERS wire, and
		// a damaged list key in the JSON text, each refused no_known_field.
		tF := opFormsOf(t, rc.timers, timers, true)
		require.Len(t, tF.bin, 189, "the TIMERS image of two timers")
		for i := 125; i <= 132; i++ {
			d := append([]byte(nil), tF.bin...)
			d[i] ^= 0xff
			for _, in := range [][]byte{d, []byte(base64.StdEncoding.EncodeToString(d))} {
				v, _, err := rc.timers.Read(in)
				if err == nil {
					assert.NotEmpty(t, v.Open, "TIMERS offset %d reads its timers", i)
					continue
				}
				opAssertRefused(t, "TIMERS", err)
			}
		}
		one := []string{"card-a"}
		oneF := opFormsOf(t, rc.members, one, true, []byte(`[{"id":"card-a"}]`))
		for i := 18; i <= 33 && i < len(oneF.bin); i++ {
			d := append([]byte(nil), oneF.bin...)
			d[i] ^= 0xff
			for _, in := range [][]byte{d, []byte(base64.StdEncoding.EncodeToString(d))} {
				v, _, err := rc.members.Read(in)
				if err == nil {
					assert.NotEmpty(t, v, "MEMBERS offset %d reads its members", i)
					continue
				}
				opAssertRefused(t, "MEMBERS", err)
			}
		}
		for _, c := range []struct {
			name string
			read func([]byte) (any, error)
			in   []byte
			key  string
		}{
			{"TIMERS", opReadAny(rc.timers), tF.js, `"open_timers"`},
			{"TIMERS", opReadAny(rc.timers), tF.legacy, `"open"`},
			{"MEMBERS", opReadAny(rc.members), oneF.js, `"members"`},
			{"MEMBERS", opReadAny(rc.members), []byte(`[{"id":"card-a"}]`), `"id"`},
		} {
			require.Contains(t, string(c.in), c.key)
			damaged := bytes.Replace(c.in, []byte(c.key), []byte(c.key[:len(c.key)-2]+"X\""), 1)
			_, err := c.read(damaged)
			assert.Equal(t, opNoKnownField, opRefusalReason(t, c.name, err), "%s: a damaged key %s in %s", c.name, c.key, damaged)
		}

		// The read sites refuse them by name too, never as an empty fence, no
		// record or an empty result.
		_, err = fenceOf(opFenceCmd(rc, `{"id":"x",`), opQueueLen(0))
		opAssertRefused(t, "OPRECORD", err)
		_, err = rc.heldID(`{"id":"x",`)
		opAssertRefused(t, "HELDLOCK", err)
		_, err = rc.queueEntries([]string{`{"verb":`})
		opAssertRefused(t, "QUEUE", err)
		_, err = rc.changeIDs(changeEvent{verb: "apply", members: `[{"id":`})
		opAssertRefused(t, "MEMBERS", err)
		_, err = rc.changeIDs(changeEvent{verb: "apply", batchDelta: `{"members":[{"id":1}]}`})
		opAssertRefused(t, "MEMBERS", err)
		_, err = replay(rc, Step{Verb: "finish", CallerOp: "cli-77"}, `{"verb":`)
		opAssertRefused(t, "STEPRESULT", err)
		// The inbox and the log skip an entry they cannot read on purpose
		// (rangeSince); the entry is refused by name and counted as refused.
		before := opRefusedCount(rc.notes.Counts())
		_, err = rc.inboxNote(map[string]any{"note": `{"id":`})
		opAssertRefused(t, "NOTES", err)
		assert.Equal(t, before+1, opRefusedCount(rc.notes.Counts()), "a skipped note is counted refused")
		_, err = rc.logLine(map[string]any{"line": `{"kind":`})
		opAssertRefused(t, "LOGLINES", err)
		// withSkips leaves a result it cannot read as recorded; replay refuses it.
		st := &Store{}
		kept := st.withSkips(OpRecord{ID: "op-2", Result: `{"verb":`}, []Skip{{Card: "card-a"}})
		assert.Equal(t, `{"verb":`, kept.Result)
	})

	t.Run("a value past a declared bound is refused clamped", func(t *testing.T) {
		rc := newOpRowCodecs()
		// HeldLock.verb is string(80); Note.alias is string(16).
		atBound := heldLock{ID: "x-lock", Verb: strings.Repeat("~", 80), At: at(1)}
		img, err := rc.lock.Write(&atBound, schemaread.FormBinary)
		require.NoError(t, err)
		_, _, err = rc.lock.Read(img)
		require.NoError(t, err, "at its bound the verb reads")
		over := opPastBound(t, img, 80)
		for _, in := range [][]byte{over, []byte(base64.StdEncoding.EncodeToString(over))} {
			v, _, err := rc.lock.Read(in)
			assert.Equal(t, schemaread.ReasonClamped, opRefusalReason(t, "HELDLOCK", err), "HELDLOCK.verb past 80")
			assert.Zero(t, v)
		}
		n := plain
		n.Alias = strings.Repeat("~", 16)
		img, err = rc.notes.Write(&n, schemaread.FormBinary)
		require.NoError(t, err)
		over = opPastBound(t, img, 16)
		for _, in := range [][]byte{over, []byte(base64.StdEncoding.EncodeToString(over))} {
			v, _, err := rc.notes.Read(in)
			assert.Equal(t, schemaread.ReasonClamped, opRefusalReason(t, "NOTES", err), "NOTES.alias past 16")
			assert.Zero(t, v)
		}
		js, err := rc.notes.Write(&n, schemaread.FormJSON)
		require.NoError(t, err)
		js = bytes.Replace(js, []byte(`"`+n.Alias+`"`), []byte(`"`+n.Alias+`~"`), 1)
		v, _, err := rc.notes.Read(js)
		assert.Equal(t, schemaread.ReasonClamped, opRefusalReason(t, "NOTES", err), "NOTES.alias past 16 in the JSON text")
		assert.Zero(t, v)
		// A writer refuses a value past its bound: no image is made.
		n.Alias += "~"
		out, err := rc.notes.Write(&n, schemaread.FormBinary)
		assert.True(t, err != nil || len(out) == 0, "the writer makes no image of NOTES.alias past 16")
		// A card text past sprint.MaxCardTextBytes and a manifest past
		// ntable.LimitManifestBytes are blobs: the reader refuses them.
		long := line
		long.Text = map[string]string{"brief": strings.Repeat("~", sprint.MaxCardTextBytes+1)}
		out, err = rc.lines.Write(&long, schemaread.FormBinary)
		assert.True(t, err != nil || len(out) == 0, "a card text past its bound makes no image")
	})

	t.Run("the counts are per store, never shared", func(t *testing.T) {
		// Two stores on two backends: a read on one counts on its check alone,
		// and every store's check lists the eight records at zero before its
		// first read (S4). A Redis holds its readers, shared with its pinned
		// copies and with the store that holds it; another Redis holds others.
		now := time.Unix(1000, 0).UTC()
		mk := func(prefix string) *Store {
			st := &Store{
				B: NewMem(), Names: sprint.Names{Prefix: prefix}, Actor: "tester",
				Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
			}
			require.NoError(t, st.Init(context.Background()))
			return st
		}
		a, b := mk("op-rows-per-store-a-"), mk("op-rows-per-store-b-")
		rF := opFormsOf(t, newOpRowCodecs().result, result, true)
		_, err := replay(a.opRowSet(), Step{Verb: "finish", CallerOp: "cli-77", Args: result.Args}, string(rF.bin))
		require.NoError(t, err)
		ra, _, err := a.Check(context.Background(), 1)
		require.NoError(t, err)
		rb, _, err := b.Check(context.Background(), 1)
		require.NoError(t, err)
		assert.EqualValues(t, 1, ra.RowForms["STEPRESULT"]["binary"], "the read counts on its own store's check")
		for _, name := range []string{"OPRECORD", "HELDLOCK", "NOTES", "LOGLINES", "TIMERS", "QUEUE", "MEMBERS", "STEPRESULT"} {
			counts, ok := rb.RowForms[name]
			require.True(t, ok, "the other store's check lists %s", name)
			for form, n := range counts {
				assert.Zero(t, n, "the other store's check counts no %s %s read", name, form)
			}
		}

		r1, r2 := &Redis{}, &Redis{}
		assert.NotSame(t, r1.opRowSet(), r2.opRowSet(), "two Redis backends count apart")
		pinned := r1.AtEpoch(3, false).(*Redis)
		assert.Same(t, r1.opRowSet(), pinned.opRowSet(), "a pinned copy counts with its backend")
		st := &Store{B: r1}
		assert.Same(t, r1.opRowSet(), st.opRowSet(), "a store reads through its Redis backend's readers")
		fenceRead, _ := r2.queueFence(context.Background(), opPipe{})
		assert.Same(t, r2.opRowSet(), fenceRead.rows, "the fence's read counts on the backend that queued it")
	})

	t.Run("check lists every op row record", func(t *testing.T) {
		now := time.Unix(1000, 0).UTC()
		st := &Store{
			B: NewMem(), Names: sprint.Names{Prefix: "op-rows-dual-read-"}, Actor: "tester",
			Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
		}
		require.NoError(t, st.Init(context.Background()))
		rep, _, err := st.Check(context.Background(), 1)
		require.NoError(t, err)
		for _, name := range []string{"OPRECORD", "HELDLOCK", "NOTES", "LOGLINES", "TIMERS", "QUEUE", "MEMBERS", "STEPRESULT"} {
			_, ok := rep.RowForms[name]
			assert.True(t, ok, "check lists %s", name)
		}
	})
}

// changeIDs is the records an event names, read with no backend through
// readers no check counts (the coverage tests call it; a backend reads them
// through its own, Redis.tableChanges).
func changeIDs(ev changeEvent) ([]string, error) {
	return newOpRowCodecs().changeIDs(ev)
}

// opForms is one value of a record in each form.
type opForms struct {
	name                 string
	bin, b64, js, legacy []byte
}

// opFormsOf writes v in every form, reads each back equal and returns them. A
// fixed table has no JSON text form (S1); its js is nil.
func opFormsOf[T any](t *testing.T, c *RowCodec[T], v T, variable bool, legacy ...[]byte) opForms {
	t.Helper()
	f := opForms{name: c.rowName()}
	var err error
	f.bin, err = c.Write(&v, schemaread.FormBinary)
	require.NoError(t, err, "%s writes its wire", f.name)
	f.b64, err = c.Write(&v, schemaread.FormBase64)
	require.NoError(t, err, "%s writes base64", f.name)
	assert.Equal(t, base64.StdEncoding.EncodeToString(f.bin), string(f.b64), "%s: padded standard base64 of the wire", f.name)
	if variable {
		f.js, err = c.Write(&v, schemaread.FormJSON)
		require.NoError(t, err, "%s writes its JSON text", f.name)
	}
	if len(legacy) > 0 {
		f.legacy = legacy[0]
	} else {
		f.legacy, err = json.Marshal(v)
		require.NoError(t, err)
	}
	want := map[schemaread.Form][]byte{schemaread.FormBinary: f.bin, schemaread.FormBase64: f.b64, schemaread.FormLegacy: f.legacy}
	if variable {
		want[schemaread.FormJSON] = f.js
	}
	for form, in := range want {
		got, read, err := c.Read(in)
		require.NoError(t, err, "%s reads its %s form", f.name, form)
		assert.Equal(t, form, read, "%s: the form is detected", f.name)
		assert.Equal(t, v, got, "%s: the %s form reads the value", f.name, form)
	}
	return f
}

func opReadAny[T any](c *RowCodec[T]) func([]byte) (any, error) {
	return func(in []byte) (any, error) {
		v, _, err := c.Read(in)
		return v, err
	}
}

// opDamages is every malformed value of good in its form: each truncation, and
// for a wire each byte changed, and for a text form its bad characters.
func opDamages(good []byte, kind string) [][]byte {
	var out [][]byte
	for n := 0; n < len(good); n += opCutStep(n, len(good)) {
		if (kind == "json" || kind == "legacy") && len(bytes.TrimSpace(good[:n])) == len(bytes.TrimSpace(good)) {
			continue // only whitespace cut: the same text
		}
		if kind == "base64" && n%4 == 0 && n > 0 {
			continue // a shorter padded base64 may decode; its wire is cut below
		}
		out = append(out, append([]byte(nil), good[:n]...))
	}
	switch kind {
	case "binary":
		out = append(out, opWireDamages(good)...)
	case "base64":
		raw, _ := base64.StdEncoding.DecodeString(string(good))
		for _, d := range opWireDamages(raw) {
			out = append(out, []byte(base64.StdEncoding.EncodeToString(d)))
		}
		out = append(out, append([]byte("!"), good[1:]...))
	case "json", "legacy":
		out = append(out, append(append([]byte(nil), good...), []byte(`,"x":`)...))
		out = append(out, bytes.Replace(good, []byte(`:`), []byte(`::`), 1))
	}
	return out
}

// opAssertRefused asserts err is a refusal by name: `<NAME> REFUSED: <reason>;
// run: nova-sprint check`, which a read site may put after what it was reading.
func opAssertRefused(t *testing.T, name string, err error) bool {
	t.Helper()
	if !assert.Error(t, err, "%s: refused", name) {
		return false
	}
	var r *schemaread.Refusal
	ok := assert.True(t, errors.As(err, &r), "%s: a *schemaread.Refusal: %v", name, err)
	ok = ok && assert.Equal(t, name, r.Name, "%s: %v", name, err)
	ok = assert.Contains(t, err.Error(), name+" REFUSED: ", name) && ok
	ok = assert.True(t, strings.HasSuffix(err.Error(), "; run: "+rowCheckHelp), "%s: %s", name, err) && ok
	return ok
}

func opRefusalReason(t *testing.T, name string, err error) string {
	t.Helper()
	opAssertRefused(t, name, err)
	var r *schemaread.Refusal
	if errors.As(err, &r) {
		return r.Reason
	}
	return ""
}

// opPastBound is an image with its string of n marker bytes grown to n+1, its
// length prefix rewritten: the image of a writer that ignored the bound. The
// table wire writes a string as kind 12, its LEB128 length and its bytes.
func opPastBound(t *testing.T, good []byte, n int) []byte {
	t.Helper()
	enc := func(k int) []byte {
		b := binary.AppendUvarint([]byte{12}, uint64(k))
		return append(b, bytes.Repeat([]byte{'~'}, k)...)
	}
	require.Equal(t, 1, bytes.Count(good, enc(n)), "the at-bound string is in the image once")
	return bytes.Replace(good, enc(n), enc(n+1), 1)
}

// opJSONValue is a stored value as a JSON value inside a batch account: a list
// or an object as itself, a wire or its base64 as a JSON string.
func opJSONValue(in []byte) []byte {
	t := bytes.TrimSpace(in)
	if len(t) > 0 && (t[0] == '[' || t[0] == '{') {
		return t
	}
	b, _ := json.Marshal(string(in))
	return b
}

// opFenceCmd is the fence's MGET reply holding body, generation 3 (Acquire wrote
// generation 4), no machine record and no stuck record, read through rows.
func opFenceCmd(rows *opRowCodecs, body string) *fenceCmd {
	cmd := redis.NewSliceCmd(context.Background())
	cmd.SetVal([]any{body, "4", nil, nil})
	return &fenceCmd{SliceCmd: cmd, rows: rows}
}

// opPipe is a pipeline that only queues the fence's two reads: nothing is
// sent, so no client and no socket is made.
type opPipe struct{ redis.Pipeliner }

func (opPipe) MGet(ctx context.Context, keys ...string) *redis.SliceCmd {
	return redis.NewSliceCmd(ctx)
}

func (opPipe) LLen(ctx context.Context, key string) *redis.IntCmd { return redis.NewIntCmd(ctx) }

func opQueueLen(n int64) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	cmd.SetVal(n)
	return cmd
}

// opWireDamages is the framing damage of a wire image: every truncation, an
// unknown form byte, a byte past its end, and each byte of its trailer changed
// (the image's framing check). A changed byte inside a value's own text or
// number is another well-formed value, not damage.
func opWireDamages(good []byte) [][]byte {
	var out [][]byte
	for n := 0; n < len(good); n += opCutStep(n, len(good)) {
		out = append(out, append([]byte(nil), good[:n]...))
	}
	out = append(out, append([]byte{0x09}, good[1:]...))
	out = append(out, append(append([]byte(nil), good...), 0))
	for i := max(len(good)-opTrailer, 1); i < len(good); i++ {
		d := append([]byte(nil), good...)
		d[i] ^= 0xff
		out = append(out, d)
	}
	return out
}

// opFlips is every byte of good flipped by each of 0x01, 0x80 and 0xff; for a
// base64 text also every byte of the wire it holds, flipped and encoded again.
func opFlips(good []byte, kind string) [][]byte {
	var out [][]byte
	flip := func(img []byte, enc func([]byte) []byte) {
		for i := range img {
			for _, m := range []byte{0x01, 0x80, 0xff} {
				d := append([]byte(nil), img...)
				d[i] ^= m
				out = append(out, enc(d))
			}
		}
	}
	same := func(b []byte) []byte { return b }
	flip(good, same)
	if kind == "base64" {
		raw, _ := base64.StdEncoding.DecodeString(string(good))
		flip(raw, func(b []byte) []byte { return []byte(base64.StdEncoding.EncodeToString(b)) })
	}
	return out
}

// opTrailer is how many closing bytes of an image are changed: the framing
// trailer the table wire ends with.
const opTrailer = 8

// opRefusedCount is every refusal a counter holds, whatever its reason.
func opRefusedCount(c *schemaread.Counts) uint64 {
	var n uint64
	for k, v := range c.Snapshot() {
		if strings.HasPrefix(k, "refused_") {
			n += v
		}
	}
	return n
}

// opCutStep is the step between truncations of an image of size n: every cut
// in its first and last 64 bytes, where its header and its trailer are, and
// every 13th between, so the test stays cheap (a unit test is light).
func opCutStep(at, n int) int {
	if at < 64 || at >= n-64 {
		return 1
	}
	return 13
}

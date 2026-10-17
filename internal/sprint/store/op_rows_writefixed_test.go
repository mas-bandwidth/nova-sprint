package store

import (
	"context"
	"encoding/json"
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

// TestOpRowsWriteFixed pins the writer switch of the operation rows (S4:
// writers second): a commit writes each queued change as the QUEUE table wire
// and each note, on the inbox and in the notes hash, as the NOTES table wire
// (S1); what it writes reads back equal through the dual reader the read sites
// call (queueEntries, inboxNote, the notes reader), counted as binary reads;
// the legacy values an older writer left still read (the dual reader is kept);
// a value the table cannot hold is refused by name before the commit queues
// it, and nothing of it is written in the legacy form instead. The commit runs
// on a pipeline that only records what it is given and a client that only
// answers the alias count: no Redis, no socket and no real time; the clock
// starts at 1000. The rows read the same on the in-memory backend's store.
func TestOpRowsWriteFixed(t *testing.T) {
	t.Parallel()

	at := func(s int64) time.Time { return time.Unix(1000+s, 0).UTC() }
	judgment := sprint.Note{
		ID: "n12", SafeDefault: "hold", Kind: sprint.Judgment, Type: "card.stuck", Stream: "v15",
		Primaries: []string{"card-a"}, Count: 1, What: "the card is stuck", Who: "coordinator",
		At: at(3), Decisions: []string{"land", "rework"}, Card: "card-a", To: "coordinator",
	}
	plain := sprint.Note{ID: "n13", Kind: sprint.Happened, Type: "card.landed", Count: 1, At: at(7), Primaries: []string{"card-a"}}
	updated := judgment
	updated.Review, updated.ReviewSet = at(60), at(4)
	queued := sprint.QueuedChange{
		Entry: &ntable.BatchMemberEntry{ID: "card-a", Set: map[string]string{"status": "done"}},
		Prop:  &sprint.PropWrite{Table: "work", Name: "promoted", Value: "a8", Was: "a7"}, Verb: "finish", Actor: "beta", Moved: "card-a",
	}
	op := OpRecord{ID: "finish-ab12", Verb: "finish", At: at(9),
		Queue: []sprint.QueuedChange{queued, {Verb: "drain", Actor: "gamma"}},
		Notes: []sprint.Note{judgment, plain}, Updates: []sprint.Note{updated}}

	// the alias count the commit reads is 4, so the judgment is the fifth
	wantJudgment := judgment.Bound()
	wantJudgment.Alias = sprint.Alias(5)

	t.Run("the commit writes the table wire and it reads back equal", func(t *testing.T) {
		t.Parallel()
		r := &Redis{C: wfAliasClient{n: "4"}, Names: sprint.Names{Prefix: "write-fixed-"}}
		p := &wfPipe{}
		require.NoError(t, r.commit(context.Background(), p, op))
		rows := r.opRowSet()

		queue := p.values("rpush", r.key(keyQueue))
		require.Len(t, queue, 2)
		for _, v := range queue {
			wfAssertWire(t, v, "QUEUE")
		}
		got, err := rows.queueEntries(queue)
		require.NoError(t, err)
		assert.Equal(t, op.Queue, got, "the queue reads back equal")

		inbox := p.streamValues(r.key(keyInbox), "note")
		require.Len(t, inbox, 2)
		for i, want := range []sprint.Note{wantJudgment, plain.Bound()} {
			wfAssertWire(t, inbox[i], "NOTES")
			n, err := rows.inboxNote(map[string]any{"note": inbox[i]})
			require.NoError(t, err)
			assert.Equal(t, want, n, "the inbox note reads back equal")
		}

		hash := p.hashValues(r.key(keyNotes), "n12")
		require.Len(t, hash, 2, "the judgment, then its update")
		for i, want := range []sprint.Note{wantJudgment, updated.Bound()} {
			wfAssertWire(t, hash[i], "NOTES")
			n, _, err := rows.notes.Read([]byte(hash[i]))
			require.NoError(t, err)
			assert.Equal(t, want, n, "the notes hash reads back equal")
		}
		assert.Empty(t, p.hashValues(r.key(keyNotes), "n13"), "a note that is not a judgment is not kept in the hash")

		assert.EqualValues(t, 2, rows.queue.Counts().Reads(schemaread.FormBinary), "QUEUE: each entry read as the table wire")
		assert.EqualValues(t, 4, rows.notes.Counts().Reads(schemaread.FormBinary), "NOTES: each note read as the table wire")
		assert.Zero(t, rows.notes.Counts().Reads(schemaread.FormLegacy), "NOTES: no switched note is legacy")

		// The note's log line stays legacy: load.go logAt reads the legacy
		// line alone, so its writer switches after that reader (S4).
		for _, l := range p.streamValues(r.key(keyLog), "line") {
			var line sprint.Line
			require.NoError(t, json.Unmarshal([]byte(l), &line), "the log line is the legacy JSON")
		}
	})

	t.Run("the legacy values an older writer left still read", func(t *testing.T) {
		t.Parallel()
		rows := newOpRowCodecs()
		legacyQueue, err := json.Marshal(queued)
		require.NoError(t, err)
		legacyNote, err := json.Marshal(plain)
		require.NoError(t, err)
		wire, err := rows.queueRow(queued)
		require.NoError(t, err)
		q, err := rows.queueEntries([]string{string(legacyQueue), wire})
		require.NoError(t, err)
		assert.Equal(t, []sprint.QueuedChange{queued, queued}, q, "a queue mixing the two forms reads whole")
		n, err := rows.inboxNote(map[string]any{"note": string(legacyNote)})
		require.NoError(t, err)
		assert.Equal(t, plain, n)
		assert.EqualValues(t, 1, rows.queue.Counts().Reads(schemaread.FormLegacy))
		assert.EqualValues(t, 1, rows.queue.Counts().Reads(schemaread.FormBinary))
		assert.EqualValues(t, 1, rows.notes.Counts().Reads(schemaread.FormLegacy))
	})

	t.Run("a value the table cannot hold is refused by name", func(t *testing.T) {
		t.Parallel()
		rows := newOpRowCodecs()
		// Note.alias is string(16): an alias past it has no image.
		over := plain
		over.Alias = strings.Repeat("~", 17)
		body, err := rows.noteRow(over)
		opAssertRefused(t, "NOTES", err)
		assert.Empty(t, body, "no value is made")

		// In a commit the refusal stops the note before it is queued: no
		// inbox entry and no legacy text in its place.
		r := &Redis{C: wfAliasClient{n: "0"}, Names: sprint.Names{Prefix: "write-fixed-refused-"}}
		p := &wfPipe{}
		err = r.commit(context.Background(), p, OpRecord{ID: "op-r", At: at(1), Notes: []sprint.Note{over}})
		opAssertRefused(t, "NOTES", err)
		assert.Empty(t, p.streamValues(r.key(keyInbox), "note"), "no inbox entry is queued")
		assert.Empty(t, p.streamValues(r.key(keyLog), "line"), "nothing of the note is queued")
	})

	t.Run("the rows read alike on the in-memory backend's store", func(t *testing.T) {
		t.Parallel()
		now := time.Unix(1000, 0).UTC()
		st := &Store{B: NewMem(), Names: sprint.Names{Prefix: "write-fixed-mem-"}, Actor: "tester",
			Now: func() time.Time { return now }, Sleep: func(time.Duration) {}}
		require.NoError(t, st.Init(context.Background()))
		rows := st.opRowSet()
		body, err := rows.noteRow(plain)
		require.NoError(t, err)
		n, err := rows.inboxNote(map[string]any{"note": body})
		require.NoError(t, err)
		assert.Equal(t, plain, n)
		rep, _, err := st.Check(context.Background(), 1)
		require.NoError(t, err)
		assert.EqualValues(t, 1, rep.RowForms["NOTES"]["binary"], "the store's check counts the table wire read")
	})
}

// wfAssertWire asserts v is the record's table wire: the form byte, never the
// legacy JSON text.
func wfAssertWire(t *testing.T, v, name string) {
	t.Helper()
	require.NotEmpty(t, v, name)
	assert.EqualValues(t, 1, v[0], "%s: the table wire's form byte", name)
	assert.False(t, LegacyObject([]byte(v)), "%s: not the legacy JSON", name)
}

// wfAliasClient answers only the alias count a commit reads; nothing is sent.
type wfAliasClient struct {
	redis.UniversalClient
	n string
}

func (c wfAliasClient) HGet(ctx context.Context, key, field string) *redis.StringCmd {
	return redis.NewStringResult(c.n, nil)
}

// wfPipe records what a commit queues; nothing is sent, so no client and no
// socket is made.
type wfPipe struct {
	redis.Pipeliner
	calls []wfCall
}

type wfCall struct {
	verb, key string
	vals      []any
}

func (p *wfPipe) RPush(ctx context.Context, key string, values ...any) *redis.IntCmd {
	p.calls = append(p.calls, wfCall{"rpush", key, values})
	return redis.NewIntCmd(ctx)
}

func (p *wfPipe) XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd {
	vals, _ := a.Values.([]any)
	p.calls = append(p.calls, wfCall{"xadd", a.Stream, vals})
	return redis.NewStringCmd(ctx)
}

func (p *wfPipe) HSet(ctx context.Context, key string, values ...any) *redis.IntCmd {
	p.calls = append(p.calls, wfCall{"hset", key, values})
	return redis.NewIntCmd(ctx)
}

// values is every value queued by verb on key, in order.
func (p *wfPipe) values(verb, key string) []string {
	var out []string
	for _, c := range p.calls {
		if c.verb == verb && c.key == key {
			for _, v := range c.vals {
				out = append(out, v.(string))
			}
		}
	}
	return out
}

// streamValues is field's value of every entry added to stream, in order.
func (p *wfPipe) streamValues(stream, field string) []string {
	var out []string
	for _, c := range p.calls {
		if c.verb == "xadd" && c.key == stream {
			for i := 0; i+1 < len(c.vals); i += 2 {
				if c.vals[i] == field {
					out = append(out, c.vals[i+1].(string))
				}
			}
		}
	}
	return out
}

// hashValues is every value set on field of hash, in order.
func (p *wfPipe) hashValues(hash, field string) []string {
	var out []string
	for _, c := range p.calls {
		if c.verb == "hset" && c.key == hash {
			for i := 0; i+1 < len(c.vals); i += 2 {
				if c.vals[i] == field {
					out = append(out, c.vals[i+1].(string))
				}
			}
		}
	}
	return out
}

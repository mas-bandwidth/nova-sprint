package store

// The seventh cold read (on aa762fbe, round 15): the two changes of round 15.
// Receipt fidelity now that an event stores its after-image once (the
// receipt column leaves it out, readReceipt puts it back), on the Fake and on
// PostgreSQL, against an event row written in the old shape; the new
// indexes (EXPLAIN of the store's own queries, custom and generic plans, the
// partial predicates against the state sets the queries use); the schema
// hash that changed with them.
//
// Folded in round 16 from the seventh cold read's probes (the targeted read
// of round 15, nsv2-coldread7). Its rulings: an event's After and its
// Receipt.After no longer alias on PostgreSQL (EventImagesAlias passes);
// round 16 kept only the two indexes a store query used (cards_working,
// cards_live); round 22 dropped cards_live with the tier check, so the
// counts below are one.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cr7Step is one op of the fidelity scenario; clear runs it through Clear.
type cr7Step struct {
	label string
	op    Op
	clear bool
}

// cr7Steps is every outcome: accepted with many touched rows of every table
// (streams, cards, rows, policies, judgements, cursors), refused (epoch,
// condition, write), a clear, and the steps after it, with strings that JSON
// and jsonb treat differently (escapes, U+2028, astral runes) and an int64
// past 2^53.
func cr7Steps() []cr7Step {
	n := 0
	op := func(epoch int, expect []Expect, w ...Write) Op {
		n++
		return Op{ID: fmt.Sprintf("cr7-%d", n), Epoch: epoch, Actor: "tester", Kind: "x", At: t0.Add(time.Duration(n) * time.Second), Expect: expect, Writes: w, Note: fmt.Sprintf("n%d <&>   \t é 😀", n)}
	}
	weird := "a <b> & \"c\" \\    \t\n é 😀 \u007f"
	var s []cr7Step
	add := func(label string, o Op) { s = append(s, cr7Step{label: label, op: o}) }
	add("seed every table", op(1, nil,
		PutStream{"s1", "mas-bandwidth/x", "main", 1}, PutStream{"s2", weird, "dev", 2},
		PutRow{Name: "w1", Session: weird, Role: "coordinator"},
		PutRow{Name: "k1"}, SetRowState{"w1", "up"},
		PutCard{Tier: "pro", Kind: "producer", ID: "c0", Stream: "s1", Brief: weird, Paths: []string{"a/b", weird}, DoneWhen: []string{"x"}, Prose: weird, Priority: "high"}, SetCardState{"c0", "landed"},
		PutCard{Tier: "pro", Kind: "producer", ID: "c1", Stream: "s1", Score: 7, Needs: []string{"c0"}, After: []string{"c0"}},
		PutCard{Tier: "pro", Kind: "sentinel", ID: "z1", Stream: "s1"},
		PutCard{Tier: "pro", Kind: "work", ID: "u1", Of: "c1", Stream: "s1", Holder: "w1", Paths: []string{"p"}},
		PutCard{Tier: "pro", Kind: "read", ID: "u2", Of: "c1", Stream: "s1", Holder: "w1"},
		PutJudgement{ID: "j1", Kind: "red", About: "c1", Question: weird, Command: "cmd", Group: "g", Deadline: t0.Add(time.Hour + 123456789)},
		PutJudgement{ID: "j2", Kind: "fix"},
		PutPolicy{"script", "p1", weird}, PutPolicy{"data", "p2", "{}"}))
	add("activate policy", op(1, nil, SetActivePolicy{1}))
	add("ready", op(1, []Expect{CardGen{"c1", 0}, RowIs{"w1", "up"}}, SetCardState{"c1", "ready"}, SetCardState{"u1", "ready"}, SetCardState{"u2", "ready"}))
	add("move and count", op(1, []Expect{CardGen{"c1", 1}, RowIs{"w1", "up"}},
		SetCardState{"u1", "working"}, SetCardState{"u2", "working"},
		SetCardCost{"u1", 1<<53 + 1, true}, SetCardResult{"u1", "ok"}, SetCardBranch{"u1", weird}, SetCardHead{"u1", "abc"}, SetCardPR{"u1", 42},
		SetCardReason{"c1", weird}, SetRowFlaps{"w1", 2}, SetRowHeld{"w1", true, weird}, SetStreamHeld{"s2", true}))
	add("cursor", op(1, nil, SetCursor{"sub", 2}, SetCursor{"sub2", 3}))
	add("judgement answered and expired", op(1, []Expect{JudgementOpen{"j1"}}, AnswerJudgement{"j1", weird, ""}, ExpireJudgement{"j2", "late"}))
	add("switch policy", op(1, nil, PutPolicy{"script", "p3", "b"}, SetActivePolicy{3}))
	add("refused: epoch", op(9, nil, SetCardScore{"c1", 1}))
	add("refused: condition", op(1, []Expect{CardGen{"c1", 0}}, SetCardScore{"c1", 1}))
	add("refused: write, after earlier writes of the op", op(1, nil, SetCardScore{"c1", 99}, SetRowState{"w1", "up"}))
	add("refused: judgement not open", op(1, []Expect{JudgementOpen{"j1"}}, SetCardScore{"c1", 1}))
	n++
	s = append(s, cr7Step{label: "clear", clear: true, op: Op{ID: fmt.Sprintf("cr7-%d", n), Epoch: 1, Actor: "tester", Kind: "clear", At: t0.Add(time.Duration(n) * time.Second), Expect: []Expect{AtSeq{11}}, Note: weird}})
	add("after the clear", op(2, nil, PutStream{"s1", "r", "main", 0}, PutCard{Tier: "pro", Kind: "producer", ID: "c9", Stream: "s1"}, SetRowFlaps{"w1", 5}, SetCursor{"sub", 11}))
	add("refused: old epoch after the clear", op(1, nil, SetCardScore{"c1", 1}))
	return s
}

func cr7JSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// cr7Run runs the steps on s and returns, per op id, the receipt JSON from
// Apply, and asserts Replay, a repeated Apply (or Clear) with the same id and
// the Event from Events are byte-identical to it, and the event's own after
// to the receipt's.
func cr7Run(t *testing.T, s Store) (map[string]string, []Event) {
	ctx := context.Background()
	got := map[string]string{}
	for _, st := range cr7Steps() {
		apply := s.Apply
		if st.clear {
			apply = s.Clear
		}
		r, err := apply(ctx, st.op)
		require.NoError(t, err, st.label)
		first := cr7JSON(t, r)
		got[st.op.ID] = first
		if strings.HasPrefix(st.label, "refused") {
			assert.False(t, r.Accepted, st.label)
		} else {
			require.True(t, r.Accepted, "%s: %s", st.label, r.Reason)
		}
		rp, ok, err := s.Replay(ctx, st.op.ID)
		require.NoError(t, err)
		require.True(t, ok, st.label)
		assert.Equal(t, first, cr7JSON(t, rp), "%s: Replay", st.label)
		again, err := apply(ctx, st.op)
		require.NoError(t, err)
		assert.Equal(t, first, cr7JSON(t, again), "%s: repeated Apply", st.label)
		evs, err := s.Events(ctx, r.Seq-1, 1)
		require.NoError(t, err)
		require.Len(t, evs, 1)
		assert.Equal(t, first, cr7JSON(t, evs[0].Receipt), "%s: Events", st.label)
		assert.Equal(t, cr7JSON(t, evs[0].After), cr7JSON(t, evs[0].Receipt.After), "%s: the event's after is the receipt's", st.label)
	}
	evs, err := s.Events(ctx, 0, 0)
	require.NoError(t, err)
	return got, evs
}

// The Fake still stores the whole receipt (after included), as PG did before
// 09b2eeb8: it is the old shape's oracle. Unit: Fake only.
func TestColdRead7ReceiptFidelityFake(t *testing.T) {
	t.Parallel()
	f := NewFake()
	_, evs := cr7Run(t, f)
	snap, err := f.Snapshot(context.Background())
	require.NoError(t, err)
	require.NoError(t, checkRebuild(snap, evs))
}

func TestFunctionalColdRead7ReceiptFidelity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, p, dsn := pgHarness(t)
	pgGot, pgEvents := cr7Run(t, p)
	fakeGot, fakeEvents := cr7Run(t, NewFake())
	assert.Equal(t, fakeGot, pgGot, "every receipt byte-identical to the Fake's, which keeps the old shape")
	require.Len(t, pgEvents, len(fakeEvents))
	for i := range pgEvents {
		assert.Equal(t, cr7JSON(t, fakeEvents[i]), cr7JSON(t, pgEvents[i]), "event %d", pgEvents[i].Seq)
	}
	snap, err := p.Snapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, checkRebuild(snap, pgEvents), "Rebuild == Snapshot")

	c := connect(t, dsn)
	var withAfter, total int
	require.NoError(t, c.QueryRow(ctx, "SELECT count(*) FILTER (WHERE receipt ? 'after'), count(*) FROM sprint.events").Scan(&withAfter, &total))
	assert.Equal(t, 0, withAfter, "the receipt column leaves the after-image out")
	assert.Equal(t, len(pgEvents), total)

	// Events written in the old shape (the receipt carrying its own after),
	// as a store made at 5c1eec16 (same schema hash as this head) holds them:
	// one accepted, whose after-image is w1 as it is now (so Rebuild still
	// equals Snapshot), one refused.
	last := pgEvents[len(pgEvents)-1].Seq
	w1now := snap.Rows["w1"]
	after := &State{Rows: map[string]Row{"w1": w1now}}
	old := Receipt{Op: "legacy-1", Kind: "x", Actor: "old", Epoch: 2, At: at(t0.Add(time.Hour)), Note: "n", Accepted: true, Seq: last + 1, After: after}
	oldRefused := Receipt{Op: "legacy-2", Kind: "x", Actor: "old", Epoch: 2, At: at(t0.Add(time.Hour)), Accepted: false, Reason: "row w1 is at gen 9, expected gen 1", Seq: last + 2}
	ins := `INSERT INTO sprint.events (seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch, policy) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err = c.Exec(ctx, ins, old.Seq, old.Op, old.Actor, old.Kind, true, "", jsonArg(after), jsonArg(after), string(encode(old)), old.At, 2, 3)
	require.NoError(t, err)
	_, err = c.Exec(ctx, ins, oldRefused.Seq, oldRefused.Op, oldRefused.Actor, oldRefused.Kind, false, oldRefused.Reason, nil, nil, string(encode(oldRefused)), oldRefused.At, 2, 3)
	require.NoError(t, err)
	for _, want := range []Receipt{old, oldRefused} {
		r, ok, err := p.Replay(ctx, want.Op)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, cr7JSON(t, want), cr7JSON(t, r), "Replay of an old-shape %s", want.Op)
		o := Op{ID: want.Op, Epoch: 2, Actor: "someone else", Kind: "x", At: t0, Writes: []Write{SetRowFlaps{"w1", 1}}}
		r, err = p.Apply(ctx, o)
		require.NoError(t, err)
		assert.Equal(t, cr7JSON(t, want), cr7JSON(t, r), "repeated Apply of an old-shape %s", want.Op)
		evs, err := p.Events(ctx, want.Seq-1, 1)
		require.NoError(t, err)
		require.Len(t, evs, 1)
		assert.Equal(t, cr7JSON(t, want), cr7JSON(t, evs[0].Receipt), "Events of an old-shape %s", want.Op)
	}
	all, err := p.Events(ctx, 0, 0)
	require.NoError(t, err)
	snap, err = p.Snapshot(ctx)
	require.NoError(t, err)
	require.NoError(t, checkRebuild(snap, all), "Rebuild == Snapshot with old-shape events in the log")
}

// An Event's receipt and its after-image: on the Fake they are two decoded
// copies; on PG, after 09b2eeb8, one pointer (readReceipt "shared with the
// event, not copied"). Editing one then edits the other on PG only.
func TestFunctionalColdRead7EventImagesAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, p, _ := pgHarness(t)
	for name, s := range map[string]Store{"fake": NewFake(), "pg": p} {
		h := &harness{t: t, s: s, ctx: ctx, epoch: 1}
		h.seed()
		evs, err := s.Events(ctx, 0, 1)
		require.NoError(t, err)
		e := evs[0]
		e.After.Cards["c1"] = Card{ID: "edited"}
		assert.Equal(t, "c1", e.Receipt.After.Cards["c1"].ID, "%s: editing an event's after leaves its receipt as it was", name)
	}
}

// cr7Node is a plan node of EXPLAIN (FORMAT JSON).
type cr7Node struct {
	Type     string    `json:"Node Type"`
	Relation string    `json:"Relation Name"`
	Index    string    `json:"Index Name"`
	Plans    []cr7Node `json:"Plans"`
}

func (n cr7Node) walk(f func(cr7Node)) {
	f(n)
	for _, c := range n.Plans {
		c.walk(f)
	}
}

// cr7Plan is the plan of sql (the store's own text, $n parameters) with args
// as SQL literals, prepared as pgx prepares it, under mode (force_custom_plan
// or force_generic_plan): "Seq Scan cards", "Index Scan cards_working", ...
func cr7Plan(t *testing.T, c *pgx.Conn, mode, sql string, args ...string) []string {
	t.Helper()
	ctx := context.Background()
	_, err := c.Exec(ctx, "SET plan_cache_mode = "+mode)
	require.NoError(t, err)
	_, err = c.Exec(ctx, "PREPARE cr7 AS "+sql)
	require.NoError(t, err, sql)
	q := "EXECUTE cr7"
	if len(args) > 0 {
		q += "(" + strings.Join(args, ", ") + ")"
	}
	if mode == "auto" { // six runs: the plan cache has chosen custom or generic by then
		for range 6 {
			_, err := c.Exec(ctx, q)
			require.NoError(t, err)
		}
	}
	q = "EXPLAIN (FORMAT JSON) " + q
	var raw []byte
	require.NoError(t, c.QueryRow(ctx, q).Scan(&raw))
	_, err = c.Exec(ctx, "DEALLOCATE cr7")
	require.NoError(t, err)
	var plans []struct {
		Plan cr7Node `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal(raw, &plans))
	var out []string
	plans[0].Plan.walk(func(n cr7Node) {
		if strings.Contains(n.Type, "Scan") {
			out = append(out, strings.TrimSpace(n.Type+" "+n.Relation+" "+n.Index))
		}
	})
	return out
}

// cr7Populate builds epoch 1 with 20 rows, 2,000 producers and 2,000
// consumers (200 ready, 200 working), 500 judgements (50 open), then clears
// into epoch 2 with a few of each, and ANALYZEs.
func cr7Populate(t *testing.T, h *harness, c *pgx.Conn) {
	var ws []Write
	flush := func() {
		if len(ws) > 0 {
			h.ok(h.op("build", nil, ws...))
			ws = nil
		}
	}
	push := func(w ...Write) {
		ws = append(ws, w...)
		if len(ws) >= 400 {
			flush()
		}
	}
	push(PutStream{"s1", "r", "main", 0})
	for r := range 20 {
		name := fmt.Sprintf("r%02d", r)
		push(PutRow{Name: name}, SetRowState{name, "up"})
	}
	for i := range 2000 {
		push(PutCard{Tier: "pro", Kind: "producer", ID: fmt.Sprintf("p%04d", i), Stream: "s1"})
	}
	flush()
	for i := range 2000 {
		id := fmt.Sprintf("k%04d", i)
		push(PutCard{Tier: "pro", Kind: "work", ID: id, Of: fmt.Sprintf("p%04d", i), Stream: "s1", Holder: fmt.Sprintf("r%02d", i%20)})
	}
	flush()
	for i := range 2000 {
		if i%10 < 2 {
			push(SetCardState{fmt.Sprintf("k%04d", i), "ready"})
		}
	}
	flush()
	for i := range 2000 {
		if i%10 == 1 {
			push(SetCardState{fmt.Sprintf("k%04d", i), "working"})
		}
	}
	for i := range 500 {
		push(PutJudgement{ID: fmt.Sprintf("j%03d", i), Kind: "red"})
	}
	flush()
	for i := range 450 {
		push(ExpireJudgement{fmt.Sprintf("j%03d", i), "x"})
	}
	flush()
	_, err := c.Exec(context.Background(), "ANALYZE")
	require.NoError(t, err)
}

func TestFunctionalColdRead7Indexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, p, dsn := pgHarness(t)
	c := connect(t, dsn)
	cr7Populate(t, h, c)

	// The partial predicates are exactly the state sets the queries use.
	var working string
	require.NoError(t, c.QueryRow(ctx, `SELECT pg_get_expr(i.indpred, i.indrelid) FROM pg_index i WHERE i.indexrelid = 'sprint.cards_working'::regclass`).Scan(&working))
	assert.Equal(t, "(state = 'working'::text)", working)

	// Round 22: the store no longer counts a row's working cards (width is
	// the roster's); the per-row count is a reader's, and cards_working
	// serves it.
	workingSQL := "SELECT count(*) FROM sprint.cards WHERE epoch = $1 AND holder = $2 AND state = 'working'"
	dashSQL := `SELECT r.name, r.state, r.held, count(c.id) FROM sprint.rows r
			LEFT JOIN sprint.cards c ON c.epoch = r.epoch AND c.holder = r.name AND c.state = 'working' WHERE r.epoch = $1 GROUP BY r.name, r.state, r.held ORDER BY r.name`
	for _, mode := range []string{"force_custom_plan", "force_generic_plan", "auto"} {
		w := cr7Plan(t, c, mode, workingSQL, "1", "'r07'")
		assert.Contains(t, strings.Join(w, "|"), "cards_working", "%s width: %v", mode, w)
		// Reversed witness: the state as a parameter loses the partial index;
		// the check above sees it.
		x := cr7Plan(t, c, mode, strings.Replace(workingSQL, "'working'", "$3", 1), "1", "'r07'", "'working'")
		if mode == "force_generic_plan" {
			assert.NotContains(t, strings.Join(x, "|"), "cards_working", "%s state as a parameter: %v", mode, x)
		}
		d := cr7Plan(t, c, mode, dashSQL, "1")
		assert.Contains(t, strings.Join(d, "|"), "cards_working", "%s dashboard per-row count: %v", mode, d)
		for _, q := range [][]string{
			{"SELECT " + cardCols + " FROM sprint.cards WHERE epoch = $1 AND id = $2", "1", "'k0007'"},
			{"SELECT " + judgeCols + " FROM sprint.judgements WHERE epoch = $1 AND id = $2", "1", "'j007'"},
		} {
			plan := cr7Plan(t, c, mode, q[0], q[1:]...)
			assert.NotContains(t, strings.Join(plan, "|"), "Seq Scan", "%s %s: %v", mode, q[0], plan)
		}
	}

	// Epoch 2, a few cards, with epoch 1's thousands still in the tables:
	// every epoch-filtered read of cards or judgements uses an index.
	o := h.op("clear", nil)
	r, err := p.Clear(ctx, o)
	require.NoError(t, err)
	require.True(t, r.Accepted)
	h.epoch = 2
	h.ok(h.op("e2", nil, PutStream{"s1", "r", "main", 0}, PutCard{Tier: "pro", Kind: "producer", ID: "q1", Stream: "s1"},
		PutCard{Tier: "pro", Kind: "work", ID: "q2", Of: "q1", Stream: "s1", Holder: "r07"}, SetCardState{"q2", "ready"},
		PutJudgement{ID: "jj", Kind: "red"}))
	h.ok(h.op("e2w", nil, SetCardState{"q2", "working"}))
	_, err = c.Exec(ctx, "ANALYZE")
	require.NoError(t, err)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan", "auto"} {
		for _, q := range [][]string{
			{"SELECT " + cardCols + " FROM sprint.cards WHERE epoch = $1", "2"},
			{"SELECT " + judgeCols + " FROM sprint.judgements WHERE epoch = $1", "2"},
			{workingSQL, "2", "'r07'"},
			{"SELECT " + cardCols + " FROM sprint.cards WHERE epoch = $1 AND id = $2", "2", "'q2'"},
			{dashSQL, "2"},
		} {
			plan := cr7Plan(t, c, mode, q[0], q[1:]...)
			for _, s := range plan {
				seq := strings.HasPrefix(s, "Seq Scan cards") || strings.HasPrefix(s, "Seq Scan judgements")
				if mode == "force_generic_plan" && !strings.Contains(q[0], "$2") {
					// A forced generic plan of a whole-epoch read estimates epoch = $1
					// as 1/ndistinct and scans; PostgreSQL's auto mode (what pgx
					// gets) keeps the custom plan, checked below. Logged, not failed.
					if seq {
						t.Logf("generic plan only: %s: %v", q[0][:40], plan)
					}
					continue
				}
				assert.False(t, seq, "%s %s: %v", mode, q[0], plan)
			}
		}
	}

	// The store's answers on the populated store, against the Fake's rule.
	snap, err := p.Snapshot(ctx)
	require.NoError(t, err)
	assert.Len(t, snap.Cards, 2)
	events, err := cr7AllEvents(ctx, p)
	require.NoError(t, err)
	require.NoError(t, checkRebuild(snap, events))
}

func cr7AllEvents(ctx context.Context, s Store) ([]Event, error) {
	var out []Event
	for {
		after := int64(0)
		if len(out) > 0 {
			after = out[len(out)-1].Seq
		}
		e, err := s.Events(ctx, after, 0)
		if err != nil || len(e) == 0 {
			return out, err
		}
		out = append(out, e...)
	}
}

// cr7OldSchema is schema.sql as it was before 5c1eec16: without the
// indexes (comments do not count in the hash).
func cr7OldSchema() string {
	var b strings.Builder
	for line := range strings.Lines(schemaSQL) {
		if strings.HasPrefix(line, "CREATE INDEX IF NOT EXISTS cards_") || strings.HasPrefix(line, "CREATE INDEX IF NOT EXISTS judgements_epoch_state") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// The hash moved with the indexes. Unit: no store.
func TestColdRead7SchemaHashMoved(t *testing.T) {
	t.Parallel()
	old := cr7OldSchema()
	assert.Less(t, len(old), len(schemaSQL))
	assert.NotEqual(t, hashSchema(old), schemaHash)
	assert.Equal(t, 1, strings.Count(schemaSQL, "CREATE INDEX IF NOT EXISTS")-strings.Count(old, "CREATE INDEX IF NOT EXISTS"))
}

func TestFunctionalColdRead7MigrateOldHash(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// A store made by the previous head: its schema, its hash.
	dsn := startPG(t)
	c := connect(t, dsn)
	old := cr7OldSchema()
	_, err := c.Exec(ctx, old)
	require.NoError(t, err)
	_, err = c.Exec(ctx, "UPDATE sprint.meta SET hash = $1 WHERE id = 1", hashSchema(old))
	require.NoError(t, err)
	q, err := Open(ctx, dsn, "")
	require.NoError(t, err)
	defer q.Close()
	err = q.Migrate(ctx)
	require.Error(t, err)
	assert.Equal(t, "store: migrate: the store's schema hash is "+hashSchema(old)+", this binary's is "+schemaHash+": "+staleShape, err.Error())
	var n int
	require.NoError(t, c.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname = 'sprint' AND indexname IN ('cards_working', 'cards_live')").Scan(&n))
	assert.Equal(t, 0, n, "a refused Migrate adds no index")

	// An empty store migrates clean, twice, with every index.
	_, p, dsn2 := pgHarness(t)
	require.NoError(t, p.Migrate(ctx))
	c2 := connect(t, dsn2)
	require.NoError(t, c2.QueryRow(ctx, "SELECT count(*) FROM pg_indexes WHERE schemaname = 'sprint' AND (tablename IN ('cards', 'judgements', 'events')) AND indexname NOT LIKE '%pkey' AND indexname NOT LIKE '%_op_key'").Scan(&n))
	assert.Equal(t, 1, n, "cards_working, and no other secondary index (round 22 dropped cards_live)")
	var hash string
	require.NoError(t, c2.QueryRow(ctx, "SELECT hash FROM sprint.meta").Scan(&hash))
	assert.Equal(t, schemaHash, hash)
}

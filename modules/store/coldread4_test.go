package store

// The fourth cold read (the lock read, on abefee69), folded in round 11 from
// nsv2-coldread4/store/coldread4_test.go: adversarial probes of rows (round
// 22: no width or tier in the store), cursors, judgements, the same-value rule, malformed ops and
// Rebuild, run on the Fake and on PostgreSQL with every outcome line (reason
// strings included) equal, under the C collation and under ICU en-US; the
// SQL guards (TRUNCATE, policy versions) and Migrate's foreign-schema check.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cr4apply is an op's outcome as one comparable line.
func cr4apply(h *harness, o Op) string {
	r, err := h.s.Apply(h.ctx, o)
	switch {
	case errors.Is(err, ErrInvalid):
		return "invalid: " + err.Error()
	case err != nil:
		return "error: " + err.Error()
	case r.Accepted:
		return "ok"
	}
	return "refused: " + r.Reason
}

// cr4Scenario runs every probe against one store and returns the outcomes in
// order, "label => outcome". Consumer B1 sorts before a1 in byte order and
// after it under ICU en-US (round 22 removed the narrowing reasons that
// named the lowest; the ids are kept so the collation runs stay comparable).
func cr4Scenario(h *harness) []string {
	var out []string
	add := func(label string, o Op) { out = append(out, label+" => "+cr4apply(h, o)) }
	w := func(label string, expect []Expect, ws ...Write) { add(label, h.op("x", expect, ws...)) }

	h.seed()
	w("setup", nil, PutRow{Name: "r2"},
		PutCard{Tier: "heavy", Kind: "producer", ID: "ph", Stream: "s1"},
		PutCard{Tier: "heavy", Kind: "work", ID: "a1", Of: "ph", Stream: "s1", Holder: "r2"},
		PutCard{Tier: "heavy", Kind: "read", ID: "B1", Of: "ph", Stream: "s1", Holder: "r2"},
		SetCardState{"a1", "ready"}, SetCardState{"B1", "ready"})

	// round 22: width and tiers are the roster's, so the store takes any
	// number of working consumers of any tier on a row that exists
	w("B1 working", nil, SetCardState{"B1", "working"})
	w("a1 working beside it", nil, SetCardState{"a1", "working"})
	w("a1 same state", nil, SetCardState{"a1", "working"})
	w("put row keeps them working", nil, PutRow{Name: "r2", Session: "s2", Role: "worker"})
	w("r3 and a heavy consumer working on it", nil, PutRow{Name: "r3"},
		PutCard{Tier: "heavy", Kind: "work", ID: "w3", Of: "ph", Stream: "s1", Holder: "r3"}, SetCardState{"w3", "working"})
	w("consumer on a row that does not exist", nil, PutCard{Tier: "heavy", Kind: "work", ID: "w4", Of: "ph", Stream: "s1", Holder: "r9"})

	// cursors
	last := h.snap().Seq
	w("cursor past the log", nil, SetCursor{"feed", last + 1})
	last = h.snap().Seq
	w("cursor at the log", nil, SetCursor{"feed", last})
	w("cursor back", nil, SetCursor{"feed", last - 1})
	w("cursor same", nil, SetCursor{"feed", last})
	w("cursor negative", nil, SetCursor{"feed2", -1})
	w("two cursor writes", nil, SetCursor{"feed3", 1}, SetCursor{"feed3", 2})

	// judgements
	open := h.op("x", nil, PutJudgement{ID: "j1", Kind: "red", Question: "q"})
	add("open j1", open)
	early := h.op("x", nil, AnswerJudgement{ID: "j1", Answer: "y"})
	early.At = open.At.Add(-time.Microsecond)
	add("answer j1 before it opened", early)
	open0 := h.op("x", nil, PutJudgement{ID: "j0", Kind: "red"})
	add("open j0", open0)
	sub := h.op("x", nil, AnswerJudgement{ID: "j0", Answer: "y"})
	sub.At = open0.At.Add(-time.Nanosecond) // the same microsecond as opened_at once truncated
	add("answer j0 a nanosecond before it opened", sub)
	w("open and answer in one op", nil, PutJudgement{ID: "j2", Kind: "red"}, AnswerJudgement{ID: "j2"})
	w("answer and expire in one op", nil, AnswerJudgement{ID: "j1"}, ExpireJudgement{ID: "j1"})
	w("answer j1", nil, AnswerJudgement{ID: "j1", Answer: "yes"})
	w("answer j1 again", nil, AnswerJudgement{ID: "j1", Answer: "no"})
	w("expire j1 after answer", nil, ExpireJudgement{ID: "j1", Reason: "late"})
	w("reopen j1", nil, PutJudgement{ID: "j1", Kind: "red"})
	w("answer a judgement that does not exist", nil, AnswerJudgement{ID: "j9"})

	// malformed and same-value
	w("write to a state outside the set", nil, SetCardState{"u1", "flying"})
	w("condition on a state outside the set", []Expect{CardState{"u1", "flying"}})
	w("row to the state it is in", nil, SetRowState{"w1", "up"})
	w("stream held to what it is", nil, SetStreamHeld{"s1", false})
	w("policy", nil, PutPolicy{"data", "p", ""}, SetActivePolicy{1})
	w("activate the active policy", nil, SetActivePolicy{1})
	w("empty card id in a condition", []Expect{CardGen{"", 0}})
	w("empty card id in a write", nil, SetCardReason{"", "r"})

	// a clear: cursors kept, judgements and cards left behind
	c := h.op("clear", nil)
	r, err := h.s.Clear(h.ctx, c)
	out = append(out, fmt.Sprintf("clear => %v %v %d", err, r.Accepted, r.NewEpoch))
	h.epoch = r.NewEpoch
	w("cursor back after a clear", nil, SetCursor{"feed", 0})
	w("cursor kept after a clear", nil, SetCursor{"feed", last})
	w("answer j1 in the new epoch", nil, AnswerJudgement{ID: "j1"})
	w("reopen j1 in the new epoch", nil, PutJudgement{ID: "j1", Kind: "red"})
	return out
}

// The outcomes the contract asks for (docs/LAYER-1-STORE.md, store/README.md),
// as prefixes; round 11 changed the malformed and same-value lines.
var cr4Want = map[string]string{
	"B1 working":                             "ok",
	"a1 working beside it":                   "ok",
	"a1 same state":                          "refused: card a1 is already working",
	"put row keeps them working":             "ok",
	"r3 and a heavy consumer working on it":  "ok",
	"consumer on a row that does not exist":  "refused: row r9 does not exist",
	"cursor past the log":                    "refused: cursor feed cannot pass the log at ",
	"cursor at the log":                      "ok",
	"cursor back":                            "refused: cursor feed is at ",
	"cursor same":                            "refused: cursor feed is already at ",
	"cursor negative":                        "invalid: ",
	"two cursor writes":                      "invalid: ",
	"open j1":                                "ok",
	"answer j1 before it opened":             "refused: judgement j1 opened at ",
	"open and answer in one op":              "invalid: ",
	"answer and expire in one op":            "invalid: ",
	"answer j1":                              "ok",
	"answer j1 again":                        "refused: judgement j1 is answered; only an open judgement can be answered",
	"expire j1 after answer":                 "refused: judgement j1 is answered; only an open judgement can be expired",
	"reopen j1":                              "refused: judgement j1 exists",
	"answer a judgement that does not exist": "refused: judgement j9 does not exist",
	"write to a state outside the set":       `invalid: store: invalid op: op x-`,
	"condition on a state outside the set":   "invalid: ",
	"row to the state it is in":              "refused: row w1 is already up",
	"stream held to what it is":              "refused: stream s1 is already not held",
	"policy":                                 "ok",
	"activate the active policy":             "refused: policy version 1 is already active",
	"empty card id in a condition":           "invalid: ",
	"empty card id in a write":               "invalid: ",
	"cursor back after a clear":              "refused: cursor feed is at ",
	"cursor kept after a clear":              "refused: cursor feed is already at ",
	"answer j1 in the new epoch":             "refused: judgement j1 does not exist",
	"reopen j1 in the new epoch":             "ok",
}

func cr4Check(t *testing.T, h *harness, out []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, line := range out {
		label, got, _ := strings.Cut(line, " => ")
		if want, ok := cr4Want[label]; ok {
			seen[label] = true
			assert.True(t, strings.HasPrefix(got, want), "%s:\n got %s\nwant %s...", label, got, want)
		}
		assert.NotContains(t, got, "  ", "%s: a reason that names nothing", label)
	}
	for label := range cr4Want {
		assert.True(t, seen[label], "probe %s did not run", label)
	}
	all, err := h.s.Events(h.ctx, 0, 0)
	require.NoError(t, err)
	require.Less(t, len(all), MaxEvents)
	assert.Equal(t, h.snap(), Rebuild(all), "Rebuild(every event) == Snapshot")
	assert.Equal(t, int64(len(all)), h.snap().Seq)
}

func fakeHarness(t *testing.T) *harness {
	return &harness{t: t, s: NewFake(), ctx: context.Background(), epoch: 1}
}

func TestColdRead4Fake(t *testing.T) {
	t.Parallel()
	h := fakeHarness(t)
	cr4Check(t, h, cr4Scenario(h))
}

// The same probes against PostgreSQL: every outcome line, reason strings
// included, equals Fake's.
func TestFunctionalColdRead4Parity(t *testing.T) {
	t.Parallel()
	fake := cr4Scenario(fakeHarness(t))
	h, _, _ := pgHarness(t)
	pg := cr4Scenario(h)
	cr4Check(t, h, pg)
	assert.Equal(t, fake, pg)
}

// icu is the CREATE DATABASE clause of a database whose default collation is
// ICU en-US, not C: the production PostgreSQL is not initdb --no-locale.
const icu = " TEMPLATE template0 ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'en-US' LOCALE 'C'"

// icuDB creates an ICU en-US database on the server at dsn and returns its
// dsn. It fails, never skips: parity under a non-C collation is a guarantee.
func icuDB(t *testing.T, admin *pgx.Conn, name string) string {
	t.Helper()
	_, err := admin.Exec(context.Background(), "CREATE DATABASE "+name+icu)
	require.NoError(t, err, "this PostgreSQL cannot make an ICU database")
	return "postgres://nova@" + admin.Config().Host + ":" + strconv.Itoa(int(admin.Config().Port)) + "/" + name + "?sslmode=disable"
}

// Every line under ICU en-US equals Fake's (round 22 dropped the
// tier-narrowing reason whose "lowest id" this run was first written for:
// PG ordered it COLLATE "C").
func TestFunctionalColdRead4ParityUnderCollation(t *testing.T) {
	t.Parallel()
	admin := connect(t, startPG(t))
	dsn := icuDB(t, admin, "icu")
	var provider string
	require.NoError(t, admin.QueryRow(context.Background(), "SELECT datlocprovider::text FROM pg_database WHERE datname = 'icu'").Scan(&provider))
	require.Equal(t, "i", provider, "the database collates by ICU")
	fake := cr4Scenario(fakeHarness(t))
	h := &harness{t: t, s: openPG(t, dsn), ctx: context.Background(), epoch: 1}
	pg := cr4Scenario(h)
	cr4Check(t, h, pg)
	assert.Equal(t, fake, pg, "Fake and PG differ in a reason string under a non-C collation")
}

// The whole conformance suite on ICU en-US databases, one per scenario.
func TestFunctionalConformanceICU(t *testing.T) {
	t.Parallel()
	admin := connect(t, startPG(t))
	var mu sync.Mutex
	var n atomic.Int32
	RunConformance(t, func(t *testing.T) Store {
		mu.Lock() // one admin connection, one statement at a time
		dsn := icuDB(t, admin, fmt.Sprintf("icu%d", n.Add(1)))
		mu.Unlock()
		return openPG(t, dsn)
	})
}

// The log and the policy versions refuse TRUNCATE, which fires no row
// trigger (the probe's TestFunctionalColdRead4TruncateLog).
func TestFunctionalTruncateRefused(t *testing.T) {
	t.Parallel()
	h, _, dsn := pgHarness(t)
	h.seed()
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "body"}))
	c := connect(t, dsn)
	for table, constraint := range map[string]string{"events": "events_no_truncate_check", "policy": "policy_no_truncate_check"} {
		_, err := c.Exec(context.Background(), "TRUNCATE sprint."+table)
		var pe *pgconn.PgError
		require.ErrorAs(t, err, &pe, table)
		assert.Equal(t, constraint, pe.ConstraintName)
	}
	var events, policies int
	require.NoError(t, c.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM sprint.events), (SELECT count(*) FROM sprint.policy)").Scan(&events, &policies))
	assert.Equal(t, 2, events)
	assert.Equal(t, 1, policies)
}

// Every column the contract calls fixed is refused by SQL, policy versions
// included (only active may change, none is removed); the probe's
// TestFunctionalColdRead4SQLFixed, its policy lines moved from logged to
// refused. Each statement runs in a transaction that is rolled back.
func TestFunctionalSQLFixed(t *testing.T) {
	t.Parallel()
	h, _, dsn := pgHarness(t)
	h.seed()
	h.ok(h.op("more", nil, PutRow{Name: "w2"}, PutStream{Name: "s2"},
		PutJudgement{ID: "j1", Kind: "red"}, SetCursor{"feed", 1}, PutPolicy{"data", "p", "body"}))
	c := connect(t, dsn)
	try := func(sql string) error {
		tx, err := c.Begin(context.Background())
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }() // ignored: every probe is rolled back
		_, err = tx.Exec(context.Background(), sql)
		return err
	}
	for _, sql := range []string{
		"UPDATE sprint.cards SET id = 'zz' WHERE id = 'c2'",
		"UPDATE sprint.cards SET epoch = 2 WHERE id = 'u3'",
		"UPDATE sprint.cards SET kind = 'sentinel' WHERE id = 'c2'",
		"UPDATE sprint.cards SET kind = 'read' WHERE id = 'u1'",
		`UPDATE sprint.cards SET "of" = 'c2' WHERE id = 'u1'`,
		"UPDATE sprint.cards SET stream = 's2' WHERE id = 'c2'",
		"UPDATE sprint.cards SET holder = 'w2' WHERE id = 'u1'",
		"UPDATE sprint.cards SET tier = 'heavy' WHERE id = 'u1'",
		"UPDATE sprint.cards SET tier = 'heavy' WHERE id = 'c2'",
		"UPDATE sprint.rows SET name = 'w9' WHERE name = 'w2'",
		"UPDATE sprint.rows SET epoch = 3 WHERE name = 'w2'",
		"UPDATE sprint.streams SET name = 's9' WHERE name = 's2'",
		"UPDATE sprint.streams SET epoch = 3 WHERE name = 's2'",
		"UPDATE sprint.judgements SET id = 'j9' WHERE id = 'j1'",
		"UPDATE sprint.judgements SET epoch = 3 WHERE id = 'j1'",
		"UPDATE sprint.judgements SET closed_at = opened_at - interval '1 second', state = 'answered' WHERE id = 'j1'",
		"UPDATE sprint.events SET reason = 'x' WHERE seq = 1",
		"DELETE FROM sprint.events WHERE seq = 1",
		"TRUNCATE sprint.events",
		"UPDATE sprint.cursors SET seq = -1",
		"UPDATE sprint.cards SET gen = -1 WHERE id = 'c2'",
		"UPDATE sprint.policy SET body = 'other rules' WHERE version = 1",
		"UPDATE sprint.policy SET version = 7 WHERE version = 1",
		"UPDATE sprint.policy SET kind = 'script', name = 'q', actor = 'x', at = now() WHERE version = 1",
		"DELETE FROM sprint.policy WHERE version = 1",
		"TRUNCATE sprint.policy",
	} {
		assert.Error(t, try(sql), "fixed by the contract, yet accepted: %s", sql)
	}
	assert.NoError(t, try("UPDATE sprint.policy SET active = true WHERE version = 1"), "active is the one policy column that changes")
}

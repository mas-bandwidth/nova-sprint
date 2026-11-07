package store

// Targeted checks of the store's guards: sets in canonical order, empty keys
// and negative integers in conditions invalid, priority "" is normal, the
// same-value wording "already has the given". Collation of every ordered
// reason, the same-value refusal of every set write, the out-of-set and
// empty-key rule, the SQL guards, the schema hash, Migrate's foreign-schema
// check, and Rebuild == Snapshot, on the Fake, on PostgreSQL (C) and on
// PostgreSQL (ICU en-US), every outcome line equal across the three.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardScenario runs every case against one store, "label => outcome".
func guardScenario(h *harness) []string {
	var out []string
	add := func(label string, o Op) { out = append(out, label+" => "+parityApply(h, o)) }
	w := func(label string, expect []Expect, ws ...Write) { add(label, h.op("x", expect, ws...)) }

	h.seed()
	// Five live heavy consumers whose byte order (B1 Z9 a1 z1 é1) and ICU
	// en-US order (a1 B1 é1 z1 Z9) differ at every step.
	ids := []string{"a1", "B1", "Z9", "z1", "é1"}
	setup := []Write{PutRow{Name: "r2"}, PutCard{Tier: "heavy", Kind: "producer", ID: "ph", Stream: "s1"}}
	for _, id := range ids {
		setup = append(setup, PutCard{Tier: "heavy", Kind: "read", ID: id, Of: "ph", Stream: "s1", Holder: "r2"}, SetCardState{id, "ready"})
	}
	w("setup", nil, setup...)
	for _, id := range []string{"B1", "Z9", "a1", "z1", "é1"} {
		w("retire "+id, nil, SetCardState{id, "done"})
	}

	// same-value: a set compared as a sequence
	w("needs two", nil, SetCardNeeds{"c2", []string{"c0", "c1"}})
	w("needs reordered", nil, SetCardNeeds{"c2", []string{"c1", "c0"}})
	w("after two", nil, SetCardAfter{"c2", []string{"c0", "c1"}})
	w("after reordered", nil, SetCardAfter{"c2", []string{"c1", "c0"}})
	// same-value: the rest of the set writes
	w("cost priced only", nil, SetCardCost{"u1", 0, true})
	w("cost same", nil, SetCardCost{"u1", 0, true})
	w("cost unpriced again", nil, SetCardCost{"u1", 0, false})
	w("paths nil on nil", nil, SetCardPaths{"c1", nil})
	w("paths empty on nil", nil, SetCardPaths{"c1", []string{}})
	w("paths empty string", nil, SetCardPaths{"c1", []string{""}})
	w("priority same", nil, SetCardPriority{"c1", "normal"})
	w("priority normal on a consumer", nil, SetCardPriority{"u1", "normal"})
	w("priority empty", nil, SetCardPriority{"c1", ""})
	w("result empty on a producer", nil, SetCardResult{"c1", ""})
	w("result same", nil, SetCardResult{"u1", ""})
	w("held same", nil, SetRowHeld{"w1", false, ""})
	w("flaps same", nil, SetRowFlaps{"w1", 0})
	w("score same", nil, SetCardScore{"c1", 10})
	w("attempt same", nil, SetCardAttempt{"c1", 0})
	w("state same after a change in one op", nil, SetCardReason{"c1", "r"}, SetCardState{"c1", "waiting"})
	w("reason same after add in one op", nil, PutCard{Tier: "pro", Kind: "producer", ID: "p9", Stream: "s1"}, SetCardReason{"p9", ""})
	w("open j1", nil, PutJudgement{ID: "j1", Kind: "red"})
	w("answer j1", nil, AnswerJudgement{ID: "j1", Answer: "yes"})
	w("answer j1 the same again", nil, AnswerJudgement{ID: "j1", Answer: "yes"})

	// out of set and empty keys
	w("put card with no stream", nil, PutCard{Tier: "pro", Kind: "producer", ID: "p8"})
	w("put card with an empty need", nil, PutCard{Tier: "pro", Kind: "producer", ID: "p7", Stream: "s1", Needs: []string{""}})
	w("put card with an empty after", nil, PutCard{Tier: "pro", Kind: "producer", ID: "p6", Stream: "s1", After: []string{""}})
	w("set needs to an empty name", nil, SetCardNeeds{"c2", []string{""}})
	w("set after to an empty name", nil, SetCardAfter{"c2", []string{""}})
	w("consumer of no producer", nil, PutCard{Tier: "pro", Kind: "work", ID: "u8", Stream: "s1", Holder: "w1"})
	w("consumer with no holder", nil, PutCard{Tier: "pro", Kind: "work", ID: "u8", Of: "c1", Stream: "s1"})
	w("gen condition below 0", []Expect{CardGen{"c1", -1}})
	w("row gen condition below 0", []Expect{RowGen{"w1", -1}})
	w("seq condition below 0", []Expect{AtSeq{-1}})
	w("activate version 0", nil, SetActivePolicy{0})

	// a clear, then the cursors and the new epoch
	c := h.op("clear", nil)
	r, err := h.s.Clear(h.ctx, c)
	out = append(out, fmt.Sprintf("clear => %v %v %d", err, r.Accepted, r.NewEpoch))
	h.epoch = r.NewEpoch
	return out
}

// What the contract asks for (AGENTS.md, Store: a set write that would change
// nothing is refused; an empty key is ErrInvalid in a condition exactly as in a
// write), as prefixes.
var guardWant = map[string]string{
	"needs reordered":                     "refused: card c2 already has the given needs",
	"after reordered":                     "refused: card c2 already has the given after",
	"cost priced only":                    "ok",
	"cost same":                           "refused: card u1 already has the given cost",
	"cost unpriced again":                 "ok",
	"paths nil on nil":                    "refused: card c1 already has the given paths",
	"paths empty on nil":                  "refused: card c1 already has the given paths",
	"paths empty string":                  "ok",
	"priority same":                       "refused: card c1 already has the given priority",
	"priority normal on a consumer":       "refused: card u1 is of kind work; priority is a producer's",
	"priority empty":                      "refused: card c1 already has the given priority", // "" is normal
	"result empty on a producer":          "refused: card c1 is of kind producer; SetCardResult is only for a consumer",
	"result same":                         "refused: card u1 already has the given result",
	"held same":                           "refused: row w1 is already not held",
	"score same":                          "refused: card c1 already has the given score",
	"attempt same":                        "refused: card c1 already has the given attempt",
	"answer j1 the same again":            "refused: judgement j1 is answered; only an open judgement can be answered",
	"put card with no stream":             "invalid: ",
	"put card with an empty need":         "invalid: ",
	"put card with an empty after":        "invalid: ",
	"set needs to an empty name":          "invalid: ",
	"set after to an empty name":          "invalid: ",
	"consumer of no producer":             "refused: card u8 is a consumer; a consumer names its producer in of",
	"consumer with no holder":             "refused: card u8 is a consumer; a consumer names its holder row at add, and it never changes",
	"reason same after add in one op":     "refused: card p9 already has the given reason",
	"state same after a change in one op": "refused: card c1 is already waiting",
	"open j1":                             "ok",
	"answer j1":                           "ok",
	"needs two":                           "ok",
	"after two":                           "ok",
	"flaps same":                          "refused: row w1 already has the given flaps",
	"retire B1":                           "ok",
	"retire é1":                           "ok",
	"setup":                               "ok",
	"activate version 0":                  "refused: policy version 0 does not exist",
	"gen condition below 0":               "invalid: ", // range-checked like writes
	"row gen condition below 0":           "invalid: ",
	"seq condition below 0":               "invalid: ",
}

func guardCheck(t *testing.T, h *harness, out []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, line := range out {
		label, got, _ := strings.Cut(line, " => ")
		if want, ok := guardWant[label]; ok {
			seen[label] = true
			assert.True(t, strings.HasPrefix(got, want), "%s:\n got %s\nwant %s...", label, got, want)
		}
		assert.NotContains(t, got, "  ", "%s: a reason that names nothing: %s", label, got)
	}
	for label := range guardWant {
		assert.True(t, seen[label], "case %s did not run", label)
	}
	all, err := h.s.Events(h.ctx, 0, 0)
	require.NoError(t, err)
	require.Less(t, len(all), MaxEvents)
	assert.Equal(t, h.snap(), Rebuild(all), "Rebuild(every event) == Snapshot")
	assert.Equal(t, int64(len(all)), h.snap().Seq)
}

func TestGuardsFake(t *testing.T) {
	t.Parallel()
	h := fakeHarness(t)
	out := guardScenario(h)
	for _, l := range out {
		t.Log(l)
	}
	guardCheck(t, h, out)
}

func TestFunctionalGuardsParity(t *testing.T) {
	t.Parallel()
	fake := guardScenario(fakeHarness(t))
	h, _, _ := pgHarness(t)
	pg := guardScenario(h)
	guardCheck(t, h, pg)
	assert.Equal(t, fake, pg, "Fake and PG differ")
}

func TestFunctionalGuardsParityICU(t *testing.T) {
	t.Parallel()
	admin := connect(t, startPG(t))
	dsn := icuDB(t, admin, "icu5")
	fake := guardScenario(fakeHarness(t))
	h := &harness{t: t, s: openPG(t, dsn), ctx: context.Background(), epoch: 1}
	pg := guardScenario(h)
	guardCheck(t, h, pg)
	assert.Equal(t, fake, pg, "Fake and PG differ under ICU en-US")
}

// A comment edit never changes the hash; a change to a literal does, except for indentation inside a literal that spans lines, which the scanner strips (see hashSchema).
func TestSchemaHashIgnoresComments(t *testing.T) {
	t.Parallel()
	// a -- inside a literal that spans lines is hashed
	assert.NotEqual(t, hashSchema("SELECT 'a\n-- b';\n"), hashSchema("SELECT 'a\n-- c';\n"), "a -- on the second line of a literal")
	// a -- inside a literal in a function body is hashed
	assert.NotEqual(t, hashSchema("CREATE FUNCTION f() RETURNS text LANGUAGE sql AS $$ SELECT '--x' $$;\n"),
		hashSchema("CREATE FUNCTION f() RETURNS text LANGUAGE sql AS $$ SELECT '--y' $$;\n"))
	// a comment after a literal holding -- is stripped
	assert.Equal(t, hashSchema("SELECT '--'; -- one\n"), hashSchema("SELECT '--'; -- two\n"))
	// a comment after a literal holding a doubled quote and -- is stripped
	assert.Equal(t, hashSchema("SELECT 'it''s -- not'; -- one\n"), hashSchema("SELECT 'it''s -- not'; -- two\n"))
	// a quote inside a double-quoted identifier opens no literal
	assert.Equal(t, hashSchema("SELECT 1 AS \"it's\"; -- one\nSELECT 2;\n"), hashSchema("SELECT 1 AS \"it's\"; -- two\nSELECT 2;\n"),
		"a ' inside a quoted identifier toggles nothing")
	// the stated limit: a dollar quote is scanned as code, since the schema's
	// are all function bodies, so a -- inside a $$ string literal is stripped
	assert.Equal(t, hashSchema("COMMENT ON SCHEMA sprint IS $$a -- b$$;\n"), hashSchema("COMMENT ON SCHEMA sprint IS $$a -- c$$;\n"),
		"the limit hashSchema's doc states (the schema has no $$ string literal)")
}

// TRUNCATE in every spelling that reaches the log or the policy versions is
// refused; policy_fixed lets active change and nothing else.
func TestFunctionalSQLGuards(t *testing.T) {
	t.Parallel()
	h, _, dsn := pgHarness(t)
	h.seed()
	h.ok(h.op("pol", nil, PutPolicy{"data", "p", "body"}, PutPolicy{"script", "q", "body2"}))
	c := connect(t, dsn)
	try := func(sql string) error {
		tx, err := c.Begin(context.Background())
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }() // ignored: every case is rolled back
		_, err = tx.Exec(context.Background(), sql)
		return err
	}
	for _, sql := range []string{
		"TRUNCATE ONLY sprint.events",
		"TRUNCATE sprint.events RESTART IDENTITY",
		"TRUNCATE sprint.streams, sprint.events CASCADE",
		"TRUNCATE sprint.policy RESTART IDENTITY CASCADE",
		"TRUNCATE TABLE sprint.cursors, sprint.policy",
		"UPDATE sprint.policy SET at = at + interval '1 microsecond' WHERE version = 1",
		"UPDATE sprint.policy SET actor = actor || '' , name = 'p2' WHERE version = 1",
		"UPDATE sprint.policy SET version = 3 WHERE version = 2",
		"UPDATE sprint.policy SET body = body || ' ' WHERE version = 2",
		"DELETE FROM sprint.policy WHERE NOT active",
	} {
		err := try(sql)
		var pe *pgconn.PgError
		if assert.ErrorAs(t, err, &pe, "accepted: %s", sql) {
			assert.Equal(t, "23514", pe.Code, sql)
		}
	}
	for _, sql := range []string{
		"UPDATE sprint.policy SET active = true WHERE version = 2",
		"UPDATE sprint.policy SET name = name, body = body, at = at WHERE version = 1",
	} {
		assert.NoError(t, try(sql), sql)
	}
}

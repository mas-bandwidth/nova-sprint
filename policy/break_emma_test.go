package policy

// Break-It campaign probes by Emma (2026-11-06) against policy/ at ed68a22d.
// Each TestBreak* fails on ed68a22d and names the exact sequence and design
// rule it breaks. TestHeld* records the adversarial attacks that held.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestBreakVerdictCountsDoneReadsFromDifferentHead breaks inputs.go:62 against
// docs/LAYER-5-POLICY.md lines 58-59 & 94:
//   - "verdict(card, reads) | the producer; its done reads of this head and review"
//   - "verdict: 'yes' or 'fix' or 'retry'; a yes needs at least one done ok read
//     of this review and head | a yes on zero yes lands unread code"
//
// Sequence:
//  1. Producer p5 is in state "review" at Review=1, Head="head-new".
//  2. Read p5-r-stale is a done "ok" read at Review=1, Head="head-old" (a stale head).
//  3. Read p5-r-curr is a done "fault" read at Review=1, Head="head-new" (the current head).
//  4. newIndex (inputs.go:62) filters ix.reads[c.Of] only by c.Kind=="read" &&
//     c.State=="done" && c.Review==p.Review, never checking c.Head == p.Head.
//  5. Both default.star and checkVerdict count p5-r-stale ("ok" on "head-old")
//     and return verdict("p5") == "yes", landing "head-new" with zero "ok" reads!
func TestBreakVerdictCountsDoneReadsFromDifferentHead(t *testing.T) {
	t.Parallel()
	snap := store.Snapshot{
		Epoch:   1,
		Streams: map[string]store.Stream{"s1": {Name: "s1"}},
		Rows: map[string]store.Row{
			"r1": {Name: "r1", Kind: "worker", Width: 2, State: "up", Tiers: []string{"pro"}},
			"r4": {Name: "r4", Kind: "kin", Width: 2, State: "up", Tiers: []string{"pro"}},
		},
		Cards: map[string]store.Card{
			"p5": {
				ID: "p5", Kind: "producer", Stream: "s1", State: "review",
				Tier: "pro", Priority: "normal", Author: "r1",
				Review: 1, Head: "head-new",
			},
			"p5-r-stale": {
				ID: "p5-r-stale", Kind: "read", Of: "p5", Stream: "s1",
				Tier: "pro", Holder: "r4", State: "done",
				Review: 1, Head: "head-old", Result: "ok",
			},
			"p5-r-curr": {
				ID: "p5-r-curr", Kind: "read", Of: "p5", Stream: "s1",
				Tier: "pro", Holder: "r4", State: "done",
				Review: 1, Head: "head-new", Result: "fault",
			},
		},
	}
	a, fs := NewEngine().Answer(Versions{}, snap, FullQuestions(snap, nil))
	require.Empty(t, fs)
	assert.NotEqual(t, "yes", a.Verdict["p5"],
		"head-new has zero ok reads (only a fault read); a stale ok read from head-old must not let verdict answer 'yes'")
}

// TestBreakDefaultRowsDropsBusyCandidatesAndFailsJointCheck breaks default.star:40-41
// against docs/LAYER-5-POLICY.md lines 92, 521 & Lesson 5.5:
//   - "rows(card, kind, candidates) | every candidate listed: the wanted rank and up,
//     nearest first, then the ranks between the tier and the wanted one, highest first;
//     those with room first, all of them when none has room (ruled 2026-11-06, so the
//     joint check passes whenever any row could take the card)"
//   - "The default's rows lists every serving row as the question table now says,
//     so the joint check passes whenever any row has work > 0."
//
// default.star:40-41 wrote:
//   use = [r for r in up + between if r.working < r.width]
//   return [r.name for r in (use or up + between)]
// Whenever ANY candidate has r.working < r.width, `use` is non-empty and `use or up + between`
// drops every candidate with r.working >= r.width instead of ordering `use` first!
func TestBreakDefaultRowsDropsBusyCandidatesAndFailsJointCheck(t *testing.T) {
	t.Parallel()

	t.Run("gates_only_row_with_room_causes_default_rows_to_drop_busy_work_row_and_fail_joint_check", func(t *testing.T) {
		t.Parallel()
		// r1 (Width: 1) has 1 working card (r1.working == 1 == r1.width) and width(r1) = {work: 1, ahead: 1}.
		// r2 (Width: 1) has 0 working cards (r2.working == 0 < r2.width) and is a gates-only row
		// with width(r2) = {work: 0, ahead: 0} per Lesson 5.5.
		// Because r2.working < r2.width, default.star's rows() returns ONLY ["r2"] and drops "r1",
		// which then fails the joint check: "every row named has work 0 in width, while row r1 could take it".
		snap := store.Snapshot{
			Epoch:   1,
			Streams: map[string]store.Stream{"s1": {Name: "s1"}},
			Rows: map[string]store.Row{
				"r1": {Name: "r1", Kind: "worker", Width: 1, State: "up", Tiers: []string{"pro"}},
				"r2": {Name: "r2", Kind: "worker", Width: 1, State: "up", Tiers: []string{"pro"}},
			},
			Cards: map[string]store.Card{
				"p0":    {ID: "p0", Kind: "producer", Stream: "s1", State: "working", Tier: "pro", Priority: "normal"},
				"p0-w1": {ID: "p0-w1", Kind: "work", Of: "p0", Stream: "s1", State: "working", Tier: "pro", Holder: "r1"},
				"p1":    {ID: "p1", Kind: "producer", Stream: "s1", State: "ready", Tier: "pro", Priority: "normal", Score: 10},
			},
		}
		body, _ := override("width", "def width(row):\n    if row.name == \"r2\":\n        return {\"work\": 0, \"ahead\": 0}\n    return {\"work\": row.width, \"ahead\": row.width}")
		a, fs := NewEngine().Answer(Versions{Active: pv(2, body)}, snap, FullQuestions(snap, nil))
		assert.Empty(t, fs, "default.star's rows() must list every candidate (those with room first, then busy ones) so joint check passes when r1 has work > 0")
		assert.Equal(t, 2, a.Version)
	})

	t.Run("default_rows_omits_busy_candidate_needed_for_second_read_or_ahead_queue", func(t *testing.T) {
		t.Parallel()
		// Two kin rows r3 (Width: 1, holding 1 working read so r3.working == 1) and
		// r4 (Width: 1, r4.working == 0) are both up candidates for p5's read.
		// Design line 92 requires every candidate listed ("those with room first"): ["r4", "r3"].
		// default.star returns only ["r4"], dropping "r3" completely.
		snap := store.Snapshot{
			Epoch:   1,
			Streams: map[string]store.Stream{"s1": {Name: "s1"}},
			Rows: map[string]store.Row{
				"r1": {Name: "r1", Kind: "worker", Width: 2, State: "up", Tiers: []string{"pro"}},
				"r3": {Name: "r3", Kind: "kin", Width: 1, State: "up", Tiers: []string{"pro"}},
				"r4": {Name: "r4", Kind: "kin", Width: 1, State: "up", Tiers: []string{"pro"}},
			},
			Cards: map[string]store.Card{
				"p0":    {ID: "p0", Kind: "producer", Stream: "s1", State: "review", Tier: "pro", Priority: "normal", Author: "r1"},
				"p0-r1": {ID: "p0-r1", Kind: "read", Of: "p0", Stream: "s1", State: "working", Tier: "pro", Holder: "r3"},
				"p5":    {ID: "p5", Kind: "producer", Stream: "s1", State: "review", Tier: "pro", Priority: "normal", Author: "r1"},
			},
		}
		a, fs := NewEngine().Answer(Versions{}, snap, FullQuestions(snap, nil))
		require.Empty(t, fs)
		assert.Equal(t, []string{"r4", "r3"}, a.Rows[RowsQuestion{"p5", KindRead}],
			"every candidate must be listed with those with room first: [r4, r3]")
	})
}

// TestBreakCheckReportsRefusedJointAnswerAsValidChangeAndCascadesFromRefusedWidth
// breaks answer.go:103-111 and show.go:108-111 against docs/LAYER-5-POLICY.md
// line 256 & Guarantee 2:
//   - "policy check <file | v> | prints every answer that would differ from the
//     active version's, by question and subject, then every refusal"
//   - "An answer out of domain is refused, never used."
//
// Sequence 1: A candidate policy returns rows(p1) = ["r5"] where r5 has width 0.
// answer.go:103 writes a.shown["rows(p1 work)"] = `["r5"]` BEFORE k.joint(&a)
// refuses it at lines 105-111, and never deletes a.shown on refusal! Check then
// prints `rows(p1 work): ["r1"] -> ["r5"]` as a valid answer change AND as a refusal.
//
// Sequence 2: A candidate policy has an out-of-domain width(row) = {"work": -1, "ahead": 0}
// while rows(card, kind, candidates) is completely untouched. Because width() is refused,
// a.Width is empty, and in Check (all=true) k.joint(&a) cascades bogus rows() refusals
// for every ready card in the sprint!
func TestBreakCheckReportsRefusedJointAnswerAsValidChangeAndCascadesFromRefusedWidth(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up()
	w.apply(
		store.PutRow{Name: "r5", Kind: "kin", Width: 0, Tiers: []string{"pro", "heavy", "frontier"}, Model: "m-frontier"},
		store.SetRowState{Row: "r5", State: "up"},
	)

	// Sequence 1: rows() returns only r5 (width 0), which fails the joint check.
	onlyZeroWidth, _ := override("rows", "def rows(card, kind, candidates):\n    return [r.name for r in candidates if r.name == \"r5\"] or [r.name for r in candidates]")
	rep, err := Check(w.ctx(), w.st, NewEngine(), w.file("zero-row.star", onlyZeroWidth))
	require.NoError(t, err)
	require.NotEmpty(t, rep.Refusals, "joint check must refuse rows naming only r5")
	for _, ch := range rep.Changes {
		assert.False(t, strings.HasPrefix(ch, "rows(p1 work):") && !strings.HasSuffix(ch, "-> (refused)"),
			"a refused rows(p1 work) answer must not be reported as a valid value change in Check: %q", ch)
	}

	// Sequence 2: width() is out of domain (-1), while rows() is the untouched default.
	badWidth, _ := override("width", "def width(row):\n    return {\"work\": -1, \"ahead\": 0}")
	rep2, err := Check(w.ctx(), w.st, NewEngine(), w.file("bad-width.star", badWidth))
	require.NoError(t, err)
	for _, ref := range rep2.Refusals {
		assert.False(t, strings.Contains(ref, "rows("),
			"an out-of-domain width() must not cascade bogus rows() joint-check refusals when rows() itself is valid: %q", ref)
	}
}

// TestBreakDataPolicyRowBlocksUpAndCompilesInCheckShowDiff breaks verbs.go:52
// and show.go:46-54,117,178 against docs/LAYER-5-POLICY.md lines 22, 253, 398
// and Guarantee 3 ("From up on, sprint.current.policy names an active version").
//
// Sequence 1: A policy row of kind "data" (e.g. stream:<name> JSON policy data)
// is stored at version 1 before Up. Up (verbs.go:52) checks only whether
// st.Policy(ctx, 1) returns ErrNotFound; finding v1 (kind "data"), Up returns
// without storing or activating default.star, leaving snap.Policy == 0 (no active
// policy after Up!).
//
// Sequence 2: After Up, a policy row of kind "data" is stored at v2. Unlike
// Activate and ReadCatalog, Check(ctx, st, e, "v2") never checks pv.Kind == "script"
// and compiles the JSON data payload as Starlark, caching the compile failure in
// Engine.progs[2].
func TestBreakDataPolicyRowBlocksUpAndCompilesInCheckShowDiff(t *testing.T) {
	t.Parallel()

	t.Run("data_row_at_v1_leaves_no_active_policy_after_up", func(t *testing.T) {
		t.Parallel()
		w := fakeWorld(t)
		w.apply(store.PutPolicy{Kind: "data", Name: "stream:s1", Body: `{"repo":"git@example.com:r.git","base":"main","pr":false,"mode":"flowing"}`})
		_, err := Up(w.ctx(), w.st, w.caller())
		require.NoError(t, err)
		assert.Greater(t, w.snap().Policy, 0,
			"Guarantee 3: after Up, sprint.current.policy must name an active script version even if a data row was stored first")
	})

	t.Run("check_compiles_data_policy_version_as_starlark", func(t *testing.T) {
		t.Parallel()
		w := fakeWorld(t)
		w.up()
		w.apply(store.PutPolicy{Kind: "data", Name: "stream:s1", Body: `{"repo":"git@example.com:r.git","base":"main","pr":false,"mode":"flowing"}`})
		e := NewEngine()
		_, err := Check(w.ctx(), w.st, e, "v2")
		var r *Refusal
		require.ErrorAs(t, err, &r, "Check on a kind='data' version must refuse with *Refusal (like Activate and ReadCatalog), not compile data as Starlark")
		assert.Contains(t, r.Error(), "kind data")
	})
}

// interceptStore injects a concurrent policy activation right after LoadVersions
// takes its initial Snapshot, before LoadVersions walks the event log.
type interceptStore struct {
	store.Store
	afterSnapshot func()
}

func (s *interceptStore) Snapshot(ctx context.Context) (store.Snapshot, error) {
	snap, err := s.Store.Snapshot(ctx)
	if s.afterSnapshot != nil {
		fn := s.afterSnapshot
		s.afterSnapshot = nil
		fn()
	}
	return snap, err
}

// TestBreakLoadVersionsWalksPastSnapshotSeqCollapsingPrevIntoActive breaks
// verbs.go:208-231 against docs/LAYER-5-POLICY.md lines 140 & 229:
//   - "Three versions exist at most in a tick's fallback order: 1. Active ...
//     2. Previous active — the version that was active immediately before the
//     active one was activated"
//
// Sequence:
//  1. v1 (default) is active; v2 (broken on s3) is activated at seq S.
//  2. LoadVersions reads Snapshot at seq S (where snap.Policy == 2, prev was 1).
//  3. Before LoadVersions's event-log walk finishes, v3 is activated at seq S+1
//     (whose event has PrevOf == 2).
//  4. Because walk() in LoadVersions does not stop at e.Seq <= snap.Seq, it
//     reads seq S+1 and overwrites prev = 2 (the same as snap.Policy == 2!).
//  5. vs.tried() sees Prev.Version == Active.Version (2 == 2) and drops Prev,
//     so when v2 fails during the tick, the tick skips v1 and falls all the way
//     back to built-in 0!
func TestBreakLoadVersionsWalksPastSnapshotSeqCollapsingPrevIntoActive(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	w.up() // v1 active (reads=1)

	// v2 sets reads=2 (clean on current world).
	v2Body := strings.Replace(defaultBody, "return 1  #", "return 2  #", 1)
	v2 := w.set("v2.star", v2Body)
	_, err := Activate(w.ctx(), w.st, NewEngine(), w.caller(), v2, "activate v2")
	require.NoError(t, err)

	// v3 is active now; previous active is v2.
	v3Body, _ := override("landing", "def landing(stream):\n    return {\"backed_up\": 4 // (0 if stream.name == \"s3\" else 1), \"flowing\": 1}")
	v3 := w.set("v3.star", v3Body)
	_, err = Activate(w.ctx(), w.st, NewEngine(), w.caller(), v3, "activate v3")
	require.NoError(t, err)

	// Prepare v4 in the store so it can be activated right after LoadVersions's Snapshot().
	v4Body := strings.Replace(defaultBody, "return 1  #", "return 3  #", 1)
	v4 := w.set("v4.star", v4Body)

	st := &interceptStore{
		Store: w.st,
		afterSnapshot: func() {
			id, at := w.next()
			_, err := w.st.Apply(w.ctx(), store.Op{
				ID: id, Epoch: 1, Actor: "rowan", Kind: "policy-activate", At: at,
				Writes: []store.Write{store.SetActivePolicy{Version: v4}},
			})
			require.NoError(t, err)
		},
	}

	vs, err := LoadVersions(w.ctx(), st)
	require.NoError(t, err)
	require.NotNil(t, vs.Active)
	require.NotNil(t, vs.Prev)
	assert.Equal(t, v3, vs.Active.Version, "Snapshot had v3 active")
	assert.Equal(t, v2, vs.Prev.Version,
		"at Snapshot's seq, the version active before v3 was v2 (not v3 from a post-snapshot event)")
}

// TestHeldAttacks records the adversarial attacks against policy/ that held cleanly.
func TestHeldAttacks(t *testing.T) {
	t.Parallel()
	w := fakeWorld(t)
	snap := w.snap()

	// 1. Starlark input structs (Card, Row, Stream, Read) are frozen/immutable:
	// attempting to set or mutate an attribute fails at runtime and falls back.
	mutateInput, _ := override("reads", "def reads(card):\n    card.tier = \"frontier\"\n    return 1")
	a, fs := NewEngine().Answer(Versions{Active: pv(2, mutateInput)}, snap, FullQuestions(snap, nil))
	require.Len(t, fs, 1)
	assert.Equal(t, 0, a.Version)

	// 2. NaN / Inf floats in order() or prices() are rejected by domain validation.
	nanOrder, _ := override("order", "def order(card):\n    return (float(\"nan\"),)")
	a, fs = NewEngine().Answer(Versions{Active: pv(2, nanOrder)}, snap, FullQuestions(snap, nil))
	require.Len(t, fs, 1)
	assert.Equal(t, 0, a.Version)

	// 3. Multi-byte UTF-8 rune boundary truncation at 4096 bytes in Judgements/storable
	// never splits a multi-byte rune into invalid UTF-8.
	utf8Fail, _ := override("limits", "def limits():\n    fail(\"é\" * 3000)")
	_, fs = NewEngine().Answer(Versions{Active: pv(2, utf8Fail)}, snap, FullQuestions(snap, nil))
	js := Judgements(snap, fs)
	require.Len(t, js, 1)
	assert.LessOrEqual(t, len(js[0].Question), 4096)
	id, at := w.next()
	_, err := w.st.Apply(w.ctx(), store.Op{
		ID: id, Epoch: snap.Epoch, Actor: "test", Kind: "tick", At: at.Add(time.Second),
		Writes: []store.Write{js[0]},
	})
	assert.NoError(t, err)
}

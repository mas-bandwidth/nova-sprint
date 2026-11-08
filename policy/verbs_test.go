package policy

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestVerbsFake(t *testing.T) {
	t.Parallel()
	verbSuite(t, func(t testing.TB) store.Store { return store.NewFake() })
}

// verbSuite is every verb row, its refusals with exact text and its
// success, on one store kind (Fake here, PostgreSQL in the functional tier).
func verbSuite(t *testing.T, mk func(testing.TB) store.Store) {
	for name, fn := range map[string]func(*testing.T, *world){
		"up": testUp, "set refusals": testSetRefusals, "set": testSet, "activate refusals": testActivateRefusals,
		"set first": testSetFirst, "activate": testActivate, "show list diff": testShow, "check": testCheck, "broken mid-sprint": testBrokenMidSprint,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := &world{t: t, st: mk(t), ro: seedRoster()}
			w.seed()
			fn(t, w)
		})
	}
}

func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *Refusal
	require.ErrorAs(t, err, &r)
	return r.Error()
}

func testUp(t *testing.T, w *world) {
	before := w.snap()
	w.up()
	s := w.snap()
	assert.Equal(t, 1, s.Policy, "up activates the default as v1")
	assert.Equal(t, before.Cards, s.Cards)
	pv, err := w.st.Policy(w.ctx(), 1)
	require.NoError(t, err)
	assert.Equal(t, defaultBody, pv.Body)
	r, err := Up(w.ctx(), w.st, w.caller())
	require.NoError(t, err)
	assert.Equal(t, 1, r.Version, "a second up changes nothing")
	_, err = w.st.Policy(w.ctx(), 2)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// testSetFirst: with no version active, the first set stores and activates
// in one op (defect 53: a fresh store took a policy only from up) and
// answers the built-in's open policy judgements; a second set only stores.
func testSetFirst(t *testing.T, w *world) {
	e := testEngine()
	w.apply(store.PutJudgement{ID: "policy.order.1", Kind: "policy", About: "order", Group: "policy:v0", Question: "v0 failed order", Command: "judge"})
	good := w.file("good.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1))
	r, err := Set(w.ctx(), w.st, w.ro, e, w.caller(), good, "first")
	require.NoError(t, err)
	assert.Equal(t, Result{Version: 1, Activated: true, Next: "nova-sprint next"}, r)
	assert.Equal(t, 1, w.snap().Policy, "the first set is active")
	assert.Equal(t, "answered", w.snap().Judgements["policy.order.1"].State, "the built-in's judgement is answered")
	other := w.file("other.star", strings.Replace(defaultBody, "return 1  #", "return 3  #", 1))
	r, err = Set(w.ctx(), w.st, w.ro, e, w.caller(), other, "second")
	require.NoError(t, err)
	assert.Equal(t, Result{Version: 2, Next: "nova-sprint policy check v2"}, r)
	assert.Equal(t, 1, w.snap().Policy, "a later set only stores")
}

func testSetRefusals(t *testing.T, w *world) {
	e := testEngine()
	good := w.file("good.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1))
	w.up()
	kin := w.caller()
	kin.Actor, kin.Coordinator = "kin1", false
	_, err := Set(w.ctx(), w.st, w.ro, e, kin, good, "r")
	assert.Equal(t, "SPRINT REFUSED policy set: policy is the coordinator's: ask over the bus: nova-bus send --as kin1 --to <coordinator>", refusal(t, err))
	_, err = Set(w.ctx(), w.st, w.ro, e, w.caller(), good, " ")
	assert.Equal(t, "SPRINT REFUSED policy set: no reason given: nova-sprint policy set ... --reason <why>", refusal(t, err))
	missing := filepath.Join(t.TempDir(), "none.star")
	assert.Equal(t, "SPRINT REFUSED policy set: cannot read "+missing+": open "+missing+": no such file or directory: name a readable file", refusal(t, setErr(w, e, missing)))
	big := w.file("big.star", strings.Repeat("#", MaxBody+1))
	assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy set: %s is over %d bytes: make the file smaller", big, MaxBody), refusal(t, setErr(w, e, big)))
	assert.Equal(t, "SPRINT REFUSED policy set: v1 has this body: nova-sprint policy activate 1", refusal(t, setErr(w, e, w.file("same.star", defaultBody))))
	arity, line := override("reads", "def reads(card, x):\n    return 1")
	unknown, unknownLine := appended("teir = 1\n")
	missingQ, _ := override("stitch", "")
	bad, badLine := override("reads", "def reads(card):\n    return 9")
	for _, tc := range []struct{ body, want string }{
		{"def order(card)\n", "POLICY REFUSED f.star line 2: got newline, want ':'"},
		{arity, fmt.Sprintf("POLICY REFUSED f.star line %d: reads takes 1 parameter(s): def reads(card)", line)},
		{unknown, fmt.Sprintf("POLICY REFUSED f.star line %d: unknown top-level name teir: a question is one of catalog landing limits order prices reads rows stitch timeouts verdict width, a helper starts with _", unknownLine)},
		{missingQ, fmt.Sprintf("POLICY REFUSED f.star line %d, end of file: question stitch is missing: define def stitch(stream)", lastLine(missingQ))},
		{bad, fmt.Sprintf("POLICY REFUSED f.star line %d reads(p3): the answer 9 is outside 1..5", badLine)},
	} {
		path := w.file("f.star", tc.body)
		assert.Equal(t, "SPRINT REFUSED policy set: "+tc.want+": nova-sprint policy check "+path, refusal(t, setErr(w, e, path)))
	}
	_, err = w.st.Policy(w.ctx(), 2)
	assert.ErrorIs(t, err, store.ErrNotFound, "nothing refused is stored")
}

func setErr(w *world, e *Engine, path string) error {
	_, err := Set(w.ctx(), w.st, w.ro, e, w.caller(), path, "test")
	return err
}

func testSet(t *testing.T, w *world) {
	w.up()
	before := w.snap()
	r, err := Set(w.ctx(), w.st, w.ro, testEngine(), w.caller(), w.file("two-reads.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1)), "two reads")
	require.NoError(t, err)
	assert.Equal(t, Result{Version: 2, Next: "nova-sprint policy check v2"}, r)
	after := w.snap()
	assert.Equal(t, 1, after.Policy, "set stores, never activates")
	assert.Equal(t, before.Cards, after.Cards, "a policy change never moves a card")
	assert.Equal(t, before.Rows, after.Rows)
	pv, err := w.st.Policy(w.ctx(), 2)
	require.NoError(t, err)
	assert.Equal(t, store.PolicyVersion{Version: 2, Kind: "script", Name: "two-reads.star", Body: pv.Body, Actor: "rowan", At: pv.At}, pv)
}

func testActivateRefusals(t *testing.T, w *world) {
	e := testEngine()
	w.up()
	kin := w.caller()
	kin.Coordinator = false
	_, err := Activate(w.ctx(), w.st, w.ro, e, kin, 1, "r")
	assert.Equal(t, "SPRINT REFUSED policy activate: policy is the coordinator's: ask over the bus: nova-bus send --as rowan --to <coordinator>", refusal(t, err))
	_, err = Activate(w.ctx(), w.st, w.ro, e, w.caller(), 9, "r")
	assert.Equal(t, "SPRINT REFUSED policy activate: policy version 9 does not exist: nova-sprint policy show --list", refusal(t, err))
	_, err = Activate(w.ctx(), w.st, w.ro, e, w.caller(), 1, "r")
	assert.Equal(t, "SPRINT REFUSED policy activate: policy version 1 is already active: nova-sprint policy show", refusal(t, err))
	w.apply(store.PutPolicy{Kind: "data", Name: "table", Body: "{}"})
	_, err = Activate(w.ctx(), w.st, w.ro, e, w.caller(), 2, "r")
	assert.Equal(t, "SPRINT REFUSED policy activate: v2 is of kind data, which policy does not run: name a script version: nova-sprint policy show --list", refusal(t, err))
	breaks, line := override("landing", "def landing(stream):\n    return {\"backed_up\": 4 // (0 if stream.name == \"s3\" else 1), \"flowing\": 1}")
	v := w.set("breaks.star", breaks)
	w.apply(store.PutStream{Name: "s3", Repo: "git@example.com:r.git", Base: "main", Pos: 3})
	_, err = Activate(w.ctx(), w.st, w.ro, e, w.caller(), v, "r")
	assert.Equal(t, fmt.Sprintf("SPRINT REFUSED policy activate: POLICY REFUSED v3 line %d landing(s3): floored division by zero; POLICY REFUSED v3 line %d landing(s3 backed_up): floored division by zero: nova-sprint policy check 3", line+1, line+1), refusal(t, err), "check asks landing on both rungs (round 5)")
	assert.Equal(t, 1, w.snap().Policy)
}

func testActivate(t *testing.T, w *world) {
	w.up()
	v := w.set("two-reads.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1))
	before := w.snap()
	r, err := Activate(w.ctx(), w.st, w.ro, testEngine(), w.caller(), v, "two reads")
	require.NoError(t, err)
	assert.Equal(t, Result{Version: 2, Changes: 2, Next: "nova-sprint next"}, r, "reads(p3) and reads(p5) change")
	after := w.snap()
	assert.Equal(t, 2, after.Policy)
	assert.Equal(t, before.Cards, after.Cards, "a policy change never moves a card")
	vs, err := LoadVersions(w.ctx(), w.st, w.snap())
	require.NoError(t, err)
	require.NotNil(t, vs.Prev)
	assert.Equal(t, []int{2, 1}, []int{vs.Active.Version, vs.Prev.Version})
	_, err = Activate(w.ctx(), w.st, w.ro, testEngine(), w.caller(), 1, "roll back")
	require.NoError(t, err, "a rollback is an activation of an old number")
	vs, err = LoadVersions(w.ctx(), w.st, w.snap())
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, []int{vs.Active.Version, vs.Prev.Version})
}

func testShow(t *testing.T, w *world) {
	_, err := Show(w.ctx(), w.st, 0)
	assert.Equal(t, "SPRINT REFUSED policy show: no policy is active: "+FirstPolicyFix, refusal(t, err))
	w.up()
	v := w.set("two-reads.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1))
	out, err := Show(w.ctx(), w.st, v)
	require.NoError(t, err)
	pv, err := w.st.Policy(w.ctx(), v)
	require.NoError(t, err)
	assert.Equal(t, "  v2 script two-reads.star by rowan at "+pv.At.Format("2006-01-02T15:04:05Z")+": test\n\n"+pv.Body, out)
	_, err = Show(w.ctx(), w.st, 7)
	assert.Equal(t, "SPRINT REFUSED policy show: policy version 7 does not exist: nova-sprint policy show --list", refusal(t, err))
	list, err := List(w.ctx(), w.st)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(list, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[0], "  v2 script two-reads.star"), lines[0])
	assert.True(t, strings.HasPrefix(lines[1], "* v1 script default.star"), lines[1])
	assert.True(t, strings.HasSuffix(lines[1], ": up: the built-in default"), lines[1])
	d, err := Diff(w.ctx(), w.st, "default", "v2")
	require.NoError(t, err)
	assert.Contains(t, d, "--- default\n+++ v2\n")
	assert.Contains(t, d, "\n-     return 1  # v1 standing rule, 2026-10-20\n+     return 2  # v1 standing rule, 2026-10-20\n")
	_, err = Diff(w.ctx(), w.st, "8", "")
	assert.Equal(t, "SPRINT REFUSED policy diff: policy version 8 does not exist: nova-sprint policy show --list", refusal(t, err))
	_, err = Diff(w.ctx(), w.st, "x", "")
	assert.Equal(t, "SPRINT REFUSED policy diff: unknown version x: nova-sprint policy show --list", refusal(t, err))
	d, err = Diff(w.ctx(), w.st, "1", "")
	require.NoError(t, err)
	assert.NotContains(t, d, "\n- ", "v1 against the active v1: nothing differs")
}

func testCheck(t *testing.T, w *world) {
	e := testEngine()
	w.up()
	rep, err := Check(w.ctx(), w.st, w.ro, e, w.file("two.star", strings.Replace(defaultBody, "return 1  #", "return 2  #", 1)))
	require.NoError(t, err)
	assert.Equal(t, []string{"reads(p3): 1 -> 2", "reads(p5): 1 -> 2"}, rep.Changes)
	assert.Equal(t, 0, rep.Code())
	warn := []string{
		`POLICY WARNING prices(): deepseek/deepseek-flash (tier flash) is billed at API rate and has no price: its runs will be recorded unpriced=no-price; add "deepseek-flash": {"in": <micro-USD per million>, "out": <...>}`,
		`POLICY WARNING prices(): deepseek/deepseek-v4-pro (tier frontier) is billed at API rate and has no price: its runs will be recorded unpriced=no-price; add "deepseek-v4-pro": {"in": <micro-USD per million>, "out": <...>}`,
		`POLICY WARNING prices(): inception/mercury-3 (tier pro) is billed at API rate and has no price: its runs will be recorded unpriced=no-price; add "mercury-3": {"in": <micro-USD per million>, "out": <...>}`,
		`POLICY WARNING prices(): inception/mercury-2.5 (tier flash) is billed at API rate and has no price: its runs will be recorded unpriced=no-price; add "mercury-2.5": {"in": <micro-USD per million>, "out": <...>}`,
	}
	assert.Equal(t, warn, rep.Warnings, "the default prices nothing: every API model is a defect found at creation")
	assert.Equal(t, "policy check two.star: 2 answer(s) change, 0 refused, 4 warning(s)\n  reads(p3): 1 -> 2\n  reads(p5): 1 -> 2\n"+strings.Join(warn, "\n")+"\n", rep.Text())
	priced := strings.Replace(defaultBody, "    return {}\n\ndef catalog", "    return {\"deepseek/deepseek-flash\": {\"in\": 270000, \"out\": 1100000}, \"deepseek-v4-pro\": {\"in\": 270000, \"out\": 1100000}, \"mercury-3\": {\"in\": 250000, \"out\": 1000000}, \"mercury-2.5\": {\"in\": 250000, \"out\": 1000000}}\n\ndef catalog", 1)
	rep, err = Check(w.ctx(), w.st, w.ro, e, w.file("priced.star", priced))
	require.NoError(t, err)
	assert.Empty(t, rep.Warnings, "a price under provider/model or the model alone is found")
	assert.Equal(t, 0, rep.Code())
	bad, line := override("stitch", "def stitch(stream):\n    return 1")
	rep, err = Check(w.ctx(), w.st, w.ro, e, w.file("bad.star", bad))
	require.NoError(t, err)
	assert.Equal(t, []string{
		fmt.Sprintf("POLICY REFUSED bad.star line %d stitch(s1): the answer is an int, not True or False", line),
		fmt.Sprintf("POLICY REFUSED bad.star line %d stitch(s2): the answer is an int, not True or False", line),
	}, rep.Refusals, "check prints every refusal")
	assert.Equal(t, 1, rep.Code())
	rep, err = Check(w.ctx(), w.st, w.ro, e, "default")
	require.NoError(t, err)
	assert.Empty(t, rep.Changes, "v1 is the default")
	_, err = Check(w.ctx(), w.st, w.ro, e, "v5")
	assert.Equal(t, "SPRINT REFUSED policy check: policy version 5 does not exist: nova-sprint policy show --list", refusal(t, err))
	gone := filepath.Join(t.TempDir(), "gone.star")
	_, err = Check(w.ctx(), w.st, w.ro, e, gone)
	assert.Equal(t, "SPRINT REFUSED policy check: cannot read "+gone+": open "+gone+": no such file or directory: name a readable file", refusal(t, err))
}

package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EnvFault classifies a failed finish's reason as an environment fault: a push refusal the
// member could not mend (no credential, auth, unreachable remote), a missing tool or
// harness, a sandbox or wall refusal and a disk-full are the member's, never the card's or
// the model's; every other reason (a refused push that is the branch's, red tests, a HOLD
// with findings, a provider failure) has no environment cause (tla/ProviderBudget.tla).
func TestEnvFaultClassifiesAFinishReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, report, want string }{
		{"no push credential", "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled", EnvPush},
		{"an auth refusal", "push refused: fatal: Authentication failed for 'https://github.com/x/y'", EnvPush},
		{"an unreachable remote", "fatal: unable to access 'https://github.com/x/y/': Could not resolve host: github.com", EnvPush},
		{"a refused push that is the branch's is not environment", "push refused: rejected (non-fast-forward); r", ""},
		{"a missing harness", "harness not found on this machine: opencode", EnvHarness},
		{"a missing tool", "exec: \"nova-work\": executable file not found in $PATH", EnvTool},
		{"a sandbox refusal", "the sandbox blocked the child: operation not permitted", EnvSandbox},
		{"a wall refusal", "blocked by the wall: the job may not read outside it", EnvSandbox},
		{"a full disk", "write /tmp/x: no space left on device", EnvDisk},
		{"red tests are the card's", "verdict not-done; TestFoo: boom", ""},
		{"a provider failure is native's", "provider failure: class=rate-limited", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, EnvFault(tc.report))
		})
	}
}

// envFaultWorld is one up member with two ready cards dealt to it.
func envFaultWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1"}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	return w
}

// A finish whose reason is an environment fault returns the card to ready untouched -- the
// work card withdraws without FieldTakeEnded, so no redeal is spent, its attempt is
// unchanged and it is never a failed work judgment -- and holds the member with one
// judgment pushed to the seat naming the remedy; a second card on the held member never
// starts (tla/ProviderBudget.tla).
func TestAnEnvironmentFailureHoldsTheMemberAndReturnsTheCard(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	attempt := w.s.Work.Card("s1-1").Int("attempt")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	p := Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"})
	require.Len(t, p.Units, 1, "%+v", p)
	w.must(p)

	c := w.s.Fleet.Card(wc)
	assert.Equal(t, Withdrawn, c.Col, "the card withdraws for the deal")
	assert.Empty(t, c.F(FieldTakeEnded), "no redeal is spent: the take did not end")
	assert.Equal(t, Ready, w.state("s1-1"), "its primary is back in ready, untouched")
	assert.Equal(t, attempt, w.s.Work.Card("s1-1").Int("attempt"), "its attempt is unchanged")
	assert.Zero(t, w.s.Work.Card("s1-1").Int("failed"), "an environment fault is never the card's failure")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
	assert.True(t, w.s.EnvHeld("m1"), "the member is held by its environment fault")
	assert.Equal(t, HeldByEnv, w.s.MemberCtl("m1").F(FieldHeldBy))
	assert.NotEmpty(t, w.s.MemberCtl("m1").F("held"))
	open := w.openOn(MemberSubject("m1"))
	require.Len(t, open, 1, "one judgment per member: %v", w.s.Open)
	assert.Equal(t, NMemberEnvFault, open[0].Note.Type)
	assert.Contains(t, open[0].Note.What, "push-credential play")
	assert.Contains(t, open[0].Note.What, "--limit m1")

	// the second card on the held member never starts
	p = Take(w.s, TakeReq{As: "m1", Sel: Sel{Limit: 10}})
	assert.Empty(t, p.Units, "the held member takes nothing: %+v", p)
	assert.Equal(t, Ready, w.s.Fleet.Card(w.s.Work.Card("s1-2").F("work")).Col, "the second card stays ready")
	_, why := takeSeat(w.s, "m1")
	assert.Contains(t, why, "environment fault")
}

// A model or card failure on the same member is not an environment fault: it holds no
// member, raises no environment judgment and leaves the card in review as failed work
// (tla/ProviderBudget.tla).
func TestAModelFailureDoesNotHoldTheMember(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "verdict not-done: the tests are red"}))
	assert.False(t, w.s.EnvHeld("m1"), "a model failure does not hold the member")
	assert.Equal(t, Review, w.state("s1-1"), "it is failed work for the coordinator")
	assert.Empty(t, w.notesOf(NMemberEnvFault), "no environment judgment")
}

// fleet up refuses a member whose last environment fault is not cleared until the member's
// own probe passes, and accepting it clears the fault and its judgment
// (tla/ProviderBudget.tla).
func TestFleetUpRefusesUntilTheProbePasses(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
	require.True(t, w.s.EnvHeld("m1"))

	p := FleetStep(w.s, FleetReq{Op: "release", Member: "m1"})
	require.Len(t, p.Refused, 1, "no probe: refused: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "environment fault")

	p = FleetStep(w.s, FleetReq{Op: "release", Member: "m1", Probe: "no push credential"})
	require.Len(t, p.Refused, 1, "a failed probe refuses: %+v", p)

	p = FleetStep(w.s, FleetReq{Op: "release", Member: "m1", Probe: EnvProbePassed})
	require.Empty(t, p.Refused, "%+v", p)
	w.must(p)
	assert.False(t, w.s.EnvHeld("m1"), "the passed probe clears the fault")
	assert.Empty(t, w.openOn(MemberSubject("m1")), "and closes its judgment")
}

// A successful push clears an uncleared environment fault by itself, closing its judgment
// (tla/ProviderBudget.tla).
func TestASuccessfulPushClearsTheEnvironmentFault(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	next := w.s.Work.Card("s1-2").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc, next}}, Gens: w.gens(wc, next)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
	require.True(t, w.s.EnvHeld("m1"))
	// the member's card already in flight pushes next
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{next}}, Gens: w.gens(next), Head: strings.Repeat("a", 40)}))
	assert.False(t, w.s.EnvHeld("m1"), "the push cleared the fault")
	assert.Empty(t, w.openOn(MemberSubject("m1")), "and closed its judgment")
}

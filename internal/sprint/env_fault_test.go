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

// envFaultWorld is one up member with two ready cards dealt to it. Its own probe passed
// when fleet up accepted it, as every new member's must (EnvProbePassed;
// tla/ProviderBudget.tla, FleetUpRefusesUncleared).
func envFaultWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Probe: EnvProbePassed}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 2}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 2}}))
	return w
}

// A member the sprint does not know runs its own environment probe before fleet up accepts
// its width: no probe, or a probe that failed, refuses it and nothing is added; a passed
// probe accepts it once (tla/ProviderBudget.tla, FleetUpRefusesUncleared).
func TestFleetUpRefusesANewMemberUntilItsProbePasses(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	p := FleetStep(w.s, FleetReq{Op: "up", Member: "m9", Width: 1})
	require.Len(t, p.Refused, 1, "a new member with no probe is refused: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "probe")
	assert.False(t, w.s.Fleet.HasRow("m9"), "nothing is added for a refused member")

	p = FleetStep(w.s, FleetReq{Op: "up", Member: "m9", Width: 1, Probe: "no push credential"})
	require.Len(t, p.Refused, 1, "a failed probe refuses: %+v", p)
	assert.Contains(t, p.Refused[0].Why, "no push credential")

	p = FleetStep(w.s, FleetReq{Op: "up", Member: "m9", Width: 1, Probe: EnvProbePassed})
	require.Empty(t, p.Refused, "%+v", p)
	w.must(p)
	assert.True(t, w.s.Fleet.HasRow("m9"), "the passed probe accepts the member")
	assert.Equal(t, CtlID("m9"), w.s.MemberCtl("m9").ID)
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

// An environment fault holds the member's STATUS, not only its held field, so the fleet's
// up set never lists it: a level after the fault deals it no card, since level works on
// UpMembers and the sweep sends a not-up member's cards away instead
// (tla/ProviderBudget.tla, HeldDealtNothing).
func TestAnEnvironmentFaultedMemberIsLevelledNoCard(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m1", Width: 2, Probe: EnvProbePassed}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 2, Probe: EnvProbePassed}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 4}))
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	var wc string
	for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-4"} {
		c := w.s.Work.Card(id)
		if c == nil {
			continue
		}
		if wk := w.s.Fleet.Card(c.F("work")); wk != nil && wk.Row == "m1" && wk.Col == Ready {
			wc = c.F("work")
			break
		}
	}
	require.NotEmpty(t, wc, "m1 was dealt no ready card to fault")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))

	require.True(t, w.s.EnvHeld("m1"))
	assert.Equal(t, Held, w.s.MemberCtl("m1").F("status"), "the fault holds the member's status: %v", w.s.MemberCtl("m1").Fields)
	assert.NotContains(t, w.s.UpMembers(), "m1", "a held member is never up")

	w.must(FleetStep(w.s, FleetReq{Op: "level", Who: MachineActor}))
	assert.Zero(t, perMember(w.s)["m1"], "a level after the fault deals the held member no card")
	assert.NotContains(t, w.s.UpMembers(), "m1")
}

// The member's own clear (its next card pushes) releases the hold it put on the member's
// status: the fault's record and hold go, the status returns up, and the member serves
// again (tla/ProviderBudget.tla, FaultClearsOnPush).
func TestTheEnvironmentFaultClearReleasesTheMember(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	next := w.s.Work.Card("s1-2").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc, next}}, Gens: w.gens(wc, next)}))
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
	require.True(t, w.s.EnvHeld("m1"))
	require.Equal(t, Held, w.s.MemberCtl("m1").F("status"), "the fault holds the status")
	require.NotContains(t, w.s.UpMembers(), "m1")

	// the member's card already in flight pushes: the clear releases the member
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{next}}, Gens: w.gens(next), Head: strings.Repeat("a", 40)}))
	assert.False(t, w.s.EnvHeld("m1"), "the push cleared the fault")
	assert.Equal(t, Up, w.s.MemberCtl("m1").F("status"), "the clear puts the member back up: %v", w.s.MemberCtl("m1").Fields)
	assert.Contains(t, w.s.UpMembers(), "m1", "and it serves again")
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

// A second, different environment cause on the same held member is surfaced: the one open
// judgment is rewritten to name the member's current cause, its reason and its own remedy,
// so the seat is never left a remedy for a cause that is no longer the member's
// (tla/ProviderBudget.tla, JudgmentNamesTheCause).
func TestASecondDistinctEnvironmentCauseUpdatesTheJudgment(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	next := w.s.Work.Card("s1-2").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc, next}}, Gens: w.gens(wc, next)}))
	// the first card fails at push: the member is held with the push remedy
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
	require.True(t, w.s.EnvHeld("m1"))
	require.Len(t, w.openOn(MemberSubject("m1")), 1)
	// the card already in flight fails for a different cause: the disk is full
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{next}}, Gens: w.gens(next), Failed: true,
		Report: "write /tmp/x: no space left on device"}))
	cause, reason := w.s.MemberEnvFault("m1")
	assert.Equal(t, EnvDisk, cause, "the member's recorded cause is the new one")
	assert.Contains(t, reason, "no space left")

	open := w.openOn(MemberSubject("m1"))
	require.Len(t, open, 1, "one judgment per member: %v", w.s.Open)
	assert.Contains(t, open[0].Note.What, "(disk)", "the judgment names the current cause")
	assert.Contains(t, open[0].Note.What, "free disk", "and its own remedy")
	assert.NotContains(t, open[0].Note.What, "push-credential play", "not the stale push remedy")
}

// Two of one member's cards failing with the SAME environment cause in one finish step open
// one judgment, not one per card, and write the member's control card once (the plan the
// model admits changes a control card once a step): rule (2), one pushed judgment per member
// and cause, holds within the step as it does across steps (a multi-card finish, --as naming
// several cards; docs/TESTS.md). Both cards are withdrawn and their primaries go back to
// ready untouched, their attempts unchanged (tla/ProviderBudget.tla).
func TestTwoCardsFailingTheSameEnvironmentCauseInOneStepAreOneJudgment(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	wc := w.s.Work.Card("s1-1").F("work")
	next := w.s.Work.Card("s1-2").F("work")
	attempt1 := w.s.Work.Card("s1-1").Int("attempt")
	attempt2 := w.s.Work.Card("s1-2").Int("attempt")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{wc, next}}, Gens: w.gens(wc, next)}))
	// both cards finish refused at push in one call: the member tells the seat once
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{wc, next}}, Gens: w.gens(wc, next), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))

	assert.Equal(t, Ready, w.state("s1-1"), "the first card is back in ready")
	assert.Equal(t, Ready, w.state("s1-2"), "the second card is back in ready")
	assert.Equal(t, attempt1, w.s.Work.Card("s1-1").Int("attempt"), "the first attempt is unchanged")
	assert.Equal(t, attempt2, w.s.Work.Card("s1-2").Int("attempt"), "the second attempt is unchanged")
	assert.Zero(t, w.s.Work.Card("s1-1").Int("failed"), "an environment fault is never the card's failure")
	assert.Zero(t, w.s.Work.Card("s1-2").Int("failed"), "either card's")
	assert.Empty(t, w.notesOf(NWorkFailed), "no failed-work judgment")
	assert.True(t, w.s.EnvHeld("m1"), "the member is held by its environment fault")
	open := w.openOn(MemberSubject("m1"))
	require.Len(t, open, 1, "one judgment per member, not one per card: %v", w.s.Open)
	assert.Equal(t, NMemberEnvFault, open[0].Note.Type)
	assert.Contains(t, open[0].Note.What, "push-credential play")
}

// An environment fault records itself BESIDE a hold that is already on the member: a
// coordinator's hold is not the environment's to overwrite, its stamp and reason stay, and
// clearing the fault (its next card's push) releases only the machine's own environment
// hold, so the coordinator's hold is still there for a person to lift
// (tla/ProviderBudget.tla).
func TestAnEnvironmentFaultRecordsBesideACoordinatorsHold(t *testing.T) {
	t.Parallel()
	w := envFaultWorld(t)
	first := w.s.Work.Card("s1-1").F("work")
	next := w.s.Work.Card("s1-2").F("work")
	w.must(Take(w.s, TakeReq{As: "m1", Sel: Sel{IDs: []string{first, next}}, Gens: w.gens(first, next)}))
	// the coordinator holds m1, letting its working cards finish
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "coordinator", Reason: "looking", Finish: true}))
	heldAt := w.s.MemberCtl("m1").F("held")
	require.NotEmpty(t, heldAt, "the coordinator's hold is on: %v", w.s.MemberCtl("m1").Fields)
	require.Empty(t, w.s.MemberCtl("m1").F(FieldHeldBy), "the coordinator's hold carries no mark: %v", w.s.MemberCtl("m1").Fields)

	// its card fails at push: the fault is recorded, the coordinator's hold is left alone
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{first}}, Gens: w.gens(first), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
	ctl := w.s.MemberCtl("m1")
	assert.True(t, w.s.EnvHeld("m1"), "the member is held by its environment fault")
	assert.Equal(t, EnvPush, ctl.F(FieldEnvFault), "the fault is recorded: %v", ctl.Fields)
	assert.Equal(t, heldAt, ctl.F("held"), "the coordinator's hold stamp is untouched: %v", ctl.Fields)
	assert.Empty(t, ctl.F(FieldHeldBy), "the fault did not take the coordinator's hold: %v", ctl.Fields)
	assert.Equal(t, "looking", ctl.F(FieldHeldReason), "nor its reason: %v", ctl.Fields)

	// the card in flight pushes: the fault clears, the coordinator's hold stays
	w.must(Finish(w.s, FinishReq{As: "m1", Sel: Sel{IDs: []string{next}}, Gens: w.gens(next), Head: strings.Repeat("a", 40)}))
	assert.False(t, w.s.EnvHeld("m1"), "the push cleared the environment fault")
	assert.Empty(t, w.openOn(MemberSubject("m1")), "and closed its judgment")
	ctl = w.s.MemberCtl("m1")
	assert.NotEmpty(t, ctl.F("held"), "the coordinator's hold stays: %v", ctl.Fields)
	assert.Equal(t, "looking", ctl.F(FieldHeldReason), "with its reason: %v", ctl.Fields)
}

// EnvProbePatch, the passed probe's clear, unsets the machine's own environment hold with
// the fault but leaves a hold of another mark (a coordinator's, the fault's, the sync's)
// alone: only the fault's record goes, so a person's hold is never lifted by a probe
// (tla/ProviderBudget.tla).
func TestAPassedProbeClearsOnlyTheEnvironmentHold(t *testing.T) {
	t.Parallel()
	coord := &Card{Fields: map[string]string{"held": "then", FieldHeldReason: "looking"}}
	_, unset := EnvProbePatch(coord, EnvProbePassed)
	assert.Contains(t, unset, FieldEnvFault, "the fault's record goes")
	assert.NotContains(t, unset, "held", "a coordinator's hold is not the probe's to clear")
	assert.NotContains(t, unset, FieldHeldReason)

	env := &Card{Fields: map[string]string{"held": "then", FieldHeldBy: HeldByEnv, FieldHeldReason: "environment fault: push"}}
	_, unset = EnvProbePatch(env, EnvProbePassed)
	assert.Contains(t, unset, "held", "the machine's own environment hold goes with the fault")
	assert.Contains(t, unset, FieldHeldBy)

	failed := &Card{Fields: map[string]string{FieldHeldBy: HeldByEnv}}
	set, unset := EnvProbePatch(failed, "no push credential")
	assert.Equal(t, "no push credential", set[FieldEnvProbe], "a failed probe is recorded")
	assert.Empty(t, unset, "nothing clears on a failed probe")
}

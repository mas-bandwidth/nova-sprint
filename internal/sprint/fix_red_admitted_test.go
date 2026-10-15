package sprint_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// A red base gets its fix-red card worked (docs/SPEC-SPRINT.md section 7, the base's class
// gate; tla/CoordinatorPass.tla, Kind "fixred": ResumesWithoutCoordinator, OneFixRedPerClass;
// the owner, 2026-10-15: the space bases went red on gofmt and vet, the lander named a
// fix-red card in its judgment and admitted none, and each base waited for the coordinator).
// The gate is a fake that answers red on gofmt at the base's first tip and green at the tip
// the fix lands, the clock is the rig's, and nothing runs but the twin store: the lander's
// steps in the order the lander takes them (the gate's red admits the card, then the refusal
// stops the stream), a tick that deals the card, a worker's finish and the reads' ok, a
// tick that resumes the stream by rule, the lander's landing of the card first as the base's
// cure, and the stream's next card. No seat verb between them.
func TestARedBaseGetsItsFixRedWorked(t *testing.T) {
	t.Parallel()
	const (
		repo = "mas-bandwidth/nova-sprint"
		base = "v1.3/comms"
		tip0 = "1111111111111111111111111111111111111111"
		tip1 = "2222222222222222222222222222222222222222"
	)
	// the fake gate: gofmt red at the base's first tip, green at the tip the fix lands
	gate := func(sha string) string {
		if sha == tip0 {
			return "gofmt -l .: exit status 1: internal/sprint/foo.go"
		}
		return ""
	}
	r := newConflictRig(t)
	brief := "c: the work tier: flash\nREPO: " + repo + "\nBASE: " + base + "\n\nThe task.\n"
	r.must(store.AddStep(sprint.AddReq{Stream: "sp", Count: 2, Brief: brief}))
	state := func() string { return r.snap().StreamCtl("sp").F("state") }
	fixCards := func() []*sprint.Card {
		var out []*sprint.Card
		for _, c := range r.snap().Work.Cards() {
			if strings.HasPrefix(c.ID, sprint.FixRedPrefix) {
				out = append(out, c)
			}
		}
		return out
	}

	// the gate runs red on gofmt: the class's card is the one the generator stamps
	why := gate(tip0)
	require.NotEmpty(t, why)
	card, class, ok := sprint.ClassGateCard(t.TempDir(), base, why)
	require.True(t, ok)
	assert.Equal(t, "gofmt", class.Name)
	assert.True(t, strings.HasPrefix(card.ID, "fix-red-gofmt-"), "one card a class and base: %s", card.ID)
	assert.Equal(t, "blocker", card.Priority)
	add := sprint.FixRedAdd{Base: base, Who: "lander", Card: sprint.CardAdd{
		ID: card.ID, File: card.ID, Base: base, Repo: repo,
		Brief: cardgen.Render(cardgen.Header{Repo: repo, Base: base, Sha: tip0}, card)}}
	admit := func() store.Result {
		return r.must(store.Step{Verb: "add fix-red", Args: store.ArgsOf(add), Load: []string{sprint.Work, sprint.Merge, sprint.Fleet}, Mirrors: true,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.FixRedAdmit(s, add) }})
	}
	refuse := func(why string) {
		q := sprint.MergeReq{Stream: "sp", Base: base, BaseRed: why, Who: "lander"}
		r.must(store.Step{Verb: "merge", Args: store.ArgsOf(q), Load: []string{sprint.Merge, sprint.Work}, Mirrors: true,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.LandBaseRefused(s, q) }})
	}

	// the red gate admits the card, a blocker first in the stream's line; the refusal then
	// opens the episode and stops the stream
	admit()
	refuse(sprint.ClassGateWhy(t.TempDir(), base, tip0, why))
	require.Len(t, fixCards(), 1, "the red base admits its fix-red card")
	fix := fixCards()[0]
	assert.Equal(t, "sp", fix.Row, "into the stream whose base is red")
	assert.Equal(t, sprint.Ready, fix.Col)
	level, _ := sprint.CardPriority(fix)
	assert.Equal(t, sprint.PriorityBlocker, level)
	for _, c := range r.snap().Work.Cell("sp", sprint.Ready) {
		if c.ID != fix.ID {
			assert.Greater(t, c.Score, fix.Score, "%s is behind the fix-red card in line", c.ID)
		}
	}
	require.Equal(t, sprint.StreamStopped, state())
	require.Len(t, r.open(sprint.NBaseRed), 1, "the episode's one judgment")

	// a second red on the same class and base, and a re-raise of the episode, admit nothing
	for range 2 {
		res := admit()
		assert.Empty(t, res.Moved, "no second card while one is open: %v", res.Moved)
	}
	assert.Len(t, fixCards(), 1, "OneFixRedPerClass")
	assert.Len(t, r.open(sprint.NBaseRed), 1)

	// the next tick deals it with no coordinator step; a tick with the card unread leaves the
	// stream stopped
	r.tick()
	assert.NotEmpty(t, r.snap().Work.Card(fix.ID).F("work"), "dealt by the tick")
	assert.Equal(t, sprint.StreamStopped, state(), "stopped while the card is unread")

	// a worker finishes it and the reads say ok: it is queued to merge; the next tick's
	// base-gate rule resumes the stream, answering the episode's judgment
	r.toMergingAt(fix.ID, "abc1234")
	r.tick()
	assert.NotEqual(t, sprint.StreamStopped, state(), "resumed by rule")
	assert.Contains(t, r.snap().StreamCtl("sp").F("did"), "answered by rule base-gate: its fix-red card "+fix.ID)
	assert.Empty(t, r.open(sprint.NBaseRed), "the episode's judgment is answered")
	require.NotEmpty(t, r.answeredBy(sprint.RuleBaseGate))

	// the lander met the red base again and the card did not cure it: the stop at the same
	// head is left to a mind, never resumed again
	refuse(why)
	r.tick()
	assert.Equal(t, sprint.StreamStopped, state(), "a card tried at this head is not resumed again")
	assert.Len(t, fixCards(), 1)
	r.must(store.ResumeStep(sprint.ResumeReq{Stream: "sp", Did: "reworked", Who: "coordinator"}))

	// the lander lands the card first as the base's cure: the gate at the new tip is green,
	// and the stream's next card lands after it
	r.must(store.MergeStep(sprint.MergeReq{Stream: "sp", Cards: []string{fix.ID}}))
	assert.Equal(t, sprint.Landed, r.snap().Work.Card(fix.ID).Col)
	assert.Empty(t, gate(tip1), "the base is green on the new tip")
	assert.Empty(t, r.open(sprint.NBaseRed))
	r.landWithCost("sp", "sp-1")
	assert.Equal(t, sprint.Landed, r.snap().Work.Card("sp-1").Col, "the stream's next landing proceeds")
	assert.NotEqual(t, sprint.StreamStopped, state())

	// a base red on a class again after the card landed needs a new card, with the next free id
	admit()
	cards := fixCards()
	require.Len(t, cards, 2)
	assert.Equal(t, fix.ID+"-2", cards[1].ID)
	assert.Contains(t, cards[1].F("brief"), "RESULT: "+fix.ID+"-2")
}

// The idempotence the model pins (tla/CoordinatorPass.tla OneFixRedPerClass), alone: with
// no stream waiting on the base nothing is admitted, and a card of the family open on the
// table, in whatever stream, admits no second.
func TestFixRedAdmitsOnlyWhereTheBaseIsWaitedOn(t *testing.T) {
	t.Parallel()
	r := newConflictRig(t)
	add := sprint.FixRedAdd{Base: "other", Who: "lander", Card: sprint.CardAdd{ID: "fix-red-gofmt-other", File: "fix-red-gofmt-other", Base: "other",
		Brief: "fix-red-gofmt-other: x tier: flash\nREPO: mas-bandwidth/nova-sprint\nBASE: other\n\nThe task.\n"}}
	assert.Empty(t, sprint.FixRedAdmit(r.snap(), add).Units, "no stream holds a card on that base")
	assert.Empty(t, sprint.FixRedStream(r.snap(), "other"))
	assert.Empty(t, sprint.FixRedStream(r.snap(), ""), "no base named, no stream")
}

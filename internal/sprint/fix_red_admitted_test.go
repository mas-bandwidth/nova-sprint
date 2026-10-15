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
// gate; tla/CoordinatorPass.tla, Kind "fixred": ResumesWithoutCoordinator, OneFixRedPerClass,
// EpisodeOpenUntilLanding; the owner, 2026-10-15: the space bases went red on gofmt and vet, the
// lander named a fix-red card in its judgment and admitted none, and each base waited for the
// coordinator). The gate is a fake that answers red on gofmt at the base's first tip and green
// at the tip the fix lands, the clock is the rig's, and nothing runs but the twin store: the
// lander's refusals in the order the lander takes them (a counted one, then the stop that
// opens the episode, which admits the card in its own plan), a tick that deals the card, a
// worker's finish and the reads' ok, a tick that resumes the stream by rule with the episode
// still open, the lander's landing of the card first as the base's cure, the tick that closes
// the episode on the landing, and the stream's next card. No seat verb between them.
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
	add := &sprint.FixRedAdd{Base: base, Who: "lander", Card: sprint.CardAdd{
		ID: card.ID, File: card.ID, Base: base, Repo: repo,
		Brief: cardgen.Render(cardgen.Header{Repo: repo, Base: base, Sha: tip0}, card)}}
	// the lander's refusal of a landing on the red base: the merge step with the card admitted
	// in the stop's plan (the lander's withFixRed), the one store step
	refuse := func(q sprint.MergeReq) store.Result {
		q.Stream, q.Base, q.Who = "sp", base, "lander"
		return r.must(store.Step{Verb: "merge", Args: store.ArgsOf(q), Load: []string{sprint.Merge, sprint.Work, sprint.Readers, sprint.Fleet}, Mirrors: true,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.WithFixRed(s, sprint.LandBaseRefused(s, q), add) }})
	}
	found := sprint.ClassGateWhy(t.TempDir(), base, tip0, why)

	// a refusal that is only counted opens no episode and admits nothing
	refuse(sprint.MergeReq{BaseRefused: found})
	assert.Empty(t, fixCards(), "no episode, no card")
	assert.NotEqual(t, sprint.StreamStopped, state())

	// the stop that opens the episode admits the card in its own plan, a blocker first in the
	// stream's line: one store step, so the episode never stands without its card
	res := refuse(sprint.MergeReq{BaseRed: found})
	require.Len(t, fixCards(), 1, "the episode admits its fix-red card")
	assert.Greater(t, len(res.Moved), 1, "the stop and the admit moved together: %v", res.Moved)
	require.Equal(t, sprint.StreamStopped, state())
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
	require.Len(t, r.open(sprint.NBaseRed), 1, "the episode's one judgment")

	// a second red on the same class and base, and a re-raise of the episode, admit nothing
	for range 2 {
		assert.Empty(t, sprint.FixRedAdmit(r.snap(), *add).Units, "no second card while one is open")
	}
	assert.Len(t, fixCards(), 1, "OneFixRedPerClass")
	assert.Len(t, r.open(sprint.NBaseRed), 1)

	// the next tick deals it with no coordinator step; a tick with the card unread leaves the
	// stream stopped
	r.tick()
	assert.NotEmpty(t, r.snap().Work.Card(fix.ID).F("work"), "dealt by the tick")
	assert.Equal(t, sprint.StreamStopped, state(), "stopped while the card is unread")

	// a worker finishes it and the reads say ok: it is queued to merge; the next tick's
	// base-gate rule resumes the stream so the lander can land it, and the episode stays open
	r.toMergingAt(fix.ID, "abc1234")
	r.tick()
	assert.NotEqual(t, sprint.StreamStopped, state(), "resumed by rule")
	assert.Contains(t, r.snap().StreamCtl("sp").F("did"), "base-gate: its fix-red card "+fix.ID)
	assert.Len(t, r.open(sprint.NBaseRed), 1, "the episode stays open until the card lands")
	r.tick()
	assert.Len(t, r.open(sprint.NBaseRed), 1, "a tick before the landing closes nothing")
	assert.NotEqual(t, sprint.Landed, r.snap().Work.Card(fix.ID).Col)

	// the lander lands the card first as the base's cure: the gate at the new tip is green
	r.must(store.MergeStep(sprint.MergeReq{Stream: "sp", Cards: []string{fix.ID}}))
	assert.Equal(t, sprint.Landed, r.snap().Work.Card(fix.ID).Col)
	assert.Empty(t, gate(tip1), "the base is green on the new tip")
	assert.Len(t, r.open(sprint.NBaseRed), 1, "the landing is the tick's to see")

	// the landing closes the episode by rule, no seat verb, and the stream's next card lands
	r.tick()
	assert.Empty(t, r.open(sprint.NBaseRed), "the card's landing closes the episode")
	require.NotEmpty(t, r.answeredBy(sprint.RuleBaseGate))
	assert.Empty(t, r.snap().StreamCtl("sp").F(sprint.FieldFixRedTried), "the resume's record is cleared")
	r.landWithCost("sp", "sp-1")
	assert.Equal(t, sprint.Landed, r.snap().Work.Card("sp-1").Col, "the stream's next landing proceeds")
	assert.NotEqual(t, sprint.StreamStopped, state())

	// a base red on a class again after the card landed needs a new card, with the next free id
	// (the stream still holds a card on the base: sp-2 is not landed)
	refuse(sprint.MergeReq{BaseRed: found})
	cards := fixCards()
	require.Len(t, cards, 2)
	assert.Equal(t, fix.ID+"-2", cards[1].ID)
	assert.Contains(t, cards[1].F("brief"), "RESULT: "+fix.ID+"-2")
}

// A card resumed once at a head and met again red at that same head resumes nothing again
// (FieldFixRedTried): the stop stays for a mind, and its episode stays open.
func TestAFixRedCardTriedAtTheSameHeadIsNotResumedAgain(t *testing.T) {
	t.Parallel()
	const (
		repo = "mas-bandwidth/nova-sprint"
		base = "v1.3/comms"
		tip0 = "1111111111111111111111111111111111111111"
	)
	r := newConflictRig(t)
	brief := "c: the work tier: flash\nREPO: " + repo + "\nBASE: " + base + "\n\nThe task.\n"
	r.must(store.AddStep(sprint.AddReq{Stream: "sp", Count: 2, Brief: brief}))
	why := "gofmt -l .: exit status 1: internal/sprint/foo.go"
	card, _, ok := sprint.ClassGateCard(t.TempDir(), base, why)
	require.True(t, ok)
	add := &sprint.FixRedAdd{Base: base, Who: "lander", Card: sprint.CardAdd{ID: card.ID, File: card.ID, Base: base, Repo: repo,
		Brief: cardgen.Render(cardgen.Header{Repo: repo, Base: base, Sha: tip0}, card)}}
	refuse := func() {
		q := sprint.MergeReq{Stream: "sp", Base: base, BaseRed: sprint.ClassGateWhy(t.TempDir(), base, tip0, why), Who: "lander"}
		r.must(store.Step{Verb: "merge", Args: store.ArgsOf(q), Load: []string{sprint.Merge, sprint.Work, sprint.Readers, sprint.Fleet}, Mirrors: true,
			Plan: func(s *sprint.Snapshot) sprint.Plan { return sprint.WithFixRed(s, sprint.LandBaseRefused(s, q), add) }})
	}
	refuse()
	r.tick()
	fix := ""
	for _, c := range r.snap().Work.Cards() {
		if strings.HasPrefix(c.ID, sprint.FixRedPrefix) {
			fix = c.ID
		}
	}
	require.NotEmpty(t, fix)
	r.toMergingAt(fix, "abc1234")
	r.tick()
	require.NotEqual(t, sprint.StreamStopped, r.snap().StreamCtl("sp").F("state"), "resumed by rule")

	// the lander met the red base again and the card did not cure it
	refuse()
	r.tick()
	assert.Equal(t, sprint.StreamStopped, r.snap().StreamCtl("sp").F("state"), "a card tried at this head is not resumed again")
	assert.Len(t, r.open(sprint.NBaseRed), 1, "the judgment stays for a mind")
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

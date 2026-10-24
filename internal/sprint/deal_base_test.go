package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The deal's two preconditions on BASE (deal_base.go; tla/DealBase.tla), on the cases of
// stream rocketnet-final on 2026-10-24, which landed 1 of 20 first time. Unit tests only: the
// world's clock (sim_test.go), the facts given as the binding would read them, no git, no
// network.

const (
	dbRepo = "mas-bandwidth/rocketnet"
	dbTipA = "a497d2b2af01a497d2b2af01a497d2b2af01a497"
	dbTipB = "7b07e97be15f7b07e97be15f7b07e97be15f7b07"
)

// dbBrief is a rocketnet-final brief header with these extra lines.
func dbBrief(lines string) string {
	return "c: x tier: pro\nREPO: " + dbRepo + "\nBASE: main\n" + lines + "\n\nThe task.\n"
}

// dbWorld is friendWorld with members whose facts carry every platform.
func dbWorld(t *testing.T, briefs ...string) *world {
	t.Helper()
	w := friendWorld(t, briefs...)
	for _, m := range []string{"m1", "m2"} {
		w.s.MemberCtl(m).Fields[FieldFacts] = "linux,darwin,macos,windows,tla,linux-xdp,go"
	}
	return w
}

func redOn(tip string, checks ...BaseCheck) []BaseCIFacts {
	return []BaseCIFacts{{Repo: dbRepo, Base: "main", Tip: tip, Red: true, FirstRed: tip, TipFailed: checks}}
}

func greenAt(tip string) []BaseCIFacts {
	return []BaseCIFacts{{Repo: dbRepo, Base: "main", Tip: tip}}
}

var linuxDebugRed = BaseCheck{Name: "ubuntu-24.04 / Debug", Platforms: []string{PlatformLinux}}

// Rule 1: a card whose GATE-NEEDS names a platform BASE is red on is never dealt: it waits
// ready with its attempt unspent, under one judgment naming why; the tick that reads BASE
// green deals it and closes the judgment (DealBase.tla NeverDealtOnRed, GreenDealsInOneTick).
func TestACardIsNotDealtWhileItsBaseIsRedOnItsPlatform(t *testing.T) {
	t.Parallel()
	w := dbWorld(t, dbBrief("GATE-NEEDS: linux, tla"))
	sub := StreamSubject(BaseSubject(dbRepo, "main"))
	red := TickReq{BaseCI: redOn(dbTipA, linuxDebugRed)}
	for range 3 {
		w.part(TickDeal, red)
		assert.Equal(t, Ready, w.state("s1-1"), "dealt onto a BASE red on its platform")
		assert.Nil(t, w.s.Fleet.Card("s1-1.w1"), "a work card was cut while BASE is red")
		assert.Equal(t, 0, w.s.Primary("s1-1").Int("attempt"), "an attempt was spent while BASE is red")
	}
	notes := w.notesOf(NBaseRedWait)
	require.Len(t, notes, 1, "one judgment for the red BASE, raised once")
	assert.Contains(t, notes[0].What, "s1-1: BASE red on linux: ubuntu-24.04 / Debug, at a497d2b2af01")
	require.Len(t, w.openOn(sub), 1)

	w.part(TickDeal, TickReq{BaseCI: greenAt(dbTipB)})
	assert.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "BASE green: the card is dealt in that tick")
	assert.Empty(t, w.openOn(sub), "BASE green: the judgment closes in that tick")
}

// Reversed witnesses: a red on another platform, no fact read, no platform named, and a
// fix-red card (the cure) are all dealt.
func TestABaseRedHoldsOnlyTheCardsOfItsPlatforms(t *testing.T) {
	t.Parallel()
	winRed := redOn(dbTipA, BaseCheck{Name: "windows / Release (a)", Platforms: []string{PlatformWindows}})
	for _, tc := range []struct {
		name, lines string
		facts       []BaseCIFacts
	}{
		{"red on windows, card on linux", "GATE-NEEDS: linux", winRed},
		{"no fact read", "GATE-NEEDS: windows", nil},
		{"no GATE-NEEDS", "", winRed},
		{"a fix-red card", "KIND: fix-red\nGATE-NEEDS: windows", winRed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := dbWorld(t, dbBrief(tc.lines))
			w.part(TickDeal, TickReq{BaseCI: tc.facts})
			assert.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "the card is held: %+v", w.notes)
			assert.Empty(t, w.notesOf(NBaseRedWait))
		})
	}
	// linux-xdp waits on an XDP red too; darwin is macos
	assert.Equal(t, []string{PlatformLinux, PlatformXDP}, GatePlatforms([]string{"linux-xdp"}))
	assert.Equal(t, []string{PlatformMacOS, PlatformWindows}, GatePlatforms([]string{"darwin", "windows", "lan"}))
	assert.Equal(t, []string{PlatformTLA}, CheckPlatforms("TLC records", "check", []string{"ubuntu-latest"}))
	assert.Equal(t, []string{PlatformWindows}, CheckPlatforms("CI", "happy path / windows", nil))
	assert.Equal(t, []string{PlatformLinux}, CheckPlatforms("CI", "tsan (loopback soak)", []string{"ubuntu-24.04"}))
	assert.Equal(t, []string{PlatformXDP}, CheckPlatforms("CI", "XDP / libxdp 1.4 / Debug", []string{"self-hosted", "linux"}))
	assert.Equal(t, []string{PlatformMacOS}, CheckPlatforms("CI", "macos-15 / Debug", nil))
}

// Rule 2a, the case of 2026-10-24: PATHS names functional/posix_hard_killed_peer.cpp, which
// BASE renamed to posix_hard_killed_client.cpp. The brief is not dealt: one brief-defect
// judgment names the missing path; a NEW file is never missing; the brief cut again deals.
func TestABriefWhosePathsFileWasRenamedAtBaseIsNotDealt(t *testing.T) {
	t.Parallel()
	w := dbWorld(t, dbBrief("PATHS: functional/main.cpp, functional/posix_hard_killed_peer.cpp, tests/\nNEW: tests/plan_rss.h\nSTART: functional/main.cpp (read), functional/posix_hard_killed_peer.cpp (read), sdk"))
	tree := []BaseTree{{Repo: dbRepo, Base: "main", Sha: dbTipA, Files: []string{
		"functional/main.cpp", "functional/posix_hard_killed_client.cpp", "sdk/rocketnet.h", "tests/suite_runner.h"}}}
	w.part(TickDeal, TickReq{BaseTrees: tree})
	assert.Nil(t, w.s.Fleet.Card("s1-1.w1"), "a brief naming a file BASE lacks was dealt")
	notes := w.notesOf(NBriefPreflight)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].What, "PATHS not at main a497d2b2af01 (and not under NEW): functional/posix_hard_killed_peer.cpp")
	assert.NotContains(t, notes[0].What, "plan_rss.h", "a NEW file is no missing file")
	assert.NotContains(t, notes[0].What, "START not", "a START file named in PATHS is named once")
	assert.Equal(t, 0, w.s.Primary("s1-1").Int("attempt"), "an attempt was spent on a defective brief")

	// the brief cut again on the renamed file passes and is dealt; the judgment closes
	w.s.Primary("s1-1").Fields["brief"] = dbBrief("PATHS: functional/main.cpp, functional/posix_hard_killed_client.cpp\nNEW: tests/plan_rss.h")
	w.part(TickDeal, TickReq{BaseTrees: tree})
	assert.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "the brief cut again was not dealt")
	assert.Empty(t, w.openOn("s1-1"), "the brief-defect judgment outlived its cause")
}

// Rule 2b, the case of 2026-10-24: rn-client-gso-linux-b said DEPENDS-ON
// rn-client-gso-linux-a beside "Needs: none" and was dealt before its parent landed. Now it
// is a brief defect naming the parent until the parent lands, then it is dealt.
func TestANeedsNoneCardIsNotDealtBeforeItsParentLands(t *testing.T) {
	t.Parallel()
	w := dbWorld(t, dbBrief("DEPENDS-ON: s1-2\nNeeds: none"), dbBrief(""))
	w.place(w.s.Work, "s1-2", "s1", string(Review)) // the parent: on the table, not landed
	w.part(TickDeal, TickReq{})
	assert.Nil(t, w.s.Fleet.Card("s1-1.w1"), "dealt before its DEPENDS-ON parent landed")
	notes := w.notesOf(NBriefPreflight)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].What, `DEPENDS-ON not landed and not in Needs ("Needs: none" beside a non-empty DEPENDS-ON): s1-2 (review)`)

	w.place(w.s.Work, "s1-2", "s1", string(Landed))
	w.part(TickDeal, TickReq{})
	assert.NotNil(t, w.s.Fleet.Card("s1-1.w1"), "the parent landed: the card is dealt")
	assert.Empty(t, w.openOn("s1-1"))
}

// Rule 1b: a failed finish whose report says the failure is reproduced at BASE is BASE's:
// no failure counted, no judgment of the card's, the primary back to ready, the attempt
// recorded inherited; it counts toward neither the attempt cap nor first-time stats.
func TestAnInheritedRedIsNotTheCardsFailure(t *testing.T) {
	t.Parallel()
	w := dbWorld(t, dbBrief("GATE-NEEDS: linux"))
	w.part(TickDeal, TickReq{})
	wc := w.s.Fleet.Card("s1-1.w1")
	require.NotNil(t, wc)
	w.must(Take(w.s, TakeReq{As: wc.Row}))
	w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, As: wc.Row, Gens: gensOf(w.s, "s1-1.w1"), Failed: true,
		Report: "HOLD: test_posix_connect_disconnect_1000 fails\nINHERITED-RED: test_posix_connect_disconnect_1000 fails at BASE a497d2b2af01 too"}))
	pr := w.s.Primary("s1-1")
	assert.Equal(t, Ready, pr.Col, "an inherited red went to review as the card's failure")
	assert.Equal(t, 0, pr.Int("failed"), "an inherited red counted as a failure")
	assert.Equal(t, "1", pr.F(FieldInheritedAttempts))
	assert.Empty(t, pr.F(FieldFailure))
	assert.Empty(t, w.notesOf(NWorkFailed), "an inherited red raised the card's failure judgment")
	require.Len(t, w.notesOf(NInheritedRed), 1)
	assert.Equal(t, 0, AttemptsSince(pr), "an inherited attempt counts toward the attempt cap")

	// BASE still red on linux: the next attempt waits
	w.part(TickDeal, TickReq{BaseCI: redOn(dbTipA, linuxDebugRed)})
	assert.Nil(t, w.s.Fleet.Card("s1-1.w2"), "the next attempt was dealt onto the red BASE")
}

// The bounds and the stats read the inherited record: attempt 3 after two inherited reds
// is the card's first; a landing on it is first time; the reversed witness without the
// record is neither.
func TestInheritedAttemptsCountTowardNoCapAndNoFirstTimeStat(t *testing.T) {
	t.Parallel()
	c := &Card{ID: "s1-1", Col: Landed, Fields: map[string]string{"attempt": "3",
		FieldRanAttempts: "attempt 1: yes\nattempt 2: yes\nattempt 3: yes", FieldInheritedAttempts: "1,2"}}
	_, at := AtAttemptCap(c, 2)
	assert.False(t, at, "two inherited attempts put the card at a cap of 2")
	assert.True(t, FirstTimeLanded(c), "landed on its first own attempt, after two inherited reds, is not first time")

	c.Fields[FieldInheritedAttempts] = ""
	_, at = AtAttemptCap(c, 2)
	assert.True(t, at, "reversed: without the record, three attempts are at a cap of 2")
	assert.False(t, FirstTimeLanded(c), "reversed: attempt 3 with no inherited record is first time")

	assert.NotEmpty(t, InheritedRedOf("FAIL: TestX fails at BASE too (reproduced on a clean checkout)"))
	assert.NotEmpty(t, InheritedRedOf("HOLD: red at BASE: ubuntu-24.04 / Debug"))
	assert.Empty(t, InheritedRedOf("FAIL: TestX fails; not red at BASE"), "a negated claim is none")
	assert.Empty(t, InheritedRedOf("FAIL: the base already has a failing TestFoo in pkg/x"))
}

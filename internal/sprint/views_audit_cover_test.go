package sprint

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func putCard(tb *Table, id, row, col string, kv ...string) *Card {
	fields := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		fields[kv[i]] = kv[i+1]
	}
	c := &Card{ID: id, Row: row, Col: col, Rev: 1, Fields: fields}
	if row != "" && !tb.HasRow(row) {
		tb.SetRows(append(tb.Rows(), row))
	}
	tb.Put(c)
	return c
}

func TestCardsByAndStreamsViewHelpers(t *testing.T) {
	t.Parallel()
	w := newWorld(t)

	// Populate streams s1 and s2 with cards across states, tiers, repos, and bases
	putCard(w.s.Merge, CtlID("s1"), "s1", Ctl,
		FieldBase, "sprint/v1",
		FieldRelease, "v1.3",
		FieldDisplay, "Stream One",
		StateCol, StreamMerging,
	)
	putCard(w.s.Merge, CtlID("s2"), "s2", Ctl,
		FieldRelease, "v1.4",
		StateCol, StreamMerging,
	)
	putCard(w.s.Work, "s1-1", "s1", Waiting,
		"brief", "s1-1: first card\nREPO: https://github.com/mas-bandwidth/nova-sprint.git\nBASE: sprint/v1\n\nTHE TASK. First sentence here. Second sentence.",
		"tier", "pro",
	)
	putCard(w.s.Work, "s1-2", "s1", Ready,
		"brief", "s1-2: second card\nREPO: mas-bandwidth/nova-tools\n\nTHE TASK: Another task.",
		FieldTierNow, "heavy",
	)
	putCard(w.s.Work, "s2-1", "s2", Landed,
		"brief", "s2-1: stream two\nREPO: mas-bandwidth/nova-sprint\nBASE: main\n\nbody without task",
	)

	// CardRepoAndBase
	rNil, bNil := CardRepoAndBase(w.s, nil)
	assert.Empty(t, rNil)
	assert.Empty(t, bNil)
	repo, base := CardRepoAndBase(w.s, w.s.Work.Card("s1-1"))
	assert.Equal(t, "https://github.com/mas-bandwidth/nova-sprint.git", repo)
	assert.Equal(t, "sprint/v1", base)

	// Fallback to stream control card FieldBase when card brief has no BASE: line
	repo2, base2 := CardRepoAndBase(w.s, w.s.Work.Card("s1-2"))
	assert.Equal(t, "mas-bandwidth/nova-tools", repo2)
	assert.Equal(t, "sprint/v1", base2)

	// anyRepo
	assert.False(t, anyRepo([]string{"mas-bandwidth/other"}, []string{"mas-bandwidth/nova-sprint"}))
	assert.True(t, anyRepo([]string{"https://github.com/mas-bandwidth/nova-sprint.git"}, []string{"mas-bandwidth/nova-sprint"}))

	// StreamTitle
	assert.Equal(t, "First sentence here", StreamTitle(w.s.Work.Card("s1-1").F("brief")))
	assert.Equal(t, "Another task", StreamTitle(w.s.Work.Card("s1-2").F("brief")))
	assert.Equal(t, "", StreamTitle(w.s.Work.Card("s2-1").F("brief")))

	// CardRows, CardsBy, and SortedKeys
	rows := CardRows(w.s, CardsFilter{})
	require.Len(t, rows, 3)

	byTier, ok := CardsBy(rows, "tier")
	require.True(t, ok)
	assert.Equal(t, map[string]int{"pro": 1, "heavy": 1, "flash": 1}, byTier)
	assert.Equal(t, []string{"flash", "heavy", "pro"}, SortedKeys(byTier))

	byStream, ok := CardsBy(rows, "stream")
	require.True(t, ok)
	assert.Equal(t, map[string]int{"s1": 2, "s2": 1}, byStream)

	byCol, ok := CardsBy(rows, "col")
	require.True(t, ok)
	assert.Equal(t, map[string]int{Waiting: 1, Ready: 1, Landed: 1}, byCol)

	byHolder, ok := CardsBy(rows, "holder")
	require.True(t, ok)
	assert.Equal(t, map[string]int{"-": 3}, byHolder)

	_, ok = CardsBy(rows, "unknown")
	assert.False(t, ok)

	// StreamsOf: all streams with Cards, filtered by Repos and Release, and Mixed detection
	viewAll := StreamsOf(w.s, StreamsReq{Cards: true})
	require.Len(t, viewAll.Streams, 2)
	assert.Equal(t, []string{"s1"}, viewAll.Mixed, "s1 spans two repos")
	assert.Equal(t, 2, viewAll.Streams[0].Open)
	assert.Len(t, viewAll.Streams[0].Cards, 2)
	assert.Equal(t, "First sentence here", viewAll.Streams[0].Cards[0].Title)
	assert.Equal(t, 1, viewAll.Streams[1].Landed)

	viewFiltered := StreamsOf(w.s, StreamsReq{Repos: []string{"mas-bandwidth/nova-tools"}, Release: "v1.3"})
	require.Len(t, viewFiltered.Streams, 1)
	assert.Equal(t, "s1", viewFiltered.Streams[0].Stream)

	viewReleaseMiss := StreamsOf(w.s, StreamsReq{Release: "v9.9"})
	assert.Empty(t, viewReleaseMiss.Streams)
}

func TestAuditAndHelpers(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	admitted := stamp(t0.Add(-20 * time.Minute))
	dealt := stamp(t0.Add(-15 * time.Minute))
	taken := stamp(t0.Add(-12 * time.Minute))

	// s1-1: Working on friend amy's row, at attempt 6 with 5 failed/idle/bound/defect attempts,
	// unchanged past Stuck bound, with an unlanded need s1-2 and a missing need s1-gone.
	putCard(w.s.Work, "s1-1", "s1", Working,
		"attempt", "6",
		"admitted", admitted,
		"work", "s1-1.w6",
		"needs", "s1-2,s1-gone,s1-landed",
	)
	putCard(w.s.Work, "s1-2", "s1", Waiting,
		"admitted", stamp(t0.Add(-w.s.DealtMax()-time.Minute)),
	)
	putCard(w.s.Work, "s1-landed", "s1", Landed, "outcome", "landed")

	// 6 attempts with various ends to exercise auditEnd, auditEnds, auditFailCount, auditFails
	putCard(w.s.Fleet, "s1-1.w1", "m1", DoneFailed, "primary", "s1-1", FieldFailure, "gate-failed")
	putCard(w.s.Fleet, "s1-1.w2", "m1", DoneDefect, "primary", "s1-1")
	putCard(w.s.Fleet, "s1-1.w3", "m1", "withdrawn", "primary", "s1-1", FieldFailureBound, "1")
	putCard(w.s.Fleet, "s1-1.w4", "m1", "withdrawn", "primary", "s1-1", "idle", "1")
	putCard(w.s.Fleet, "s1-1.w5", "m1", "ok", "primary", "s1-1", "end", "failed-custom")
	putCard(w.s.Fleet, "s1-1.w6", FriendRow("amy"), Working,
		"primary", "s1-1",
		"dealt", dealt,
		"taken", taken,
	)

	// s2-1: Working with no holder on fleet table
	putCard(w.s.Work, "s2-1", "s2", Working, "attempt", "0")

	res := Audit(w.s, AuditOpts{})
	assert.Equal(t, 3, res.Flagged)

	// Narrow to stream s1
	s1Res := Audit(w.s, AuditOpts{Stream: "s1", Stuck: 5 * time.Minute})
	require.NotEmpty(t, s1Res.Rows)
	r1 := s1Res.Rows[0]
	assert.Equal(t, "s1-1", r1.ID)
	assert.Equal(t, "amy", r1.Holder)
	assert.Equal(t, FriendRow("amy"), r1.Lane)
	assert.Equal(t, taken, r1.Since)
	assert.Equal(t, "12m0s", r1.AgeState)
	assert.Equal(t, "15m0s", r1.AgeAttempt)
	assert.Len(t, r1.Ends, 4, "last four attempts (3..6)")
	assert.Equal(t, []AuditNeed{{ID: "s1-2", State: string(Waiting)}, {ID: "s1-gone", State: "-"}}, r1.Needs)
	assert.Contains(t, r1.Flags, FlagAttempt)
	assert.Contains(t, r1.Flags, FlagStuck)
	assert.Contains(t, r1.Flags, FlagNeedsGone)

	// Make attempts 4 and 6 also count as fail so FlagFails=6 triggers
	w.s.Fleet.Card("s1-1.w4").Fields["end"] = "idle"
	w.s.Fleet.Card("s1-1.w6").Fields["end"] = "failed"
	s1Res2 := Audit(w.s, AuditOpts{Stream: "s1"})
	assert.Contains(t, s1Res2.Rows[0].Flags, FlagFails+"=6")

	// s1-2 is waiting past DealtMax with no holder -> FlagNoHolder
	assert.Contains(t, s1Res.Rows[1].Flags, FlagNoHolder)

	// s2-1 is working with no holder and zero timestamps -> FlagNoHolder, Age "-"
	s2Res := Audit(w.s, AuditOpts{Stream: "s2"})
	require.Len(t, s2Res.Rows, 1)
	assert.Equal(t, "-", s2Res.Rows[0].Holder)
	assert.Equal(t, "-", s2Res.Rows[0].AgeState)
	assert.Contains(t, s2Res.Rows[0].Flags, FlagNoHolder)

	// Direct edge cases on auditEnd and auditAge
	assert.Equal(t, "ok", auditEnd(&Card{Col: "ok"}))
	assert.Equal(t, "withdrawn", auditEnd(&Card{Col: "withdrawn"}))
	assert.Equal(t, "idle", auditEnd(&Card{Col: Ready, Fields: map[string]string{"idle": "yes"}}))
	assert.Equal(t, "0s", auditAge(t0, t0.Add(time.Minute)))
}

func TestCoordinatorReadAndPromoted(t *testing.T) {
	t.Parallel()

	t.Run("CoordinatorRead validates arguments, refuses own worker, and records ok or broken read", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 1)
		// empty IDs
		p := CoordinatorRead(w.s, CoordinatorReadReq{Verdict: "ok"})
		assert.Len(t, p.Refused, 1)

		// bad verdict
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"s1-1"}, Verdict: "maybe"})
		assert.Len(t, p.Refused, 1)

		// missing card and card not in review
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"no-card", "s1-1"}, Verdict: "ok"})
		assert.Len(t, p.Refused, 2)

		// finish s1-1 into Review
		finished(w, "s1-1", false)
		wc := w.s.Fleet.Card("s1-1.w1")

		// card's own worker cannot coordinator-read it
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"s1-1"}, Verdict: "ok", Who: wc.Row})
		assert.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "the card's own worker reads it not")

		// broken without finding is refused
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"s1-1"}, Verdict: "broken", Finding: "   "})
		assert.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "names its defect")

		// ok coordinator read
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"s1-1"}, Verdict: "ok"})
		w.must(p)
		pr := w.s.Work.Card("s1-1")
		assert.Equal(t, 1, coordinatorReadStanding(pr))

		// broken coordinator read raises NReadBroken
		p = CoordinatorRead(w.s, CoordinatorReadReq{IDs: []string{"s1-1"}, Verdict: "broken", Finding: "race in foo.go:12", Who: "rowan"})
		w.must(p)
		assert.Len(t, openOf(w, NReadBroken, "s1-1"), 1)
		assert.Equal(t, 0, coordinatorReadStanding(w.s.Work.Card("s1-1")))
	})

	t.Run("PromotedSha, DevLag.Decisions, and Promoted record merge sha and returned cards", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"promoted", "wait 30m"}, (DevLag{}).Decisions())
		sha, why := PromotedSha("  ABCDEF1234567  ")
		assert.Empty(t, why)
		assert.Equal(t, "abcdef1234567", sha)
		_, why = PromotedSha("not-hex")
		assert.Contains(t, why, "7 to 40 hex digits")

		w := setup(t, 2)
		// non-coordinator refused
		p := Promoted(w.s, PromotedReq{Sha: "abcdef1", Who: "m1"})
		assert.Len(t, p.Refused, 1)

		// invalid sha refused
		p = Promoted(w.s, PromotedReq{Sha: "123", Who: "coordinator"})
		assert.Len(t, p.Refused, 1)

		// returned card not on table or not landed refused
		p = Promoted(w.s, PromotedReq{Sha: "abcdef1", Returned: []string{"no-card"}, Who: "coordinator"})
		assert.Len(t, p.Refused, 1)
		p = Promoted(w.s, PromotedReq{Sha: "abcdef1", Returned: []string{"s1-1"}, Who: "coordinator"})
		assert.Len(t, p.Refused, 1)

		// land s1-1 and open NDevBehind, then record promotion with s1-1 returned
		w.place(w.s.Work, "s1-1", "s1", Landed)
		devBehind := judgment(NDevBehind, "", w.s.Now, 0)
		devBehind.StreamLevel = true
		w.note(devBehind)

		p = Promoted(w.s, PromotedReq{Sha: "abcdef1234", Returned: []string{"s1-1"}, Who: "coordinator"})
		w.must(p)
		gotSha, _ := w.s.Work.Prop(PropPromotedSha)
		assert.Equal(t, "abcdef1234", gotSha)
		assert.Equal(t, "abcdef1234", w.s.Work.Card("s1-1").F(FieldReturnedByDevSha))
		assert.Empty(t, openOf(w, NDevBehind, "sprint"))
	})
}

func TestStepsReviewAndRemainingZeroSuiteHelpers(t *testing.T) {
	t.Parallel()

	t.Run("NeedsSetLive answers a merging card whose need is gone in place", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 2)
		// non-merging card delegates to NeedsSet
		p := NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Clear: true, Who: "coordinator"})
		w.must(p)

		// put s1-2 in Merging with a gone need ("s1-ghost")
		c2 := w.s.Work.Card("s1-2")
		c2.Col = Merging
		c2.Fields["needs"] = "s1-ghost"

		// non-coordinator refused
		p = NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Clear: true, Who: "m1"})
		assert.Len(t, p.Refused, 1)

		// self-need, duplicate need, and missing need refused
		p = NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-2", "s1-1", "s1-1", "no-card"}, Who: "coordinator"})
		assert.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "s1-2 is the card itself")
		assert.Contains(t, p.Refused[0].Why, "s1-1 is named twice")
		assert.Contains(t, p.Refused[0].Why, "no-card is no primary on the table")

		// cycle refused
		w.s.Work.Card("s1-1").Fields["needs"] = "s1-2"
		p = NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-1"}, Who: "coordinator"})
		assert.Len(t, p.Refused, 1)
		delete(w.s.Work.Card("s1-1").Fields, "needs")

		// valid in-place replacement of needs on merging card, then clear
		p = NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"s1-1"}, Who: "coordinator"})
		w.must(p)
		assert.Equal(t, "s1-1", w.s.Work.Card("s1-2").F("needs"))

		w.s.Work.Card("s1-2").Fields["needs"] = "s1-ghost"
		p = NeedsSetLive(w.s, NeedsSetReq{ID: "s1-2", Clear: true, Who: "coordinator"})
		w.must(p)
		assert.Empty(t, w.s.Work.Card("s1-2").F("needs"))
	})

	t.Run("RecordLanded validates reason and card states and delegates to MergeStep", func(t *testing.T) {
		t.Parallel()
		w := setup(t, 2)
		putCard(w.s.Work, "s2-1", "s2", Merging)
		putCard(w.s.Work, "s1-dropped", "", "", "outcome", "dropped", "reason", "obsolete")
		putCard(w.s.Work, "s1-off", "", "", "outcome", "recut")
		putCard(w.s.Work, "s1-landed", "s1", Landed)
		putCard(w.s.Work, "s1-review", "s1", Review)

		// empty reason
		p := RecordLanded(w.s, LandedReq{Sha: "abc1234"})
		assert.Len(t, p.Refused, 1)

		// no pins
		p = RecordLanded(w.s, LandedReq{Sha: "abc1234", Reason: "manual cherry-pick"})
		assert.Len(t, p.Refused, 1)

		// various invalid card states and cross-stream pins
		w.s.Work.Card("s1-1").Col = Merging
		w.s.Work.Card("s1-1").Fields["head"] = "abc1234"
		p = RecordLanded(w.s, LandedReq{
			Sha:    "abc1234",
			Reason: "manual cherry-pick",
			Pins: []LandedPin{
				{ID: "no-card"},
				{ID: "s1-dropped"},
				{ID: "s1-off"},
				{ID: "s1-landed"},
				{ID: "s1-review"},
				{ID: "s1-2"}, // Waiting
				{ID: "s1-1"}, // Merging in s1
				{ID: "s2-1"}, // Merging in s2
			},
		})
		assert.Len(t, p.Refused, 7)

		// valid merging card in s1
		putCard(w.s.Merge, "s1-1", "s1", Queued, "head", "abc1234")
		ctl := w.s.StreamCtl("s1")
		ctl.Fields[StateCol] = StreamMerging
		p = RecordLanded(w.s, LandedReq{
			Sha:    "abc1234",
			Reason: "landed on branch",
			Pins:   []LandedPin{{ID: "s1-1", Head: "abc1234", InBase: true}},
			Who:    "coordinator",
		})
		w.must(p)
		assert.Equal(t, Landed, w.s.Work.Card("s1-1").Col)
	})

	t.Run("returnReadUnit, IdenticalFindings.Why, reviewNoReader, and small pure helpers", func(t *testing.T) {
		t.Parallel()
		w := newWorld(t)
		rc := &Card{ID: "s1-1.r1.amy", Row: FriendRow("amy"), Col: Asked, Rev: 1, Fields: map[string]string{"primary": "s1-1", "stream": "s1", "attempt": "1"}}
		w.s.Fleet.Put(rc)
		u := returnReadUnit(w.s, rc, "going offline", "coordinator")
		assert.Equal(t, Fleet, u.Changes[0].Table)
		assert.True(t, u.Changes[0].Entry.Remove)
		assert.Contains(t, u.Moved, "returned (retired: coordinator returned it)")

		idF := IdenticalFindings{ID: "s1-1", Key: "a.go:1", Attempts: []int{1, 2}, Findings: []string{"bug one", "bug one"}}
		assert.Contains(t, idF.Why(), "s1-1 has failed the same way twice")
		assert.Equal(t, "review has no reader for s1-1, s1-2: the readers table is empty", reviewNoReader([]string{"s1-1", "s1-2"}))

		// lastPushedHead
		pr := &Card{ID: "s1-1", Fields: map[string]string{"attempt": "2"}}
		assert.Equal(t, "", func() string { h, _ := lastPushedHead(&Snapshot{}, pr); return h }())
		putCard(w.s.Fleet, "s1-1.w1", "m1", DoneFailed, "head", "1111111111111111111111111111111111111111", "attempt", "1")
		h, att := lastPushedHead(w.s, pr)
		assert.Equal(t, "1111111111111111111111111111111111111111", h)
		assert.Equal(t, 1, att)

		// MemberRouteKeys
		assert.Nil(t, MemberRouteKeys(nil, "m1"))
		w.s.Routes = []Route{
			{Name: "flash-1", Provider: "google", Enabled: true},
			{Name: "pro-1", Provider: "anthropic", Enabled: false},
		}
		assert.Equal(t, []PreflightRouteKey{{Route: "flash-1", Key: "GOOGLE_API_KEY"}}, MemberRouteKeys(w.s, "m1"))

		// FriendBeatNamesJob and openJudgmentOn
		wc := &Card{ID: "s1-1.w1", Fields: map[string]string{"primary": "s1-1", "gen": "2"}}
		assert.False(t, FriendBeatNamesJob(Beat{}, wc, 3))
		assert.True(t, FriendBeatNamesJob(Beat{Friend: &FriendReport{Running: []string{"s1-1.w1~3.g2"}}}, wc, 3))
		assert.False(t, openJudgmentOn(w.s, "s1-1"))
		w.note(judgment(NWorkFailed, "s1", w.s.Now, 0, "s1-1"))
		assert.True(t, openJudgmentOn(w.s, "s1-1"))

		// LiveHeldAt, LiveNamedAt, Beat.SaysDown
		b := Beat{
			At:        t0,
			LiveKnown: true,
			Live:      []string{"s1-1.w1@2:held"},
			Friend:    &FriendReport{Until: t0.Add(time.Hour)},
		}
		assert.True(t, b.SaysDown())
		assert.True(t, LiveHeldAt(b, "s1-1.w1", 2, t0))
		assert.True(t, LiveNamedAt(b, "s1-1.w1", 2))
		assert.False(t, LiveNamedAt(Beat{}, "s1-1.w1", 2))

		// Need.Evidence
		need := Need{Kind: "judgment", ID: "j1", Cards: []string{"c1", "c2", "c3", "c4", "c5", "c6", "c7", "c8", "c9", "c10", "c11"}, Paths: []string{"p1"}}
		assert.Contains(t, need.Evidence(), "judgment j1: cards c1,c2,c3")
		assert.Contains(t, need.Evidence(), "; evidence p1")

		// refusalSaid
		ctlCard := &Card{Fields: map[string]string{FieldConflictPaths: "a.go"}}
		assert.Equal(t, "a conflict in a.go, no ledger", refusalSaid(RefusedConflict, ctlCard))
		assert.Equal(t, "files outside its PATHS", refusalSaid(RefusedPaths, ctlCard))
		assert.Equal(t, "the lander's checks failed", refusalSaid(RefusedChecks, ctlCard))
		assert.Equal(t, "the merged tree fails the tree gate", refusalSaid("other", ctlCard))

		// PushRecord.AdapterName, PushSetup, NotPushTarget
		assert.Equal(t, "-", (PushRecord{}).AdapterName())
		assert.Equal(t, "codex", (PushRecord{Harness: "codex"}).AdapterName())
		assert.Equal(t, "folder", (PushRecord{Harness: "claude", Adapter: "folder"}).AdapterName())
		assert.Contains(t, PushSetup("amy", PushRecord{}, false), "--harness <harness> --target <session dir>")
		assert.Contains(t, PushSetup("amy", PushRecord{Harness: "codex", Target: "/dir"}, true), "--harness codex --target /dir")
		assert.Contains(t, NotPushTarget(PushRecord{Name: "bad name"}), "wants --actor")
		assert.Contains(t, NotPushTarget(PushRecord{Name: "amy"}), "--harness <name> is required")
		assert.Contains(t, NotPushTarget(PushRecord{Name: "amy", Harness: "codex"}), "--target <dir> is required")
		assert.Empty(t, NotPushTarget(PushRecord{Name: "amy", Harness: "codex", Target: "/dir"}))

		// drainNote, landWord, FinishedAs, gcPass.fail
		dn := drainNote(w.s, Note{}, "machine", "drained")
		assert.Equal(t, "coordinator", dn.To)
		assert.Equal(t, DoneOK, landWord(&Card{Fields: map[string]string{"ok": "yes", "head": "abc"}}))
		assert.Equal(t, "", landWord(&Card{Fields: map[string]string{"ok": "no"}}))
		assert.Equal(t, "", FinishedAs(Line{Table: Work}))
		assert.Equal(t, DoneFailed, FinishedAs(Line{Table: Fleet, From: "m1:working", To: "m1:finished", Set: map[string]string{"ok": "no"}}))
		assert.Equal(t, DoneOK, FinishedAs(Line{Table: Fleet, From: "m1:working", To: "m1:finished", Set: map[string]string{"ok": "yes"}}))
		assert.Equal(t, DoneOK, FinishedAs(Line{Table: Fleet, From: "m1:working", To: "m1:ok"}))
		assert.Equal(t, "", FinishedAs(Line{Table: Fleet, From: "m1:finished", To: "m1:ok"}))

		gp := &gcPass{}
		gp.begin("worktrees")
		gp.fail("/bad/path", errors.New("permission denied"))
		assert.Equal(t, 1, gp.res.Failed)
		assert.Equal(t, 1, gp.class.Failed)

		// WithReadCards, TierBlockedStreams, ServerBase.Refusal, OffBaseError.Error, LandError.Error
		sOut, pOut := w.s.WithReadCards(nil)
		assert.Equal(t, w.s, sOut)
		assert.Empty(t, pOut.Units)
		assert.Empty(t, w.s.TierBlockedStreams())

		sb := ServerBase{Base: "v1.3/comms", Commit: "1234567890abcdef", Tip: "fedcba0987654321", Why: "behind tip"}
		assert.Contains(t, (&OffBaseError{Binary: "/bin/nova-sprint", Check: sb}).Error(), "commit 1234567890ab")
		assert.Contains(t, (ServerBase{Base: "v1.3/comms", Tip: "fedcba0987654321", Why: "no vcs"}).Refusal("/bin/nova-sprint"), "no source commit")
		assert.Equal(t, "merge failed", (&LandError{Msg: "merge failed", Code: 2}).Error())

		// holdElsewhere, markReturnedFor, failedSinceAck, fleetFailureOpen
		assert.Empty(t, holdElsewhere("x", nil, false))
		assert.Contains(t, holdElsewhere("m1", []string{HoldMember}, false), "fleet hold m1 --reason <text>")
		assert.Contains(t, holdElsewhere("s1", []string{HoldStream}, true), "unhold s1")

		var retPlan Plan
		markReturnedFor(&retPlan, pr, "needs")
		assert.Len(t, retPlan.Units, 1)
		assert.Equal(t, "needs", retPlan.Units[0].Changes[0].Entry.Set[FieldReturnedFor])
		markReturnedFor(&retPlan, pr, "needs-2")
		assert.Equal(t, "needs-2", retPlan.Units[0].Changes[0].Entry.Set[FieldReturnedFor])

		assert.True(t, failedSinceAck(w.s, "harness-fault", []string{"s1-1"}))
		assert.False(t, fleetFailureOpen(w.s, "harness-fault"))
		w.s.Open = append(w.s.Open, Open{Note: Note{Kind: Judgment, Type: NFleetFailure, What: fleetFailureWhat("harness-fault") + ": 3 cards"}})
		assert.True(t, fleetFailureOpen(w.s, "harness-fault"))
		w.s.Acked = append(w.s.Acked, Open{Note: Note{Kind: Judgment, Type: NFleetFailure, What: fleetFailureWhat("harness-fault") + ": 3 cards", At: w.s.Now.Add(10_000_000_000)}})
		assert.False(t, failedSinceAck(w.s, "harness-fault", []string{"s1-1"}))
		putCard(w.s.Work, "s1-1", "s1", Review, FieldFinishedAt, w.s.Now.Add(20_000_000_000).Format("2006-01-02T15:04:05Z"))
		assert.True(t, failedSinceAck(w.s, "harness-fault", []string{"s1-1"}))
	})
}

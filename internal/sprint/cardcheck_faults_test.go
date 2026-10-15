package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func emptyFixture() *Snapshot {
	return &Snapshot{
		Now:         time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC),
		Work:        NewTable(Work),
		Readers:     NewTable(Readers),
		Merge:       NewTable(Merge),
		Fleet:       NewTable(Fleet),
		Coordinator: "coordinator",
		Actor:       "coordinator",
		Providers:   map[string]Provider{"fixture": {Name: "fixture", Billing: "plan", Funded: false}},
	}
}

func addTestStream(s *Snapshot, stream, display string) {
	s.Merge.Put(&Card{ID: CtlID(stream), Row: stream, Col: Ctl, Fields: map[string]string{
		"kind": "stream", FieldDisplay: display, FieldStreamBranch: "sprint/" + stream, FieldTrunk: "dev"}})
}

func TestCheckSentinelWide(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "schema-shared", "Schema Shared")
	addTestStream(s, "schema-js", "Schema JS")

	// Stream schema-shared: producer p1 and sentinel s1
	p1Brief := "KIND: primary\nPATHS: internal/codegen/go/gen.go\nTEST: none reason\n"
	s1Brief := "KIND: sentinel\nCONSUMERS: c1\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"
	p1 := &Card{ID: "p1", Row: "schema-shared", Col: Ready, Score: 10.0, Fields: map[string]string{"brief": p1Brief}}
	s1 := &Card{ID: "s1", Row: "schema-shared", Col: Waiting, Score: 20.0, Fields: map[string]string{
		"kind": "sentinel", "brief": s1Brief, "needs": "p1"}}

	// Stream schema-js: consumer c1 needs sentinel s1
	// c1 writes only js files, sharing no non-ledger glob with p1
	c1Brief := "KIND: primary\nPATHS: internal/codegen/js/gen.js\nTEST: none reason\n"
	c1 := &Card{ID: "c1", Row: "schema-js", Col: Waiting, Score: 100.0, Fields: map[string]string{"brief": c1Brief, "needs": "s1"}}

	s.Work.Put(p1)
	s.Work.Put(s1)
	s.Work.Put(c1)

	fs := CheckCards(s, CardCheckIn{})
	wide := cardFindingsOf(fs, CheckSentinelWide)

	require.Len(t, wide, 1, "expected 1 sentinel-wide warning, got: %+v", fs)
	w := wide[0]
	assert.Equal(t, "s1", w.Subject)
	assert.Equal(t, SeverityWarn, w.Severity)
	assert.Equal(t, "sentinel s1", w.Serialized)
	assert.Contains(t, w.Finding, "waits on 1 producer(s) sharing no PATHS/NEW glob with consumer c1: p1")
	assert.Equal(t, "move p1 after s1, or narrower sentinel", w.Remedy)

	// Now if p1 also wrote a shared file with c1:
	p1SharedBrief := "KIND: primary\nPATHS: internal/codegen/shared/common.go,internal/codegen/js/gen.js\nTEST: none reason\n"
	p1.Fields["brief"] = p1SharedBrief

	fsClean := CheckCards(s, CardCheckIn{})
	wideClean := cardFindingsOf(fsClean, CheckSentinelWide)
	assert.Empty(t, wideClean, "expected 0 sentinel-wide findings when producer shares glob with consumer")
}

func TestCheckSentinelConsumers(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "schema-shared", "Schema Shared")
	addTestStream(s, "schema-a", "Schema A")
	addTestStream(s, "schema-b", "Schema B")

	// Sentinel with CONSUMERS header that only names c1
	s1Brief := "KIND: sentinel\nCONSUMERS: c1\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"
	s1 := &Card{ID: "s1", Row: "schema-shared", Col: Waiting, Score: 20.0, Fields: map[string]string{"kind": "sentinel", "brief": s1Brief}}

	// Consumer c1 and consumer c2 both need s1
	c1Brief := "KIND: primary\nPATHS: a.go\nTEST: none reason\n"
	c1 := &Card{ID: "c1", Row: "schema-a", Col: Waiting, Score: 30.0, Fields: map[string]string{"brief": c1Brief, "needs": "s1"}}
	c2Brief := "KIND: primary\nPATHS: b.go\nTEST: none reason\n"
	c2 := &Card{ID: "c2", Row: "schema-b", Col: Waiting, Score: 40.0, Fields: map[string]string{"brief": c2Brief, "needs": "s1"}}

	s.Work.Put(s1)
	s.Work.Put(c1)
	s.Work.Put(c2)

	fs := CheckCards(s, CardCheckIn{})
	scFindings := cardFindingsOf(fs, CheckSentinelConsumers)

	require.Len(t, scFindings, 1, "expected 1 sentinel-consumers refusal for missing c2: %+v", fs)
	f := scFindings[0]
	assert.Equal(t, "s1", f.Subject)
	assert.Equal(t, SeverityRefuse, f.Severity)
	assert.Contains(t, f.Finding, "CONSUMERS line missing c2")
	assert.Contains(t, f.Remedy, "nova-sprint brief s1 --brief-file <a brief naming every consumer: CONSUMERS: c1, c2>")

	// Fix the brief to declare both consumers (with optional stream parens)
	s1.Fields["brief"] = "KIND: sentinel\nCONSUMERS: c1 (schema-a), c2 (schema-b)\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"
	fsFixed := CheckCards(s, CardCheckIn{})
	scFixed := cardFindingsOf(fsFixed, CheckSentinelConsumers)
	assert.Empty(t, scFixed, "expected 0 sentinel-consumers findings after brief is updated")
}

func TestNeedsSetSentinelConsumersEnforced(t *testing.T) {
	t.Parallel()
	w := setup(t, 2) // s1-1, s1-2

	// Add sentinel stop in stream s2 with CONSUMERS naming only s1-1
	sBrief := "KIND: sentinel\nCONSUMERS: s1-1\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"
	w.must(Add(w.s, AddReq{Stream: "s2", IDs: []string{"stop"}, Sentinel: true, Brief: sBrief, Who: "coordinator"}))

	// s1-2 tries to set need on stop, but is omitted from stop's CONSUMERS line
	p := NeedsSet(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"stop"}, Who: "coordinator"})
	require.Len(t, p.Refused, 1, "expected NeedsSet to refuse when consumer omitted from sentinel CONSUMERS line")
	assert.Contains(t, p.Refused[0].Why, "CONSUMERS line omits s1-2")

	// Update sentinel's brief to include s1-2
	w.s.Work.Card("stop").Fields["brief"] = "KIND: sentinel\nCONSUMERS: s1-1, s1-2\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"

	// Now s1-2 setting need on stop succeeds
	pOK := NeedsSet(w.s, NeedsSetReq{ID: "s1-2", Needs: []string{"stop"}, Who: "coordinator"})
	require.Empty(t, pOK.Refused, "NeedsSet should succeed when consumer is declared in CONSUMERS")
	w.must(pOK)
	assert.Equal(t, "stop", w.s.Work.Card("s1-2").F("needs"))
}

func TestCheckOwner(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "s1", "Stream One")

	// Case 1: card has who=friend but brief has no WHO: line
	c1Brief := "KIND: primary\nPATHS: c1.go\nTEST: none reason\n"
	c1 := &Card{ID: "c1", Row: "s1", Col: Waiting, Score: 10.0, Fields: map[string]string{"who": "friend", "brief": c1Brief}}

	// Case 2: card has who=friend, brief has WHO: friend, but attempts ran on fleet members
	c2Brief := "KIND: primary\nWHO: friend\nPATHS: c2.go\nTEST: none reason\n"
	c2 := &Card{ID: "c2", Row: "s1", Col: Waiting, Score: 20.0, Fields: map[string]string{
		"who": "friend", "brief": c2Brief, "attempt": "2", "routes": "flash-a,flash-b"}}

	// Case 3: valid fleet card
	c3Brief := "KIND: primary\nPATHS: c3.go\nTEST: none reason\n"
	c3 := &Card{ID: "c3", Row: "s1", Col: Waiting, Score: 30.0, Fields: map[string]string{"who": "fleet", "brief": c3Brief}}

	// Case 4: valid friend card, no fleet attempts
	c4Brief := "KIND: primary\nWHO: friend\nPATHS: c4.go\nTEST: none reason\n"
	c4 := &Card{ID: "c4", Row: "s1", Col: Waiting, Score: 40.0, Fields: map[string]string{"who": "friend", "brief": c4Brief}}

	s.Work.Put(c1)
	s.Work.Put(c2)
	s.Work.Put(c3)
	s.Work.Put(c4)

	fs := CheckCards(s, CardCheckIn{})
	ownerFindings := cardFindingsOf(fs, CheckOwner)

	require.Len(t, ownerFindings, 2, "expected 2 owner findings, got: %+v", ownerFindings)

	// c1: lacking WHO line
	assert.Equal(t, "c1", ownerFindings[0].Subject)
	assert.Equal(t, SeverityRefuse, ownerFindings[0].Severity)
	assert.Equal(t, "who=friend but brief has no WHO: line", ownerFindings[0].Finding)
	assert.Equal(t, "nova-sprint card c1 --who fleet (or set WHO: line in brief)", ownerFindings[0].Remedy)

	// c2: attempts ran on fleet members
	assert.Equal(t, "c2", ownerFindings[1].Subject)
	assert.Equal(t, SeverityRefuse, ownerFindings[1].Severity)
	assert.Equal(t, "who=friend but attempts ran on fleet members: flash-a,flash-b", ownerFindings[1].Finding)
	assert.Equal(t, "nova-sprint card c2 --who fleet (or set WHO: line in brief)", ownerFindings[1].Remedy)
}

func TestFriendCardCorrectedOwnerInDealAndRules(t *testing.T) {
	t.Parallel()

	// Once owner is corrected to fleet, FriendCard returns false
	c1 := &Card{ID: "c1", Fields: map[string]string{"who": "fleet", "brief": "KIND: primary\nPATHS: a.go\n"}}
	name, ok := FriendCard(c1)
	assert.False(t, ok, "FriendCard should return false when who=fleet")
	assert.Empty(t, name)

	// Legitimate friend card
	c3 := &Card{ID: "c3", Fields: map[string]string{"who": "friend", "brief": "KIND: primary\nWHO: friend\nPATHS: a.go\n"}}
	name, ok = FriendCard(c3)
	assert.True(t, ok)
	assert.Empty(t, name)

	// In ruleFailed: failed card whose owner is corrected to fleet is reworked, not parked
	s := emptyFixture()
	addTestStream(s, "s1", "Stream One")

	cFailed := &Card{ID: "failed-card", Row: "s1", Col: Review, Fields: map[string]string{
		"who": "fleet", "brief": "KIND: primary\nWHO: friend\nPATHS: a.go\n", "attempt": "1", "routes": "opencode-space", "result": "failed"}}
	s.Work.Put(cFailed)

	ans := &RuleAnswer{Subject: "failed-card"}
	ruleFailed(s, ans)

	// Because owner is corrected to fleet, FriendCard returns false, and ruleFailed reworks it
	assert.NotContains(t, ans.Why, "a friend's card: this work needs a mind")
	assert.Equal(t, ActRework, ans.Act)
}

func TestCheckFailedInReview(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "s1", "Stream One")

	c1 := &Card{ID: "c1", Row: "s1", Col: Review, Fields: map[string]string{
		"attempt": "3", "result": "failed", "brief": "KIND: primary\nPATHS: a.go\nTEST: none reason\n"}}
	c2 := &Card{ID: "c2", Row: "s1", Col: Review, Fields: map[string]string{
		"attempt": "6", "last_finish": "6:failed", "brief": "KIND: primary\nPATHS: b.go\nTEST: none reason\n"}}
	c3OK := &Card{ID: "c3", Row: "s1", Col: Review, Fields: map[string]string{
		"attempt": "1", "result": "ok", "brief": "KIND: primary\nPATHS: c.go\nTEST: none reason\n"}}
	c4Working := &Card{ID: "c4", Row: "s1", Col: Working, Fields: map[string]string{
		"attempt": "1", "result": "failed", "brief": "KIND: primary\nPATHS: d.go\nTEST: none reason\n"}}

	s.Work.Put(c1)
	s.Work.Put(c2)
	s.Work.Put(c3OK)
	s.Work.Put(c4Working)

	fs := CheckCards(s, CardCheckIn{})
	failFindings := cardFindingsOf(fs, CheckFailedInReview)

	require.Len(t, failFindings, 2, "expected 2 failed-in-review findings: %+v", fs)
	assert.Equal(t, "c1", failFindings[0].Subject)
	assert.Equal(t, SeverityRefuse, failFindings[0].Severity)
	assert.Equal(t, "attempt 3 failed and remains in review", failFindings[0].Finding)
	assert.Contains(t, failFindings[0].Remedy, "rework, redo or drop: nova-sprint rework c1")

	assert.Equal(t, "c2", failFindings[1].Subject)
	assert.Equal(t, SeverityRefuse, failFindings[1].Severity)
	assert.Equal(t, "attempt 6 failed and remains in review", failFindings[1].Finding)
	assert.Contains(t, failFindings[1].Remedy, "rework, redo or drop: nova-sprint rework c2")
}

func TestDoctorFailedInReview(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "s1", "Stream One")

	// Case 1: no failed card in review -> OK
	inOK := DoctorInput{Snap: s}
	outOK := DoctorFailedInReview(inOK)
	require.Len(t, outOK, 1)
	assert.Equal(t, DoctorOK, outOK[0].Status)
	assert.Equal(t, "all", outOK[0].Subject)

	// Case 2: card in review with result failed
	c := &Card{ID: "c-failed", Row: "s1", Col: Review, Fields: map[string]string{
		"attempt": "6", "result": "failed", "head": "e802cb56a9395e30d59b9775632637df22080318"}}
	s.Work.Put(c)

	inFail := DoctorInput{Snap: s}
	outFail := DoctorFailedInReview(inFail)
	require.Len(t, outFail, 1)
	assert.Equal(t, DoctorFail, outFail[0].Status)
	assert.Equal(t, "c-failed", outFail[0].Subject)
	assert.Contains(t, outFail[0].Evidence, "attempt=6")
	assert.Contains(t, outFail[0].Evidence, "result=failed")
	assert.Contains(t, outFail[0].Evidence, "head=e802cb56a939")
	assert.Equal(t, "nova-sprint rework c-failed", outFail[0].Remedy)

	// Check Doctor(in) incorporates it
	allChecks := Doctor(inFail)
	found := false
	for _, ch := range allChecks {
		if ch.Check == CheckFailedInReview && ch.Subject == "c-failed" {
			found = true
			break
		}
	}
	assert.True(t, found, "Doctor(in) must include DoctorFailedInReview check")
}

func TestLiveAudit20261013Incident(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "schema-shared", "Schema Shared")
	addTestStream(s, "schema-c", "Schema C")
	addTestStream(s, "schema-dart", "Schema Dart")
	addTestStream(s, "schema-elixir", "Schema Elixir")
	addTestStream(s, "schema-java", "Schema Java")
	addTestStream(s, "schema-rust", "Schema Rust")
	addTestStream(s, "schema-js", "Schema JS")

	// 1. Wide sentinel with omitted consumers
	sentinel := &Card{
		ID:    "sentinel-schema-shared-guard-chain-ready",
		Row:   "schema-shared",
		Col:   Waiting,
		Score: 902.0,
		Fields: map[string]string{
			"kind":  "sentinel",
			"needs": "the-guard-chain-answers-the-outer-tag",
			"brief": "KIND: sentinel\nPATHS: docs/roadmap.sexp\nTEST: none reason\nCONSUMERS: js-carries-maps-and-union-arms-on-the-table-wire (schema-js)\n",
		},
	}
	producer := &Card{ID: "the-guard-chain-answers-the-outer-tag", Row: "schema-shared", Col: Ready, Score: 800.0, Fields: map[string]string{
		"brief": "KIND: primary\nPATHS: internal/codegen/shared/tag.go\nTEST: none reason\n"}}
	s.Work.Put(sentinel)
	s.Work.Put(producer)

	consumerIDs := []string{
		"a-c-card-round-trips-and-renders-the-shared-corpus",
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"elixir-carries-the-variable-class-beyond-the-card-constructs",
		"java-carries-the-variable-class-beyond-the-card-constructs",
		"rust-builds-blocks-and-cooks-in-the-runtime",
		"fixed-js-t12-guards-and-wide-text-6",
		"js-builds-blocks-and-cooks-in-the-runtime",
		"js-carries-maps-and-union-arms-on-the-table-wire",
	}
	consumerStreams := []string{
		"schema-c", "schema-dart", "schema-elixir", "schema-java", "schema-rust", "schema-js", "schema-js", "schema-js",
	}
	consumerPaths := []string{
		"internal/codegen/c/corpus.c",
		"internal/codegen/dart/variable.dart",
		"internal/codegen/elixir/variable.ex",
		"internal/codegen/java/Variable.java",
		"internal/codegen/rust/runtime.rs",
		"internal/codegen/js/guards.js",
		"internal/codegen/js/runtime.js",
		"internal/codegen/js/maps.js",
	}

	for i, id := range consumerIDs {
		c := &Card{ID: id, Row: consumerStreams[i], Col: Waiting, Score: float64(100 + i), Fields: map[string]string{
			"needs": sentinel.ID, "brief": "KIND: primary\nPATHS: " + consumerPaths[i] + "\nTEST: none reason\n"}}
		// The two friend cards in waiting with who=friend but no WHO: line
		if id == "dart-carries-the-variable-class-beyond-the-card-constructs" || id == "java-carries-the-variable-class-beyond-the-card-constructs" {
			c.Fields["who"] = "friend"
		}
		s.Work.Put(c)
	}

	// 2. Failed card in review that ran on fleet
	failedCard := &Card{
		ID:    "the-fixed-form-corpus-builds-on-every-bench",
		Row:   "schema-shared",
		Col:   Review,
		Score: 83.0,
		Fields: map[string]string{
			"attempt":     "6",
			"result":      "failed",
			"last_finish": "6:failed",
			"who":         "friend",
			"routes":      "opencode-space,opencode-space,opencode-space,opencode-space,opencode-space,opencode-space",
			"head":        "e802cb56a9395e30d59b9775632637df22080318",
			"brief":       "KIND: primary\nPATHS: fixedversioning_test.go\nTEST: none reason\n",
		},
	}
	s.Work.Put(failedCard)

	// Check findings before fixes
	findings := CheckCards(s, CardCheckIn{})

	byKind := map[string][]CardFinding{}
	for _, f := range findings {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}

	// Finding 1: sentinel-wide warnings for the consumers sharing no paths with producer
	require.NotEmpty(t, byKind[CheckSentinelWide], "expected sentinel-wide warnings")
	assert.Equal(t, SeverityWarn, byKind[CheckSentinelWide][0].Severity)

	// Finding 2: sentinel-consumers refusal naming the 7 omitted consumers
	require.NotEmpty(t, byKind[CheckSentinelConsumers], "expected sentinel-consumers refusal")
	assert.Equal(t, SeverityRefuse, byKind[CheckSentinelConsumers][0].Severity)
	assert.Contains(t, byKind[CheckSentinelConsumers][0].Finding, "CONSUMERS line missing")

	// Finding 3: owner refusals on dart, java (who=friend but no WHO line) and the-fixed-form-corpus (who=friend but ran on fleet)
	require.Len(t, byKind[CheckOwner], 3, "expected 3 owner findings: %+v", byKind[CheckOwner])
	for _, f := range byKind[CheckOwner] {
		assert.Equal(t, SeverityRefuse, f.Severity)
	}

	// Finding 4: failed-in-review refusal on the-fixed-form-corpus
	require.Len(t, byKind[CheckFailedInReview], 1, "expected 1 failed-in-review finding")
	assert.Equal(t, "the-fixed-form-corpus-builds-on-every-bench", byKind[CheckFailedInReview][0].Subject)
	assert.Equal(t, SeverityRefuse, byKind[CheckFailedInReview][0].Severity)

	// Doctor check flags the failed card in review
	doc := DoctorFailedInReview(DoctorInput{Snap: s})
	require.Len(t, doc, 1)
	assert.Equal(t, DoctorFail, doc[0].Status)
	assert.Equal(t, "the-fixed-form-corpus-builds-on-every-bench", doc[0].Subject)

	// --- Now apply fixes 2-4 ---
	// Fix 2 & 4: update sentinel brief with all consumers
	sentinel.Fields["brief"] = "KIND: sentinel\nPATHS: docs/roadmap.sexp\nTEST: none reason\nCONSUMERS: " + strings.Join(consumerIDs, ", ") + "\n"

	// Fix 3: set who=fleet on dart, java, and the failed card
	s.Work.Card("dart-carries-the-variable-class-beyond-the-card-constructs").Fields["who"] = "fleet"
	s.Work.Card("java-carries-the-variable-class-beyond-the-card-constructs").Fields["who"] = "fleet"
	failedCard.Fields["who"] = "fleet"

	// Fix 4: rework the failed card (moves review -> ready, resets result)
	failedCard.Col = Ready
	failedCard.Fields["result"] = ""
	failedCard.Fields["attempt"] = "7"
	delete(failedCard.Fields, "last_finish")

	// Re-check after fixes
	findingsAfter := CheckCards(s, CardCheckIn{})
	byKindAfter := map[string][]CardFinding{}
	for _, f := range findingsAfter {
		byKindAfter[f.Kind] = append(byKindAfter[f.Kind], f)
	}

	assert.Empty(t, byKindAfter[CheckSentinelConsumers], "all consumers now declared")
	assert.Empty(t, byKindAfter[CheckOwner], "owners corrected")
	assert.Empty(t, byKindAfter[CheckFailedInReview], "failed card reworked")

	docAfter := DoctorFailedInReview(DoctorInput{Snap: s})
	require.Len(t, docAfter, 1)
	assert.Equal(t, DoctorOK, docAfter[0].Status)
}

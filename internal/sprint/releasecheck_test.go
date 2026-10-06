package sprint

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRelease is the release check's facts as a test builds them.
type fakeRelease struct {
	now  time.Time
	snap *Snapshot
}

func (f fakeRelease) Now() time.Time      { return f.now }
func (f fakeRelease) Snapshot() *Snapshot { return f.snap }

// auditWorld is readers r1..r4, up, and n primaries landed after since, each
// read at attempt 1 by r1 (its review), its work done by member m1, and five
// more landed before since.
func auditWorld(t *testing.T, since time.Time, n int) *world {
	t.Helper()
	w := newWorld(t, "r1", "r2", "r3", "r4")
	w.s.ReaderStates = map[string]string{"r1": ReaderUp, "r2": ReaderUp, "r3": ReaderUp, "r4": ReaderUp}
	w.s.Work.SetRows([]string{"s1"})
	w.s.Fleet.SetRows([]string{"m1", FriendRow("r3")})
	land := func(id string, at time.Time, score float64) {
		w.s.Work.Put(&Card{ID: id, Row: "s1", Col: Landed, Score: score, Rev: 1, Fields: map[string]string{
			"kind": "primary", "stream": "s1", "attempt": "1", "head": "h-" + id, "base": "main", "landed": stamp(at), "asked": "r1"}})
		w.s.Readers.Put(&Card{ID: ReadCardID(id, 1, "r1"), Row: "r1", Col: OK, Score: score, Rev: 1, Fields: map[string]string{
			"kind": "read", "primary": id, "attempt": "1", "reader": "r1", "head": "h-" + id}})
		w.s.Fleet.Put(&Card{ID: WorkCardID(id, 1), Rev: 1, Fields: map[string]string{"kind": "work", "primary": id, "attempt": "1", "member": "m1"}})
	}
	for i := 1; i <= 5; i++ {
		land(fmt.Sprintf("old-%d", i), since.Add(-time.Duration(i)*time.Hour), float64(i))
	}
	for i := 1; i <= n; i++ {
		land(fmt.Sprintf("c%02d", i), since.Add(time.Duration(i)*time.Minute), float64(10+i))
	}
	return w
}

// The cold audit: --audit samples 20 cards landed since the last release with a
// seed it records, so the sample redraws; asks each, through the ask's room,
// round and route, of a reader up that never saw it (not its reviewer, not its
// author), refusing when there is none; and the plain check is ok only while
// the audit is under 48 hours old and each of its own reads came back ok,
// naming each broken read with its finding and each unanswered. The card's
// review reads, and an earlier audit's, are never counted.
func TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk(t *testing.T) {
	t.Parallel()
	since := t0.Add(-24 * time.Hour)
	req := ColdAuditReq{Seed: 42, Since: "v0.9.0", SinceTime: since, Who: "coordinator"}
	check := func(w *world) ReleaseResult { return ColdAudit(fakeRelease{now: w.s.Now, snap: w.s}) }

	t.Run("no audit fails", func(t *testing.T) {
		w := auditWorld(t, since, 30)
		r := check(w)
		assert.False(t, r.OK)
		assert.Contains(t, r.Evidence, "no cold audit has been asked; run: nova-sprint release check --audit")
	})

	t.Run("fewer than 20 landed since refuses", func(t *testing.T) {
		w := auditWorld(t, since, 19)
		p, _ := PlanColdAudit(w.s, req)
		require.Len(t, p.Refused, 1)
		assert.Contains(t, p.Refused[0].Why, "19 cards landed since v0.9.0")
		assert.Empty(t, p.Units)
		assert.Empty(t, p.Props)
	})

	t.Run("the review reads alone are not the audit", func(t *testing.T) {
		// the reader's probe: an audit recorded, no cold read placed, each card's
		// review read ok: not ok, every card unanswered
		w := auditWorld(t, since, 30)
		p, rec := PlanColdAudit(w.s, req)
		require.Empty(t, p.Refused)
		b, _ := json.Marshal(rec)
		w.s.Work.SetProp(PropColdAudit, string(b))
		r := check(w)
		assert.False(t, r.OK, r.Evidence)
		assert.Contains(t, r.Evidence, "20 unanswered")
	})

	t.Run("no reader up that never saw it refuses the audit", func(t *testing.T) {
		// the reader's probe: every reader up saw every card: nothing asked
		w := auditWorld(t, since, 30)
		w.s.ReaderStates = map[string]string{"r1": ReaderUp, "r2": ReaderAway, "r3": ReaderDown, "r4": ReaderAway}
		p, _ := PlanColdAudit(w.s, req)
		require.Len(t, p.Refused, 20)
		assert.Empty(t, p.Units, "all or nothing")
		assert.Empty(t, p.Props, "no audit recorded")
		assert.Contains(t, p.Refused[0].Why, "no reader up that never saw it has room")
		assert.Contains(t, p.Refused[0].Why, "saw it: m1,r1")
	})

	t.Run("the author is not a cold reader", func(t *testing.T) {
		// r3 is the friend whose row did the work of every card: up, it is never asked
		w := auditWorld(t, since, 30)
		for _, c := range w.s.Fleet.cards {
			c.Fields["member"] = FriendRow("r3")
		}
		w.s.ReaderStates = map[string]string{"r1": ReaderUp, "r2": ReaderDown, "r3": ReaderUp, "r4": ReaderDown}
		p, _ := PlanColdAudit(w.s, req)
		require.Len(t, p.Refused, 20)
		assert.Contains(t, p.Refused[0].Why, "saw it: friend.r3,r1,r3")
	})

	t.Run("a reader at width is given nothing", func(t *testing.T) {
		w := auditWorld(t, since, 30)
		w.s.Readers.SetRows([]string{"r1", "reader-m2", "reader-m3"})
		w.s.ReaderStates = map[string]string{"r1": ReaderUp, "reader-m2": ReaderUp, "reader-m3": ReaderUp}
		w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m2", Width: 2}))
		w.must(FleetStep(w.s, FleetReq{Op: "up", Member: "m3", Width: 18}))
		p, _ := PlanColdAudit(w.s, req)
		require.Empty(t, p.Refused)
		w.must(p)
		assert.Equal(t, 2, w.s.readerLoad("reader-m2"))
		assert.Equal(t, 18, w.s.readerLoad("reader-m3"))
	})

	t.Run("the level leaves an audit read with its cold reader", func(t *testing.T) {
		// r2 cold-reads every card; r3 and r4 are up with room: the level moves nothing
		w := auditWorld(t, since, 30)
		w.s.ReaderStates = map[string]string{"r1": ReaderUp, "r2": ReaderUp, "r3": ReaderDown, "r4": ReaderDown}
		p, rec := PlanColdAudit(w.s, req)
		require.Empty(t, p.Refused)
		w.must(p)
		w.s.ReaderStates = map[string]string{"r1": ReaderUp, "r2": ReaderUp, "r3": ReaderUp, "r4": ReaderUp}
		lp, _ := TickLevelReads(w.s, TickReq{})
		assert.Empty(t, lp.Units)
		for _, rd := range rec.Reads {
			rc := w.s.Readers.Card(rd.Read)
			require.NotNil(t, rc)
			assert.Equal(t, "r2", rc.Row)
			assert.Equal(t, "1", rc.F(FieldLeveled))
		}
	})

	w := auditWorld(t, since, 30)
	p, rec := PlanColdAudit(w.s, req)
	require.Empty(t, p.Refused)
	ids := rec.IDs()
	require.Len(t, ids, ColdAuditCards)
	assert.Equal(t, int64(42), rec.Seed)
	assert.Equal(t, "v0.9.0", rec.Since)
	for _, id := range ids {
		assert.False(t, strings.HasPrefix(id, "old-"), "%s landed before the release", id)
	}
	_, again := PlanColdAudit(w.s, req)
	assert.Equal(t, ids, again.IDs(), "one seed over one table is one sample")
	other := req
	other.Seed = 7
	_, drawn := PlanColdAudit(w.s, other)
	assert.NotEqual(t, ids, drawn.IDs(), "another seed draws another sample")

	asked := map[string]int{}
	for _, u := range p.Units {
		require.Len(t, u.Changes, 1)
		e := u.Changes[0].Entry
		rd := e.Create.Row
		assert.NotEqual(t, "r1", rd, "r1 read the card in its review")
		assert.Equal(t, Asked, e.Create.Col)
		assert.Equal(t, ReadCardID(u.Key, 1, rd), e.ID)
		assert.Equal(t, stamp(t0), e.Set[FieldColdAudit])
		assert.Equal(t, "h-"+u.Key, e.Set["head"])
		assert.Equal(t, "main", e.Set["base"])
		assert.NotEmpty(t, e.Set[FieldTier], "the read carries its tier as the ask's does")
		asked[rd]++
	}
	assert.Equal(t, map[string]int{"r2": 7, "r3": 7, "r4": 6}, asked, "spread by room, ties round the readers")
	w.must(p)
	_, idx := w.s.Readers.Prop(PropAskIndex)
	assert.True(t, idx, "the ask's index is written with the audit")

	r := check(w)
	assert.False(t, r.OK)
	assert.Contains(t, r.Evidence, "20 unanswered")

	// the readers report through the read verb; the first finds it broken
	readOf := map[string]ColdAuditRead{}
	for _, rd := range rec.Reads {
		readOf[rd.Card] = rd
	}
	for i, id := range ids {
		rc := w.s.Readers.Card(readOf[id].Read)
		rr := ReadReq{Sel: Sel{IDs: []string{rc.ID}}, As: rc.Row, Verdict: "ok", Who: rc.Row}
		if i == 0 {
			rr.Verdict, rr.Finding = "broken", "internal/sprint/x.go:12: the guard is inverted; flip it"
		}
		if i == 1 {
			continue // still asked
		}
		w.must(Read(w.s, rr))
	}
	r = check(w)
	assert.False(t, r.OK)
	assert.Contains(t, r.Evidence, "1 broken: "+ids[0])
	assert.Contains(t, r.Evidence, "internal/sprint/x.go:12: the guard is inverted")
	assert.Contains(t, r.Evidence, "1 unanswered: "+ids[1])
	assert.Contains(t, r.Evidence, "look at: nova-sprint card")

	w.s.Readers.Card(readOf[ids[0]].Read).Col = OK
	w.must(Read(w.s, ReadReq{Sel: Sel{IDs: []string{readOf[ids[1]].Read}}, As: w.s.Readers.Card(readOf[ids[1]].Read).Row, Verdict: "ok"}))
	r = check(w)
	assert.True(t, r.OK, r.Evidence)
	assert.Equal(t, "RELEASE CHECK cold-audit ok all 20 cold reads ok (seed 42, since v0.9.0, asked "+stamp(t0)+", 0s ago)", r.Line())

	// 48 hours on, the audit has expired
	w.tick(ColdAuditMaxAge)
	r = check(w)
	assert.False(t, r.OK)
	assert.Contains(t, r.Evidence, "48h0m0s old, limit 48h0m0s; run: nova-sprint release check --audit")

	// a new audit's check reads only its own reads: the last audit's, all ok, count for nothing
	p2, rec2 := PlanColdAudit(w.s, ColdAuditReq{Seed: 42, Since: "v0.9.0", SinceTime: since})
	require.Empty(t, p2.Refused)
	for _, u := range p2.Units {
		rd := u.Changes[0].Entry.Create.Row
		if _, ok := readOf[u.Key]; !ok {
			continue // not in the last audit's sample
		}
		assert.NotEqual(t, "r1", rd)
		assert.NotEqual(t, w.s.Readers.Card(readOf[u.Key].Read).Row, rd, "the last audit's reader saw it")
	}
	w.must(p2)
	r = check(w)
	assert.False(t, r.OK, r.Evidence)
	assert.Contains(t, r.Evidence, "20 unanswered")
	// a read card of the last audit named by this record is not this audit's
	b, _ := json.Marshal(ColdAuditRecord{Seed: rec2.Seed, Since: rec2.Since, Asked: rec2.Asked, Reads: rec.Reads})
	w.s.Work.SetProp(PropColdAudit, string(b))
	r = check(w)
	assert.False(t, r.OK, r.Evidence)
	assert.Contains(t, r.Evidence, "20 unanswered")
	assert.Contains(t, r.Evidence, "of this audit")
}

func TestTheReleaseReportSaysOKOrNotReadyByTheChecksRun(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	rep, err := RunReleaseChecks(fakeRelease{now: t0, snap: w.s}, nil)
	require.NoError(t, err)
	assert.False(t, rep.Ready)
	assert.Equal(t, "RELEASE NOT READY failed=1", rep.Summary)
	_, err = RunReleaseChecks(fakeRelease{now: t0, snap: w.s}, []string{"nope"})
	assert.EqualError(t, err, `no release check named "nope"; the checks are cold-audit`)
}

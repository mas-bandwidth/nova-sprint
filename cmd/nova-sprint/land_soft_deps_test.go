package main

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The lander's open of a stream with soft dependencies (docs/SPEC-SPRINT.md, LAND-AFTER;
// tla/SoftDependency.tla SoftIsOrderOnly, HeldNeverEmptiesBatch). On 2026-10-16 four
// streams were refused whole, "LAND REFUSED stream=schema-go cards=0 ... soft dependency
// on fixed-count-and-length-past-bound-refuse-or-clamp-2 not landed", while the target
// was in review, not queued, and six finished cards sat in merging for hours.
func TestLanderOpensAStreamWhoseSoftDependencyIsNotQueuedOrIsHeld(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	dir := t.TempDir()
	for _, c := range []struct{ id, after string }{{"a", ""}, {"b", "a"}, {"c", ""}, {"d", ""}, {"e", "d"}} {
		ta.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, "mas-bandwidth/nova-sprint", "", "internal/"+c.id+".go"))
	}
	ta.deal(5)
	ta.ok("take --as m1 --limit 100")
	for _, id := range []string{"a", "b", "c", "e"} {
		ta.ok("finish --as m1 " + id + ".w1@1")
	}
	ta.ok("ask")
	ta.ok("read --as reader-a --ok --limit 100")
	ta.ok("read --as reader-b --ok --limit 100")
	ta.ok("accept --read-ok")
	ta.markProtected()

	st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
	require.NoError(t, err)
	open := func(hold bool) ([]string, []landBatch) {
		s, err := st.Load(t.Context(), []string{sprint.Work, sprint.Merge, sprint.Fleet, sprint.Readers}, nil)
		require.NoError(t, err)
		require.Equal(t, "working", s.Work.Card("d").Col, "d, e's soft dependency, is working, not queued")
		if hold {
			s.Work.Card("a").Fields["needs"] = "d" // the fake hold: a is queued and cannot be placed
		}
		l := &lander{a: ta.a, st: st, base: "main", prose: map[string][]string{}, diffs: map[string]string{}, scope: map[string][]string{}}
		cards, ok := l.openStream(s, "s1")
		require.True(t, ok, "the stream opens: %+v", l.out)
		var ids []string
		for _, c := range cards {
			ids = append(ids, c.id)
		}
		return ids, l.out
	}

	// 1. e's soft dependency d is not queued: it orders nothing, e lands with the batch;
	// a and b are both queued, and a is placed ahead of b.
	ids, out := open(false)
	assert.Equal(t, []string{"a", "b", "c", "e"}, ids)
	assert.Empty(t, out, "nothing is refused")

	// 2. a is queued and held: b cannot be placed after it and is skipped, named with its
	// reason; c and e land.
	ids, out = open(true)
	assert.Equal(t, []string{"c", "e"}, ids, "every other queued card lands")
	require.Len(t, out, 1)
	assert.Equal(t, "refused", out[0].Status)
	assert.Equal(t, "b: soft dependency on a not landed; b is skipped this batch", out[0].Reason)
}

// The D8 interleaving: the LAND-AFTER target enters merging after the pass opens
// the stream but before MarkLanding. The mark cuts the pushed batch to the safe
// prefix, and the next pass preserves the dependency order.
func TestLandRechecksSoftDependencyAtMark(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	r.commit("go.mod", "module example.com/d8\n\ngo 1.24\n", "module")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	dir := t.TempDir()
	for _, c := range []struct{ id, after string }{{"c", ""}, {"b", ""}, {"a", "c"}} {
		r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, r.remote, "", c.id+".txt"))
	}
	r.ok("rank b --first")
	heads := map[string]string{}
	for _, id := range []string{"a", "b", "c"} {
		heads[id] = r.head(id, "main", id+".txt", id+"\n")
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(3)
	r.ok("take --as m1 --limit 100")
	for _, id := range []string{"a", "b"} {
		r.ok("finish --as m1 " + id + ".w1@1 --head " + heads[id])
	}
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	r.markProtected()
	require.Equal(t, map[string]string{"a": "merging/queued", "b": "merging/queued"}, r.places("a", "b"))

	var queueC sync.Once
	r.a.gateRan = func(string, bool) {
		queueC.Do(func() {
			r.ok("finish --as m1 c.w1@1 --head " + heads["c"])
			r.ok("ask")
			r.ok("read --as reader-a --ok --limit 100")
			r.ok("read --as reader-b --ok --limit 100")
			r.ok("accept --read-ok")
		})
	}
	code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	require.Equal(t, 1, code, out+errs)
	assert.Contains(t, errs, "a: soft dependency on c not landed; a is skipped this batch")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1")
	assert.Equal(t, map[string]string{"a": "merging/queued", "b": "landed/merged", "c": "merging/queued"}, r.places("a", "b", "c"))
	assert.Equal(t, []string{"land b (sprint stream s1)", "module", "base"}, r.mainLog())

	r.a.gateRan = nil
	out = r.ok("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "LAND OK stream=s1 cards=2")
	assert.Equal(t, map[string]string{"a": "landed/merged", "b": "landed/merged", "c": "landed/merged"}, r.places("a", "b", "c"))
	assert.Equal(t, []string{"land a (sprint stream s1)", "land c (sprint stream s1)", "land b (sprint stream s1)", "module", "base"}, r.mainLog())
	r.clean()
}

// A target queued after MarkLanding cannot retroactively refuse the pushed batch:
// FieldLandingHead records that the LAND-AFTER predicate was already judged.
func TestLandReportHonorsSoftDependencyMark(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	dir := t.TempDir()
	for _, c := range []struct{ id, after string }{{"c", ""}, {"a", "c"}} {
		r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, r.remote, "", c.id+".txt"))
	}
	heads := map[string]string{}
	for _, id := range []string{"a", "c"} {
		heads[id] = r.head(id, "main", id+".txt", id+"\n")
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(2)
	r.ok("take --as m1 --limit 100")
	r.ok("finish --as m1 a.w1@1 --head " + heads["a"])
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	r.markProtected()

	var queueC sync.Once
	r.a.beforePush = func(int) {
		queueC.Do(func() {
			r.ok("finish --as m1 c.w1@1 --head " + heads["c"])
			r.ok("ask")
			r.ok("read --as reader-a --ok --limit 100")
			r.ok("read --as reader-b --ok --limit 100")
			r.ok("accept --read-ok")
		})
	}
	out := r.ok("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1")
	assert.Equal(t, map[string]string{"a": "landed/merged", "c": "merging/queued"}, r.places("a", "c"))
	assert.Equal(t, []string{"land a (sprint stream s1)", "base"}, r.mainLog())
	r.clean()
}

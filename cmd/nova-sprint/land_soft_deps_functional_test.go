//go:build functional

package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/gitrun"
)

// The lander replays of LAND-AFTER at the mark (tla/SoftDependency.tla BaseRecorded,
// BaseSoftOrder). They run real git in a local rig (origin, worker, clone), so they are
// functional tests (internal/ci unit rule: a file that runs a program); the unit-tier
// soft-dependency test stays in land_soft_deps_test.go.

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
	var gatedMu sync.Mutex
	gated := map[string]bool{} // the trees the gate ran on
	r.a.gateRan = func(dir string, _ bool) {
		head := r.git(dir, "rev-parse", "HEAD")
		gatedMu.Lock()
		gated[head] = true
		gatedMu.Unlock()
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
	assert.Contains(t, errs, "a: soft dependency on c not landed; a is skipped this batch; the batch ends there: b go on to the push, a stay queued")
	assert.Contains(t, out, "LAND OK stream=s1 cards=1")
	assert.Equal(t, map[string]string{"a": "merging/queued", "b": "landed/merged", "c": "merging/queued"}, r.places("a", "b", "c"))
	assert.Equal(t, []string{"land b (sprint stream s1)", "module", "base"}, r.mainLog())
	// every pushed commit was recorded by the report in the same pass: nothing is left
	// pushed-unreported (tla/SoftDependency.tla BaseRecorded)
	r.noPushedUnreported("a", "b", "c")
	// the prefix tip was never gated as a tree when it was pushed: it is cached as gated
	// only once a gate has run on it (tla/LandPass.tla CachedIsGreen)
	tip := r.git(r.remote, "rev-parse", "main")
	if _, cached := r.a.baseGateCache[tip]; cached {
		gatedMu.Lock()
		assert.True(t, gated[tip], "the cut prefix's tip %s was cached as gated with no gate run on it", tip)
		gatedMu.Unlock()
	}

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
	r.noPushedUnreported("a", "c")
	r.clean()
}

// Dogfood run 3 ("LAND FAILED stream=e3s cards=1 ... the batch was pushed ... and NOT
// reported: owner-pushes-route-to-coordinator: soft dependency on <3 LAND-AFTER targets>
// not landed"): a batch of one card with three LAND-AFTER targets, all three accepted into
// merging after the pass opens the stream and before the mark. The cut has no earlier pin to
// keep: nothing is pushed, the card is reported skipped naming all three targets, it stays
// queued, and nothing is left pushed-unreported (tla/SoftDependency.tla MarkPush,
// BaseRecorded).
func TestLandOneCardBatchWithThreeSoftTargetsPushesNothing(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	r.commit("go.mod", "module example.com/e3s\n\ngo 1.24\n", "module")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	dir := t.TempDir()
	for _, c := range []struct{ id, after string }{{"c1", ""}, {"c2", ""}, {"c3", ""}, {"a", "c1, c2, c3"}} {
		r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, r.remote, "", c.id+".txt"))
	}
	heads := map[string]string{}
	for _, id := range []string{"a", "c1", "c2", "c3"} {
		heads[id] = r.head(id, "main", id+".txt", id+"\n")
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(4)
	r.ok("take --as m1 --limit 100")
	r.ok("finish --as m1 a.w1@1 --head " + heads["a"])
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	r.markProtected()
	require.Equal(t, map[string]string{"a": "merging/queued"}, r.places("a"))
	before := r.mainLog()

	var queueTargets sync.Once
	r.a.gateRan = func(string, bool) {
		queueTargets.Do(func() {
			for _, id := range []string{"c1", "c2", "c3"} {
				r.ok("finish --as m1 " + id + ".w1@1 --head " + heads[id])
			}
			r.ok("ask")
			r.ok("read --as reader-a --ok --limit 100")
			r.ok("read --as reader-b --ok --limit 100")
			r.ok("accept --read-ok")
		})
	}
	_, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, errs, "a: soft dependency on c1, c2, c3 not landed; a is skipped this batch", out+errs)
	assert.NotContains(t, out+errs, "NOT reported")
	assert.NotContains(t, out+errs, "LAND FAILED")
	assert.Equal(t, before, r.mainLog(), "a one-card batch whose only pin was skipped pushed something")
	_, err := gitrunIsAncestor(r, heads["a"], r.git(r.remote, "rev-parse", "main"))
	assert.Error(t, err, "the skipped a was pushed")
	assert.Equal(t, map[string]string{"a": "merging/queued", "c1": "merging/queued", "c2": "merging/queued", "c3": "merging/queued"}, r.places("a", "c1", "c2", "c3"))
	r.noPushedUnreported("a", "c1", "c2", "c3")

	// the next pass, the three targets queued, lands all four with a last
	r.a.gateRan = nil
	out = r.ok("land --repo-dir " + r.clone + " --base main")
	assert.Contains(t, out, "LAND OK stream=s1 cards=4")
	assert.Equal(t, "land a (sprint stream s1)", r.mainLog()[0])
	r.noPushedUnreported("a", "c1", "c2", "c3")
	r.clean()
}

// noPushedUnreported says no card of ids carries a pushed-unreported mark.
func (r *landRig) noPushedUnreported(ids ...string) {
	r.t.Helper()
	st, err := r.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(r.t, err)
	s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(r.t, err)
	for _, id := range ids {
		assert.Empty(r.t, sprint.PushedUnreportedSHA(s, id), "%s is left pushed-unreported", id)
	}
}

// markLanding ends the batch early only for the LAND-AFTER skip (sprint.LandOrderSkipped):
// the pins before it are marked and go on to the push. Every other refusal of the mark
// (not queued, head moved, re-pointed, reads short) keeps the all-or-nothing rule: 0 pins
// pushable, nothing marked, and the reason says nothing pushed (tla/Land.tla
// PushedNeverSentBack, NoLandWithoutReads; tla/SoftDependency.tla MarkPush). No clock, no
// network: the store is mem:0 and the heads are a local repository's.
func TestMarkLandingEndsTheBatchEarlyOnlyForTheSkip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind, why string
		skip      bool
		// firstBase is the base the first pin was built on ("" = main); premark marks the
		// first pin landing before, so only the second pin meets the refusal
		firstBase string
		premark   bool
		pin       func(r *landRig, a landCard) landCard
		before    func(r *landRig)
	}{
		{kind: "not queued", why: "zz: not merging and queued in stream s1", pin: func(_ *landRig, a landCard) landCard {
			a.id = "zz"
			return a
		}},
		{kind: "head moved", why: "a: at head ", pin: func(_ *landRig, a landCard) landCard {
			a.head = strings.Repeat("d", 40)
			return a
		}},
		{kind: "re-pointed", why: "a: re-pointed to BASE other during the gate, not main", firstBase: "other", before: func(r *landRig) {
			// another base that holds every PATHS entry, the stream re-pointed to it
			r.git(r.worker, "switch", "-q", "--detach", "refs/remotes/origin/main")
			for _, id := range []string{"a", "b", "c"} {
				r.commit(id+".txt", "other\n", "other "+id)
			}
			r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/other")
			r.ok("stream set s1 --base other")
		}},
		{kind: "reads short", why: "a: a has ok reads at head ", premark: true, before: func(r *landRig) { r.directProp(sprint.PropReadsNeeded, "3") }},
		{kind: "skip", why: "a: soft dependency on c not landed; a is skipped this batch", skip: true},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			r := newLandRig(t)
			dir := t.TempDir()
			for _, c := range []struct{ id, after string }{{"c", ""}, {"b", ""}, {"a", "c"}} {
				r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, r.remote, "", c.id+".txt"))
			}
			heads := map[string]string{}
			for _, id := range []string{"a", "b", "c"} {
				heads[id] = r.head(id, "main", id+".txt", id+"\n")
			}
			r.queued(heads, "a", "b", "c")
			require.Equal(t, map[string]string{"a": "merging/queued", "b": "merging/queued", "c": "merging/queued"}, r.places("a", "b", "c"))
			if tc.before != nil {
				tc.before(r)
			}
			a := landCard{id: "a", head: heads["a"], attempt: "1", base: "main"}
			if tc.pin != nil {
				a = tc.pin(r, a)
			}
			st, err := r.a.store(common{redis: "mem:0", actor: "coordinator"})
			require.NoError(t, err)
			epoch, err := st.EpochNow(context.Background())
			require.NoError(t, err)
			l := &lander{a: r.a, st: st, epoch: epoch.N, c: common{actor: "coordinator"}}
			// the hard refusals are met with a's LAND-AFTER target c ahead of it in the pins,
			// so its order is met; the skip with b ahead, c queued and not pinned
			first, firstBase := "c", "main"
			if tc.skip {
				first = "b"
			}
			if tc.firstBase != "" {
				firstBase = tc.firstBase
			}
			if tc.premark {
				r.markLanding(first)
			}
			n, why := l.markLanding("s1", []landCard{{id: first, head: heads[first], attempt: "1", base: firstBase}, a})
			assert.Contains(t, why, tc.why)
			s, err := st.Load(context.Background(), []string{sprint.Work, sprint.Merge}, nil)
			require.NoError(t, err)
			assert.False(t, sprint.LandingMarked(s, "a"), "the refused pin was marked")
			if tc.skip {
				assert.Equal(t, 1, n, "the skip ends the batch after b")
				assert.True(t, sprint.LandingMarked(s, first), "the prefix before the skip was not marked")
				return
			}
			assert.Equal(t, 0, n, "a %s refusal pushed a prefix", tc.kind)
			assert.Contains(t, why, "nothing pushed")
			assert.Equal(t, tc.premark, sprint.LandingMarked(s, first), "a %s refusal marked the prefix", tc.kind)
		})
	}
}

// The skip's prefix tip holds every head before the skip, not only the last: with pins x,
// y, z, y's head already on the base (a no-op merge) and z skipped for its LAND-AFTER
// target c, queued while the batch gates, the base itself holds y's head; the prefix pushed
// is the tip after x, and x is on the base it is reported landed on.
func TestLandSkipPrefixTipHoldsEveryHeadBeforeTheSkip(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	r.stubGreenGate()
	r.commit("go.mod", "module example.com/xyz\n\ngo 1.24\n", "module")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	dir := t.TempDir()
	for _, c := range []struct{ id, after string }{{"c", ""}, {"x", ""}, {"y", ""}, {"z", "c"}} {
		r.ok("add --stream s1 --one --brief-file " + writeSoftHeaderBrief(t, dir, c.id, "-", c.after, r.remote, "", c.id+".txt"))
	}
	heads := map[string]string{}
	for _, id := range []string{"c", "x", "y", "z"} {
		heads[id] = r.head(id, "main", id+".txt", id+"\n")
	}
	r.git(r.worker, "push", "-q", "origin", "refs/heads/sprint/*:refs/heads/sprint/*")
	r.deal(4)
	r.ok("take --as m1 --limit 100")
	for _, id := range []string{"x", "y", "z"} {
		r.ok("finish --as m1 " + id + ".w1@1 --head " + heads[id])
	}
	r.ok("ask")
	r.ok("read --as reader-a --ok --limit 100")
	r.ok("read --as reader-b --ok --limit 100")
	r.ok("accept --read-ok")
	r.markProtected()
	r.ok("rank z --first")
	r.ok("rank y --first")
	r.ok("rank x --first")
	// y's head is on the base before the pass: its merge is a no-op
	r.git(r.worker, "push", "-q", "origin", heads["y"]+":refs/heads/main")
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
	assert.Contains(t, errs, "z: soft dependency on c not landed; z is skipped this batch")
	main := r.git(r.remote, "rev-parse", "main")
	r.git(r.remote, "merge-base", "--is-ancestor", heads["x"], main) // x's head is on the base
	_, err := gitrunIsAncestor(r, heads["z"], main)
	assert.Error(t, err, "the skipped z was pushed")
	places := r.places("x", "y", "z", "c")
	assert.Equal(t, "merging/queued", places["z"])
	assert.Equal(t, "landed/merged", places["x"])
	r.noPushedUnreported("x", "y", "z", "c")
	r.clean()
}

// gitrunIsAncestor is git merge-base --is-ancestor in the rig's origin, its error unchecked.
func gitrunIsAncestor(r *landRig, a, b string) (string, error) {
	res, err := gitrun.Run(context.Background(), gitrun.Options{C: r.remote, Env: r.env, OwnRepo: true}, "merge-base", "--is-ancestor", a, b)
	return string(res.Stdout), err
}

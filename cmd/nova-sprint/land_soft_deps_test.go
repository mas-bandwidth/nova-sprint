package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The lander's open keeps an unlanded soft target out of the cut even when the
// target is not queued; a blocked dependent is named while other cards may proceed.
// tla/SoftDependency.tla SoftIsOrderOnly, HeldNeverEmptiesBatch.
func TestLanderOpensAStreamWhoseSoftDependencyIsNotLandedOrIsHeld(t *testing.T) {
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

	// 1. e's target d is working and unlanded, so e stays out; a and b are both
	// queued, and a is placed ahead of b.
	ids, out := open(false)
	assert.Equal(t, []string{"a", "b", "c"}, ids)
	require.Len(t, out, 1)
	assert.Equal(t, "e: soft dependency on d not landed; e is skipped this batch", out[0].Reason)

	// 2. a is queued and held: b cannot be placed after it and e still has an
	// unlanded working target; c lands while both dependents are named.
	ids, out = open(true)
	assert.Equal(t, []string{"c"}, ids, "an independent queued card still lands")
	require.Len(t, out, 1)
	assert.Equal(t, "refused", out[0].Status)
	assert.Contains(t, out[0].Reason, "b: soft dependency on a not landed; b is skipped this batch")
	assert.Contains(t, out[0].Reason, "e: soft dependency on d not landed; e is skipped this batch")
}

package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestANewBriefBouncesAnOldHeadToRework(t *testing.T) {
	t.Parallel()
	const head = "0123456789012345678901234567890123456789"
	started := func(t *testing.T) *world {
		t.Helper()
		w := setup(t, 1)
		w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 1}}))
		c := w.s.Fleet.Card("s1-1.w1")
		w.must(Take(w.s, TakeReq{As: c.Row, Sel: Sel{IDs: []string{c.ID}}, Gens: gensOf(w.s, c.ID)}))
		return w
	}
	finish := func(w *world) {
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{"s1-1.w1"}}, Gens: gensOf(w.s, "s1-1.w1"), Head: head, Report: "implemented the earlier task"}))
	}
	t.Run("working replaced", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		w.must(Brief(w.s, briefOne("s1-1", "a different task")))
		finish(w)
		c := w.s.Work.Card("s1-1")
		require.Equal(t, Ready, c.Col)
		assert.Empty(t, c.F("head"))
		assert.Contains(t, w.s.Fleet.Card("s1-1.w1").F("report"), "brief replaced: the head of attempt 1 was made under the earlier brief")
		assert.Empty(t, BaseOf(w.s.Fleet.Of("s1-1")).Head)
		assert.Equal(t, head, w.s.Fleet.Card("s1-1.w1").F("head"), "history retains the original SHA")
	})
	t.Run("review replaced", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		finish(w)
		w.must(Brief(w.s, briefOne("s1-1", "a different task")))
		assert.Equal(t, Ready, w.state("s1-1"))
		assert.Empty(t, w.s.Work.Card("s1-1").F("head"))
		assert.Empty(t, BaseOf(w.s.Fleet.Of("s1-1")).Head)
	})
	t.Run("first brief remains current", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		finish(w)
		assert.Equal(t, Review, w.state("s1-1"))
		assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
		assert.False(t, strings.Contains(w.s.Fleet.Card("s1-1.w1").F("report"), "brief replaced:"))
	})
	t.Run("working explicitly kept", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		req := briefOne("s1-1", "a different task")
		req.KeepHead = true
		p := w.must(Brief(w.s, req))
		assert.Contains(t, strings.Join(p.Said, "\n"), "head kept: eligible for review (--keep-head)")
		finish(w)
		assert.Equal(t, Review, w.state("s1-1"))
		assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
	})
	t.Run("review explicitly kept", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		finish(w)
		w.must(Ask(w.s, AskReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		reads := len(w.s.Readers.Of("s1-1"))
		require.NotZero(t, reads)
		req := briefOne("s1-1", "a different task")
		req.KeepHead = true
		w.must(Brief(w.s, req))
		assert.Equal(t, Review, w.state("s1-1"))
		assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
		assert.Len(t, w.s.Readers.Of("s1-1"), reads, "explicitly kept head retains its reads")
		unchanged := Brief(w.s, req)
		require.NotEmpty(t, unchanged.Refused)
		assert.Empty(t, unchanged.Units)
		assert.Len(t, w.s.Readers.Of("s1-1"), reads, "refused unchanged brief cannot retire reads")
	})
	t.Run("fresh attempt after replacement", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		w.must(Brief(w.s, briefOne("s1-1", "a different task")))
		finish(w)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := w.s.Fleet.Card("s1-1.w2")
		require.NotNil(t, wc)
		packet := PacketOf("", 1, wc, w.s.Work.Card("s1-1"), []*Card{w.s.Fleet.Card("s1-1.w1")}, nil)
		assert.Empty(t, packet.Base)
		assert.Empty(t, packet.BaseHead)
		w.must(Take(w.s, TakeReq{As: wc.Row, Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID)}))
		w.must(Finish(w.s, FinishReq{Sel: Sel{IDs: []string{wc.ID}}, Gens: gensOf(w.s, wc.ID), Head: head}))
		assert.Equal(t, Review, w.state("s1-1"))
		assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
	})

	t.Run("routing edit preserves bound but supersedes head", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		c := w.s.Work.Card("s1-1")
		before := c.F(FieldBriefAttempt)
		brief := strings.Replace(c.F("brief"), "tier: pro", "tier: heavy", 1)
		require.NotEqual(t, c.F("brief"), brief)
		w.must(Brief(w.s, briefOne("s1-1", brief)))
		assert.Equal(t, before, w.s.Work.Card("s1-1").F(FieldBriefAttempt))
		finish(w)
		assert.Equal(t, Ready, w.state("s1-1"))
		assert.Empty(t, w.s.Work.Card("s1-1").F("head"))
	})

	t.Run("explicit carry remains a coordinator choice", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		w.must(Brief(w.s, briefOne("s1-1", "a different task\nCARRY: s1-1 attempt 1 head="+head)))
		finish(w)
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		wc := w.s.Fleet.Card("s1-1.w2")
		require.NotNil(t, wc)
		packet := PacketOf("", 1, wc, w.s.Work.Card("s1-1"), []*Card{w.s.Fleet.Card("s1-1.w1")}, nil)
		assert.Equal(t, head, packet.BaseHead)
		assert.Equal(t, 1, packet.BaseAttempt)
	})

	t.Run("kept review cannot acquire an unmet need", func(t *testing.T) {
		t.Parallel()
		w := started(t)
		finish(w)
		w.must(Add(w.s, AddReq{Stream: "s1", IDs: []string{"need"}, Brief: proBrief}))
		req := briefOne("s1-1", "a different task")
		req.KeepHead = true
		req.Cards[0].Needs = []string{"need"}
		p := Brief(w.s, req)
		require.NotEmpty(t, p.Refused)
		assert.Empty(t, p.Units)
		assert.Equal(t, Review, w.state("s1-1"))
		assert.Equal(t, head, w.s.Work.Card("s1-1").F("head"))
	})

}

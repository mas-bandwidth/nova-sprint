package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecheckClosesOnlyEndedJudgmentConditions(t *testing.T) {
	t.Parallel()
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "obsolete", true: "still live"}[live], func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.readersTable()
			w.s.Work.SetRows([]string{"s1"})
			w.s.Fleet.SetRows([]string{"m1"})
			w.s.Work.Put(&Card{ID: "p", Row: "s1", Col: Ready, Fields: map[string]string{"brief": "tier: pro", FieldTierNow: "pro", "attempt": "1"}})
			col := Withdrawn
			if live {
				col = Working
			}
			w.s.Fleet.Put(&Card{ID: "p.w1", Row: "m1", Col: col, Fields: map[string]string{"primary": "p", "stream": "s1"}})
			readCol := OK
			if live {
				readCol = Asked
			}
			w.s.Readers.SetRows([]string{"reader"})
			w.s.Readers.Put(&Card{ID: "p.r1.reader", Row: "reader", Col: readCol, Fields: map[string]string{"primary": "p", "stream": "s1"}})
			w.s.Routes = []Route{{Name: "pro", Tier: "pro", Enabled: !live}}
			w.s.Open = []Open{
				{Key: OpenKey("late", "p"), Note: Note{ID: "late", Kind: Judgment, Type: NWorkLate, Card: "p.w1", Primaries: []string{"p"}, What: "not finished"}},
				{Key: OpenKey("read-late", "p"), Note: Note{ID: "read-late", Kind: Judgment, Type: NReadLate, Card: "p.r1.reader", Primaries: []string{"p"}, What: "not reported"}},
				{Key: OpenKey("route", StreamSubject(TierSubject("pro"))), Note: Note{ID: "route", Kind: Judgment, Type: NNoRoute, Stream: TierSubject("pro"), StreamLevel: true, Primaries: []string{"p"}}},
				{Key: OpenKey("unknown", SprintSubject), Note: Note{ID: "unknown", Kind: Judgment, Type: "unknown obligation", SprintLevel: true}},
			}
			p := RecheckJudgments(w.s, "seat")
			assert.Empty(t, p.Units)
			assert.Empty(t, p.Updates)
			assert.Empty(t, p.Rows)
			assert.Empty(t, p.Stop)
			if live {
				assert.Empty(t, p.Closes)
				return
			}
			var ids []string
			for _, o := range p.Closes {
				ids = append(ids, o.Note.ID)
			}
			assert.ElementsMatch(t, []string{"late", "read-late", "route"}, ids)
			require.Len(t, p.Notes, 3)
			for _, n := range p.Notes {
				assert.Equal(t, Decided, n.Kind)
				assert.NotEmpty(t, n.Answers)
			}
			assert.Len(t, p.Said, 3)
		})
	}
}

func TestRecheckRetiresAnAbsentCardAndPreservesLiveUnknownJudgments(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.s.Open = []Open{{Key: OpenKey("gone", "absent"), Note: Note{ID: "gone", Kind: Judgment, Type: NWorkFailed, Primaries: []string{"absent"}}}}
	p := RecheckJudgments(w.s, "seat")
	require.Len(t, p.Closes, 1)
	require.Len(t, p.Notes, 1)
	assert.Equal(t, "gone", p.Notes[0].Answers)
	assert.Contains(t, p.Notes[0].What, "epoch")
	assert.Len(t, p.Said, 1)
	assert.Empty(t, p.Units)
}

func TestRecheckKeepsTheLiveSubjectOfAGroupedOverdueHold(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.readersTable()
	w.s.Work.SetRows([]string{"s1"})
	w.s.Work.Put(&Card{ID: "p", Row: "s1", Col: Ready, Fields: map[string]string{"brief": "tier: pro", FieldTierNow: "pro"}})
	w.s.Routes = []Route{{Name: "flash", Tier: "flash", Enabled: true}}
	for _, tier := range []string{"flash", "pro"} {
		subject := StreamSubject(TierSubject(tier))
		w.s.Open = append(w.s.Open, Open{Key: OpenKey("group", subject), Note: Note{ID: "group", Kind: Judgment, Type: NNoRoute, Stream: TierSubject(tier), StreamLevel: true, Primaries: []string{"p"}}})
		w.s.Acked = append(w.s.Acked, Open{Key: OpenKey("marker", subject), Note: Note{ID: "marker", Kind: Acknowledged, Type: NOverdue, What: "group"}})
	}
	p := RecheckJudgments(w.s, "seat")
	var keys []string
	for _, o := range p.Closes {
		keys = append(keys, o.Key)
	}
	assert.ElementsMatch(t, []string{OpenKey("group", StreamSubject(TierSubject("flash"))), OpenKey("marker", StreamSubject(TierSubject("flash")))}, keys)
	require.Len(t, p.Said, 1, "the bookkeeping marker is not a judgment")
	assert.Contains(t, p.Said[0], "stream:tier:flash")
	assert.Empty(t, p.Units)
}

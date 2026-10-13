package sprint

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestACannotAskClosesWhenAReaderMayRead(t *testing.T) {
	t.Parallel()
	for _, roomOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "with room", true: "without room"}[roomOnly], func(t *testing.T) {
			t.Parallel()
			w, none, room, free := cannotAskEpisodeWorld(t)
			raised, _ := askTick(t, w, none)
			require.Len(t, raised, 1)
			seats := free
			if roomOnly {
				seats = room
			}
			_, closed := askTick(t, w, seats)
			require.Len(t, closed, 1)
			require.Empty(t, openCannotAsk(w, "s1-1"))
			noted := false
			for _, n := range w.notes {
				noted = noted || strings.HasPrefix(n.What, "a reader may read s1-1 now")
			}
			require.True(t, noted)
			for i := 0; i < 3; i++ {
				raised, _ = askTick(t, w, seats)
				require.Empty(t, raised)
			}
			raised, _ = askTick(t, w, none)
			require.Len(t, raised, 1)
			raised, _ = askTick(t, w, none)
			require.Empty(t, raised)
		})
	}
	t.Run("ask answers and refuses with its reason", func(t *testing.T) {
		t.Parallel()
		w, none, _, free := cannotAskEpisodeWorld(t)
		askTick(t, w, none)
		j := openCannotAsk(w, "s1-1")[0]
		req := AskReq{Sel: Sel{IDs: []string{"s1-1"}}, Answers: []string{j.Note.ID}, Who: w.s.Coordinator}
		w.s.Friends = none
		p := Ask(w.s, req)
		require.NotEmpty(t, p.Refused)
		require.Contains(t, p.Refused[0].Why, NoReaderMayRead)
		require.NotContains(t, p.Refused[0].Why, "resolves no obligation")
		putReviewBy(w, "s1-2", "s1-2: other (s1) tier: heavy\n", "bob", 2)
		w.s.Friends = free[:2] // one eligible reader is enough to deal one read and answer
		p = Ask(w.s, req)
		require.Empty(t, p.Refused)
		require.NotEmpty(t, p.Units)
		require.Len(t, p.Units[0].Closes, 1)
		w.must(p)
		require.NotEmpty(t, readCardsOf(w, "s1-1"))
		require.Empty(t, readCardsOf(w, "s1-2"), "ask affects only its named primary")
		require.Empty(t, openCannotAsk(w, "s1-1"))
	})
	for _, acknowledged := range []bool{false, true} {
		for _, roomOnly := range []bool{false, true} {
			t.Run(map[bool]string{false: "open", true: "acknowledged"}[acknowledged]+" deal before ask "+map[bool]string{false: "free", true: "full"}[roomOnly], func(t *testing.T) {
				t.Parallel()
				w, none, room, free := cannotAskEpisodeWorld(t)
				askTick(t, w, none)
				j := openCannotAsk(w, "s1-1")[0]
				if acknowledged {
					w.closeAll([]Open{j})
					j.Note.Kind = Acknowledged
					w.s.Acked = append(w.s.Acked, j)
				}
				seats := free
				if roomOnly {
					seats = room
				}
				deal := w.part(TickDeal, TickReq{Friends: seats})
				var closes []Open
				for _, u := range deal.Units {
					closes = append(closes, u.Closes...)
				}
				if len(closes) > 0 {
					w.s.Acked = nil
				} // world.closeAll handles Open; the store removes both lists
				ask := w.part(friendAskPart(TickAsk), TickReq{Friends: seats})
				closes = append(closes, ask.Closes...)
				require.Len(t, closes, 1)
				require.Equal(t, j.Key, closes[0].Key)
				noted := false
				for _, n := range w.notes {
					noted = noted || n.Kind == Decided && n.Answers == j.Note.ID && strings.HasPrefix(n.What, "a reader may read s1-1 now (")
				}
				require.True(t, noted, "the actual deal-before-ask order records the factual answer")
				require.Empty(t, openCannotAsk(w, "s1-1"))
			})
		}
	}
	t.Run("rework answers", func(t *testing.T) {
		t.Parallel()
		w, none, _, _ := cannotAskEpisodeWorld(t)
		askTick(t, w, none)
		j := openCannotAsk(w, "s1-1")[0]
		p := Rework(w.s, ReworkReq{Sel: Sel{IDs: []string{"s1-1"}}, Fix: "reader unavailable: prepare the work again", Answers: []string{j.Note.ID}, Who: w.s.Coordinator})
		require.Empty(t, p.Refused)
		w.must(p)
		require.Empty(t, openCannotAsk(w, "s1-1"))
		require.Equal(t, Ready, w.s.Work.Card("s1-1").Col)
	})
}

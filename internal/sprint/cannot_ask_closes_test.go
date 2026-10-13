package sprint

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
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
	t.Run("wait parks and reappears at its bound", func(t *testing.T) {
		t.Parallel()
		w, none, _, _ := cannotAskEpisodeWorld(t)
		askTick(t, w, none)
		j := openCannotAsk(w, "s1-1")[0]
		until := w.s.Now.Add(30 * time.Minute)
		w.must(Wait(w.s, WaitReq{Note: j.Note.ID, Until: until, Who: w.s.Coordinator}))
		hold := w.notes[len(w.notes)-1]
		require.Equal(t, Acknowledged, hold.Kind)
		// The core world records the hold note; the store also indexes it in Acked.
		w.s.Acked = append(w.s.Acked, Open{Key: OpenKey(hold.ID, "s1-1"), Note: hold})
		raised, closed := askTick(t, w, none)
		require.Empty(t, raised)
		require.Empty(t, closed)
		require.Empty(t, openCannotAsk(w, "s1-1"))
		w.s.Now = until
		raised, closed = askTick(t, w, none)
		require.Len(t, closed, 1)
		require.Equal(t, hold.ID, closed[0].Note.ID)
		require.Len(t, raised, 1, "the explicit wait expires into one reminder")
		w.s.Acked = nil // apply the store's acknowledgement closure in the core world
		raised, closed = askTick(t, w, none)
		require.Empty(t, raised, "no reminder every tick")
		require.Empty(t, closed)
	})
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

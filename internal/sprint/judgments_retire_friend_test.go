package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Replay JudgmentRetire's RaiseFriend, FriendOff, Tick witness, including the
// three historical bare-name stall judgments and today's explicit friend row.
func TestAFriendOffTheTableRetiresItsJudgments(t *testing.T) {
	t.Parallel()
	for _, emptyRoster := range []bool{false, true} {
		t.Run(map[bool]string{false: "one friend stays", true: "last friends leave"}[emptyRoster], func(t *testing.T) {
			t.Parallel()
			w := setup(t, 0)
			w.s.Friends = []FriendSeat{{Name: "friend-a"}, {Name: "friend-b"}, {Name: "friend-c"}, {Name: "friend-d"}}
			const stalled = "a friend stalled: two wakes unanswered"
			for i, name := range []string{"friend-a", "friend-b", "friend-c", "friend-d"} {
				n := Note{ID: "stall-" + itoa(i), Kind: Judgment, Type: stalled, Primaries: []string{name}, At: w.s.Now,
					What: "friend " + name + " stalled 30m: two wakes unanswered"}
				w.s.Open = append(w.s.Open, Open{Key: OpenKey(n.ID, n.Primaries[0]), Note: n})
			}
			row := Note{ID: "row-idle", Kind: Judgment, Type: NFriendIdle, Primaries: []string{FriendRow("friend-a")}, At: w.s.Now}
			w.s.Open = append(w.s.Open, Open{Key: OpenKey(row.ID, row.Primaries[0]), Note: row})
			assert.Empty(t, Check(w.s, nil), "a judgment about a friend still on the table is not an orphan")
			var stay Plan
			RetireJudgments(w.s, &stay, MachineActor)
			assert.Empty(t, stay.Closes)

			w.s.Friends = nil
			if !emptyRoster {
				w.s.Friends = []FriendSeat{{Name: "friend-d"}}
			}
			w.s.Now = w.s.Now.Add(time.Minute)
			p := w.part(TickCheck, TickReq{})
			want := 4
			if emptyRoster {
				want = 5
			}
			assert.Len(t, p.Closes, want)
			answered := map[string]Note{}
			for _, n := range p.Notes {
				if n.Kind == Decided {
					answered[n.Answers] = n
				}
			}
			require.Len(t, answered, want)
			for _, n := range answered {
				assert.Equal(t, MachineActor, n.Who)
				assert.Contains(t, n.What, "not on the friends table")
			}
			assert.Empty(t, Check(w.s, nil), "retirement repairs rule 8 without changing its check")
			w.part(TickCheck, TickReq{})
			if emptyRoster {
				assert.Empty(t, w.s.Open)
			} else {
				require.Len(t, w.s.Open, 1)
				assert.Equal(t, "stall-3", w.s.Open[0].Note.ID)
			}
		})
	}
}

// Membership, not scheduling or session health, determines FriendOff.
func TestAFriendStillOnTheTableKeepsItsJudgment(t *testing.T) {
	t.Parallel()
	for _, status := range []string{Up, Down, Held} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			w := setup(t, 0)
			w.s.Friends = []FriendSeat{workerSeatFixture(FriendSeat{Name: "friend-a", Status: status})}
			w.s.Work.SetProps(map[string]string{PropFriends: SwitchOff})
			w.note(Note{ID: "idle", Kind: Judgment, Type: NFriendIdle, Primaries: []string{FriendRow("friend-a")}, At: w.s.Now})
			var p Plan
			RetireJudgments(w.s, &p, MachineActor)
			assert.Empty(t, p.Closes)
			assert.Empty(t, p.Notes)
		})
	}
}

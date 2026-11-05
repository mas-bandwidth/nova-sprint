package sprint

import (
	"fmt"
	"slices"
	"time"
)

// FriendDownReturnGrace is how long, in running time, a friend whose status is down keeps a
// card she took and never started before the tick returns it to the pool (friendDownReturn):
// five minutes from the later of her session's last answer to a challenge (AnsweredAt) and
// the card's deal or return to her row, so a friend who drops for a moment keeps her row, and one who
// stays down frees it for a friend or a member up.
const FriendDownReturnGrace = 5 * time.Minute

// friendDownReturn is the tick's return of the cards a friend took and never started while
// her status is down (docs/SPEC-SPRINT.md section 1, a friend's card; tla/FriendUnhookGrace.tla
// DownReturn): each work card ready on the row of a friend down (not held: a hold keeps no
// card already), that she has not started (startedNow, friendStarted: her beat naming it, a
// progress stamp), that carries no report, and that is not her hard pin (a head carried from
// an earlier taker's push stays on the card for the next taker, as any withdrawal keeps it),
// once FriendDownReturnGrace has passed since the later of her last answer and the card's
// deal or return to her row (WorkDeadline), is withdrawn at its next generation, its primary back to
// ready, no redeal spent, so the deal gives it to someone up. She is not barred from it (no
// FieldTakenFrom): she may be dealt it again when she is up. A card working with no start of
// hers goes back ready on her row first (friendUnstartedWorking), and is returned here on a
// later tick; a card she started stays hers until its deadline, as before. skip is the cards
// another unit of the tick moves already. On 2026-11-05 emma went down holding 16 cards taken
// and not started, the roots of 60 waiting cards, for 22 minutes: friend take --all-unstarted
// refused them ("a take is recorded on it") and nothing else returned them.
func friendDownReturn(s *Snapshot, seats []FriendSeat, since func(string) (time.Duration, bool), skip map[string]bool) (p Plan, returned map[string]bool) {
	returned = map[string]bool{}
	for _, f := range seats {
		if f.Status != Down || f.Held {
			continue
		}
		row := FriendRow(f.Name)
		heard := ""
		if !f.AnsweredAt.IsZero() {
			heard = stamp(f.AnsweredAt)
		}
		for _, c := range s.Fleet.Cell(row, Ready) {
			if c.F("kind") != "work" || skip[c.ID] || startedNow(c) || friendStarted(s, f, c) || c.F(FieldReported) != "" {
				continue
			}
			pr := s.Work.Placed(c.F("primary"))
			if pr == nil || pinnedTo(pr, f.Name) {
				continue
			}
			// the card's own clock as every deadline reads it (WorkDeadline: its deal to her, or
			// its return to ready on her row since, a lane gone or a take-back), and her last answer
			field, _, _, own := WorkDeadline(s, c)
			at := slices.Max([]string{heard, c.F(field), c.F(own)})
			if d, ok := since(at); !ok || d < FriendDownReturnGrace {
				continue
			}
			set := map[string]string{FieldTakenBack: "returned to the pool: friend " + f.Name + " is down and never started it", "untaken_since": stamp(s.Now)}
			p.Units = append(p.Units, withdrawUnit(s, c, set, []string{"first_taken"}, NTakenBack, MachineActor,
				fmt.Sprintf("%s %s:ready -> withdrawn (friend %s is down; taken and not started, past the grace %s)", c.ID, row, f.Name, FriendDownReturnGrace)))
			returned[c.ID] = true
		}
	}
	return p, returned
}

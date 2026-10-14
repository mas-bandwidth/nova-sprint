package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A member's beat carries the words nova-tools pkg/member sends (drainArgs, noRoomArgs):
// --draining, when the binary it runs was replaced under it, and --no-room <why>, when it
// is at its disk floor. The record holds each (Beat.Draining, Beat.NoRoom) and the member
// stays up: a draining member takes no new card while its running cards finish, and a
// member at its floor starts no card, and neither is a lapse. The deal passes both by as
// it passes a quiet machine (notQuiet), naming the word in its refusal (quietWhy), and the
// drain's own word is shown (NoRoomNow, DrainWord). The server refusing these words on the
// wire made a draining member read down for the length of its drain
// (the-server-beat-accepts-draining-and-no-room; docs/SPEC-SPRINT.md, the fleet; the beat).
func TestADrainingOrNoRoomBeat(t *testing.T) {
	t.Parallel()
	t.Run("ADrainingOrNoRoomBeatKeepsTheMemberUp", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 10, 11, 18, 45, 0, 0, time.UTC)
		why := "free disk on /slots is 0.0 GiB, under the floor of 10 GiB"

		// a member at its disk floor: its why is kept and shown, and it stays up
		b := Beat{At: now, Load: 5, NoRoom: why}
		assert.False(t, b.Draining, "a no-room beat is not a drain")
		assert.Equal(t, why, NoRoomNow(b, now), "the beat's no-room why is what the server holds")
		assert.Equal(t, Up, MemberStatus(nil, b, now), "a member at its disk floor is up, not down")
		s := &Snapshot{NoRoom: map[string]string{"m1": NoRoomNow(b, now)}}
		assert.False(t, s.Draining("m1"))
		assert.Equal(t, []string{"m2"}, notQuiet(s, []string{"m1", "m2"}), "the deal gives it no card")
		assert.Equal(t, "starts no card: m1 ("+why+")", quietWhy(s, []string{"m1", "m2"}), "the refusal names the why")

		// a member that drains: the first beat stamps the drain and keeps it up
		draining, since := DrainingFrom(Beat{}, true, now)
		require.True(t, draining)
		assert.Equal(t, now, since)
		bd := Beat{At: now, Load: 5, Draining: draining, DrainingSince: since}
		assert.Equal(t, DrainWord(since), NoRoomNow(bd, now), "a draining member's word is the drain's")
		assert.Equal(t, Up, MemberStatus(nil, bd, now), "a draining member is up, not down")
		ds := &Snapshot{NoRoom: map[string]string{"m1": NoRoomNow(bd, now)}}
		assert.True(t, ds.Draining("m1"), "the server holds the drain: %q", ds.NoRoom["m1"])
		assert.Equal(t, []string{"m2"}, notQuiet(ds, []string{"m1", "m2"}), "the deal gives it no new card")

		// both words: the drain's word, then the member's own reason
		bd.NoRoom = why
		assert.Equal(t, DrainWord(since)+"; "+why, NoRoomNow(bd, now))

		// a later drain beat keeps the first stamp; a beat without the word clears it
		_, kept := DrainingFrom(bd, true, now.Add(5*time.Second))
		assert.Equal(t, since, kept, "a later drain beat keeps the first one's stamp")
		cleared, zero := DrainingFrom(bd, false, now.Add(5*time.Second))
		assert.False(t, cleared)
		assert.True(t, zero.IsZero(), "a beat without --draining clears the drain")
	})

	// A fresh beat that says it drains is a drain's word, and a stale one says nothing.
	// (Its other assertions -- both words, notQuiet, DrainingFrom's stamp kept and cleared
	// -- were exact copies of the subtest above, from the former drain_test.go.)
	t.Run("ADrainingBeatIsADrainsWord", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
		since := now.Add(-time.Hour)
		b := Beat{At: now, Draining: true, DrainingSince: since}
		assert.True(t, IsDrainWord(NoRoomNow(b, now)))
		b.NoRoom = "free disk under the floor"
		assert.Empty(t, NoRoomNow(b, now.Add(2*BeatDeadline)), "a stale beat says nothing")
		assert.False(t, IsDrainWord("free disk under the floor"))
	})

	// With no member out of room the deal passes every member and its refusal names none.
	// (Its no-room notQuiet and quietWhy assertions were exact copies of the first subtest,
	// from the former fleet_noroom_test.go.)
	t.Run("NoRoomIsOutOfTheDealWithItsWord", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, quietWhy(&Snapshot{}, []string{"m1", "m2"}))
		assert.Equal(t, []string{"m1", "m2"}, notQuiet(&Snapshot{}, []string{"m1", "m2"}))
	})
}

//go:build functional

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Ghost reads, end to end (v1.2.15 BLOCKER, a-dealt-read-is-held-or-returned-and-ask-agrees-
// with-itself): a read card dealt to a member that takes it and never reports (its beat names
// it held) is returned within HeldGrace plus one tick and re-dealt to another member, never
// left working to the 60-minute read deadline.

// livePlaced is the placed (ready or working) read cards of the primary.
func livePlaced(all []*sprint.Card) []*sprint.Card {
	var out []*sprint.Card
	for _, c := range all {
		if c.Placed() && c.Col != sprint.Withdrawn {
			out = append(out, c)
		}
	}
	return out
}

// TestAReadDealtToAMemberThatNeverTakesItIsReDealtWithinTheBound pins the held bound on a
// store: a read card dealt to member A, whose process takes it and never reports (its beat
// names it held past HeldGrace), is retired lost (spending no one) and dealt to member B
// within HeldGrace plus one tick; ask and ask --another agree throughout, the doctor's
// readers check FAILs naming the ghost before the return, and once B reads ok the primary
// reaches merging (docs/SPEC-SPRINT.md section 6, the reads).
func TestAReadDealtToAMemberThatNeverTakesItIsReDealtWithinTheBound(t *testing.T) {
	t.Parallel()
	wall := time.Now()
	ta := newTestApp(t)
	ta.live = []string{"m1"} // m2, m3 and m4 beat only when the test says so
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-m2,reader-m3,reader-m4 --members m1:8,m2:8,m3:8,m4:8")
	ta.ok("set --read-cards on")
	ta.ok("reader set reader-m2 --tiers all")
	ta.ok("reader set reader-m3 --tiers all")
	ta.ok("reader set reader-m4 --tiers all")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"),
		[]byte(passingBrief("s1-1: a flash card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick") // presence: m1 up (m1 beats from ta.live)
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-1.w1@1 --report done --head " + landHead)
	ta.ok("fleet beat m2 --live - --load 0")
	ta.ok("fleet beat m3 --live - --load 0")
	ta.ok("fleet beat m4 --live - --load 0")
	ta.ok("tick") // m2, m3 and m4 up from this tick's presence part
	ta.ok("fleet beat m2 --live - --load 0")
	ta.ok("fleet beat m3 --live - --load 0")
	ta.ok("fleet beat m4 --live - --load 0")
	ta.ok("tick") // and one of them is dealt the read

	_, all := readsOn(t, ta, "s1-1")
	placed := livePlaced(all)
	require.Len(t, placed, 1, "the read is dealt to one member, never its worker m1")
	rc := placed[0]
	holder := rc.Row
	require.NotEqual(t, "m1", holder, "never the worker")
	var rest []string
	for _, m := range []string{"m2", "m3", "m4"} {
		if m != holder {
			rest = append(rest, m)
		}
	}
	require.Len(t, rest, 2, "two other members for the re-deal and the agree check")

	// member A takes the read, then its beat names it held: the read stays working
	ta.ok("take --as " + holder + " --limit 1")
	held := sprint.LiveKey(rc.ID, 1) + ":" + sprint.LiveHeld
	ta.ok("fleet beat " + holder + " --live " + held + " --load 0")
	ta.ok("tick") // held_since is recorded
	_, all = readsOn(t, ta, "s1-1")
	require.Len(t, livePlaced(all), 1, "named held within HeldGrace: the read stays working")

	// held past HeldGrace: before the tick that re-deals it, the doctor's readers check
	// FAILs naming the ghost, and ask and ask --another agree (ask refuses "asked already",
	// ask --another never says "not asked yet"); within HeldGrace plus one tick the read is
	// re-dealt to another member, the beats kept fresh all the way.
	var checked bool
	var elapsed time.Duration
	for elapsed <= sprint.HeldGrace+sprint.BeatDeadline {
		ta.step(10 * time.Second)
		elapsed += 10 * time.Second
		ta.ok("fleet beat " + holder + " --live " + held + " --load 0")
		ta.ok("fleet beat " + rest[0] + " --live - --load 0")
		ta.ok("fleet beat " + rest[1] + " --live - --load 0")
		if !checked && elapsed > sprint.HeldGrace {
			// the read is held past HeldGrace and not yet re-dealt (docs/SPEC-SPRINT.md
			// section 6, the reads).
			code, doc, docErr := ta.do("doctor --area readers")
			require.NotZero(t, code, "doctor FAILs while the read is held past HeldGrace\n%s%s", doc, docErr)
			require.Contains(t, doc, rc.ID, "doctor names the ghost: %s", doc)
			code, _, askErr := ta.do("ask s1-1")
			require.NotZero(t, code, "ask refuses while a read card stands: %s", askErr)
			require.Contains(t, askErr, "asked already", "ask names its reads out: %s", askErr)
			_, _, anotherErr := ta.do("ask s1-1 --another")
			require.NotContains(t, anotherErr, "not asked yet", "ask --another must not say it was never asked: %s", anotherErr)
			// ask --another cut one more read card; hand it back (no verdict) so the tick's
			// own deal is what re-deals the ghost, the third member left for it.
			_, all2 := readsOn(t, ta, "s1-1")
			var extra *sprint.Card
			for _, c := range all2 {
				if c.Placed() && c.ID != rc.ID {
					extra = c
				}
			}
			require.NotNil(t, extra, "ask --another cut one more read card")
			ta.ok("read --as " + extra.Row + " --return " + extra.ID + " --reason 'agree check'")
			checked = true
		}
		ta.ok("tick")
		_, all = readsOn(t, ta, "s1-1")
		placed = livePlaced(all)
		if len(placed) == 1 && placed[0].Row != holder && placed[0].Row != "m1" {
			break
		}
	}
	_, all = readsOn(t, ta, "s1-1")
	placed = livePlaced(all)
	require.Len(t, placed, 1, "the read is dealt again in the same tick's deal")
	require.NotEqual(t, holder, placed[0].Row, "dealt to another member, never the worker's or the ghost's")
	require.NotEqual(t, "m1", placed[0].Row, "never the worker")
	require.LessOrEqual(t, elapsed, sprint.HeldGrace+sprint.BeatDeadline, "back within HeldGrace plus one tick")
	var old *sprint.Card
	for _, c := range all {
		if c.ID == rc.ID {
			old = c
		}
	}
	require.NotNil(t, old)
	require.Equal(t, sprint.RetiredByLost, old.F("retired_by"), "retired lost, spending no one")

	// once B reads ok the primary reaches merging
	ta.ok("take --as " + placed[0].Row + " --limit 1")
	ta.ok("read --as " + placed[0].Row + " --ok " + placed[0].ID)
	ta.ok("tick")
	var v struct {
		Column string `json:"column"`
	}
	ta.json("card s1-1", &v)
	require.Equal(t, "merging", v.Column, "the primary reaches merging once B reads ok")
	t.Logf("wall time %s", time.Since(wall).Round(time.Millisecond))
}

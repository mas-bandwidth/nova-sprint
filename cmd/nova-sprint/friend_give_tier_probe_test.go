package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The manual command must consult the synced roster's tier admission just as
// the planner does (docs/SPEC-SPRINT.md section 1, a friend's card).
func TestManualFriendGiveUsesTheSyncedRosterTierAdmission(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"pro", "flash"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			ta, _ := friendApp(t, "amy") // config's fixture row serves flash only
			ta.ok("friend sync --root " + t.TempDir())
			ta.beatUp("amy")
			ta.ok("add --stream s1 --count 1 --one")
			ta.ok("tier s1-1 --set " + tier + " --reason tier-admission-probe")
			// Keep the machine stopped so this manual verb commits directly;
			// a running machine queues work-table writes until its pump.

			st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
			require.NoError(t, err)
			seats, err := st.FriendSeats(context.Background(), ta.a.now())
			require.NoError(t, err)
			require.Len(t, seats, 1, "the roster really contains the target friend")
			require.Equal(t, "amy", seats[0].Name)
			require.Equal(t, "flash", seats[0].Class, "the stored roster excludes pro")
			before := freshCard(ta, "s1-1")
			require.Equal(t, tier, before.Tier, "the CLI stores the intended deal tier")
			require.Equal(t, sprint.Ready, before.Primary.Col)
			require.Empty(t, before.Work, "this is a never-dealt card, not an undo of friend take")

			code, out, errs := ta.do("friend give amy s1-1")
			after := freshCard(ta, "s1-1")
			t.Logf("friend roster class=%s; card tier=%s; friend give exit=%d\nstdout:\n%s\nstderr:\n%s\nafter state=%s work=%v", seats[0].Class, tier, code, out, errs, after.Primary.Col, after.Work)
			for _, work := range after.Work {
				t.Logf("committed work id=%s row=%s col=%s primary tier=%s", work.ID, work.Row, work.Col, after.Tier)
			}
			if tier == "pro" {
				assert.Equal(t, 1, code, "a pro card cannot be handed to a flash-only friend")
				assert.Contains(t, errs, "does not serve tier pro")
				assert.NotContains(t, out, "handed to amy")
				assert.Equal(t, sprint.Ready, after.Primary.Col, "a refused card stays ready")
				assert.Empty(t, after.Work, "a refusal creates no work card on the friend's row")
				return
			}
			assert.Zero(t, code, out+errs)
			assert.Contains(t, out, "handed to amy", "the matching-tier control reaches the actual handout")
			require.Len(t, after.Work, 1)
			assert.Equal(t, sprint.FriendRow("amy"), after.Work[0].Row)
			assert.Equal(t, sprint.Working, after.Primary.Col)
		})
	}
}

// Undo only clears the take-back mark. It remains valid after the card's tier
// changes beyond the original friend's class, and must not create a new handout.
func TestManualFriendGiveUndoesTakeBackAfterTheCardTierChanges(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.a.tip = func(context.Context, string, string) (string, error) { return "", nil }
	ta.ok("friend sync --root " + t.TempDir())
	ta.beatUp("amy")
	brief := filepath.Join(t.TempDir(), "s1-1.md")
	require.NoError(t, os.WriteFile(brief, []byte(passingBrief("s1-1: a tier-changing undo card\nREPO: mas-bandwidth/nova-tools\nWHO: friend amy")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	ta.ok("tier s1-1 --set flash --reason initial-compatible-tier")
	ta.ok("friend give amy s1-1")
	ta.ok("friend take amy s1-1 --reason returned-before-start")
	taken := freshCard(ta, "s1-1")
	require.Len(t, taken.Work, 1)
	require.Equal(t, sprint.FriendRow("amy"), taken.Work[0].F(sprint.FieldTakenFrom))
	require.Equal(t, sprint.Withdrawn, taken.Work[0].Col)
	require.Equal(t, sprint.Ready, taken.Primary.Col)

	ta.ok("tier s1-1 --set pro --reason changed-after-take-back")
	changed := freshCard(ta, "s1-1")
	require.Equal(t, "pro", changed.Tier)
	require.Equal(t, sprint.FriendRow("amy"), changed.Work[0].F(sprint.FieldTakenFrom))

	code, out, errs := ta.do("friend give amy s1-1 --reason undo-take-back")
	assert.Zero(t, code, out+errs)
	assert.Contains(t, out, "may be dealt to amy again (undo-take-back)")
	assert.NotContains(t, errs, "does not serve tier pro")
	after := freshCard(ta, "s1-1")
	require.Len(t, after.Work, 1, "the undo creates no second work card")
	assert.Equal(t, taken.Work[0].ID, after.Work[0].ID)
	assert.Empty(t, after.Work[0].F(sprint.FieldTakenFrom))
	assert.Equal(t, sprint.Withdrawn, after.Work[0].Col, "the undo is not a fresh wrong-tier handout")
	assert.Equal(t, sprint.Ready, after.Primary.Col)
	assert.Equal(t, "pro", after.Tier)
}

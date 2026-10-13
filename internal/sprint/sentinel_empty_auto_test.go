package sprint

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// This gate uses only baseline APIs so RED is a missing runtime behavior,
// rather than a newly introduced symbol or compiler failure.
func TestP8EmptySentinelReleasesWithoutCoordinator(t *testing.T) {
	s := newWorld(t).s
	s.Work.SetRows([]string{"s"})
	c := &Card{ID: "stop", Row: "s", Col: Waiting, Score: 1, Fields: map[string]string{"kind": "sentinel", "held": "yes"}}
	s.Work.Put(c)
	units, _, _, landing := SentinelReleases(s, MachineActor)
	require.True(t, landing[c.ID], "zero producers must auto-release despite a legacy held mark and absent reached judgment")
	require.Len(t, units, 1)
	require.Contains(t, units[0].Notes[0].What, "no producers")
}

package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFleetSyncCoverHeldInInventory: HeldInInventory names the members of want
// the coordinator holds down and refuses the rest. Its main path is the
// coordinator's hold, returned sorted by name; its refusal is a member the sync
// held (the sync's own mark, which the sync leaves), a member no one holds, and
// a name with no control card at all. A hold outside want is not that sync's to
// report.
func TestFleetSyncCoverHeldInInventory(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		up, holds  []string // members brought up, then held by the coordinator, in order
		syncM1Only bool     // a sync that names m1 alone a member and m2 a machine
		want       []string // the inventory's members, each at DefaultWidth
		held       []string
	}{
		{"the coordinators hold is named", []string{"m1"}, []string{"m1"}, false, []string{"m1"}, []string{"m1"}},
		{"the syncs hold is left alone", []string{"m1", "m2"}, nil, true, []string{"m1", "m2"}, nil},
		{"a member no one holds is not named", []string{"m1"}, nil, false, []string{"m1"}, nil},
		{"a name with no control card is not named", nil, nil, false, []string{"ghost"}, nil},
		{"holds are named sorted, not in the want order", []string{"m2", "m1"}, []string{"m2", "m1"}, false, []string{"m2", "m1"}, []string{"m1", "m2"}},
		{"a hold outside want is not named", []string{"m1", "m2"}, []string{"m1"}, false, []string{"m2"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-a")
			for _, m := range c.up {
				w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: m}))
			}
			for _, m := range c.holds {
				w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: m, Who: "coordinator"}))
			}
			if c.syncM1Only {
				w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync", Sync: []SyncMember{{Name: "m1", Width: DefaultWidth}}, Machines: []string{"m1", "m2"}}))
			}
			var want []SyncMember
			for _, m := range c.want {
				want = append(want, SyncMember{Name: m, Width: DefaultWidth})
			}
			assert.Equal(t, c.held, HeldInInventory(w.s, want))
		})
	}
}

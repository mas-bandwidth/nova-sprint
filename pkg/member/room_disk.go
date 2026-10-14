package member

import (
	"fmt"
	"strings"
)

// DiskFloorPctDefault is the default --disk-floor-pct: the percent free below
// which a fleet member starts no card. 0 turns the floor off. The member's
// flag is wired in the repository that builds the member's Config; this is
// the default it uses (docs/SPEC-SPRINT.md, the fleet member's disk floor).
const DiskFloorPctDefault = 15.0

// DiskRoom is Config.Room of a fleet member whose bench root (the root its
// job directories live under) sits on a disk: it reports false, and a why
// naming the percent free and the floor, while the root's free space is under
// floorPct percent (avail*100/total < floorPct); floorPct 0 turns the floor
// off. It returns true when the statfs read fails: an unreadable disk is
// reported in the why but does not stop the member, because the existing
// preflight's MISSING reading covers that case. free is injected (the
// production value is StatfsFree, a syscall.Statfs of root; the tests pass a
// fake). The word this returns is the member's NoRoom word, carried by its
// beat and read by the deal, which passes the member by while it is set: the
// model is tla/NoRoom.tla (read), WordIsThisTicks.
func DiskRoom(root string, floorPct float64, free func(string) (avail, total uint64, err error)) func() (bool, string) {
	return func() (bool, string) {
		avail, total, err := free(root)
		if err != nil {
			return true, "unreadable disk at " + root + ": " + err.Error()
		}
		pct := float64(avail) * 100 / float64(total)
		if floorPct > 0 && pct < floorPct {
			return false, fmt.Sprintf("free disk %.0f%% is under the floor %.0f%%", pct, floorPct)
		}
		return true, ""
	}
}

// ComposeRooms is Config.Room of a fleet member whose disk floor (DiskRoom) is
// composed with an existing Room: it refuses when either refuses, joining the
// whys, so a member under either its disk floor or the existing room starts no
// card.
func ComposeRooms(a, b func() (bool, string)) func() (bool, string) {
	return func() (bool, string) {
		okA, whyA := a()
		okB, whyB := b()
		if !okA || !okB {
			return false, strings.TrimSpace(strings.Join([]string{whyA, whyB}, "; "))
		}
		return true, ""
	}
}

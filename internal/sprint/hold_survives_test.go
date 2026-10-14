package sprint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A coordinator's hold (fleet hold, no held_by mark) survives every path that
// unsets or overwrites a member's hold: a sync's removal of a member whose
// machine row left the inventory, its rejoin, and each mechanism that holds
// and releases a member it did not hold (the probe pass, the environment
// fault clear, the sync's drift release, the adopt step). docs/SPEC-SPRINT.md
// section 5, the fleet; tla/DealFill.tla PersonHoldStays.

// coordHeld is a world with the server host m1 up, then held by the
// coordinator (no held_by) with reason R and a finish marker.
func coordHeld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t, "reader-a")
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: "m1", Who: "coordinator", Reason: "R", Finish: true}))
	ctl := w.s.MemberCtl("m1")
	require.NotEmpty(t, ctl.F("held"), "m1 is held: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "m1 is held by the coordinator: %v", ctl.Fields)
	require.Equal(t, "R", ctl.F(FieldHeldReason), "m1's reason: %v", ctl.Fields)
	require.NotEmpty(t, ctl.F(FieldHeldFinish), "m1's finish marker: %v", ctl.Fields)
	return w
}

// holdSurvives asserts a coordinator's hold is intact on ctl: held stays set,
// the mark stays empty, and the reason and finish marker are unchanged.
func holdSurvives(t *testing.T, ctl *Card) {
	t.Helper()
	require.NotNil(t, ctl)
	require.NotEmpty(t, ctl.F("held"), "the hold was dropped: %v", ctl.Fields)
	require.Empty(t, ctl.F(FieldHeldBy), "the coordinator's empty mark was overwritten: %v", ctl.Fields)
	require.Equal(t, "R", ctl.F(FieldHeldReason), "the reason was dropped: %v", ctl.Fields)
	require.NotEmpty(t, ctl.F(FieldHeldFinish), "the finish marker was dropped: %v", ctl.Fields)
}

// TestACoordinatorHoldSurvivesASyncRemovalAndRejoin: the server host is up,
// the coordinator holds it (with and without --finish), a sync runs with the
// server host's machine row missing (fleet_sync.go FleetDrift), the row comes
// back and a second sync runs. At every step the hold, its empty mark, its
// reason and its finish marker stay, and the member is never up.
func TestACoordinatorHoldSurvivesASyncRemovalAndRejoin(t *testing.T) {
	t.Parallel()
	for _, finish := range []bool{false, true} {
		t.Run(fmt.Sprintf("finish=%v", finish), func(t *testing.T) {
			t.Parallel()
			w := newWorld(t, "reader-a")
			w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
			require.Contains(t, w.s.UpMembers(), "m1", "the server host is up")

			hold := FleetReq{Op: "hold", Member: "m1", Who: "coordinator", Reason: "R", Finish: finish}
			w.must(FleetStep(w.s, hold))
			ctl := w.s.MemberCtl("m1")
			require.NotEmpty(t, ctl.F("held"), "the coordinator holds m1")
			require.Empty(t, ctl.F(FieldHeldBy), "the coordinator's hold carries no mark")
			require.Equal(t, "R", ctl.F(FieldHeldReason), "its reason")
			if finish {
				require.NotEmpty(t, ctl.F(FieldHeldFinish), "its finish marker")
			} else {
				require.Empty(t, ctl.F(FieldHeldFinish), "no finish marker")
			}

			// a sync with the server host's machine row missing: it leaves the
			// fleet, its control card off the table, held by whoever held it
			w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync"}))
			rec := w.s.Fleet.Card(CtlID("m1"))
			require.NotNil(t, rec, "the control card's record is kept")
			require.False(t, rec.Placed(), "its control card is off the table")
			require.NotEmpty(t, rec.F("held"), "the hold survives the removal")
			require.Empty(t, rec.F(FieldHeldBy), "the hold stays the coordinator's")
			require.Equal(t, "R", rec.F(FieldHeldReason), "its reason is unchanged")
			if finish {
				require.NotEmpty(t, rec.F(FieldHeldFinish), "its finish marker is unchanged")
			} else {
				require.Empty(t, rec.F(FieldHeldFinish))
			}

			// the row comes back: RejoinMembers places the same control card
			w.place(w.s.Fleet, CtlID("m1"), "m1", Ctl)
			require.NotNil(t, w.s.MemberCtl("m1"), "the card is placed again")

			// a second sync with the row back must not release the hold
			w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
				Sync: []SyncMember{{"m1", DefaultWidth}}, Machines: []string{"m1"}}))
			ctl = w.s.MemberCtl("m1")
			require.NotNil(t, ctl, "the member is back")
			require.NotEmpty(t, ctl.F("held"), "the hold survives the second sync")
			require.Empty(t, ctl.F(FieldHeldBy), "the hold stays the coordinator's")
			require.Equal(t, "R", ctl.F(FieldHeldReason), "its reason is unchanged")
			if finish {
				require.NotEmpty(t, ctl.F(FieldHeldFinish), "its finish marker is unchanged")
			} else {
				require.Empty(t, ctl.F(FieldHeldFinish))
			}

			// it beats: a held member is never up
			presenceWith(w, "m1")
			assert.NotContains(t, w.s.UpMembers(), "m1", "the held member is never listed up")
		})
	}
}

// TestNoUnsetterTouchesACoordinatorsHold: table-driven over every path that
// unsets or overwrites a member's hold, run on a member held by the
// coordinator (no held_by), and the hold, reason and finish marker survive.
// The release is excepted: it is the coordinator's own act. The go/ast check
// below fails on any site that unsets "held" and is not named in the table.
func TestNoUnsetterTouchesACoordinatorsHold(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		site string // file.go:line of the "held" unset, "" for a write that is not an unset
		run  func(t *testing.T, w *world)
	}{
		{
			name: "the passed probe", site: "steps_work.go:3222",
			run: func(t *testing.T, w *world) {
				w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1"}))
				holdSurvives(t, w.s.MemberCtl("m1"))
			},
		},
		{
			name: "the environment fault clear", site: "steps_work.go:2897",
			run: func(t *testing.T, w *world) {
				w.s.MemberCtl("m1").Fields[FieldEnvFault] = "push"
				var u Unit
				clearEnvFault(w.s, "m1", &u)
				w.must(Plan{Units: []Unit{u}})
				holdSurvives(t, w.s.MemberCtl("m1"))
			},
		},
		{
			name: "the environment probe patch", site: "balance.go:180",
			run: func(t *testing.T, w *world) {
				_, unset := EnvProbePatch(w.s.MemberCtl("m1"), EnvProbePassed)
				require.NotContains(t, unset, "held", "EnvProbePatch unsets a coordinator's hold: %v", unset)
				require.NotContains(t, unset, FieldHeldBy, "EnvProbePatch unsets a coordinator's mark: %v", unset)
			},
		},
		{
			name: "the sync's drift release", site: "fleet_sync.go:255",
			run: func(t *testing.T, w *world) {
				w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync",
					Sync: []SyncMember{{"m1", DefaultWidth}}, Machines: []string{"m1"}}))
				holdSurvives(t, w.s.MemberCtl("m1"))
			},
		},
		{
			name: "the adopt step", site: "fleet_back.go:173",
			run: func(t *testing.T, w *world) {
				require.False(t, AdoptingHeld(w.s.MemberCtl("m1")), "a coordinator's hold is not the adopt's")
				p, _ := FleetBackPresence(&marksAdopter{eps: map[string]string{}, runs: map[string]MachineAdoption{}})(w.s, beating(w, "m1"))
				w.must(p)
				holdSurvives(t, w.s.MemberCtl("m1"))
			},
		},
		{
			name: "the sync's removal head", site: "",
			run: func(t *testing.T, w *world) {
				w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync"}))
				rec := w.s.Fleet.Card(CtlID("m1"))
				require.NotNil(t, rec, "the control card's record is kept")
				require.False(t, rec.Placed(), "its control card is off the table")
				holdSurvives(t, rec)
			},
		},
		{
			name: "the rejoin", site: "",
			run: func(t *testing.T, w *world) {
				w.must(FleetStep(w.s, FleetReq{Op: "sync", Who: "sync"}))
				w.place(w.s.Fleet, CtlID("m1"), "m1", Ctl)
				holdSurvives(t, w.s.MemberCtl("m1"))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := coordHeld(t)
			c.run(t, w)
		})
	}

	// the release is the coordinator's own act: named so the go/ast check
	// knows it, and it alone clears a coordinator's hold.
	excepted := map[string]string{
		"hold_cause.go:247": "the release (holdFields), the coordinator's own act",
	}
	named := map[string]bool{}
	for _, c := range cases {
		if c.site != "" {
			named[c.site] = true
		}
	}
	for k := range excepted {
		named[k] = true
	}
	for s := range heldUnsetSites(t) {
		assert.Truef(t, named[s], "a site unsets \"held\" and is not named in the table: %s; add it (and its guard) to the cases", s)
	}
}

// heldUnsetSites is every file.go:line in the package where the string
// literal "held" is written as an unset: appended to an `unset` slice, passed
// as a variadic unset to setEntry/moveEntry, or in the holdFields slice.
func heldUnsetSites(t *testing.T) map[string]struct{} {
	t.Helper()
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	fset := token.NewFileSet()
	sites := map[string]struct{}{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		node, err := parser.ParseFile(fset, f, nil, 0)
		require.NoError(t, err)
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if (fn.Name == "setEntry" || fn.Name == "moveEntry") && len(call.Args) >= 3 {
				for _, a := range call.Args[2:] {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Value == `"held"` {
						sites[siteKey(fset, lit)] = struct{}{}
					}
				}
			}
			if fn.Name == "append" && len(call.Args) >= 2 {
				if id, ok := call.Args[0].(*ast.Ident); ok && strings.HasSuffix(id.Name, "unset") {
					for _, a := range call.Args[1:] {
						if lit, ok := a.(*ast.BasicLit); ok && lit.Value == `"held"` {
							sites[siteKey(fset, lit)] = struct{}{}
						}
					}
				}
			}
			return true
		})
		ast.Inspect(node, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, nm := range vs.Names {
				if nm.Name != "holdFields" || len(vs.Values) == 0 {
					continue
				}
				if cl, ok := vs.Values[0].(*ast.CompositeLit); ok {
					for _, el := range cl.Elts {
						if lit, ok := el.(*ast.BasicLit); ok && lit.Value == `"held"` {
							sites[siteKey(fset, lit)] = struct{}{}
						}
					}
				}
			}
			return true
		})
	}
	return sites
}

func siteKey(fset *token.FileSet, n ast.Node) string {
	p := fset.Position(n.Pos())
	return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
}

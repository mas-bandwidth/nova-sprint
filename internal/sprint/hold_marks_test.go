package sprint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A member's hold is three fields of its control card: held, held_by
// (FieldHeldBy) and the status beside them. The mark in held_by says who made
// the hold -- none (the coordinator's), the sync, the machine for a fault, the
// tick while it adopts, the machine for an environment fault -- and every
// reader of the hold is written against those marks. This test is the
// mechanical check the reader could not be: a table of every mark, the step
// that writes it and the release that clears it, and a scan of the package
// that every held_by write names one of them (docs/SPEC-SPRINT.md section 5,
// the fleet; section 11, hold; tla/DealFill.tla PersonHoldStays and
// FaultHoldClears; tla/ProviderBudget.tla).

// holdMarkRow is one mark of a member's hold: how the writer spells it, the
// step that writes it, the release that clears it, whether it is the machine's
// own mark-scoped release (a machine mark, not the coordinator's), and the
// source spellings that write it (for the package scan).
type holdMarkRow struct {
	name    string
	mark    string
	auto    bool
	write   func(t *testing.T, w *world, member string)
	release func(t *testing.T, w *world, member string)
	// written is how the mark is spelled in a held_by write in the source: a
	// constant, or a carrier whose value is one of the marks.
	written []string
}

// holdByCarriers are the variables the source passes an existing held_by value
// through: downPlan carries FleetReq.HeldBy (the coordinator's "", the sync's,
// the fault's) and RestoreShape restores a shape's stored held_by. A carrier is
// not a mark itself; the marks it may hold are the table's.
var holdByCarriers = []string{"r.HeldBy", "by"}

// holdMarks is every mark a member's hold carries, in the order the card names
// them. Each is written by the step its own writer is, never by setting fields
// by hand (docs/SPEC-SPRINT.md section 11, hold).
var holdMarks = []holdMarkRow{
	{
		name:  "none",
		mark:  "",
		write: coordinatorHold,
		// fleet up (the verb's Op release) lifts the coordinator's hold; a
		// person's release is the one release that lifts any hold, so it is
		// the none mark's own and is not in the foreign-release matrix.
		release: fleetUpRelease,
	},
	{
		name:    "sync",
		mark:    HeldBySync,
		auto:    true,
		write:   syncHold,
		release: syncRelease,
		written: []string{"HeldBySync"},
	},
	{
		name:    "fault",
		mark:    HeldByFault,
		auto:    true,
		write:   faultDown,
		release: faultClearRelease,
		written: []string{"HeldByFault"},
	},
	{
		name:    "adopt",
		mark:    HeldByAdopt,
		auto:    true,
		write:   adoptHold,
		release: adoptFinish,
		written: []string{"HeldByAdopt"},
	},
	{
		name:    "environment",
		mark:    HeldByEnv,
		auto:    true,
		write:   envFaultHold,
		release: envFaultRelease,
		written: []string{"HeldByEnv"},
	},
	{
		name:    "idle-reap",
		mark:    HeldByIdleReap,
		auto:    true,
		write:   idleReapHoldMark,
		release: idleReapRelease,
		written: []string{"HeldByIdleReap"},
	},
}

// idleReapHoldMark is the idle-reap mark: the member's takes ended with no model call
// IdleReapHoldAfter times on a route another member served, and idleReapHolds, the
// deal's, holds it with its cause (hold_cause.go).
func idleReapHoldMark(t *testing.T, w *world, member string) {
	t.Helper()
	other := "m2"
	if member == "m2" {
		other = "m1"
	}
	for i := 1; i <= IdleReapHoldAfter; i++ {
		putReap(w, member+"-reap-"+itoa(i)+".w1", "ds", member, w.s.Now.Add(-time.Duration(i)*time.Minute))
	}
	putOK(w, other+"-served.w1", "ds", other, w.s.Now.Add(-time.Minute))
	var p Plan
	idleReapHolds(w.s, TickReq{}, &p)
	w.must(p)
}

// idleReapRelease is the idle-reap mark's own release: its judgment answered, the tick's
// holdCauseClear ends the hold it is the cause of, and no hold with another cause or none.
func idleReapRelease(t *testing.T, w *world, member string) {
	t.Helper()
	for _, o := range w.openOn(StreamSubject(MemberSubject(member))) {
		if o.Note.Type == NMemberIdleReaps {
			w.closeAll([]Open{o})
		}
	}
	var p Plan
	holdCauseClear(w.s, TickReq{Beats: freshBeats(w, member, "m2")}, &p)
	if len(p.Units) > 0 {
		w.must(p)
	}
}

// held says the control card is held with this row's mark: held_by is the
// mark, and the held stamp is beside it. The machine's fault mark is the one
// hold whose status is down rather than held (presence marks it for the tick
// to lift), so its held stamp is empty and FaultHeld is the test.
func (r holdMarkRow) held(ctl *Card) bool {
	if ctl == nil || ctl.F(FieldHeldBy) != r.mark {
		return false
	}
	if r.mark == HeldByFault {
		return FaultHeld(ctl)
	}
	return ctl.F("held") != ""
}

// cleared says the member's hold is gone: no held stamp and no mark.
func markCleared(ctl *Card) bool {
	return ctl != nil && ctl.F("held") == "" && ctl.F(FieldHeldBy) == ""
}

// markWorld is two up members at width 2 and four ready primaries in s1: room
// to deal and to level so a hold's absence from the deal can be seen.
func markWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 2}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m2", Width: 2}))
	w.must(Add(w.s, AddReq{Stream: "s1", Count: 4}))
	return w
}

// coordinatorHold is the none mark: fleet hold with no HeldBy, the
// coordinator's own (downPlan, steps_work.go).
func coordinatorHold(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: member, Who: "coordinator"}))
}

// fleetUpRelease is fleet up, the verb's Op release: the coordinator's own
// release (fleetStepPlan, steps_work.go).
func fleetUpRelease(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(FleetStep(w.s, FleetReq{Op: "release", Member: member}))
}

// syncHold is the sync's hold: the downPlan the sync plans, carrying HeldBySync
// (fleetSyncPlan, fleet_sync.go). set[FieldHeldBy] is written through
// FleetReq.HeldBy, exactly as the sync's writer does.
func syncHold(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(FleetStep(w.s, FleetReq{Op: "hold", Member: member, Who: "sync", HeldBy: HeldBySync}))
}

// syncRelease is the sync's own release: the member back in the inventory with
// room, which unsets held and held_by for a sync mark alone (FleetDrift
// DriftRelease, fleet_sync.go).
func syncRelease(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(FleetStep(w.s, FleetReq{Op: "sync",
		Sync:     []SyncMember{{Name: member, Width: 2}, {Name: "m2", Width: 2}},
		Machines: []string{member, "m2"}}))
}

// faultDown is the machine's fault hold: presence sees the member stop
// beating, downPlan marks it held_by=fault with the fault stamp (presence,
// presence.go; the mark is read and lifted by faultClear, steps_tick.go).
func faultDown(t *testing.T, w *world, member string) {
	t.Helper()
	beats := map[string]Beat{}
	for _, m := range []string{"m1", "m2"} {
		if m != member {
			beats[m] = Beat{At: w.s.Now}
		}
	}
	p, _ := presence(w.s, TickReq{Beats: beats})
	w.must(p)
}

// faultClearRelease is faultClear after the member's clean finish: the
// machine's own release of its own fault hold (steps_tick.go,
// FaultHoldClears).
func faultClearRelease(t *testing.T, w *world, member string) {
	t.Helper()
	w.s.Fleet.Put(&Card{ID: member + "-ok-1", Row: member, Col: DoneOK, Score: 1, Rev: 1,
		Fields: map[string]string{"kind": "work", "primary": "s1-1", "finished": stamp(w.s.Now.Add(time.Minute))}})
	w.must(faultClear(w.s, TickReq{Beats: freshBeats(w, member, "m2")}))
}

// marksAdopter is the Adopter the adopt's tick reads, by hand: one release
// finishes the one adoption it was given.
type marksAdopter struct {
	eps  map[string]string
	runs map[string]MachineAdoption
}

func (a *marksAdopter) Target() string { return "v1.2.0" }

func (a *marksAdopter) Start(machine, version, episode string) bool {
	a.eps[machine] = episode
	return true
}

func (a *marksAdopter) Adoption(machine, episode string) MachineAdoption {
	if a.eps[machine] != episode {
		return MachineAdoption{}
	}
	return a.runs[machine]
}

// adoptHold is the adopt mark: holdAdopting, the unit the adopt's tick appends
// for a member back from down (FleetBackPresence, fleet_back.go). set maps
// FieldHeldBy to HeldByAdopt.
func adoptHold(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(Plan{Units: []Unit{holdAdopting(w.s, member, "v1.2.0")}})
}

// adoptFinish is the adopt's own release: adoptStep reads the adoption
// finished at the target and brings the member up, unsetting held and held_by
// (fleet_back.go). It acts on a member held adopting alone.
func adoptFinish(t *testing.T, w *world, member string) {
	t.Helper()
	a := &marksAdopter{eps: map[string]string{}, runs: map[string]MachineAdoption{}}
	if ctl := w.s.MemberCtl(member); ctl.F(FieldHeldBy) == HeldByAdopt {
		a.eps[member] = ctl.F(FieldAdoptSince)
		a.runs[member] = MachineAdoption{State: AdoptFinished, From: "v1.1.0", To: ctl.F(FieldAdoptTo)}
	}
	if u, ok := adoptStep(w.s, a, member, "coordinator"); ok {
		w.must(Plan{Units: []Unit{u}})
	}
}

// envFaultHold is the environment mark: a finish whose report is an
// environment fault calls envFaulted, which records the fault and, for a
// member not already held, takes the machine's own environment hold
// (set[FieldHeldBy] = HeldByEnv, steps_work.go).
func envFaultHold(t *testing.T, w *world, member string) {
	t.Helper()
	w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 4}}))
	cards := w.s.Fleet.Cell(member, Ready)
	require.NotEmpty(t, cards, "no ready card was dealt to %s", member)
	wc := cards[0].ID
	w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
	w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true,
		Report: "push refused: fatal: could not read Username for 'https://github.com': terminal prompts disabled"}))
}

// envFaultRelease is clearEnvFault, the change the member's next pushed
// finish runs: it unsets the environment's own hold (HeldByEnv) with the
// fault's record and closes the judgment (steps_work.go).
func envFaultRelease(t *testing.T, w *world, member string) {
	t.Helper()
	var u Unit
	clearEnvFault(w.s, member, &u)
	if len(u.Changes) > 0 || len(u.Closes) > 0 {
		w.must(Plan{Units: []Unit{u}})
	}
}

// TestAHoldMarkKeepsTheMemberOffTheDeal is the behaviour of every mark: the
// member is not up and is given no card by the deal or the level; a tick
// presence with a fresh beat for it leaves held and held_by as they were; and
// the mark's own release clears it (docs/SPEC-SPRINT.md section 5, the fleet;
// section 11, hold; tla/DealFill.tla PersonHoldStays).
func TestAHoldMarkKeepsTheMemberOffTheDeal(t *testing.T) {
	for _, row := range holdMarks {
		row := row
		t.Run(row.name, func(t *testing.T) {
			w := markWorld(t)
			row.write(t, w, "m1")
			ctl := w.s.MemberCtl("m1")
			require.True(t, row.held(ctl), "the writer did not hold m1 with %q: %v", row.name, ctl.Fields)
			require.NotContains(t, w.s.UpMembers(), "m1", "a held member is not up: %v", ctl.Fields)

			ready, work := w.s.Fleet.Count("m1", Ready), w.s.Fleet.Count("m1", Working)
			w.must(Deal(w.s, DealReq{Sel: Sel{Limit: 10}}))
			require.LessOrEqual(t, w.s.Fleet.Count("m1", Ready), ready, "the deal gave the held member a card")
			require.LessOrEqual(t, w.s.Fleet.Count("m1", Working), work, "the deal gave the held member work")

			ready = w.s.Fleet.Count("m1", Ready)
			lvl := w.must(FleetStep(w.s, FleetReq{Op: "level"}))
			require.LessOrEqual(t, w.s.Fleet.Count("m1", Ready), ready, "the level gave the held member a card")
			for _, u := range lvl.Units {
				require.NotContains(t, u.Moved, "-> m1", "the level moved a card onto the held member: %s", u.Moved)
			}

			held, by := w.s.MemberCtl("m1").F("held"), w.s.MemberCtl("m1").F(FieldHeldBy)
			p, _ := TickPresence(w.s, TickReq{Beats: freshBeats(w, "m1", "m2")})
			w.must(p)
			ctl = w.s.MemberCtl("m1")
			require.Equal(t, held, ctl.F("held"), "TickPresence changed held: %v", ctl.Fields)
			require.Equal(t, by, ctl.F(FieldHeldBy), "TickPresence changed held_by: %v", ctl.Fields)

			row.release(t, w, "m1")
			require.True(t, markCleared(w.s.MemberCtl("m1")), "the mark's own release did not clear it: %v", w.s.MemberCtl("m1").Fields)
		})
	}
}

// TestAnotherMarksReleaseLeavesTheHold: a machine's own releases are
// mark-scoped -- faultClear lifts only HeldByFault, clearEnvFault only
// HeldByEnv, the adopt's finish only HeldByAdopt, the sync's release only
// HeldBySync -- so applying another mark's release leaves a member held with
// its own mark. The coordinator's fleet up is a person's release and may lift
// any hold, so it is not in this matrix (docs/SPEC-SPRINT.md section 11, hold;
// tla/DealFill.tla PersonHoldStays).
func TestAnotherMarksReleaseLeavesTheHold(t *testing.T) {
	for _, row := range holdMarks {
		for _, other := range holdMarks {
			if other.name == row.name || !other.auto {
				continue
			}
			row, other := row, other
			t.Run(row.name+"_vs_"+other.name, func(t *testing.T) {
				w := markWorld(t)
				row.write(t, w, "m1")
				other.release(t, w, "m1")
				ctl := w.s.MemberCtl("m1")
				require.True(t, row.held(ctl), "%s's release cleared %s's hold: %v", other.name, row.name, ctl.Fields)
				require.Equal(t, row.mark, ctl.F(FieldHeldBy), "%s's release changed held_by", other.name)
			})
		}
	}
}

// TestEveryHeldByWriteNamesAMark is the enumeration: every value assigned to
// the held_by field in the package's non-test .go files (the forms
// name[FieldHeldBy] = X and FieldHeldBy: X) must be a mark in the table, or a
// carrier the source passes a mark through; a value no row names fails, naming
// it and its file:line, so a new hold kind cannot ship without the table
// checked against it (docs/SPEC-SPRINT.md section 5, the fleet).
func TestEveryHeldByWriteNamesAMark(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	known := map[string]bool{}
	for _, row := range holdMarks {
		for _, e := range row.written {
			known[e] = true
		}
	}
	for _, c := range holdByCarriers {
		known[c] = true
	}
	reIndex := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*\[\s*FieldHeldBy\s*\]\s*=\s*([^\n]+)`)
	reField := regexp.MustCompile(`FieldHeldBy\s*:\s*([^,\n}]+)`)
	found := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		require.NoError(t, err)
		for i, line := range strings.Split(string(data), "\n") {
			for _, re := range []*regexp.Regexp{reIndex, reField} {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					val := strings.TrimSpace(m[1])
					found++
					require.Truef(t, known[val],
						"%s:%d: held_by is written %s, which no mark in the table names", name, i+1, val)
				}
			}
		}
	}
	require.NotZero(t, found, "the scan found no held_by write: the scan is broken")
}

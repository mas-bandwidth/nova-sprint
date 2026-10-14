package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// The doctor (the owner, 2026-10-10 ~04:18Z: "sprint doctor can check this, but i still
// dislike these silent stops/failures"). This is its stops layer, one check per kind of
// automatic stop (internal/sprint/stops.go, LiveStops) and the seat's waits (SeatWaitsOf),
// each live stop listed with its age, its effect and the verb that undoes it. The card
// the-doctor-checks-every-layer-and-refuses-a-red builds the layers below it (binary, store,
// routes and providers, machines, readers, friends, cards) onto this verb; each stop is
// said under the layer it stops.

func init() {
	verbEffect["doctor"] = "inspection: reads only, writes nothing; --all runs nova-doctor run alongside this sprint doctor concurrently, prints both named reports and preserves each exit and stderr (with --json, one envelope holds both reports); --area scopes the sprint checks; --preflight remains the candidate-binary gate and cannot be combined with --all. Otherwise it reports every definition-of-done fault, one line each with evidence (the seat check's machinery verdicts, automatic stops with age, effect and undo verb, broken always-true rules, a dry deal with waiting cards, and overdue seat judgments), then every desired-state check against the sprint and hosts: ready cards the deal does not take and why, routes each tier can draw, readers, loops that did not dial again, each member's tools, push credential, seat, disk and build, the lander's loop, fleet rows against nova-config, resting providers, the seat push loop, and members failing on their environment"
}

// stopLayer is the doctor's layer a kind of stop is said under.
var stopLayer = map[string]string{
	sprint.StopKindProviderRest:  "routes",
	sprint.StopKindRouteRest:     "routes",
	sprint.StopKindMemberDown:    "machines",
	sprint.StopKindMemberAdopt:   "machines",
	sprint.StopKindFriendStalled: "friends",
	sprint.StopKindFriendDown:    "friends",
	sprint.StopKindPinWaits:      "cards",
	sprint.StopKindStreamStopped: "cards",
}

// doctorLayers is the layers the stops are said under, in the doctor's order, then the seat.
var doctorLayers = []string{"routes", "machines", "friends", "cards"}

// doctorFaults names each definition-of-done fault in one line with its evidence, the
// joins the doctor is allowed to make (the owner, 2026-10-10; docs/SPEC-SPRINT.md, "The
// seat check", and section 9, "What is always true"):
//   - every machinery verdict the seat check reached DOWN: the friends and the fleet up,
//     the loop ticking, the dashboard answering with fresh data (JudgeSeatCheck; a member
//     that does not beat is its verdict there, never a second classification here);
//   - every automatic stop the coordinator's ledger holds, with its age, effect and undo
//     verb (store.StopsNow, LiveStops);
//   - every always-true violation Check found: a card working with no live work, the
//     read, merge and land flows a step out of place (sprint.Check);
//   - the deal dry with cards waiting (nothing ready to feed the fleet); and
//   - a judgment waiting on the seat past its deadline (SeatWaitsOf).
func doctorFaults(r sprint.SeatCheckReport, rec store.StopsRecord, violations []sprint.Violation, snap *sprint.Snapshot, now time.Time) []string {
	faults := []string{}
	for _, line := range r.Lines {
		if !line.Up {
			faults = append(faults, line.String())
		}
	}
	for _, stop := range rec.Stops {
		faults = append(faults, "STOP "+oneline.Escape(stop.Line(now)))
	}
	for _, v := range violations {
		faults = append(faults, "RULE "+oneline.Escape(v.String()))
	}
	if snap != nil && !snap.FleetOff() {
		if ready, waiting := len(snap.Work.Column(sprint.Ready)), len(snap.Work.Column(sprint.Waiting)); ready == 0 && waiting > 0 {
			faults = append(faults, fmt.Sprintf("MACHINERY deal DOWN ready=0 waiting=%d: nothing ready to feed the fleet", waiting))
		}
	}
	if rec.Seat.Overdue > 0 {
		faults = append(faults, "MACHINERY seat DOWN "+oneline.Escape(rec.Seat.Line(now)))
	}
	return faults
}

// cmdDoctor is `nova-sprint doctor`: each layer GREEN, or RED with every automatic stop that
// holds in it, one line each with its age, effect and undo verb; then every definition-of-done
// fault (the seat check's DOWN machinery verdicts, section 9's broken rules, the deal dry and
// the seat's waits), one line each with its evidence; then the checks of the desired state
// (internal/sprint/doctor.go: the fleet against nova-config and each member's host, the
// friends, the readers, the routes and providers, the ready cards the deal does not take, the
// lander, the seat), one line each, DOCTOR OK|WARN|DRIFT|FAIL <check> <subject> <evidence>
// remedy=<verb>. It counts them now from a fresh read, whether or not the machine ticks, and
// writes nothing. --area runs one group. Exit 0 when nothing is RED or FAIL, 1 when a fault
// holds or a check fails, 2 when the store does not read.
func (a *app) cmdDoctor(args []string, stdout, stderr io.Writer) int {
	const name = "doctor"
	fs, c := a.verbSetup(name)
	all := fs.Bool("all", false, "run nova-doctor and the sprint doctor in parallel")
	area := fs.String("area", "", "run one group of checks: "+strings.Join(sprint.DoctorAreas, ", ")+" (the sprint area is the stops, rules and machinery lines)")
	preflight := fs.String("preflight", "", "run the release gate's frame against this candidate binary before a switch or adopt: its version and its tick --shadow --json against the store this doctor reads, the store's tables against the candidate's TABLES.lock (a locked-shape refusal is one FAIL whose evidence is the shadow's own error text, naming the columns and the nova-table commands that mend them); the gate's later checks are WARN rows until their cards land; exit 1 on any FAIL")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, name, "takes no positional words: it checks the whole sprint")
	}
	if *area != "" && !slices.Contains(sprint.DoctorAreas, *area) {
		return refuse(stderr, name, "--area names "+oneline.Escape(*area)+", which is no area: "+strings.Join(sprint.DoctorAreas, ", "))
	}
	if *all {
		if *preflight != "" {
			return refuse(stderr, name, "--all cannot be combined with --preflight: it checks a candidate binary, not the live sprint")
		}
		pair := runDoctorPair(
			func() doctorRunResult { return runNovaDoctor(c.json) },
			func() doctorRunResult {
				var out, errb strings.Builder
				code := a.cmdDoctorParsed(*c, *area, "", &out, &errb)
				return doctorRunResult{code: code, stdout: out.String(), stderr: errb.String()}
			},
		)
		if c.json {
			printDoctorPairJSON(stdout, pair)
		} else {
			printDoctorPair(stdout, stderr, pair)
		}
		return pairExit(pair)
	}
	return a.cmdDoctorParsed(*c, *area, *preflight, stdout, stderr)
}

func (a *app) cmdDoctorParsed(c common, area, preflight string, stdout, stderr io.Writer) int {
	const name = "doctor"
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	if preflight != "" {
		return a.cmdDoctorPreflight(ctx, c, preflight, stdout, stderr)
	}
	rec, err := st.StopsNow(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s: %s; run: nova-sprint where\n", prog, name, oneline.Escape(noSprintYet(err).Error()))
		return 2
	}
	check, snap, err := st.Check(ctx, 5)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s: %s; run: nova-sprint where\n", prog, name, oneline.Escape(noSprintYet(err).Error()))
		return 2
	}
	now := a.now()
	machinery := a.withConfigSeat(a.seatCheck(ctx, st, c.redis))
	faults := doctorFaults(machinery, rec, check.Violations, snap, now)
	checks, refused, err := a.doctorChecks(ctx, st, machinery.Measures, rec, area)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s: %s; run: nova-sprint where\n", prog, name, oneline.Escape(noSprintYet(err).Error()))
		return 2
	}
	sprintArea := area == "" || area == sprint.AreaSprint
	if !sprintArea {
		faults, refused = nil, nil
	}
	// The refused-verb checks (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form
	// checks"): the server's own record, keyed by epoch, names a member whose beats it refused
	// (member-beat) and every verb word it refused (refused-verbs), before and independent of
	// the member-down stop that names only the symptom.
	refusedFaults := append(sprint.RefusedBeatLines(refused, now), sprint.RefusedVerbLines(refused, now)...)
	refusedBeat := sprint.RefusedBeats(refused, now)
	byLayer := map[string][]sprint.Stop{}
	for _, x := range rec.Stops {
		layer := stopLayer[x.Kind]
		if layer == "" {
			layer = "cards"
		}
		byLayer[layer] = append(byLayer[layer], x)
	}
	counts := map[string]int{}
	for _, ch := range checks {
		counts[ch.Status]++
	}
	red := len(faults) > 0 || len(refusedFaults) > 0 || counts[sprint.DoctorFail] > 0
	code := 0
	if red {
		code = 1
	}
	if c.json {
		type stopJSON struct {
			sprint.Stop
			Layer      string `json:"layer"`
			AgeSeconds int64  `json:"age_seconds"`
		}
		stops := []stopJSON{}
		if sprintArea {
			for _, layer := range doctorLayers {
				for _, x := range byLayer[layer] {
					stops = append(stops, stopJSON{Stop: x, Layer: layer, AgeSeconds: int64(x.Age(now) / time.Second)})
				}
			}
		}
		status := "ok"
		if red {
			status = "red"
		}
		if faults == nil {
			faults = []string{}
		}
		if checks == nil {
			checks = []sprint.DoctorCheck{}
		}
		if refusedFaults == nil {
			refusedFaults = []string{}
		}
		b, err := json.Marshal(map[string]any{"verb": name, "status": status, "exit": code, "at": now.UTC(), "area": area, "faults": faults, "refused": refusedFaults, "machinery": machinery, "stops": stops, "seat": rec.Seat,
			"checks": checks, "counts": counts, "fix_safe": sprint.DoctorFixSafe})
		if err != nil {
			fmt.Fprintf(stderr, "%s %s: %s; run: nova-sprint doctor\n", prog, name, oneline.Escape(err.Error()))
			return 2
		}
		fmt.Fprintln(stdout, string(b))
		return code
	}
	if sprintArea {
		// the refused-verb FAILs are printed before the automatic stop lines: a beat the
		// server refused is the cause, the member-down stop the symptom, so the cause is read
		// first (docs/SPEC-SPRINT.md, "The doctor's refused-verb and beat-form checks").
		for _, line := range refusedFaults {
			fmt.Fprintln(stdout, "DOCTOR FAIL "+line)
		}
		for _, layer := range doctorLayers {
			stops := byLayer[layer]
			if len(stops) == 0 {
				fmt.Fprintf(stdout, "DOCTOR %s GREEN: no automatic stop holds\n", layer)
				continue
			}
			fmt.Fprintf(stdout, "DOCTOR %s RED: %d automatic stops hold\n", layer, len(stops))
			for _, x := range stops {
				fmt.Fprintln(stdout, "  STOP "+oneline.Escape(x.Line(now)))
			}
		}
		for _, fault := range faults {
			fmt.Fprintln(stdout, "DOCTOR fault "+fault)
		}
		if snap != nil {
			for _, m := range snap.Members() {
				if len(refusedBeat[m]) == 0 {
					fmt.Fprintln(stdout, sprint.RefusedBeatOK(m))
				}
			}
		}
		seat := "GREEN"
		if rec.Seat.Overdue > 0 {
			seat = "RED"
		}
		fmt.Fprintf(stdout, "DOCTOR seat %s: %s\n", seat, oneline.Escape(rec.Seat.Line(now)))
	}
	for _, ch := range checks {
		fmt.Fprintln(stdout, oneline.Escape(ch.Line()))
	}
	word := "OK"
	if red {
		word = "RED"
	}
	fmt.Fprintf(stdout, "DOCTOR %s stops=%d judgments=%d overdue=%d faults=%d checks=%d fail=%d warn=%d drift=%d\n", word, len(rec.Stops), rec.Seat.Judgments, rec.Seat.Overdue, len(faults),
		len(checks), counts[sprint.DoctorFail], counts[sprint.DoctorWarn], counts[sprint.DoctorDrift])
	return code
}

// envExitChecks is the doctor's member-env line for each member with at least
// sprint.EnvExitMinN children in the last hour (docs/SPEC-SPRINT.md, "An environment exit"):
// the counts come from the attempt records on the epoch's cards (sprint.EnvExitChildren:
// each take's and read run's consumer record, its end and when), and each is
// sprint.EnvExitCheck's DOCTOR FAIL|WARN|OK member-env line, a FAIL or WARN with the first
// line of the last harness tail and the card to read. A member under the floor is no line.
// now is the app's clock: the store's read snapshot carries none.
func envExitChecks(snap *sprint.Snapshot, now time.Time) []sprint.DoctorCheck {
	if snap == nil {
		return nil
	}
	var out []sprint.DoctorCheck
	for _, c := range sprint.EnvExitChildren(snap, now, time.Hour) {
		if ch, ok := sprint.EnvExitCheck(c.Member, c.Env, c.N, c.Last, c.Card); ok {
			out = append(out, ch)
		}
	}
	return out
}

// doctorChecks reads what the checks of the desired state need (store.DoctorRead, nova-config,
// the server's start, the seat's build, each member's host) and runs them, the area's alone when
// area names one. A reach the outside does not configure is left unmeasured, and its check says so.
func (a *app) doctorChecks(ctx context.Context, st *store.Store, m sprint.SeatCheckMeasures, rec store.StopsRecord, area string) ([]sprint.DoctorCheck, []sprint.RefusedVerb, error) {
	read, err := st.DoctorRead(ctx)
	if err != nil {
		return nil, nil, err
	}
	if area == sprint.AreaSprint {
		return nil, read.Refused, nil
	}
	// every reach past the store under the run's deadline (doctorRunBudget)
	ctx, cancel := context.WithTimeout(ctx, doctorRunBudget)
	defer cancel()
	o := a.outside
	if o.serverAddr == nil {
		o = a.realOutside()
	}
	in := sprint.DoctorInput{Snap: read.Snap, Req: read.Req, ReaderBeats: read.ReaderBeats, FriendBeats: read.FriendBeats, Hook: m.Hook, Seat: rec.Seat}
	in.HostGitEmail = hostGitEmail(ctx)
	var cfg doctorConfig
	if o.doctorConfig != nil {
		if cfg, err = o.doctorConfig(ctx); err != nil {
			in.ConfigErr = err.Error()
		} else {
			in.Machines = cfg.Machines
		}
	} else {
		in.ConfigErr = "not measured: no config reach configured"
	}
	self := ""
	if o.hostname != nil {
		self = o.hostname()
	}
	// the server: the address this process knows, the seat's record, else the server loop's
	// own --listen in nova-config (sprint-server-<this host>)
	addr := m.Server.Addr
	if ls := listenOf(cfg.Loops["sprint-server-"+self]); addr == "" && len(ls) > 0 {
		addr = ls[0]
	}
	switch {
	case o.serverStarted == nil:
		in.ServerHow = "not measured: no server probe configured"
	case addr == "":
		in.ServerHow = "no server address: NOVA_SPRINT_SERVER is not set and nova-config has no loop sprint-server-" + self
	default:
		in.ServerStarted, in.ServerHow = o.serverStarted(ctx, addr)
	}
	if o.declared != nil {
		in.Declared = o.declared(ctx)
	}
	probe := area == "" || area == sprint.AreaFleet
	switch {
	case !probe:
	case o.probeMembers == nil:
		in.ProbeErr = "no host probe configured"
	case in.Machines == nil:
		in.ProbeErr = "nova-config not read: " + in.ConfigErr
	default:
		var members []sprint.DoctorMachine
		for _, mm := range in.Machines {
			if slices.Contains(read.Snap.Members(), mm.Name) {
				members = append(members, mm)
			}
		}
		tools := func(member string) []string {
			return append(slices.Clone(sprint.MemberTools), sprint.MemberHarnesses(read.Snap, member)...)
		}
		in.Probes = o.probeMembers(ctx, members, tools, self)
	}
	checks := sprint.Doctor(in)
	// The member's environment exits, counted from the attempt records on the epoch's cards
	// (docs/SPEC-SPRINT.md, "An environment exit"): a member whose children end 134 or 137
	// (HardEnvExit; a no-result at another exit is an environment exit, recorded but not in
	// the count) over the rate is named under member-env, with the first line of the last
	// harness tail and the card to read (sprint.EnvExitChildren, EnvExitCheck).
	checks = append(checks, envExitChecks(in.Snap, a.now())...)
	// The beat-form probe runs only where the doctor's own reaches do: a test gives its own
	// outside, and only the real one reaches a member's host (docs/SPEC-SPRINT.md, "The
	// doctor's refused-verb and beat-form checks").
	if a.outside.serverAddr == nil && probe && in.Probes != nil {
		forms := a.probeBeatForms(ctx, in.Machines, self)
		checks = append(checks, beatFormChecks(in.Machines, in.Probes, forms, in.Declared)...)
	}
	if area == "" {
		return checks, read.Refused, nil
	}
	var out []sprint.DoctorCheck
	for _, ch := range checks {
		if ch.Area == area {
			out = append(out, ch)
		}
	}
	return out, read.Refused, nil
}

// hostGitEmail is the host's global git user.email, read by the doctor for the git-identity
// check; "" when it cannot be read (no global config, or git fails).
func hostGitEmail(ctx context.Context) string {
	out, err := gitrun.Output(ctx, gitrun.Options{Timeout: 5 * time.Second}, "config", "--global", "user.email")
	if err != nil {
		return ""
	}
	return out
}

package verbs

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// checkSprint is check's health of the sprint, read from one snapshot: each
// stream's repository and base at origin, each worker bench's service, the
// open judgements by kind, who runs the sprint, the width and why nothing
// deals, why each stream's producers wait and its long NEEDS chains, and
// each landing's wait.
func (e *Engine) checkSprint(ctx context.Context, r *checkReport, who map[string]bus.Presence) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		r.red(err)
		return
	}
	heads := e.checkStreams(ctx, r, snap)
	if line, f := identityCheck(snap); f != nil {
		r.red(f)
	} else {
		r.ok("%s", line)
	}
	e.checkPolicy(ctx, r, snap)
	e.checkCards(ctx, r, snap)
	checkWorking(r, snap, who)
	e.checkWorkers(ctx, r, snap, who)
	e.checkWorkerVersions(ctx, r, snap)
	checkJudgements(r, snap)
	running := e.checkRun(ctx, r, who)
	ws := steps.Waits(snap.Cards)
	e.checkWidth(ctx, r, snap, heads, running, ws)
	e.checkWaits(ctx, r, snap, ws)
	e.checkLandings(ctx, r, snap, heads, running)
}

// checkStreams is one line per stream: stream add's admission again
// (admitStream: its repository reachable at its clone address and its base
// there, its card branches creatable, its forge able to act), every stream
// at once. It returns each readable stream's base tip.
func (e *Engine) checkStreams(ctx context.Context, r *checkReport, snap store.Snapshot) map[string]string {
	heads := map[string]string{}
	if len(snap.Streams) == 0 {
		r.ok("streams: none yet (nova-sprint stream add <s> --repo <org/repo> --forge github)")
		return heads
	}
	if e.Origin == nil {
		r.red(broken("the git edge is not configured, so no stream's repository can be read", "take it to the project's own tracker with this line"))
		return heads
	}
	ss := streamsInOrder(snap)
	shas, errs, agree := make([]string, len(ss)), make([]error, len(ss)), make([][2]string, len(ss))
	reds, redErrs := make([][]string, len(ss)), make([]error, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss { // the streams' reads are independent: all at once
		wg.Go(func() {
			shas[i], errs[i] = e.admitStream(ctx, s, "nova-sprint check")
			agree[i][0], agree[i][1] = e.agreementWarning(ctx, snap, s)
			if errs[i] == nil {
				reds[i], redErrs[i] = e.baseRed(ctx, s, shas[i])
			}
		})
	}
	wg.Wait()
	for i, s := range ss {
		sha, err := shas[i], errs[i]
		if err != nil {
			r.red(admissionWhat(s.Name, err))
			continue
		}
		heads[s.Name] = sha
		r.ok("stream %s: %s reachable, base %s at %s", s.Name, s.Repo, s.Base, shortSHA(sha))
		checkStreamGates(r, s)
		if agree[i][0] != "" {
			r.warn(agree[i][0], agree[i][1])
		}
		at := shortSHA(sha)
		if redErrs[i] != nil {
			r.warn("stream "+s.Name+": the checks of "+s.Repo+"'s "+s.Base+" at "+at+" cannot be read ("+errText(redErrs[i])+"), so it is not known green",
				"gh auth status on this host shows who reads them; run check again")
		} else if len(reds[i]) > 0 {
			r.red(refuse("stream "+s.Name+": base "+s.Repo+" "+s.Base+" is red at "+at+": "+redChecks(reds[i])+", so no card of it lands",
				"make "+s.Repo+"'s "+s.Base+" green at "+at+" (cards for that are admitted), then check again"))
		}
	}
	return heads
}

// checkStreamGates is one line per gate of stream s, each re-parsed as
// stream set admitted it (a restore or an older build is held to today's
// rule): ok when it is one command, red naming the fix when it is not.
func checkStreamGates(r *checkReport, s store.Stream) {
	for _, g := range s.Gates {
		if _, why := edge.ParseGate(g); why != "" {
			r.red(refuse("stream "+s.Name+": gate "+clip(g)+" is not one command: "+why, edge.GateFix+", then nova-sprint stream set "+s.Name+" --gate <command> again"))
			continue
		}
		r.ok("stream %s gate: %s (every card of the stream passes it at its finished head)", s.Name, g)
	}
}

// streamsInOrder is the streams by position, then name.
func streamsInOrder(snap store.Snapshot) []store.Stream {
	out := make([]store.Stream, 0, len(snap.Streams))
	for _, s := range snap.Streams {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b store.Stream) int {
		if a.Pos != b.Pos {
			return a.Pos - b.Pos
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func shortSHA(sha string) string { return sha[:min(len(sha), 12)] }

// checkJudgements is one red line per kind of open judgement: how many,
// which, the oldest's question, and the command that answers them.
func checkJudgements(r *checkReport, snap store.Snapshot) {
	open := openJudgements(snap)
	if len(open) == 0 {
		r.ok("judgements: none open")
		return
	}
	byKind := map[string][]judgementLine{}
	var kinds []string
	for _, j := range open {
		k := orWord(j.Kind, "unkinded")
		if _, seen := byKind[k]; !seen {
			kinds = append(kinds, k)
		}
		byKind[k] = append(byKind[k], j)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		r.red(judgementsOpen(k, byKind[k]))
	}
}

// judgementsOpen is one kind's open judgements, oldest first, as one
// refusal: the ids (five, then a count), the oldest's question, and its
// command with the id left for the reader when there are several.
func judgementsOpen(kind string, js []judgementLine) *Fail {
	first := js[0]
	cmd := orWord(first.Command, "nova-sprint show "+first.ID)
	if len(js) == 1 {
		return refuse(fmt.Sprintf("judgement %s is open: %s", first.ID, clipN(first.Question, 200)), cmd)
	}
	var ids []string
	for _, j := range js[:min(len(js), 5)] {
		ids = append(ids, j.ID)
	}
	list := strings.Join(ids, ", ")
	if more := len(js) - len(ids); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	return refuse(fmt.Sprintf("%d %s judgements are open (%s); the oldest: %s", len(js), kind, list, clipN(first.Question, 200)),
		strings.ReplaceAll(cmd, first.ID, "<id>")+" (each id; nova-sprint next lists them)")
}

// clipN is s on one line, at most n bytes.
func clipN(s string, n int) string {
	s = oneLine(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// checkRun is who holds the chooser lock: run on a host, and the sprint's
// name on the bus. It returns whether run serves the sprint.
func (e *Engine) checkRun(ctx context.Context, r *checkReport, who map[string]bus.Presence) bool {
	st, err := e.Doctor.Run(ctx)
	if f := runUnmet(st, err); f != nil {
		r.red(f)
		return false
	}
	if on, known := present(who, "sprint"); known && !on {
		r.red(refuse("run holds the lock on "+st.Host+" but the name sprint is not on the bus, so nothing is pushed", runFix))
		return true
	}
	r.ok("run on %s (pid %d)", st.Host, st.PID)
	return true
}

// checkWorkerVersions is one red line per hooked worker row whose binary
// (its beat's version) is below the run's release, which check takes as its
// own build's (the one up installs as run): the tick deals that row nothing
// (roster.Advertised; defect D3). A build that is not a release compares
// nothing and prints no line, as the tick does. Only the roster's bench
// machines are read, as Advertised pins only them: a friend hooks as a
// worker too and never beats. An unreadable roster prints nothing here (the
// width line names it).
func (e *Engine) checkWorkerVersions(ctx context.Context, r *checkReport, snap store.Snapshot) {
	version := e.Version
	if !roster.Behind("", version) || e.Roster == nil {
		return
	}
	ro, err := e.Roster.ReadRosterAt(ctx, 1)
	if err != nil {
		return
	}
	n, behind := 0, false
	for _, name := range slices.Sorted(maps.Keys(snap.Rows)) {
		row := snap.Rows[name]
		if _, bench := ro.Workers[name]; !bench || row.Session == "" {
			continue
		}
		n++
		if roster.Behind(row.Version, version) {
			r.red(refuse(fmt.Sprintf("worker %s: binary %s below run's %s", name, orWord(row.Version, "(unknown)"), version),
				"nova-fleet install --repo mas-bandwidth/nova-sprint --tag "+version+" "+name))
			behind = true
		}
	}
	if n > 0 && !behind {
		r.ok("worker binaries: %d hooked rows at the run's %s or above", n, version)
	}
}

// runUnmet is run not holding the chooser lock, or nil: check's run line and
// next's answer (unmet) from the same read.
func runUnmet(st run.ChooserStatus, err error) *Fail {
	switch {
	case err != nil:
		return classify(RefusalOr(err, broken("who runs the sprint cannot be read ("+errText(err)+")", "nova-sprint show run")))
	case !st.Held:
		return refuse("nothing runs the sprint: the chooser lock is free, so nothing is dealt or landed", runFix)
	case st.Operation != "run":
		return refuse(fmt.Sprintf("the chooser lock is held by %s on %s, not run", orWord(st.Operation, "an unknown holder"), orWord(st.Host, "an unknown host")), "nova-sprint show run names it; when it ends, "+runFix)
	}
	return nil
}

// noPolicy is check's red line for a store with no active policy: its fix
// works on any store, one up did not make included (L5.21).
func noPolicy() *Fail {
	return refuse("no policy is active: the tick runs on the built-in default", policy.FirstPolicyFix)
}

// unmet is the first of check's red lines before the sprint can deal: no
// commit identity, no active policy, and, for the coordinator, run not
// holding the chooser lock (a worker's next skips that read). The same facts
// and fixes check prints, so next never says start or wait while check is
// red on them (L6.5).
func (e *Engine) unmet(ctx context.Context, snap store.Snapshot, coordinator bool) *Fail {
	if _, f := identityCheck(snap); f != nil {
		return f
	}
	if snap.Policy == 0 {
		return noPolicy()
	}
	if !coordinator || e.Doctor == nil {
		return nil
	}
	return runUnmet(e.Doctor.Run(ctx))
}

// runFix is how run is brought back: its service's log, then up.
const runFix = "read the run service's log (nova-sprint show run), then nova-sprint up --as <coordinator>"

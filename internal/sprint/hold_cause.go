package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
)

// A COORDINATOR'S HOLD ENDS WHEN ITS CAUSE ENDS (the owner, 2026-10-13: "I am frustrated
// that our new machine, that is fast and awesome, keeps flipping to 'held' when we sprint.
// Fix this now."). mini-m5 sat held about ten hours on a free-text hold, "three
// idle-reaps in 15 minutes ... held until the cause is found", after the coordinator had
// found the cause (the providers, fleet-wide) and nothing ended it. The model is
// tla/HoldCause.tla.
//
//   - A member's hold carries a cause (hold --cause): a judgment (its alias j<n> or its id,
//     kept as the id the alias named, so an alias of a later epoch never stands for it), or
//     a doctor check on that member (doctor:<check>). The tick ends the hold in the step
//     that sees the cause end (holdCauseClear): the judgment answered or retired, or the
//     doctor's pass of that check on that member recorded after the hold
//     (DoctorPassPlan). The release is a line of the log, "released: cause <x> resolved",
//     addressed to the seat (NHoldReleased); a member that beats is up in that step.
//   - A hold with no cause still holds, and is never silent: every HoldNoCauseEvery it
//     stands the tick raises one judgment to the seat, "<m> held 2h by <who> with no
//     cause: release or name a cause" (holdNoCauseConds), the period's own, the last one
//     closed as it opens.
//   - An idle reap (a take that ended with no result and no model call) is attributed by
//     where it happened (idleReaps): on two or more members on one route in the window it
//     is the route's, which rests by the route-rest path (noModelCallRests, cause
//     RestNoModelCall) with its note to the seat, and no member is held; a member is held
//     for its reaps only when every one is on a route that served another member in the
//     window and no other member reaped there (idleReapHolds), the machine's hold, its
//     cause the judgment it raises (CauseIdleReap).

// The control card's fields of a hold's cause.
const (
	// FieldHeldCause is the cause as the hold named it: j<n> (or a judgment id),
	// doctor:<check>, or CauseIdleReap for the machine's hold of an idle-reaping member.
	FieldHeldCause = "held_cause"
	// FieldHeldCauseNote is the id of the judgment a j<n> cause named when the hold was
	// made: the judgment the tick reads, whatever an alias names later.
	FieldHeldCauseNote = "held_cause_note"
	// FieldHeldWho is who made the coordinator's hold, said in its report.
	FieldHeldWho = "held_who"
	// FieldDoctorPassed is the doctor's record of the checks that passed on a member held
	// on one: <check>=<stamp>, comma separated (DoctorPassPlan).
	FieldDoctorPassed = "doctor_passed"
	// FieldIdleReapMark is when the machine last held the member for its idle reaps: a reap
	// at or before it never holds the member again.
	FieldIdleReapMark = "idle_reap_mark"
)

// CausePrefixDoctor begins a cause that is a doctor check on the held member.
const CausePrefixDoctor = "doctor:"

// CauseIdleReap is the cause of the machine's hold of a member for its idle reaps: the
// judgment the hold raised (NMemberIdleReaps) on the member.
const CauseIdleReap = "idle-reap"

// HeldByIdleReap marks the machine's hold of a member for its idle reaps (FieldHeldBy).
const HeldByIdleReap = "idle-reap"

// The notes of a hold's cause.
const (
	// NHoldReleased is the happened note of a hold the tick ended because its cause ended,
	// addressed to the seat.
	NHoldReleased = "a member's hold ended with its cause"
	// NHoldNoCause is the judgment of a coordinator's hold that names no cause, raised
	// once every HoldNoCauseEvery it stands.
	NHoldNoCause = "a member is held with no cause"
	// NMemberIdleReaps is the judgment the machine raises when it holds a member for its
	// idle reaps: the hold's cause, so answering it ends the hold.
	NMemberIdleReaps = "a member's takes end with no model call"
)

// The bounds of the cause rules.
const (
	// HoldNoCauseEvery is how long a cause-less hold stands before it is reported, and
	// again every such period.
	HoldNoCauseEvery = 2 * time.Hour
	// IdleReapWindow is the window idle reaps are attributed over.
	IdleReapWindow = 15 * time.Minute
	// IdleReapRouteMembers is how many members reaping on one route make it the route's.
	IdleReapRouteMembers = 2
	// IdleReapHoldAfter is how many idle reaps of one member, each on a route that serves
	// another, hold it.
	IdleReapHoldAfter = 3
)

// RestNoModelCall is the cause of a route's rest for takes that ended with no model call
// on IdleReapRouteMembers or more members within IdleReapWindow: the route's, not the
// members'.
const RestNoModelCall = "no-model-call"

// doctorCheckNames is every doctor check a hold's cause may name.
var doctorCheckNames = []string{CheckReadyNotDealt, CheckRoutesPerTier, CheckReaders, CheckLoopsRedial, CheckMemberEnv, CheckLanderLoop,
	CheckFleetRows, CheckProviders, CheckSeat, CheckFinishFailures, CheckFriends, CheckDraining, CheckGitIdentity}

// DoctorCause is the check a doctor cause names, and whether the cause is one.
func DoctorCause(cause string) (string, bool) { return strings.CutPrefix(cause, CausePrefixDoctor) }

// holdCauseWhy is why a hold's cause is refused, "" when it may be held: a cause names a
// fleet member's hold alone; a doctor cause names a doctor check; a judgment cause names a
// judgment open now (one answered already would end the hold at once).
func holdCauseWhy(s *Snapshot, r HoldReq, targets []HoldTarget) string {
	if r.Cause == "" || r.Release {
		return ""
	}
	for _, t := range targets {
		if t.Kind != HoldMember {
			return "a cause names a fleet member's hold: " + t.Name + " is a " + holdKindWords[t.Kind]
		}
	}
	if check, ok := DoctorCause(r.Cause); ok {
		if !slices.Contains(doctorCheckNames, check) {
			return "--cause " + r.Cause + " names no doctor check: one of " + strings.Join(doctorCheckNames, ", ")
		}
		return ""
	}
	if r.CauseNote == "" || !judgmentOpen(s, r.CauseNote) {
		return "--cause " + r.Cause + " names no open judgment: a cause that has ended would end the hold at once; run: nova-sprint inbox"
	}
	return ""
}

// judgmentOpen says the judgment of that id is open on some subject.
func judgmentOpen(s *Snapshot, id string) bool {
	for _, o := range s.Open {
		if o.Note.ID == id && o.Note.Kind == Judgment {
			return true
		}
	}
	return false
}

// causeEnded is how the held member's cause ended, "" while it stands: the judgment is
// no longer open (answered or retired), the doctor's pass of the check was recorded after
// the hold, or the machine's idle-reap judgment on the member is no longer open.
func causeEnded(s *Snapshot, member string, ctl *Card) string {
	cause := ctl.F(FieldHeldCause)
	switch check, doctor := DoctorCause(cause); {
	case cause == "":
		return ""
	case cause == CauseIdleReap:
		for _, o := range s.Open {
			if o.Note.Type == NMemberIdleReaps && o.Subject() == StreamSubject(MemberSubject(member)) {
				return ""
			}
		}
		return "its judgment was answered"
	case doctor:
		at, ok := doctorPassedAt(ctl, check)
		if held := stampAt(ctl, "held"); !ok || held.IsZero() || at.Before(held) {
			return ""
		}
		return "the doctor's " + check + " check passed on " + member + " at " + stamp(at)
	default:
		if note := ctl.F(FieldHeldCauseNote); note != "" && !judgmentOpen(s, note) {
			return "the judgment was answered or retired"
		}
		return ""
	}
}

// doctorPassedAt is when the doctor last recorded the check passing on the member.
func doctorPassedAt(ctl *Card, check string) (time.Time, bool) {
	for _, kv := range Split(ctl.F(FieldDoctorPassed)) {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k != check {
			continue
		}
		at, err := time.Parse(time.RFC3339, v)
		return at, err == nil
	}
	return time.Time{}, false
}

// holdFields is every field of a member's hold: a release unsets them all.
var holdFields = []string{"held", FieldHeldBy, FieldHeldReason, FieldHeldFinish, FieldHeldCause, FieldHeldCauseNote, FieldHeldWho, FieldDoctorPassed}

// ctlWritten says a unit of the plan writes the member's control card already: the step
// writes a control card once, so a second write waits for the next tick.
func ctlWritten(p *Plan, member string) bool {
	id := CtlID(member)
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == Fleet && ch.Entry.ID == id {
				return true
			}
		}
	}
	return false
}

// holdCauseClear is the tick's end of every hold whose cause ended (tla/HoldCause.tla
// Tick, NoHoldOutlivesItsCause): the hold's fields go, a member whose beat is alive is up
// in this step (as unhold brings it up), and the release is a line of the log to the seat.
// A hold with no cause, and the machine's other holds (fault, environment, probe, sync,
// adopt), are not this rule's.
func holdCauseClear(s *Snapshot, r TickReq, p *Plan) {
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") == "" || ctl.F(FieldHeldCause) == "" || ctlWritten(p, m) {
			continue
		}
		how := causeEnded(s, m, ctl)
		if how == "" {
			continue
		}
		cause := ctl.F(FieldHeldCause)
		set := map[string]string{"since": stamp(s.Now), "status": Down}
		state := "down until it beats"
		if r.Beats != nil && r.Beats[m].Alive(s.Now) {
			set["status"], state = Up, "up"
		}
		n := happened(NHoldReleased, "", s.Now)
		n.Who, n.To = r.who(), s.Coordinator
		n.What = fmt.Sprintf("%s released: cause %s resolved (%s); it is %s", m, cause, how, state)
		p.Units = append(p.Units, Unit{Key: CtlID(m), Changes: []Change{change(Fleet, setEntry(ctl, set, holdFields...))},
			Notes: []Note{n}, Moved: fmt.Sprintf("%s released: cause %s resolved", m, cause)})
	}
}

// holdNoCauseConds is one judgment for each coordinator's hold that names no cause and
// has stood HoldNoCauseEvery or longer, its period in its words, so a new period is a new
// judgment and the last one closes (tla/HoldCause.tla NoSilentHold).
func holdNoCauseConds(s *Snapshot, r TickReq) []cond {
	var out []cond
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") == "" || ctl.F(FieldHeldBy) != "" || ctl.F(FieldHeldCause) != "" {
			continue
		}
		age, ok := r.running(s.Now, ctl.F("held"))
		if !ok || age < HoldNoCauseEvery {
			continue
		}
		period := age.Truncate(HoldNoCauseEvery)
		who := ctl.F(FieldHeldWho)
		if who == "" {
			who = "the coordinator"
		}
		out = append(out, cond{typ: NHoldNoCause, stream: MemberSubject(m), streamLevel: true,
			what: fmt.Sprintf("%s held %s by %s with no cause: release or name a cause: nova-sprint unhold %s, or nova-sprint fleet hold %s --cause <j<n>|doctor:<check>> --reason '<why>'; its reason: %s",
				m, durationWords(period), who, m, m, orDash(ctl.F(FieldHeldReason))),
			decisions: []string{"ack", "wait"}})
	}
	return out
}

// durationWords is a whole duration as a line says it: 2h, 10h5m, 45m.
func durationWords(d time.Duration) string {
	d = d.Truncate(time.Minute)
	h, m := int(d/time.Hour), int((d%time.Hour)/time.Minute)
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dm", m)
}

// NoModelCall says a take ended with no result and no model call: its child wrote no
// result and its usage reported no token (none at all, or zero of every class).
func NoModelCall(t ProviderTake) bool {
	if !IsNoResult(strings.TrimSpace(t.Error)) {
		return false
	}
	u := cardcost.ParseUsage(t.Usage)
	return !u.Tokens.Reported() || u.Tokens.Total() == 0
}

// idleReap is one take that ended with no result and no model call.
type idleReap struct {
	card, member, route string
	at                  time.Time
}

// idleReaps is every idle reap the fleet table's work cards record on a machine route
// within IdleReapWindow before now (a friend's lane is her own model, never a route's).
func idleReaps(s *Snapshot) []idleReap {
	var out []idleReap
	if s.Fleet == nil {
		return nil
	}
	for _, c := range s.Fleet.Column(Ready, Working, Finished, DoneOK, DoneFailed, DoneDefect, Withdrawn) {
		takes, _ := ProviderTakes(c)
		for _, t := range takes {
			if t.Route == "" || t.Route == RoutePin || IsFriendRow(t.Member) || t.Member == "" || !NoModelCall(t) {
				continue
			}
			at, err := time.Parse(time.RFC3339, t.Finished)
			if err != nil || at.After(s.Now) || s.Now.Sub(at) > IdleReapWindow {
				continue
			}
			out = append(out, idleReap{card: c.ID, member: t.Member, route: t.Route, at: at})
		}
	}
	return out
}

// servedBy is each route's members with a take that ended ok on it within IdleReapWindow.
func servedBy(s *Snapshot) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, c := range s.Fleet.Column(Finished, DoneOK, DoneFailed) {
		if !okEnd(c) || IsFriendRow(c.Row) {
			continue
		}
		route := c.F(FieldRoute)
		at, err := time.Parse(time.RFC3339, c.F("finished"))
		if route == "" || route == RoutePin || err != nil || at.After(s.Now) || s.Now.Sub(at) > IdleReapWindow {
			continue
		}
		if out[route] == nil {
			out[route] = map[string]bool{}
		}
		out[route][c.Row] = true
	}
	return out
}

// reapersBy is each route's members with an idle reap on it.
func reapersBy(reaps []idleReap) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, x := range reaps {
		if out[x.route] == nil {
			out[x.route] = map[string]bool{}
		}
		out[x.route][x.member] = true
	}
	return out
}

// noModelCallRests is the rest of each route not resting whose idle reaps since its last
// rest ended came from IdleReapRouteMembers or more members within the window: the
// route's fault, rested by the route-rest path for RouteRestFor, naming the members and
// the cards (tla/HoldCause.tla RouteFaultRests). It holds no member.
func noModelCallRests(s *Snapshot, rests map[string]RouteRest) map[string]RouteRest {
	out := map[string]RouteRest{}
	byRoute := map[string][]idleReap{}
	for _, x := range idleReaps(s) {
		if last, had := rests[x.route]; had && !x.at.After(last.Mark()) {
			continue // ended before the route's last rest ended: that rest's evidence
		}
		byRoute[x.route] = append(byRoute[x.route], x)
	}
	for route, xs := range byRoute {
		members := slices.Sorted(maps.Keys(reapersBy(xs)[route]))
		if len(members) < IdleReapRouteMembers {
			continue
		}
		var cards []string
		for _, x := range xs {
			if !slices.Contains(cards, x.card) {
				cards = append(cards, x.card)
			}
		}
		slices.Sort(cards)
		out[route] = RouteRest{Route: route, At: s.Now, Until: s.Now.Add(RouteRestFor), Cards: cards, Cause: RestNoModelCall,
			Why: fmt.Sprintf("takes ended with no model call on %d members (%s) within %s: the route's fault, not a member's; no member is held for it", len(members), strings.Join(members, ", "), IdleReapWindow)}
	}
	return out
}

// idleReapHolds is the machine's hold of each member up whose idle reaps since its last
// such hold number IdleReapHoldAfter or more within the window, every one on a route that
// served another member in the window and on which no other member reaped, and that rests
// not: the member's fault (tla/HoldCause.tla HeldOnlyWhenAlone). The hold lets its working
// cards finish, carries its cause (CauseIdleReap) and its mark, and raises the judgment
// that is its cause; answering it ends the hold (holdCauseClear).
func idleReapHolds(s *Snapshot, r TickReq, p *Plan) {
	reaps := idleReaps(s)
	if len(reaps) == 0 {
		return
	}
	reapers, served := reapersBy(reaps), servedBy(s)
	byMember := map[string][]idleReap{}
	for _, x := range reaps {
		byMember[x.member] = append(byMember[x.member], x)
	}
	for _, m := range slices.Sorted(maps.Keys(byMember)) {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") != "" || ctl.F("status") != Up || ctlWritten(p, m) {
			continue
		}
		mark := stampAt(ctl, FieldIdleReapMark)
		var mine []idleReap
		alone := true
		for _, x := range byMember[m] {
			if !mark.IsZero() && !x.at.After(mark) {
				continue
			}
			if _, resting := s.resting(x.route); resting || len(reapers[x.route]) > 1 || !servesAnother(served[x.route], m) {
				alone = false
				break
			}
			mine = append(mine, x)
		}
		if !alone || len(mine) < IdleReapHoldAfter {
			continue
		}
		routes, cards := []string{}, []string{}
		for _, x := range mine {
			if !slices.Contains(routes, x.route) {
				routes = append(routes, x.route)
			}
			cards = append(cards, x.card)
		}
		slices.Sort(routes)
		slices.Sort(cards)
		why := fmt.Sprintf("%d takes ended with no model call within %s on routes that served other members (%s): the member's fault, not the route's", len(mine), IdleReapWindow, strings.Join(routes, ", "))
		now := stamp(s.Now)
		set := map[string]string{"held": now, FieldHeldBy: HeldByIdleReap, FieldHeldReason: why, FieldHeldCause: CauseIdleReap,
			FieldHeldFinish: now, FieldIdleReapMark: now, "status": Held, "since": now}
		n := judgment(NMemberIdleReaps, MemberSubject(m), s.Now, 0)
		n.StreamLevel, n.Who, n.To = true, r.who(), s.Coordinator
		n.Decisions = []string{"ack", "wait"}
		n.What = fmt.Sprintf("%s held: %s; cards %s; its working cards finish and it is dealt nothing; answering this ends the hold (ack it once its harness and keys are seen to): the tick releases it", m, why, strings.Join(cards, ","))
		p.Units = append(p.Units, Unit{Key: CtlID(m), Changes: []Change{change(Fleet, setEntry(ctl, set, FieldHeldCauseNote, FieldHeldWho, FieldDoctorPassed))},
			Notes: []Note{n}, Moved: fmt.Sprintf("%s held: %s", m, why)})
	}
}

// servesAnother says a member other than m ended a take ok on the route.
func servesAnother(members map[string]bool, m string) bool {
	for x := range members {
		if x != m {
			return true
		}
	}
	return false
}

// DoctorPass is one doctor check that passed on a member held on it.
type DoctorPass struct {
	Member string `json:"member"`
	Check  string `json:"check"`
}

// DoctorPasses is each member held on a doctor cause whose check this run measured and
// found passing on it: no FAIL line of the check names it (or names all), and for
// member-env its host answered the probe in full. A check not run, or not measured, is no
// pass (tla/HoldCause.tla Resolve).
func DoctorPasses(in DoctorInput, checks []DoctorCheck) []DoctorPass {
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	var out []DoctorPass
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") == "" {
			continue
		}
		check, ok := DoctorCause(ctl.F(FieldHeldCause))
		if !ok {
			continue
		}
		ran, failed := false, false
		for _, c := range checks {
			if c.Check != check {
				continue
			}
			ran = true
			if (c.Subject == m || c.Subject == "all") && (c.Status == DoctorFail || strings.HasPrefix(c.Evidence, "not measured")) {
				failed = true
			}
		}
		if check == CheckMemberEnv {
			p, probed := in.Probes[m]
			ran = ran && probed && p.Reached && p.Complete
		}
		if ran && !failed {
			out = append(out, DoctorPass{Member: m, Check: check})
		}
	}
	return out
}

// DoctorPassPlan records each pass on its member's control card (FieldDoctorPassed, the
// check at now), for the tick to end the hold it is the cause of; a pass of a member no
// longer held on that check records nothing.
func DoctorPassPlan(s *Snapshot, passes []DoctorPass, who string) Plan {
	var p Plan
	for _, x := range passes {
		ctl := s.MemberCtl(x.Member)
		if ctl == nil || ctl.F("held") == "" || ctl.F(FieldHeldCause) != CausePrefixDoctor+x.Check {
			continue
		}
		var keep []string
		for _, kv := range Split(ctl.F(FieldDoctorPassed)) {
			if k, _, _ := strings.Cut(kv, "="); k != x.Check {
				keep = append(keep, kv)
			}
		}
		keep = append(keep, x.Check+"="+stamp(s.Now))
		p.Units = append(p.Units, Unit{Key: CtlID(x.Member), Changes: []Change{change(Fleet, setEntry(ctl, map[string]string{FieldDoctorPassed: strings.Join(keep, ",")}))},
			Moved: fmt.Sprintf("%s: doctor %s passed (by %s); the tick ends its hold", x.Member, x.Check, who)})
	}
	return p
}

// HoldLine is one hold in force as where says it: the name, how long it has stood and by
// whom, its cause (or that it names none) and reason, and the verb that releases it.
func HoldLine(h HoldView, now time.Time) string {
	age := "-"
	if at, err := time.Parse(time.RFC3339, h.At); err == nil {
		age = durationWords(now.Sub(at))
	}
	by := h.Who
	switch {
	case by != "":
	case h.By != "":
		by = "the machine (" + h.By + ")"
	default:
		by = "the coordinator"
	}
	cause := "no cause"
	if h.Cause != "" {
		cause = "cause " + h.Cause
	}
	line := fmt.Sprintf("held %s %s %s by %s: %s", h.Kind, h.Name, age, by, cause)
	if h.Reason != "" {
		line += " (" + h.Reason + ")"
	}
	return line + "; release: " + HoldReleaseVerb(h)
}

// HoldReleaseVerb is the verb that releases a hold by hand.
func HoldReleaseVerb(h HoldView) string {
	switch h.Kind {
	case HoldMember:
		return "nova-sprint unhold " + h.Name
	case HoldFriend:
		return "nova-sprint friend unhold " + h.Name
	}
	return "nova-sprint unhold " + h.Name
}

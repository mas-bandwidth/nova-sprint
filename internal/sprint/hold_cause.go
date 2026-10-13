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
//     (DoctorPassPlan). A check passes on a member only when no failing line of it keeps
//     the hold (doctorFailKeeps): a line naming the member, or a global one (all, identity,
//     hook, judgments, a provider, a tier, a route) that fails for every member at once;
//     only a line naming another member is that member's alone. The release is a line of
//     the log, "released: cause <x> resolved", addressed to the seat (NHoldReleased); a
//     member that beats is up in that step.
//     A waited judgment is parked, not resolved: while its wait stands (an acknowledgement
//     with a review time), and once it is raised again when the wait runs out, the hold
//     stays (judgmentLive). The machine's idle-reap judgment is a condition the tick keeps
//     (idleReapConds, TickDecisions), so its wait parks it the same way and the tick raises
//     it again when the wait runs out; every answer a judgment lists works on the state it
//     leaves.
//   - A hold with no live cause still holds, and is never silent: every HoldNoCauseEvery it
//     stands the tick raises one judgment to the seat, "<m> held 2h by <who> with no
//     cause: release or name a cause" (holdNoCauseConds), the period's own, the last one
//     closed as it opens. That is every hold with no cause, whoever made it (the
//     coordinator, the sync, the probe, the environment, the adopt): a mark is no cause.
//   - A clear carries a member's hold with its cause, its reason and who made it
//     (RestoreShape, Shape.HeldFields): a judgment of the epoch before is retired by the
//     clear, so the first tick of the new epoch ends that hold and says so; a doctor cause
//     stands until the doctor passes the member again.
//   - An idle reap is a take that ended with no result and no model call (NoModelCall: no
//     token reported, none at all or zero of every class). The member's word is all the
//     sprint has: a child killed after it did call the model, whose harness reported no
//     token, counts as no model call. Idle reaps are attributed by where they happened
//     (idleReaps):
//       - on two or more members of one route in the window: the route's, which rests
//         (noModelCallRests, cause RestNoModelCall) and no member is held;
//       - IdleReapHoldAfter or more on one route by one member, and no other member
//         ended a take ok on it after that member's first reap: a dead provider is the
//         likelier cause, so the route rests and the member is not held, the seat told
//         "route <r>: 3 no-model-call ends, all on <m>; route rested, <m> not held";
//       - a member is held for its reaps only when IdleReapHoldAfter or more of them are
//         each on a route that another member ended a take ok on after the member's first
//         reap, and no other member reaped there (idleReapHolds), the machine's hold, its
//         cause the judgment it raises (CauseIdleReap), its routes recorded
//         (FieldIdleReapRoutes): when one of them rests for no model call, the tick ends
//         the hold in the same step (the route's fault after all).
//     Every such rest is the route-rest path's: it ends by itself after RouteRestFor, and
//     its start is a note to the seat (restWrites, NRouteRested); nothing of it is silent.

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
	// FieldHeldCauseType and FieldHeldCauseSubject are the type and subject of the
	// judgment a judgment cause named: a wait closes the judgment into an acknowledgement
	// of that type and subject, and the tick raises it again, a new note, when the wait
	// runs out (judgmentLive).
	FieldHeldCauseType    = "held_cause_type"
	FieldHeldCauseSubject = "held_cause_subject"
	// FieldIdleReapRoutes is the routes the machine's idle-reap hold was placed for: a
	// rest of one of them for no model call ends the hold.
	FieldIdleReapRoutes = "idle_reap_routes"
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

// openEntry is the open judgment of that id, on its first subject.
func openEntry(s *Snapshot, id string) (Open, bool) {
	for _, o := range s.Open {
		if o.Note.ID == id && o.Note.Kind == Judgment {
			return o, true
		}
	}
	return Open{}, false
}

// judgmentLive says the judgment a cause names stands: it is open (id, or a judgment of
// its type on its subject: the one the tick raises again when a wait runs out), or it is
// waited (an acknowledgement of its type on its subject with a review time). A wait parks
// a judgment and never resolves it (the coordinator's ruling on #206); an ack, an answer
// or the judgment's retirement (a clear retires every judgment of the epoch) ends it.
func judgmentLive(s *Snapshot, id, typ, subject string) bool {
	if id != "" && judgmentOpen(s, id) {
		return true
	}
	if typ == "" || subject == "" {
		return false
	}
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == typ && o.Subject() == subject {
			return true
		}
	}
	for _, o := range s.Acked {
		if !o.Note.Review.IsZero() && o.Note.Type == typ && o.Subject() == subject {
			return true
		}
	}
	return false
}

// causeEnded is how the held member's cause ended, "" while it stands: the judgment no
// longer stands (answered or retired; a wait is no answer), the doctor's pass of the check
// was recorded after the hold, or, for the machine's idle-reap hold, its judgment no
// longer stands or one of its routes rests for no model call (the route's fault after all).
func causeEnded(s *Snapshot, member string, ctl *Card) string {
	cause := ctl.F(FieldHeldCause)
	switch check, doctor := DoctorCause(cause); {
	case cause == "":
		return ""
	case cause == CauseIdleReap:
		for _, route := range Split(ctl.F(FieldIdleReapRoutes)) {
			if rest, ok := s.resting(route); ok && rest.Cause == RestNoModelCall {
				return "route " + route + " rests for no model call: the route's fault, not " + member + "'s"
			}
		}
		if judgmentLive(s, "", NMemberIdleReaps, StreamSubject(MemberSubject(member))) {
			return ""
		}
		return "its judgment was answered or retired"
	case doctor:
		at, ok := doctorPassedAt(ctl, check)
		if held := stampAt(ctl, "held"); !ok || held.IsZero() || at.Before(held) {
			return ""
		}
		return "the doctor's " + check + " check passed on " + member + " at " + stamp(at)
	default:
		if judgmentLive(s, ctl.F(FieldHeldCauseNote), ctl.F(FieldHeldCauseType), ctl.F(FieldHeldCauseSubject)) {
			return ""
		}
		return "the judgment was answered or retired"
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
var holdFields = []string{"held", FieldHeldBy, FieldHeldReason, FieldHeldFinish, FieldHeldCause, FieldHeldCauseNote,
	FieldHeldCauseType, FieldHeldCauseSubject, FieldHeldWho, FieldDoctorPassed, FieldIdleReapRoutes}

// HeldCarried is every field of a member's hold a clear carries into the new epoch beside
// held and held_by (RestoreShape): its cause, its reason and who made it.
var HeldCarried = []string{FieldHeldReason, FieldHeldFinish, FieldHeldCause, FieldHeldCauseNote, FieldHeldCauseType,
	FieldHeldCauseSubject, FieldHeldWho, FieldIdleReapRoutes}

// ctlWritten says a unit of the plan writes the member's control card already: the step
// writes a control card once, so a second write waits for the next tick (tla/HoldCause.tla,
// Busy and the deferral).
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
// Tick, NoHoldOutlivesItsCause and NoHoldForARestedRoute): the hold's fields go, a member
// whose beat is alive is up in this step (as unhold brings it up), the idle-reap judgment
// a route's rest answered closes, and the release is a line of the log to the seat. A hold
// with no cause is holdNoCauseConds', and the machine's other holds (fault, environment,
// probe, sync, adopt) are their own releases'.
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
		u := Unit{Key: CtlID(m), Changes: []Change{change(Fleet, setEntry(ctl, set, holdFields...))},
			Notes: []Note{n}, Moved: fmt.Sprintf("%s released: cause %s resolved", m, cause)}
		if cause == CauseIdleReap {
			for _, o := range s.Open {
				if o.Note.Type == NMemberIdleReaps && o.Subject() == StreamSubject(MemberSubject(m)) {
					u.Closes = append(u.Closes, o)
				}
			}
		}
		p.Units = append(p.Units, u)
	}
}

// holdNoCauseConds is one judgment for each hold that names no cause, whoever made it (a
// mark is no cause), once it has stood HoldNoCauseEvery or longer, its period in its
// words, so a new period is a new judgment and the last one closes (tla/HoldCause.tla
// NoSilentHold).
func holdNoCauseConds(s *Snapshot, r TickReq) []cond {
	var out []cond
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") == "" || ctl.F(FieldHeldCause) != "" {
			continue
		}
		age, ok := r.running(s.Now, ctl.F("held"))
		if !ok || age < HoldNoCauseEvery {
			continue
		}
		period := age.Truncate(HoldNoCauseEvery)
		who := ctl.F(FieldHeldWho)
		switch {
		case who != "":
		case ctl.F(FieldHeldBy) != "":
			who = "the machine (" + ctl.F(FieldHeldBy) + ")"
		default:
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

// NoModelCall says a take ended with no result and no model call, by its member's word:
// its child wrote no result and its usage reported no token (none at all, or zero of every
// class). The sprint sees only what the member reported, so a child killed after it did
// call the model, whose harness reported no token, counts here too.
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

// okEnds is each route's takes that ended ok within IdleReapWindow: when, by member.
func okEnds(s *Snapshot) map[string]map[string][]time.Time {
	out := map[string]map[string][]time.Time{}
	if s.Fleet == nil {
		return out
	}
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
			out[route] = map[string][]time.Time{}
		}
		out[route][c.Row] = append(out[route][c.Row], at)
	}
	return out
}

// servedAfter says a member other than m ended a take ok on the route after t: an ok end
// before m's first reap says nothing of the route since (the cold read of #206: m2 ok at
// -14m, m1's reaps at -9, -6 and -3m, and m1 was held for a dead route).
func servedAfter(ends map[string][]time.Time, m string, t time.Time) bool {
	for x, ats := range ends {
		if x == m {
			continue
		}
		for _, at := range ats {
			if at.After(t) {
				return true
			}
		}
	}
	return false
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

// firstReap is the earliest of the reaps.
func firstReap(xs []idleReap) time.Time {
	var first time.Time
	for _, x := range xs {
		if first.IsZero() || x.at.Before(first) {
			first = x.at
		}
	}
	return first
}

// noModelCallRests is the rest of each route whose idle reaps since its last rest ended
// are the route's (tla/HoldCause.tla RouteFaultRests), rested by the route-rest path for
// RouteRestFor, naming the members and the cards; it holds no member:
//   - IdleReapRouteMembers or more members reaped on it within the window; or
//   - one member reaped on it IdleReapHoldAfter or more times, and no other member ended
//     a take ok on it after that member's first reap: a dead provider is the likelier cause
//     than the member (the coordinator's ruling on #206), so the route rests, the member is
//     not held, and the note names both.
func noModelCallRests(s *Snapshot, rests map[string]RouteRest) map[string]RouteRest {
	out := map[string]RouteRest{}
	byRoute := map[string][]idleReap{}
	for _, x := range idleReaps(s) {
		if last, had := rests[x.route]; had && !x.at.After(last.Mark()) {
			continue // ended before the route's last rest ended: that rest's evidence
		}
		byRoute[x.route] = append(byRoute[x.route], x)
	}
	ends := okEnds(s)
	for route, xs := range byRoute {
		members := slices.Sorted(maps.Keys(reapersBy(xs)[route]))
		var cards []string
		for _, x := range xs {
			if !slices.Contains(cards, x.card) {
				cards = append(cards, x.card)
			}
		}
		slices.Sort(cards)
		rest := RouteRest{Route: route, At: s.Now, Until: s.Now.Add(RouteRestFor), Cards: cards, Cause: RestNoModelCall}
		switch {
		case len(members) >= IdleReapRouteMembers:
			rest.Why = fmt.Sprintf("%d no-model-call ends on %d members (%s) within %s; route rested, the route's fault, no member held", len(xs), len(members), strings.Join(members, ", "), IdleReapWindow)
		case len(xs) >= IdleReapHoldAfter && !servedAfter(ends[route], members[0], firstReap(xs)):
			rest.Why = fmt.Sprintf("%d no-model-call ends, all on %s; route rested, %s not held", len(xs), members[0], members[0])
		default:
			continue
		}
		out[route] = rest
	}
	return out
}

// idleReapHolds is the machine's hold of each member up whose idle reaps since its last
// such hold number IdleReapHoldAfter or more within the window, every one on a route that
// another member ended a take ok on after the member's first reap, on which no other
// member reaped, and that rests not: the member's fault (tla/HoldCause.tla
// HeldOnlyWhenAlone). The hold lets its working cards finish, carries its cause
// (CauseIdleReap), its mark and its routes, and raises the judgment that is its cause;
// answering it, or a rest of one of its routes for no model call, ends the hold
// (holdCauseClear).
func idleReapHolds(s *Snapshot, r TickReq, p *Plan) {
	reaps := idleReaps(s)
	if len(reaps) == 0 {
		return
	}
	reapers, ends := reapersBy(reaps), okEnds(s)
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
		var fresh []idleReap
		for _, x := range byMember[m] {
			if mark.IsZero() || x.at.After(mark) {
				fresh = append(fresh, x)
			}
		}
		if len(fresh) < IdleReapHoldAfter {
			continue
		}
		first := firstReap(fresh)
		alone := true
		for _, x := range fresh {
			if _, resting := s.resting(x.route); resting || len(reapers[x.route]) > 1 || !servedAfter(ends[x.route], m, first) {
				alone = false
				break
			}
		}
		if !alone {
			continue
		}
		routes, cards := []string{}, []string{}
		for _, x := range fresh {
			if !slices.Contains(routes, x.route) {
				routes = append(routes, x.route)
			}
			cards = append(cards, x.card)
		}
		slices.Sort(routes)
		slices.Sort(cards)
		why := fmt.Sprintf("%d takes ended with no model call within %s on routes that other members served after its first (%s): the member's fault, not the route's", len(fresh), IdleReapWindow, strings.Join(routes, ", "))
		now := stamp(s.Now)
		set := map[string]string{"held": now, FieldHeldBy: HeldByIdleReap, FieldHeldReason: why, FieldHeldCause: CauseIdleReap,
			FieldHeldFinish: now, FieldIdleReapMark: now, FieldIdleReapRoutes: strings.Join(routes, ","), "status": Held, "since": now}
		n := judgment(NMemberIdleReaps, MemberSubject(m), s.Now, 0)
		n.StreamLevel, n.Who, n.To = true, r.who(), s.Coordinator
		n.Decisions = []string{"ack", "wait"}
		n.What = fmt.Sprintf("%s held: %s; cards %s; its working cards finish and it is dealt nothing; answering this ends the hold (ack it once its harness and keys are seen to), and so does a rest of %s for no model call: the tick releases it", m, why, strings.Join(cards, ","), strings.Join(routes, " or "))
		p.Units = append(p.Units, Unit{Key: CtlID(m), Changes: []Change{change(Fleet, setEntry(ctl, set, FieldHeldCauseNote, FieldHeldCauseType, FieldHeldCauseSubject, FieldHeldWho, FieldDoctorPassed))},
			Notes: []Note{n}, Moved: fmt.Sprintf("%s held: %s", m, why)})
	}
}

// idleReapConds is the condition the tick keeps for each of the machine's idle-reap holds
// whose judgment stands (open, or waited): the judgment the hold raised, keyed by its type
// and subject, so the open one is never written twice; a wait parks it (TickDecisions,
// Wait) and when the wait runs out the tick raises it again with these words (notify),
// the hold standing throughout (judgmentLive). An answered judgment is no condition: the
// tick ends the hold in that step (holdCauseClear) and raises nothing in its place. The
// cold read of #206: the judgment offered a wait that Wait refused, as its type was not
// one the tick keeps.
func idleReapConds(s *Snapshot) []cond {
	var out []cond
	for _, m := range s.Members() {
		ctl := s.MemberCtl(m)
		if ctl == nil || ctl.F("held") == "" || ctl.F(FieldHeldCause) != CauseIdleReap {
			continue
		}
		if !judgmentLive(s, "", NMemberIdleReaps, StreamSubject(MemberSubject(m))) {
			continue
		}
		routes := strings.ReplaceAll(ctl.F(FieldIdleReapRoutes), ",", " or ")
		out = append(out, cond{typ: NMemberIdleReaps, stream: MemberSubject(m), streamLevel: true, decisions: TickDecisions[NMemberIdleReaps],
			what: fmt.Sprintf("%s held: %s; its working cards finish and it is dealt nothing; answering this ends the hold (ack it once its harness and keys are seen to), and so does a rest of %s for no model call: the tick releases it", m, ctl.F(FieldHeldReason), routes)})
	}
	return out
}

// DoctorPass is one doctor check that passed on a member held on it.
type DoctorPass struct {
	Member string `json:"member"`
	Check  string `json:"check"`
}

// DoctorPasses is each member held on a doctor cause whose check this run measured and
// found passing on it: no failing line of the check keeps the hold (doctorFailKeeps), and
// for member-env its host answered the probe in full. A check not run, or not measured, is
// no pass (tla/HoldCause.tla Doctor, DoctorPassIsMeasured).
func DoctorPasses(in DoctorInput, checks []DoctorCheck) []DoctorPass {
	s := in.Snap
	if s == nil || s.Fleet == nil {
		return nil
	}
	members := s.Members()
	var out []DoctorPass
	for _, m := range members {
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
			if doctorFailKeeps(members, m, c) {
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

// doctorFailKeeps says a line of the check keeps m's hold on it: it is a FAIL or a "not
// measured" line, and it names m or it names no fleet member at all (all, identity, hook,
// judgments, a provider, a tier, a route, a reader: the check's global subjects, which
// fail for every member at once). Only a failing line that names another member is that
// member's alone (the cold read of #206: a hold on doctor:git-identity was released while
// its identity line failed, because the line named "identity", not the member).
func doctorFailKeeps(members []string, m string, c DoctorCheck) bool {
	if c.Status != DoctorFail && !strings.HasPrefix(c.Evidence, "not measured") {
		return false
	}
	return c.Subject == m || !slices.Contains(members, c.Subject)
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

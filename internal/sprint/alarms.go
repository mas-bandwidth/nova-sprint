package sprint

import (
	"fmt"
	"strconv"
)

// The backlog alarms (docs/SPEC-SPRINT.md section 8, "Backlog alarms"): four conditions
// of the whole sprint the tick keeps, each off until the coordinator sets its threshold
// (`set --alarm-review`, `--alarm-merging`, `--alarm-fleet`, `--alarm-ready`, the work
// table's properties below). Each is one judgment while its condition stands, written
// once when it starts (notify: one per episode, so a push of new judgments pushes it
// once) and closed once when it ends, with one cleared note to the coordinator.
const (
	NAlarmReview  = "review above its alarm"
	NAlarmMerging = "merging above its alarm"
	NAlarmReady   = "nothing ready while cards wait"
	NAlarmFleet   = "the fleet works below its alarm"
	// NAlarmCleared is the happened note, to the coordinator, that an alarm's episode
	// ended: its what opens with the alarm's type and a colon.
	NAlarmCleared = "an alarm cleared"
	// NAlarmQuiet is the happened note, to the coordinator, that an alarm's condition
	// stands but an open cause already explains it: the alarm is not raised.
	NAlarmQuiet = "an alarm is quiet: an open cause explains it"
	// NAlarmQuietMark is the record of an alarm's quiet episode (an acknowledged note
	// whose What is the alarm's type), written with the quiet note so one is written
	// once an episode.
	NAlarmQuietMark = "an alarm's quiet episode"

	PropAlarmReview  = "alarm_review"  // a count: raised while review holds more
	PropAlarmMerging = "alarm_merging" // a count: raised while merging holds more
	PropAlarmFleet   = "alarm_fleet"   // a percent: raised while the up members work below it of their width
	PropAlarmReady   = "alarm_ready"   // on: raised while nothing is ready and a card waits
	// AlarmOff is the word that takes an alarm off.
	AlarmOff = "off"
)

// AlarmTypes is the backlog alarms' judgment types, in the order the tick checks them.
var AlarmTypes = []string{NAlarmReview, NAlarmMerging, NAlarmReady, NAlarmFleet}

// alarmProps is each alarm's property, the flag that sets it and what its value wants.
var alarmProps = []struct{ typ, prop, flag, wants string }{
	{NAlarmReview, PropAlarmReview, "--alarm-review", "a whole number of primaries, 0 or more"},
	{NAlarmMerging, PropAlarmMerging, "--alarm-merging", "a whole number of primaries, 0 or more"},
	{NAlarmFleet, PropAlarmFleet, "--alarm-fleet", "a percent of the up members' width, 1 to 100"},
	{NAlarmReady, PropAlarmReady, "--alarm-ready", "on"},
}

// alarmExplained says the alarm typ's condition, when it stands, is already explained by
// an open cause the tables show: a recorded act or an open judgment that names it. While
// it is, the tick does not raise the alarm and does not re-raise a seat's wait on it, and
// an open alarm judgment closes with one quiet note naming the cause. Each alarm is one
// row; a situation not listed is not explained, and the alarm raises as today. ok is
// false when nothing explains it.
//
// docs/SPEC-SPRINT.md section 8, "Backlog alarms".
func alarmExplained(s *Snapshot, typ string) (string, bool) {
	switch typ {
	case NAlarmFleet:
		return fleetExplained(s)
	case NAlarmReady:
		return readyExplained(s)
	case NAlarmReview, NAlarmMerging:
		return mergeExplained(s)
	}
	return "", false
}

// heldByCoordinatorWithReason says the member is held by the coordinator and the hold names
// a reason: the coordinator's hold carries no mark (FieldHeldBy) and its reason is the
// control card's FieldHeldReason (hold.go).
func heldByCoordinatorWithReason(ctl *Card) bool {
	return ctl != nil && ctl.F("held") != "" && ctl.F(FieldHeldBy) == "" && ctl.F(FieldHeldReason) != ""
}

// memberDownJudged says the member is down and an open judgment names its being down
// (NStopMemberDown, stops.go): the cause of its down is already before the seat.
func memberDownJudged(s *Snapshot, m string) bool {
	ctl := s.MemberCtl(m)
	if ctl == nil || ctl.F("status") != Down {
		return false
	}
	for _, o := range s.Open {
		if o.Note.Type == NStopMemberDown && contains(o.Note.Primaries, m) {
			return true
		}
	}
	return false
}

// openProviderRestJudgment says an open judgment names a provider's rest: out of funds,
// low on funds, all out of credit, or its key (provider_funds.go).
func openProviderRestJudgment(s *Snapshot) bool {
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && contains([]string{NProviderFunds, NProviderLow, NProviderKey, NAllOutOfCredit}, o.Note.Type) {
			return true
		}
	}
	return false
}

// fleetExplained explains NAlarmFleet: the fleet works below its alarm because every member
// that does not work is held by the coordinator with a reason, down with an open judgment,
// or held adopting, or because the providers of the waiting cards' tiers rest.
func fleetExplained(s *Snapshot) (string, bool) {
	if s.Fleet == nil {
		return "", false
	}
	if openProviderRestJudgment(s) {
		return "the providers of the waiting cards' tiers rest", true
	}
	found := false
	for _, m := range s.Members() {
		if work, _ := rowWorking(s, m); work > 0 {
			continue
		}
		found = true
		ctl := s.MemberCtl(m)
		switch {
		case heldByCoordinatorWithReason(ctl):
		case memberDownJudged(s, m):
		case AdoptingHeld(ctl):
		default:
			return "", false
		}
	}
	if !found {
		return "", false
	}
	return "every member that does not work is held with a reason, down with an open judgment, or held adopting", true
}

// readyExplained explains NAlarmReady: nothing is ready while cards wait, because every
// waiting card waits on a need, a sentinel, a hold or a friend pin the table already shows.
func readyExplained(s *Snapshot) (string, bool) {
	if s.Work == nil {
		return "", false
	}
	waiting := s.Work.Column(Waiting)
	if len(waiting) == 0 {
		return "", false
	}
	for _, c := range waiting {
		if len(UnresolvedNeeds(s, c.ID, Split(c.F("needs")), nil)) > 0 {
			continue // waits on a need the table shows: missing, dropped, held or a cycle
		}
		if placedNamedWait(s, c) {
			continue // waits on a named need that is placed and not landed: the table shows it
		}
		if IsSentinel(c) || len(PositionWaits(s, c, nil)) > 0 {
			continue // waits on a sentinel the table shows
		}
		if IsHeld(c) {
			continue // admitted held: the table shows it
		}
		if OnlyFriend(c) {
			continue // pinned to a friend the table shows
		}
		return "", false
	}
	return "every waiting card waits on a need, a sentinel, a hold or a friend pin", true
}

// placedNamedWait says the card waits on a named need that is placed on the table and not
// landed (steps_work.go NamedWaits): the ready alarm's ordinary wait, which resolvePlan
// leaves in Waiting and UnresolvedNeeds names nothing for.
func placedNamedWait(s *Snapshot, c *Card) bool {
	for _, n := range NamedWaits(s, c, nil) {
		if nc := s.Work.Placed(n); nc != nil && nc.Col != Landed {
			return true
		}
	}
	return false
}

// mergeExplained explains NAlarmReview and NAlarmMerging: a merge window is open (the
// lander's pause, mergewindow.go) or the landing base is red with its judgment open.
func mergeExplained(s *Snapshot) (string, bool) {
	if w, err := s.MergeWindow(); err == nil && !w.Until.IsZero() && s.Now.Before(w.Until) {
		return "merge window open until " + stamp(w.Until), true
	}
	if baseRedJudged(s) {
		return "the landing base is red with its judgment open", true
	}
	return "", false
}

// baseRedJudged says an open judgment names the landing base red: the base's CI after a
// landing (NBaseCIRed, base_ci.go) or the base red at its tip (NDriftBaseRed, drift.go).
func baseRedJudged(s *Snapshot) bool {
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && (o.Note.Type == NBaseCIRed || o.Note.Type == NDriftBaseRed) {
			return true
		}
	}
	return false
}

// alarmValid says a value is one the alarm's property takes: its threshold, or off.
func alarmValid(prop, v string) bool {
	if v == AlarmOff {
		return true
	}
	if prop == PropAlarmReady {
		return v == "on"
	}
	n, err := strconv.Atoi(v)
	if prop == PropAlarmFleet {
		return err == nil && n >= 1 && n <= 100
	}
	return err == nil && n >= 0
}

// alarmSetting is an alarm's threshold as the work table's property holds it, ok false
// when it is off (no property, off, or a value it does not take).
func (s *Snapshot) alarmSetting(prop string) (int, bool) {
	v, ok := s.Work.Prop(prop)
	if !ok {
		// a cleared epoch reads its working threshold before its set-defaults step ran
		// (a start inside the clear): never an absent off (defaults.go clearedDefault)
		v, ok = s.clearedDefault(prop)
	}
	if !ok || !alarmValid(prop, v) || v == AlarmOff {
		return 0, false
	}
	if prop == PropAlarmReady {
		return 0, true
	}
	n, _ := strconv.Atoi(v)
	return n, true
}

// alarmFacts is each alarm whose condition stands, with what it says
// (docs/SPEC-SPRINT.md section 8, "Backlog alarms"):
//   - review: more primaries in review than its count;
//   - merging: more primaries merging than its count;
//   - ready: none ready (work table) and one or more waiting;
//   - fleet: the up members working fewer work cards than its percent of their width,
//     while a primary is ready or waiting, so the fleet has work it does not run.
func alarmFacts(s *Snapshot) map[string]string {
	out := map[string]string{}
	review, merging := len(s.Work.Column(Review)), len(s.Work.Column(Merging))
	ready, waiting := len(s.Work.Column(Ready)), len(s.Work.Column(Waiting))
	if n, on := s.alarmSetting(PropAlarmReview); on && review > n {
		out[NAlarmReview] = fmt.Sprintf("%d primaries in review, above the alarm of %d: reads or accepts are behind; run: nova-sprint where", review, n)
	}
	if n, on := s.alarmSetting(PropAlarmMerging); on && merging > n {
		out[NAlarmMerging] = fmt.Sprintf("%d primaries merging, above the alarm of %d: landing is behind; run: nova-sprint land", merging, n)
	}
	if _, on := s.alarmSetting(PropAlarmReady); on && ready == 0 && waiting > 0 {
		out[NAlarmReady] = fmt.Sprintf("0 primaries ready and %d waiting: the deal has nothing to feed the fleet; run: nova-sprint where --all", waiting)
	}
	if pct, on := s.alarmSetting(PropAlarmFleet); on && ready+waiting > 0 && !s.FleetOff() {
		working, width := 0, 0
		for _, m := range s.UpMembers() {
			work, _ := rowWorking(s, m)
			working += work
			width += s.Width(m)
		}
		if width > 0 && working*100 < pct*width {
			out[NAlarmFleet] = fmt.Sprintf("the members up work %d of their width %d, below the alarm of %d%%, with %d primaries ready or waiting; run: nova-sprint where", working, width, pct, ready+waiting)
		}
	}
	return out
}

// tickAlarms is the tick's backlog alarms, planned with the deadlines (TickDeadlines;
// docs/SPEC-SPRINT.md section 8, "Backlog alarms"): a judgment for each alarm whose
// condition starts, none while it stands (notify keys an alarm by its type alone, so a
// count that moves is the same episode), and, for each whose judgment or hold the
// condition's end closes, one cleared note to the coordinator; and the members' open
// files over their alarm bounds the same way (tickFiles, fd.go). It writes notes, no
// table.
func tickAlarms(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	facts := alarmFacts(s)
	explained := map[string]string{}
	var conds []cond
	for _, typ := range AlarmTypes {
		what, stands := facts[typ]
		if !stands {
			continue
		}
		if cause, ok := alarmExplained(s, typ); ok {
			explained[typ] = cause
			continue // not raised: an open cause already explains it
		}
		conds = append(conds, cond{typ: typ, streamLevel: true, what: what})
	}
	due := notify(&p, s, conds, AlarmTypes, r)
	for _, o := range p.Closes {
		typ := o.Note.Type
		if !contains(AlarmTypes, typ) {
			continue
		}
		if _, stands := facts[typ]; stands {
			continue // a wait run out on a condition that stands is raised again, not cleared
		}
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NAlarmCleared, Who: r.who(), To: s.Coordinator, At: s.Now,
			What: typ + ": " + alarmNow(s, typ)})
	}
	p = quietAlarmNotes(p, s, r, explained)
	// each member's open files over its alarm bound, from its beat (fd.go)
	f, filesDue := tickFiles(s, r)
	p.Notes, p.Closes, p.Updates = append(p.Notes, f.Notes...), append(p.Closes, f.Closes...), append(p.Updates, f.Updates...)
	return p, due + filesDue
}

// quietAlarmNotes writes, for each alarm that is explained while its condition stands, one
// quiet note per episode naming the cause, and closes the marker of an alarm whose cause
// ended or whose condition ended (the alarm then raises or clears as today).
func quietAlarmNotes(p Plan, s *Snapshot, r TickReq, explained map[string]string) Plan {
	mark := map[string]bool{}
	for _, o := range s.Acked {
		if o.Note.Type == NAlarmQuietMark {
			mark[o.Note.What] = true
		}
	}
	to := s.Coordinator
	if to == "" {
		to = "coordinator"
	}
	for _, typ := range AlarmTypes {
		cause, ok := explained[typ]
		if !ok || mark[typ] {
			continue
		}
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NAlarmQuiet, Who: r.who(), To: to, At: s.Now,
			What: typ + ": quiet, explained by " + cause})
		p.Notes = append(p.Notes, Note{Kind: Acknowledged, Type: NAlarmQuietMark, What: typ, Who: r.who(), At: s.Now, SprintLevel: true})
	}
	for _, o := range s.Acked {
		if o.Note.Type == NAlarmQuietMark {
			if _, ok := explained[o.Note.What]; !ok {
				p.Closes = append(p.Closes, o)
			}
		}
	}
	return p
}

// alarmNow is what an alarm's cleared note says of the sprint now.
func alarmNow(s *Snapshot, typ string) string {
	for _, a := range alarmProps {
		if a.typ != typ {
			continue
		}
		if _, on := s.alarmSetting(a.prop); !on {
			return "its alarm is off"
		}
	}
	switch typ {
	case NAlarmReview:
		return fmt.Sprintf("%d primaries in review", len(s.Work.Column(Review)))
	case NAlarmMerging:
		return fmt.Sprintf("%d primaries merging", len(s.Work.Column(Merging)))
	case NAlarmReady:
		return fmt.Sprintf("%d primaries ready, %d waiting", len(s.Work.Column(Ready)), len(s.Work.Column(Waiting)))
	}
	return "the fleet works at its alarm or above, or has no work ready or waiting"
}

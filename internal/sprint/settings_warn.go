package sprint

import (
	"fmt"
	"slices"
	"strings"
)

// The design settings' warnings (docs/SPEC-SPRINT.md section 8, "Design settings off";
// the owner, 2026-10-11: "I am sick of these invisible settings being off", "breaking
// everything wasting hours of my life", "There must be an alert, every time cards are added
// to "review" set, if read cards is off, and you get it via push as a warning", and "These
// must be push notifications."). The machine works as designed only with read cards on and
// the four backlog alarms at their working thresholds, and each is the default (ReadCardsOn,
// alarmSetting): a fresh store and every store after a clear has them.
//
// A setting moved off its working value is never invisible. The tick's warnings part
// (PartWarnings, TickWarnings) writes a WARNING: a happened note addressed to the
// coordinator, stored in the sprint's epoch, which the hook pushes as a numbered PUSH like
// every note to the coordinator, and which the inbox lists whether or not a hook is attached:
//
//   - NSettingsOff, each time what the settings say changes to something off: every setting
//     off or away from its working value, each with the exact set that restores it. The
//     set and the tick record what they last warned of in the work table
//     (PropSettingsWarned), so each change is written once; back at the working values the
//     record is cleared, so an off -> on -> off is warned of twice, and a clear starts the
//     next epoch with no record.
//   - NReviewOffReadCards, while read cards are off, for every primary that entered review,
//     by card and attempt: the tick records the ones it warned of (PropReviewWarned), only
//     those still in review, so a card that leaves review and comes back is warned of
//     again, and the record is cleared with read cards back on. The ones new at a tick are
//     one note naming only them, and how many others were in review already.
//
// A set that leaves a design setting off writes the warning itself, at once, whether or not
// the machine runs, and records it (setWarning): every set of a design setting that leaves one
// off is a warning, so an off -> on -> off is two. The tick writes the rest: an off it finds
// unsaid (a store written before the warnings, a property written by hand), and every card
// into review, which only the tick moves.

// The warnings' note types: happened notes to the coordinator.
const (
	NSettingsOff        = "a design setting is off its working default"
	NReviewOffReadCards = "a card entered review with read cards off"
)

// The work table's records of what the tick last warned of: the settings' text, and the
// cards in review it warned of while read cards were off, <id>@<attempt>#<entry> comma separated (ReviewOffReadCards);
// WarnedNone when there is nothing (a property is never written empty).
const (
	PropSettingsWarned = "settings_warned"
	PropReviewWarned   = "review_warned"
	WarnedNone         = "none"
)

// warnedRecord is a record's value, "" for none (absent or WarnedNone).
func warnedRecord(s *Snapshot, name string) string {
	v, _ := s.Work.Prop(name)
	if v == WarnedNone {
		return ""
	}
	return v
}

// WarningWord opens the text of every warning.
const WarningWord = "WARNING: "

// DesignSetting is one setting the design depends on as the sprint holds it now: its flag,
// the value it has, its working value, and whether it is at it.
type DesignSetting struct {
	Flag    string `json:"flag"`
	Value   string `json:"value"`
	Working string `json:"working"`
	At      bool   `json:"at_working"`
}

// Restore is the set that puts the setting back at its working value.
func (d DesignSetting) Restore() string { return "nova-sprint set " + d.Flag + " " + d.Working }

// DesignSettings is every setting the design depends on, as the snapshot holds it: read
// cards (on), then the backlog alarms at their working thresholds (alarmDefault).
func DesignSettings(s *Snapshot) []DesignSetting {
	rc := DesignSetting{Flag: "--read-cards", Value: SwitchOff, Working: ReadCardsOnWord}
	if s.ReadCardsOn() {
		rc.Value = ReadCardsOnWord
	}
	rc.At = rc.Value == rc.Working
	out := []DesignSetting{rc}
	for _, a := range []struct{ prop, flag string }{
		{PropAlarmReview, "--alarm-review"},
		{PropAlarmReady, "--alarm-ready"},
		{PropAlarmMerging, "--alarm-merging"},
		{PropAlarmFleet, "--alarm-fleet"},
	} {
		d := DesignSetting{Flag: a.flag, Working: alarmDefault(a.prop)}
		n := 0
		if s != nil && s.Work != nil {
			n = s.alarmSetting(a.prop)
		}
		switch {
		case s.alarmStoredOff(a.prop):
			// off written before set refused it: the alarm runs at its working threshold,
			// and the warning says so until a set clears the property
			d.Value = AlarmOff + " (written before off was refused, read as " + d.Working + ")"
		case a.prop == PropAlarmReady:
			d.Value = "on"
		default:
			d.Value = fmt.Sprint(n)
		}
		d.At = d.Value == d.Working
		out = append(out, d)
	}
	return out
}

// SettingsOffWhat is the warning's text for the settings away from their working values,
// "" when every one is at it: each with its value and working value, and one set that
// restores them all.
func SettingsOffWhat(ds []DesignSetting) string {
	var said, flags []string
	for _, d := range ds {
		if d.At {
			continue
		}
		said = append(said, fmt.Sprintf("%s is %s, working value %s (restore: %s)", d.Flag, d.Value, d.Working, d.Restore()))
		flags = append(flags, d.Flag+" "+d.Working)
	}
	if len(said) == 0 {
		return ""
	}
	return WarningWord + "design settings off their working values, the machine is degraded: " + strings.Join(said, "; ") +
		"; run: nova-sprint set " + strings.Join(flags, " ")
}

// FieldReviewEntered is a work card's revision when it last moved into review (moveEntry):
// each entry into review is its own, so a card that leaves review and comes back between two
// ticks, at the same attempt, is a new entry the review warning says.
const FieldReviewEntered = "review_entered"

// ReviewOffReadCards is each primary in review, as <id>@<attempt>#<entry> (the entry its
// FieldReviewEntered, absent on a card that entered before it was written), while read cards
// are off; none while they are on.
func ReviewOffReadCards(s *Snapshot) []string {
	if s == nil || s.Work == nil || s.ReadCardsOn() {
		return nil
	}
	var out []string
	for _, c := range s.Work.Column(Review) {
		if !IsSentinel(c) {
			k := c.ID + "@" + itoa(max(c.Int("attempt"), 1))
			if e := c.F(FieldReviewEntered); e != "" {
				k += "#" + e
			}
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// reviewSaid is a review record's key as the warning says it, <id>@<attempt>.
func reviewSaid(k string) string {
	said, _, _ := strings.Cut(k, "#")
	return said
}

// ReviewOffReadCardsWhat is the review warning's text for the cards that entered review,
// with how many others were in review already.
func ReviewOffReadCardsWhat(entered []string, already int) string {
	also := ""
	if already > 0 {
		also = fmt.Sprintf(" (%d more already in review, warned of before)", already)
	}
	return WarningWord + "read cards are off and " + strings.Join(entered, ", ") + " entered review" + also +
		": each is asked one read per tick through the readers table, not every read at once; run: nova-sprint set --read-cards on"
}

// warnedList is a record's cards, comma separated; none for "".
func warnedList(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// setWarning is the warning a set writes at once (Set): the settings as the set leaves them
// (after, the work table's properties with its writes), said when anything is off, the note
// and the record's write; back at the working values, the record cleared. nil for nothing.
func setWarning(s *Snapshot, after map[string]string, who string) (*Note, *PropWrite) {
	w := &Snapshot{Work: NewTable(Work)}
	w.Work.SetProps(after)
	what := SettingsOffWhat(DesignSettings(w))
	was, had := s.Work.Prop(PropSettingsWarned)
	rec := what
	if rec == "" {
		rec = WarnedNone
	}
	var pw *PropWrite
	if !had && what != "" || had && was != rec {
		pw = &PropWrite{Table: Work, Name: PropSettingsWarned, Value: rec, Was: was, WasAbsent: !had}
	}
	if what == "" {
		return nil, pw
	}
	return &Note{Kind: Happened, Type: NSettingsOff, Who: who, To: s.Coordinator, At: s.Now, What: what}, pw
}

// PartWarnings is the tick's warnings part, after the deadlines (TickEnd).
const PartWarnings = "warnings"

// TickWarnings is the warnings part: tickSettingsWarnings. It writes notes and the work
// table's two records, nothing else.
func TickWarnings(s *Snapshot, r TickReq) (Plan, int) { return tickSettingsWarnings(s, r), 0 }

// tickSettingsWarnings is the tick's design settings' warnings (the comment above): the notes
// of what changed since the work table's records, and the records' writes.
func tickSettingsWarnings(s *Snapshot, r TickReq) Plan {
	var p Plan
	if s == nil || s.Work == nil {
		return p
	}
	record := func(name, v string) {
		if v == warnedRecord(s, name) {
			return
		}
		if v == "" {
			v = WarnedNone
		}
		was, had := s.Work.Prop(name)
		p.Props = append(p.Props, PropWrite{Table: Work, Name: name, Value: v, Was: was, WasAbsent: !had})
	}
	what := SettingsOffWhat(DesignSettings(s))
	if last := warnedRecord(s, PropSettingsWarned); what != last {
		if what != "" {
			p.Notes = append(p.Notes, Note{Kind: Happened, Type: NSettingsOff, Who: r.who(), To: s.Coordinator, At: s.Now, What: what})
		}
		record(PropSettingsWarned, what)
	}
	in := ReviewOffReadCards(s)
	warned := warnedList(warnedRecord(s, PropReviewWarned))
	var entered []string
	for _, c := range in {
		if !slices.Contains(warned, c) {
			entered = append(entered, c)
		}
	}
	if len(entered) > 0 {
		var ids, said []string
		for _, c := range entered {
			id, _, _ := strings.Cut(c, "@")
			ids = append(ids, id)
			said = append(said, reviewSaid(c))
		}
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NReviewOffReadCards, Who: r.who(), To: s.Coordinator, At: s.Now,
			Primaries: ids, Count: len(ids), What: ReviewOffReadCardsWhat(said, len(in)-len(entered))})
	}
	// the record holds the cards in review it warned of, no others: one that left is warned
	// of again when it comes back
	record(PropReviewWarned, strings.Join(in, ","))
	return p
}

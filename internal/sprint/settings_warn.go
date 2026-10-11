package sprint

import (
	"fmt"
	"strings"
)

// The design settings' warnings (docs/SPEC-SPRINT.md section 8, "Design settings off";
// the owner, 2026-10-11: "I am sick of these invisible settings being off", "breaking
// everything wasting hours of my life", and "There must be an alert, every time cards are
// added to "review" set, if read cards is off, and you get it via push as a warning").
// The machine works as designed only with read cards on and the four backlog alarms at
// their working thresholds, and each is the default (ReadCardsOn, alarmSetting): a fresh
// store and every store after a clear has them. A setting moved off its working value is
// never invisible: the server pushes a WARNING over the coordinator's hook
// (cmd/nova-sprint/hook.go, look), on the hook's every look, of what the store holds now:
//
//   - the settings off their working values (SettingsOffWhat), each with the exact set
//     that restores it: pushed once a subscription for each change of what it says, so a
//     server's start (every hook ends with it, and the next is pushed afresh) and every set
//     that turns one off push it; after a clear every setting is at its default and none is;
//   - while read cards are off, every card that enters review (ReviewOffReadCards), each
//     card at each attempt pushed once, a look's new ones together in one push: never
//     silenced while read cards stay off.
//
// A push the seat leaves unacknowledged past its bound refuses every coordinator command
// (HookGate), as every push does.

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
		n, on := 0, true
		if s != nil && s.Work != nil {
			n, on = s.alarmSetting(a.prop)
		}
		switch {
		case !on:
			d.Value = AlarmOff
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
		said = append(said, fmt.Sprintf("%s is %s, working default %s (restore: %s)", d.Flag, d.Value, d.Working, d.Restore()))
		flags = append(flags, d.Flag+" "+d.Working)
	}
	if len(said) == 0 {
		return ""
	}
	return WarningWord + "design settings off their working defaults, the machine is degraded: " + strings.Join(said, "; ") +
		"; run: nova-sprint set " + strings.Join(flags, " ")
}

// ReviewOffReadCards is each primary in review, as <id>@<attempt>, while read cards are off;
// none while they are on.
func ReviewOffReadCards(s *Snapshot) []string {
	if s == nil || s.Work == nil || s.ReadCardsOn() {
		return nil
	}
	var out []string
	for _, c := range s.Work.Column(Review) {
		if !IsSentinel(c) {
			out = append(out, c.ID+"@"+itoa(max(c.Int("attempt"), 1)))
		}
	}
	return out
}

// ReviewOffReadCardsWhat is the review warning's text for the cards (ReviewOffReadCards).
func ReviewOffReadCardsWhat(cards []string) string {
	return WarningWord + "read cards are off: " + strings.Join(cards, ", ") + " entered review and each is asked one read per tick through the readers table, not every read at once; run: nova-sprint set --read-cards on"
}

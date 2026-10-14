package sprint

import (
	"slices"
	"strconv"
	"strings"
)

// SettingRow is one setting `set` takes: its flag, its work-table property,
// its default (the Defaults row's Value, the single source), and the
// accessor the tick reads it through. SettingsRows is built beside the
// props (settings.go, defaults.go): TestEverySetFlagHasASettingsRow fails
// when a flag of `set` has no row or a row's default drifts from Defaults.
type SettingRow struct {
	Flag     string `json:"flag"`
	Prop     string `json:"prop"`
	Default  string `json:"default"`
	Accessor string `json:"accessor"`
	value    func(s *Snapshot) string
}

// SettingValue is one setting as a snapshot reads it: its effective value,
// its default, and whether it is set (its value is not its default).
type SettingValue struct {
	Flag     string `json:"flag"`
	Prop     string `json:"prop"`
	Value    string `json:"value"`
	Default  string `json:"default"`
	Set      bool   `json:"set"`
	Accessor string `json:"accessor"`
}

func settingRow(flag, prop, accessor string, value func(s *Snapshot) string) SettingRow {
	d, _ := DefaultOf(prop)
	return SettingRow{Flag: flag, Prop: prop, Default: d.Value, Accessor: accessor, value: value}
}

func propOrDefault(prop string) func(s *Snapshot) string {
	return func(s *Snapshot) string {
		if s != nil && s.Work != nil {
			if v, ok := s.Work.Prop(prop); ok {
				return v
			}
		}
		d, _ := DefaultOf(prop)
		return d.Value
	}
}

// SettingsRows is every setting `set` takes, in flag order. The retired
// --attempts has no row: it is nova-config's sprint row (NotSettings).
func SettingsRows() []SettingRow {
	rows := []SettingRow{
		settingRow("alarm-fleet", PropAlarmFleet, "Snapshot.alarmSetting", propOrDefault(PropAlarmFleet)),
		settingRow("alarm-merging", PropAlarmMerging, "Snapshot.alarmSetting", propOrDefault(PropAlarmMerging)),
		settingRow("alarm-ready", PropAlarmReady, "Snapshot.alarmSetting", propOrDefault(PropAlarmReady)),
		settingRow("alarm-review", PropAlarmReview, "Snapshot.alarmSetting", propOrDefault(PropAlarmReview)),
		settingRow("base", PropSprintBase, "SprintBase", func(s *Snapshot) string { return SprintBase(s) }),
		settingRow("dealt-max", PropDealtMax, "Snapshot.DealtMax", func(s *Snapshot) string { return s.DealtMax().String() }),
		settingRow("fleet", PropFleet, "Snapshot.FleetOff", func(s *Snapshot) string {
			if s == nil {
				return SwitchOn
			}
			props := map[string]string{}
			if s.Work != nil {
				props = s.Work.Props()
			}
			return SwitchWord(props, PropFleet)
		}),
		settingRow("fleet-tiers", PropFleetTiers, "Snapshot.FleetTakes", func(s *Snapshot) string {
			var props map[string]string
			if s != nil && s.Work != nil {
				props = s.Work.Props()
			}
			if t := SideTiers(props, PropFleetTiers); t != nil {
				return strings.Join(t, ",")
			}
			return TiersAll
		}),
		settingRow("friend-finish", PropFriendFinish, "Snapshot.FriendFinishAfter", func(s *Snapshot) string { return s.FriendFinishAfter().String() }),
		settingRow("friend-idle", PropFriendIdle, "Snapshot.FriendIdleAfter", func(s *Snapshot) string { return s.FriendIdleAfter().String() }),
		settingRow("friend-deal", PropFriendDeal, "Snapshot.FriendDealOff", func(s *Snapshot) string {
			if s == nil || s.Work == nil {
				return SwitchOn
			}
			return SwitchWord(s.Work.Props(), PropFriendDeal)
		}),
		settingRow("friends", PropFriends, "Snapshot.FriendsOff", func(s *Snapshot) string {
			if s == nil {
				return SwitchOn
			}
			props := map[string]string{}
			if s.Work != nil {
				props = s.Work.Props()
			}
			return SwitchWord(props, PropFriends)
		}),
		settingRow("friends-tiers", PropFriendsTiers, "Snapshot.FriendsTake", func(s *Snapshot) string {
			var props map[string]string
			if s != nil && s.Work != nil {
				props = s.Work.Props()
			}
			if t := SideTiers(props, PropFriendsTiers); t != nil {
				return strings.Join(t, ",")
			}
			return TiersAll
		}),
		settingRow("go-lanes", PropGoLanes, "LaneWidth", func(s *Snapshot) string {
			v, ok := "", false
			if s != nil && s.Work != nil {
				v, ok = s.Work.Prop(PropGoLanes)
			}
			return strconv.Itoa(LaneWidth(v, ok))
		}),
		settingRow("landing-stall-bound", PropLandingStallBound, "Snapshot.landingStallBound", func(s *Snapshot) string { return s.landingStallBound().String() }),
		settingRow("pegged-bound", PropPeggedBound, "Snapshot.PeggedBound", func(s *Snapshot) string { return s.PeggedBound().String() }),
		settingRow("read-cards", PropReadCards, "Snapshot.ReadCardsOn", func(s *Snapshot) string {
			if s.ReadCardsOn() {
				return ReadCardsOnWord
			}
			return SwitchOff
		}),
		settingRow("read-tier", PropReadTier, "Snapshot.readTierSetting", func(s *Snapshot) string {
			if t := s.readTierSetting(""); t != "" {
				return t
			}
			return ReadTierDefault
		}),
		settingRow("reads", PropReadsNeeded, "ReadsSetting", func(s *Snapshot) string {
			var props map[string]string
			if s != nil && s.Work != nil {
				props = s.Work.Props()
			}
			if n, ok := ReadsSetting(props); ok {
				return strconv.Itoa(n)
			}
			return ReadTierDefault
		}),
		settingRow("rework-priority", PropReworkPriority, "Snapshot (PropReworkPriority)", propOrDefault(PropReworkPriority)),
		settingRow("unserved-tier-falls-to", PropUnservedTierFallsTo, "Snapshot.UnservedTierFallsTo", func(s *Snapshot) string {
			if t := s.UnservedTierFallsTo(); t != "" {
				return t
			}
			return SwitchOff
		}),
	}
	slices.SortFunc(rows, func(a, b SettingRow) int { return strings.Compare(a.Flag, b.Flag) })
	return rows
}

// SettingsOf is every setting as the snapshot reads it, in flag order.
func SettingsOf(s *Snapshot) []SettingValue {
	rows := SettingsRows()
	out := make([]SettingValue, 0, len(rows))
	for _, r := range rows {
		v := r.value(s)
		out = append(out, SettingValue{Flag: r.Flag, Prop: r.Prop, Value: v, Default: r.Default, Set: v != r.Default, Accessor: r.Accessor})
	}
	return out
}

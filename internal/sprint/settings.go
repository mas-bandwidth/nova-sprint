package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// The sprint's settings, the coordinator's (`set`, `stream set`; nova-tools#5096
// items 22 and 27): the dealt bound, how long a work card may wait dealt and never
// taken before it is a judgment, and the read tier, the tier a primary's reads draw
// their route from when it is stronger than the card's own. The sprint's are the
// work table's properties, the stream's a field of its control card; a clear starts
// the next epoch with neither, as it starts every property.
const (
	// PropDealtMax is the work table's property: the dealt bound, a duration.
	PropDealtMax = "dealt_max"
	// PropReadyMax is the work table's property: how long a ready card may sit undealt
	// before the tick raises the ready-over judgment, a duration (a-card-ready-over-a-minute-pushes-the-seat).
	PropReadyMax = "ready_max"
	// PropFriendIdle is the work table's property: how long a friend holding cards may
	// show no session activity before it is an alarm, a duration.
	PropFriendIdle = "friend_idle"
	// PropFriendStallAfter is the work table's property: how long a friend holding cards
	// may show neither session activity nor card progress before she is stalled, a duration.
	PropFriendStallAfter = "friend_stall_after"
	// PropFriendStallStep is the work table's property: the duration of each rung of
	// the friend stall ladder.
	PropFriendStallStep = "friend_stall_step"
	// PropReadTier is the work table's property: the sprint's read tier.
	PropReadTier = "read_tier"
	// PropUnservedTierFallsTo is the work table's property: the tier an unserved card
	// falls to, off by default. Set to heavy, pro or flash, the tick's fallback part gives
	// every unlanded primary whose brief tier no route serves and no up friend serves the
	// strongest served tier at or below the setting, and the deal draws from it; when the
	// brief tier is served again the fallback is cleared on every card not yet taken
	// (unserved_tier.go, docs/SPEC-SPRINT.md section 5).
	PropUnservedTierFallsTo = "unserved_tier_falls_to"
	// FieldReadTier is a stream's control card's field: the stream's read tier,
	// over the sprint's.
	FieldReadTier = "read_tier"
	// FieldRelease is a stream's control card's field: the release the stream
	// belongs to (e.g. "v1.2.0"), set by stream set --release (docs/SPEC-SPRINT.md section 11).
	FieldRelease = "release"
	// FieldDisplay is a stream's control card's field: the stream's display name, the
	// human label the dashboard's Work table shows in place of the key, set by stream
	// set --display (docs/SPEC-SPRINT.md section 11). It is not the stream's key: the
	// key stays the stream's name everywhere (the every stream= field, the rows and the
	// verbs); the display is a label alone.
	FieldDisplay = "display"
	// FieldProse is a stream's control card's field: the globs (PATHS globs, comma
	// separated) of the files whose backquotes are their own, which the lander does not
	// read for a code span (land_repair.go); set by stream set --prose.
	FieldProse = "prose"
	// FieldSeatAccept is a stream's control card's field: on, the seat accepts its cards by
	// hand ("ready to accept") instead of the tick accepting a card whose reads have all
	// passed; set by stream set --seat-accept on, taken off by off (seat_answers.go, rule (a)).
	FieldSeatAccept = "seat_accept"
	// FieldStreamBranch is a stream's control card's field: the branch the stream lands on,
	// sprint/<stream> by default (docs/SPEC-SPRINT.md section 7).
	FieldStreamBranch = "stream_branch"
	// FieldTrunk is a stream's control card's field: the trunk branch (dev or main) of the stream's branch.
	FieldTrunk = "trunk"
	// PropFleet and PropFriends are the work table's properties: the switches of the
	// fleet's work and the friends' work (the owner, 2026-10-06: "We should be able to
	// enable/disable fleet, enable/disable friends. default enabled."), SwitchOn or
	// SwitchOff, on when absent. Off, the deal hands that side no work card; reads flow.
	PropFleet   = "fleet"
	PropFriends = "friends"
	// SwitchOn and SwitchOff are a switch's words.
	SwitchOn  = "on"
	SwitchOff = "off"
	// PropFleetTiers and PropFriendsTiers are the work table's properties: the tiers each
	// side may take (the owner, 2026-10-06: "a setting for friends, what tiers they may
	// take, default to all. Same setting for the fleet."), comma joined in the ladder's
	// order, or TiersAll, all when absent. The deal hands a side only cards whose tier is
	// in its set, on top of each row's own tiers, work cards and read cards alike; a card
	// pinned to a model is no exception (the set is the owner's switch), and a frontier card
	// waits for the coordinator whatever the sets.
	PropFleetTiers   = "fleet_tiers"
	PropFriendsTiers = "friends_tiers"
	// PropReadsNeeded is the work table's property: how many readers' ok reads at its
	// head every card in review needs, whatever its tier (nova-sprint set --reads): "0",
	// "1" or "2"; absent or default, each card's own rule (ReadsNeeded). 0 asks no read:
	// a primary whose work finished LAND at its head is accepted on its own work.
	PropReadsNeeded = "reads_needed"
	// PropReworkPriority controls priority on the next attempt: fix (default), high or keep.
	PropReworkPriority = "rework_priority"
	ReworkKeep         = "keep"
	// PropLowBalanceUsd is the work table's property: the low balance threshold in USD.
	PropLowBalanceUsd = "low_balance_usd"
	// TiersAll is the word of a side that may take every tier, the default.
	TiersAll = "all"
	// ReadTierDefault is the word that takes a read tier off: a stream's back to
	// the sprint's, the sprint's back to each card's own tier.
	ReadTierDefault = "default"
)

// FleetOff says the fleet's work is switched off (nova-sprint set --fleet off): the deal
// hands no work card to a machine, and the fleet's idle alarms are not raised; the fleet's
// readers still read.
func (s *Snapshot) FleetOff() bool { return s.switchOff(PropFleet) }

// FriendsOff says the friends' work is switched off (nova-sprint set --friends off): the
// deal hands no work card to a friend (friendDealable), and her empty row is no alarm; her
// reads still flow.
func (s *Snapshot) FriendsOff() bool { return s.switchOff(PropFriends) }

func (s *Snapshot) switchOff(prop string) bool {
	if s == nil || s.Work == nil {
		return false
	}
	v, _ := s.Work.Prop(prop)
	return v == SwitchOff
}

// FleetTakes says the deal may hand the fleet a card of the tier (set --fleet-tiers): a
// machine's work card's tier, or a member's read card's read tier, is in the fleet's set.
func (s *Snapshot) FleetTakes(tier string) bool { return s.sideTakes(PropFleetTiers, tier) }

// FriendsTake says the deal may hand a friend a card of the tier (set --friends-tiers), as
// FleetTakes says it of the fleet; her row's own tiers still bound what she takes.
func (s *Snapshot) FriendsTake(tier string) bool { return s.sideTakes(PropFriendsTiers, tier) }

func (s *Snapshot) sideTakes(prop, tier string) bool {
	if s == nil || s.Work == nil {
		return true
	}
	v, _ := s.Work.Prop(prop)
	return v == "" || v == TiersAll || slices.Contains(Split(v), tier)
}

// LowBalanceUsdDefault is the low balance threshold when the coordinator set none: five dollars.
const LowBalanceUsdDefault = 5.0

// lowBalanceUsdSetting is the low balance threshold in USD as the work table's properties
// hold them (PropLowBalanceUsd): a positive number, and ok false while none is set.
func lowBalanceUsdSetting(props map[string]string) (float64, bool) {
	v := props[PropLowBalanceUsd]
	if v == "" {
		return LowBalanceUsdDefault, false
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// readsWords is the counts set --reads takes (PropReadsNeeded).
var readsWords = []string{"0", "1", "2", "3"}

// ReadsSetting is the reads every card in review needs as the work table's properties
// hold them (PropReadsNeeded): 0, 1, 2 or 3, and ok false while none is set (default: each
// card's own rule, ReadsNeeded).
func ReadsSetting(props map[string]string) (n int, ok bool) {
	v := props[PropReadsNeeded]
	if !slices.Contains(readsWords, v) {
		return 0, false
	}
	n, _ = strconv.Atoi(v)
	return n, true
}

// SideTiers is a side's tiers as the work table's properties hold them (PropFleetTiers,
// PropFriendsTiers): nil for all, the default, else the tiers in the ladder's order.
func SideTiers(props map[string]string, prop string) []string {
	if v := props[prop]; v != "" && v != TiersAll {
		return Split(v)
	}
	return nil
}

// sideTiersWord is the value a side's tiers setting writes: TiersAll, or the tiers named in
// the ladder's order, each once; why says what is wrong with v, naming the four tiers.
func sideTiersWord(flag, v string) (word, why string) {
	if v == TiersAll {
		return TiersAll, ""
	}
	named := Split(v)
	for _, t := range named {
		if !slices.Contains(capLadder, t) {
			named = nil
			break
		}
	}
	if len(named) == 0 {
		return "", flag + " wants tiers of " + strings.Join(capLadder, ", ") + ", comma separated, or " + TiersAll + "; found " + v
	}
	var out []string
	for _, t := range capLadder {
		if slices.Contains(named, t) {
			out = append(out, t)
		}
	}
	return strings.Join(out, ","), ""
}

// SwitchWord is a switch's word as the work table's properties hold it: SwitchOff when
// set off, else SwitchOn (the default).
func SwitchWord(props map[string]string, prop string) string {
	if props[prop] == SwitchOff {
		return SwitchOff
	}
	return SwitchOn
}

// UnservedTierFallsTo is the tier an unserved card falls to (PropUnservedTierFallsTo):
// heavy, pro or flash, "" when the setting is off or default (today's behaviour, the
// fallback part does nothing).
func (s *Snapshot) UnservedTierFallsTo() string {
	if s == nil || s.Work == nil {
		return ""
	}
	v, _ := s.Work.Prop(PropUnservedTierFallsTo)
	if v == "" || v == ReadTierDefault || v == SwitchOff {
		return ""
	}
	return v
}

// DealtMaxDefault is the dealt bound when the coordinator set none: three times
// the deadline a taken card is held to. A member holds at most DealAhead times its
// width, ready and working, so a card at the back of its ready queue is taken
// within two take deadlines of a member that works; the third is the margin.
const DealtMaxDefault = 3 * DeadlineUnfinished

// DealtMax is the dealt bound the tick holds a work card dealt and never taken to:
// the sprint's setting, else DealtMaxDefault.
func (s *Snapshot) DealtMax() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropDealtMax); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return DealtMaxDefault
}

// ReadyMaxDefault is how long a ready card may sit undealt before the tick raises the
// ready-over judgment (a-card-ready-over-a-minute-pushes-the-seat).
const ReadyMaxDefault = time.Minute

// ReadyMax is that bound: the sprint's setting, else ReadyMaxDefault.
func (s *Snapshot) ReadyMax() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropReadyMax); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return ReadyMaxDefault
}

// FriendIdleDefault is how long a friend holding cards may show no file write under her
// working directory and outbox before it is an alarm (nova-sprint set --friend-idle).
const FriendIdleDefault = 20 * time.Minute

// FriendIdleAfter is that bound: the sprint's setting, else FriendIdleDefault.
func (s *Snapshot) FriendIdleAfter() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropFriendIdle); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return FriendIdleDefault
}

// FriendStallAfterDefault is how long a friend holding cards may show neither file write
// under her working directory nor card progress before the stall ladder begins.
const FriendStallAfterDefault = 20 * time.Minute

// FriendStallAfter is that bound: the sprint's setting, else FriendStallAfterDefault.
func (s *Snapshot) FriendStallAfter() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropFriendStallAfter); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return FriendStallAfterDefault
}

// FriendStallStepDefault is the duration between rungs of the friend stall ladder.
const FriendStallStepDefault = 5 * time.Minute

// FriendStallStep is that step: the sprint's setting, else FriendStallStepDefault.
func (s *Snapshot) FriendStallStep() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropFriendStallStep); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return FriendStallStepDefault
}

// readTierSetting is the read tier set for the stream's reads: the stream's own,
// else the sprint's, else "" (each card's own tier).
func (s *Snapshot) readTierSetting(stream string) string {
	if s.Merge != nil {
		if t := s.StreamCtl(stream).F(FieldReadTier); t != "" {
			return t
		}
	}
	if s.Work != nil {
		if t, ok := s.Work.Prop(PropReadTier); ok && t != ReadTierDefault {
			return t
		}
	}
	return ""
}

// readTiers is the tiers a read tier may name, weakest first: a setting raises a
// card's reads to it and never lowers them (the coordinator, 2026-10-02: "A pro
// card's reads should run on a tier at least as strong as the writer's").
var readTiers = []string{cardhdr.RouteFlash, cardhdr.RoutePro, cardhdr.RouteHeavy}

// stronger is the stronger of two read tiers.
func stronger(a, b string) string {
	if slices.Index(readTiers, b) > slices.Index(readTiers, a) {
		return b
	}
	return a
}

// SetReq is the coordinator's settings: with Streams, each stream's read tier,
// attempt cap, release, or protected-branch mark; without, the sprint's dealt
// bound, read tier and attempt cap (brief_bound.go, AttemptsCap). An empty value
// leaves that setting as it is; ReadTierDefault takes one off.
type SetReq struct {
	ReworkPriority    string   `json:",omitempty"`
	Streams           []string `json:",omitempty"`
	ReadTier          string   `json:",omitempty"`
	DealtMax          string   `json:",omitempty"`
	Attempts          string   `json:",omitempty"`
	FriendIdle        string   `json:",omitempty"`
	LandingStallBound string   `json:",omitempty"`
	FriendStallAfter  string   `json:",omitempty"`
	FriendStallStep   string   `json:",omitempty"`
	// Reason, with Streams and ReadTier, is why the read tier is set, recorded on the
	// stream's control card (FieldReadTierReason); Answers the judgments this answers.
	Reason  string   `json:",omitempty"`
	Answers []string `json:",omitempty"`
	// LandProtected is the streams' mark: the repositories whose protected branches
	// they land on (FieldLandProtected, docs/SPEC-SPRINT.md section 7).
	LandProtected string `json:",omitempty"`
	Release       string `json:",omitempty"`
	// Display is the streams' display name (FieldDisplay, docs/SPEC-SPRINT.md section
	// 11): one line of printable text, at most DisplayMax bytes, the label the dashboard
	// shows in place of the key; default or none takes it off.
	Display string `json:",omitempty"`
	// Prose is the streams' prose globs (FieldProse): comma separated, default or none
	// takes them off.
	Prose string `json:",omitempty"`
	// SeatAccept is the streams' seat-accept switch (FieldSeatAccept): on or off.
	SeatAccept string `json:",omitempty"`
	// DefaultTier is the streams' default tier (FieldDefaultTier): the tier add stores a card
	// with when its brief's line 1 names none; default or none takes it off.
	DefaultTier string `json:",omitempty"`
	// The backlog alarms' thresholds (alarms.go): a count, a percent, on, or off.
	AlarmReview  string `json:",omitempty"`
	AlarmMerging string `json:",omitempty"`
	AlarmFleet   string `json:",omitempty"`
	AlarmReady   string `json:",omitempty"`
	GoLanes      string `json:",omitempty"` // the Go lanes of every machine (PropGoLanes, section 18)
	// The drift alarm's thresholds of the base ahead of dev (drift.go): commits and hours,
	// whole numbers from 1, or default.
	DriftCommits string `json:",omitempty"`
	DriftHours   string `json:",omitempty"`
	// Fleet and Friends are the work switches (PropFleet, PropFriends): on or off.
	Fleet   string `json:",omitempty"`
	Friends string `json:",omitempty"`
	// FleetTiers and FriendsTiers are the tiers each side may take (PropFleetTiers,
	// PropFriendsTiers): tiers comma separated, or all.
	FleetTiers   string `json:",omitempty"`
	FriendsTiers string `json:",omitempty"`
	// ReadCards turns read cards on or off (PropReadCards, read_cards.go): on, off, or
	// default (off).
	ReadCards string `json:",omitempty"`
	// Reads is the reads every card in review needs (PropReadsNeeded): 0, 1, 2, 3, or
	// default (each card's own rule, ReadsNeeded).
	Reads string `json:",omitempty"`
	// UnservedTierFallsTo is the tier an unserved card falls to (PropUnservedTierFallsTo):
	// heavy, pro, flash, off, or default (off).
	UnservedTierFallsTo string `json:",omitempty"`
	// Base, with Streams, re-points every card of the stream not yet landed to
	// another base branch (stream set --base, streamSetBaseCards).
	Base string `json:",omitempty"`
	// BaseChecked is the cards Base re-points, as the verb read and checked them
	// at the base (StreamSetBaseChecks): Set re-points exactly these and refuses
	// whole, nothing written, when the snapshot it reads holds other cards or
	// briefs, so no brief is rewritten without its PATHS held to the base. It is
	// no part of the verb's arguments (its value changes as the stream does): the
	// store's operation id digests the caller's words alone.
	BaseChecked []StreamSetBaseCheck `json:"-"`
	Who         string
}

// Set writes the settings: refused whole, writing nothing, for an actor who is not
// the coordinator, a read tier that is not flash, pro or default, a dealt bound that
// is not a positive duration, a mark that names no repository or is not a stream's,
// an alarm's threshold it does not take, a side's tiers that are not of the four tiers or
// all, nothing to set, or a stream that is not a stream (docs/SPEC-SPRINT.md section 11).
func Set(s *Snapshot, r SetReq) Plan {
	var p Plan
	var why []string
	if w := notCoordinator(s, r.Who, "set"); w != "" {
		why = append(why, strings.Replace(w, "answers a judgment, which is", "is", 1))
	}
	if r.ReadTier != "" && r.ReadTier != ReadTierDefault && !slices.Contains(readTiers, r.ReadTier) {
		why = append(why, "--read-tier wants "+strings.Join(readTiers, " or ")+", or "+ReadTierDefault+" to take it off; found "+r.ReadTier)
	}
	if r.DealtMax != "" && r.DealtMax != ReadTierDefault {
		if d, err := time.ParseDuration(r.DealtMax); err != nil || d <= 0 {
			why = append(why, "--dealt-max wants a duration above zero (6h, 90m), or "+ReadTierDefault+" for 3 times the take deadline; found "+r.DealtMax)
		}
	}
	if r.LandProtected != "" {
		if w := landProtectedWhy(r.LandProtected); w != "" {
			why = append(why, w)
		}
		if len(r.Streams) == 0 {
			why = append(why, "--land-protected is a stream's, not the sprint's: nova-sprint stream set <stream> --land-protected "+r.LandProtected)
		}
	}
	if r.Prose != "" {
		if len(r.Streams) == 0 {
			why = append(why, "--prose is a stream's, not the sprint's: nova-sprint stream set <stream> --prose "+r.Prose)
		}
		for _, g := range strings.Split(r.Prose, ",") {
			if g = strings.TrimSpace(g); g == "" || strings.HasPrefix(g, "-") || strings.HasPrefix(g, "/") || strings.ContainsAny(g, " \t") {
				why = append(why, "--prose wants path globs (security/**, ratings/**), comma separated, or "+ReadTierDefault+" to take them off; found "+r.Prose)
				break
			}
		}
	}
	if r.SeatAccept != "" {
		if len(r.Streams) == 0 {
			why = append(why, "--seat-accept is a stream's, not the sprint's: nova-sprint stream set <stream> --seat-accept "+r.SeatAccept)
		}
		if r.SeatAccept != SwitchOn && r.SeatAccept != SwitchOff {
			why = append(why, "--seat-accept wants "+SwitchOn+" or "+SwitchOff+"; found "+r.SeatAccept)
		}
	}
	if r.DefaultTier != "" {
		if len(r.Streams) == 0 {
			why = append(why, "--default-tier is a stream's, not the sprint's: nova-sprint stream set <stream> --default-tier "+r.DefaultTier)
		}
		if r.DefaultTier != ReadTierDefault && r.DefaultTier != "none" && !slices.Contains(tierLadder, r.DefaultTier) {
			why = append(why, "--default-tier wants flash, pro or heavy, or "+ReadTierDefault+" to take it off; found "+r.DefaultTier)
		}
	}
	if r.GoLanes != "" && r.GoLanes != ReadTierDefault {
		if n, err := strconv.Atoi(r.GoLanes); err != nil || n < 1 {
			why = append(why, "--go-lanes wants a whole number from 1, the Go lanes of every machine, or "+ReadTierDefault+" for "+strconv.Itoa(LaneWidthDefault)+"; found "+r.GoLanes)
		}
	}
	alarms := r.alarms()
	for _, a := range alarmProps {
		if v := alarms[a.prop]; v != "" && !alarmValid(a.prop, v) {
			why = append(why, a.flag+" wants "+a.wants+", or "+AlarmOff+" to take it off; found "+v)
		}
		if v := alarms[a.prop]; v != "" && len(r.Streams) > 0 {
			why = append(why, a.flag+" is the sprint's, not a stream's: nova-sprint set "+a.flag+" "+v)
		}
	}
	for _, d := range [][2]string{{"--drift-commits", r.DriftCommits}, {"--drift-hours", r.DriftHours}} {
		if d[1] == "" {
			continue
		}
		if !driftValid(d[1]) {
			why = append(why, d[0]+" wants a whole number from 1, or "+ReadTierDefault+"; found "+d[1])
		}
		if len(r.Streams) > 0 {
			why = append(why, d[0]+" is the sprint's, not a stream's: nova-sprint set "+d[0]+" "+d[1])
		}
	}
	for _, sw := range [][2]string{{"--fleet", r.Fleet}, {"--friends", r.Friends}} {
		if sw[1] == "" {
			continue
		}
		if sw[1] != SwitchOn && sw[1] != SwitchOff {
			why = append(why, sw[0]+" wants "+SwitchOn+" or "+SwitchOff+"; found "+sw[1])
		}
		if len(r.Streams) > 0 {
			why = append(why, sw[0]+" is the sprint's, not a stream's: nova-sprint set "+sw[0]+" "+sw[1])
		}
	}
	sideTiers := map[string]string{}
	for _, st := range [][3]string{{"--fleet-tiers", r.FleetTiers, PropFleetTiers}, {"--friends-tiers", r.FriendsTiers, PropFriendsTiers}} {
		if st[1] == "" {
			continue
		}
		word, w := sideTiersWord(st[0], st[1])
		if w != "" {
			why = append(why, w)
		}
		if len(r.Streams) > 0 {
			why = append(why, st[0]+" is the sprint's, not a stream's: nova-sprint set "+st[0]+" "+st[1])
		}
		sideTiers[st[2]] = word
	}
	if r.Attempts != "" {
		if _, err := ParseAttempts(r.Attempts); err != nil {
			why = append(why, err.Error())
		}
	}
	if r.FriendIdle != "" && r.FriendIdle != ReadTierDefault {
		if d, err := time.ParseDuration(r.FriendIdle); err != nil || d <= 0 {
			why = append(why, "--friend-idle wants a duration above zero (20m, 1h), or "+ReadTierDefault+" for "+FriendIdleDefault.String()+"; found "+r.FriendIdle)
		}
	}
	if r.LandingStallBound != "" && r.LandingStallBound != ReadTierDefault {
		if d, err := time.ParseDuration(r.LandingStallBound); err != nil || d <= 0 {
			why = append(why, "--landing-stall-bound wants a duration above zero (1m, 5m), or "+ReadTierDefault+" for "+LandingStallBound.String()+"; found "+r.LandingStallBound)
		}
	}
	if r.FriendStallAfter != "" && r.FriendStallAfter != ReadTierDefault {
		if d, err := time.ParseDuration(r.FriendStallAfter); err != nil || d <= 0 {
			why = append(why, "--friend-stall-after wants a duration above zero (20m, 1h), or "+ReadTierDefault+" for "+FriendStallAfterDefault.String()+"; found "+r.FriendStallAfter)
		}
	}
	if r.FriendStallStep != "" && r.FriendStallStep != ReadTierDefault {
		if d, err := time.ParseDuration(r.FriendStallStep); err != nil || d <= 0 {
			why = append(why, "--friend-stall-step wants a duration above zero (5m, 10m), or "+ReadTierDefault+" for "+FriendStallStepDefault.String()+"; found "+r.FriendStallStep)
		}
	}
	if r.ReadCards != "" {
		if r.ReadCards != ReadCardsOnWord && r.ReadCards != "off" && r.ReadCards != ReadTierDefault {
			why = append(why, "--read-cards wants on, off or "+ReadTierDefault+" (off); found "+r.ReadCards)
		}
		if len(r.Streams) > 0 {
			why = append(why, "--read-cards is the sprint's, not a stream's: nova-sprint set --read-cards "+r.ReadCards)
		}
	}
	if r.ReworkPriority != "" {
		if !slices.Contains([]string{PriorityFix, PriorityHigh, ReworkKeep}, r.ReworkPriority) {
			why = append(why, "--rework-priority wants fix, high or keep; found "+r.ReworkPriority)
		}
		if len(r.Streams) > 0 {
			why = append(why, "--rework-priority is the sprint's, not a stream's: nova-sprint set --rework-priority "+r.ReworkPriority)
		}
	}
	if r.Reads != "" {
		if r.Reads != ReadTierDefault && !slices.Contains(readsWords, r.Reads) {
			why = append(why, "--reads wants 0, 1, 2, 3 or "+ReadTierDefault+" (one for a flash card, two above); found "+r.Reads)
		}
		if len(r.Streams) > 0 {
			why = append(why, "--reads is the sprint's, not a stream's: nova-sprint set --reads "+r.Reads)
		}
	}
	if r.UnservedTierFallsTo != "" {
		if r.UnservedTierFallsTo != ReadTierDefault && r.UnservedTierFallsTo != SwitchOff && !slices.Contains(tierLadder, r.UnservedTierFallsTo) {
			why = append(why, "--unserved-tier-falls-to wants heavy, pro, flash or "+SwitchOff+" (the default); found "+r.UnservedTierFallsTo)
		}
		if len(r.Streams) > 0 {
			why = append(why, "--unserved-tier-falls-to is the sprint's, not a stream's: nova-sprint set --unserved-tier-falls-to "+r.UnservedTierFallsTo)
		}
	}
	if r.Base != "" && len(r.Streams) == 0 {
		why = append(why, "--base is a stream's, not the sprint's: nova-sprint stream set <s> --base <branch>")
	}
	if r.ReadTier == "" && r.DealtMax == "" && r.LandProtected == "" && r.Release == "" && r.Display == "" && r.Prose == "" && r.SeatAccept == "" && r.DefaultTier == "" && r.Base == "" && len(alarms) == 0 && r.GoLanes == "" && r.Attempts == "" && r.FriendIdle == "" && r.LandingStallBound == "" && r.FriendStallAfter == "" && r.FriendStallStep == "" && r.DriftCommits == "" && r.DriftHours == "" && r.Fleet == "" && r.Friends == "" && len(sideTiers) == 0 && r.ReadCards == "" && r.Reads == "" && r.UnservedTierFallsTo == "" && r.ReworkPriority == "" {
		why = append(why, "nothing to set: --rework-priority, --read-tier, --read-cards, --reads, --unserved-tier-falls-to, --prose, --base, --dealt-max, --go-lanes, --attempts, --friend-idle, --landing-stall-bound, --friend-stall-after, --friend-stall-step, --drift-commits, --drift-hours, --fleet, --friends, --fleet-tiers, --friends-tiers or an --alarm-... threshold")
	}
	if len(r.Streams) > 0 && r.GoLanes != "" {
		why = append(why, "--go-lanes is the sprint's, not a stream's: nova-sprint set --go-lanes "+r.GoLanes)
	}
	if len(r.Streams) > 0 && r.DealtMax != "" {
		why = append(why, "--dealt-max is the sprint's, not a stream's: nova-sprint set --dealt-max "+r.DealtMax)
	}
	if len(r.Streams) == 0 && r.Release != "" {
		why = append(why, "--release is a stream's, not the sprint's: nova-sprint stream set <s> --release "+r.Release)
	}
	if len(r.Streams) == 0 && r.Display != "" {
		why = append(why, "--display is a stream's, not the sprint's: nova-sprint stream set <s> --display "+r.Display)
	}
	if len(r.Streams) > 0 && r.FriendIdle != "" {
		why = append(why, "--friend-idle is the sprint's, not a stream's: nova-sprint set --friend-idle "+r.FriendIdle)
	}
	if len(r.Streams) > 0 && r.LandingStallBound != "" {
		why = append(why, "--landing-stall-bound is the sprint's, not a stream's: nova-sprint set --landing-stall-bound "+r.LandingStallBound)
	}
	if len(r.Streams) > 0 && r.FriendStallAfter != "" {
		why = append(why, "--friend-stall-after is the sprint's, not a stream's: nova-sprint set --friend-stall-after "+r.FriendStallAfter)
	}
	if len(r.Streams) > 0 && r.FriendStallStep != "" {
		why = append(why, "--friend-stall-step is the sprint's, not a stream's: nova-sprint set --friend-stall-step "+r.FriendStallStep)
	}
	for _, st := range r.Streams {
		if s.StreamCtl(st) == nil {
			why = append(why, "no stream "+st)
			continue
		}
		// the floor: a stream's read tier is never below its work tier (readtier.go)
		if work := StreamWorkTier(s, st); r.ReadTier != "" && r.ReadTier != ReadTierDefault && slices.Contains(readTiers, r.ReadTier) && stronger(work, r.ReadTier) != r.ReadTier {
			why = append(why, st+"'s work tier is "+work+": its read tier is never below it; run: nova-sprint stream set "+st+" --read-tier "+work)
		}
	}
	if len(why) > 0 {
		p.refuse("set", strings.Join(why, "; "))
		return p
	}
	// stream set --base holds each rewritten brief to the check add runs at the
	// new base before its step (cmd/nova-sprint's holdBriefBase), and the step
	// plans over a snapshot of its own: bind the write to the candidates the check
	// read. A card added to the stream, landed, or revised between the check and
	// the step is refused whole, nothing written, never rewritten with its PATHS
	// unchecked (docs/SPEC-SPRINT.md section 11, stream set --base).
	if r.Base != "" && len(r.Streams) > 0 {
		if why := baseChecksHeld(s, r.Streams, r.Base, r.BaseChecked); why != "" {
			p.refuse("set", why)
			return p
		}
		// THE FENCE. A card whose landing is marked (MarkLanding wrote FieldLandingHead on
		// its merge card: the lander is between its mark and its push) is about to land on
		// the base it was built onto. Re-pointing it here would leave the push on the old
		// base with the brief on the new one; skipping it alone would leave one stream with
		// two targets. So the whole base change is refused, naming the busy card, inside
		// this step, the one that writes the base, so there is no interval between the
		// check and the write; the seat runs the verb again when the pass has ended.
		for _, st := range r.Streams {
			for _, c := range streamSetBaseCards(s, st) {
				if s.Merge == nil {
					break
				}
				if m := s.Merge.Placed(c.ID); m != nil && m.F(FieldLandingHead) != "" {
					p.refuse("set", fmt.Sprintf("%s of stream %s is landing (its landing is marked at head %s): stream set --base waits for the pass to end; nothing was written; run the verb again", c.ID, st, m.F(FieldLandingHead)))
					return p
				}
			}
		}
	}
	// a display name is one line of printable text, at most DisplayMax bytes: refused
	// whole, nothing written, when it is not (stream set --display, docs/SPEC-SPRINT.md
	// section 11).
	if r.Display != "" && r.Display != ReadTierDefault && r.Display != "none" {
		if why := DisplayNameWhy(r.Display); why != "" {
			p.refuse("set", why)
			return p
		}
	}
	if len(r.Streams) > 0 {
		for _, st := range r.Streams {
			ctl := s.StreamCtl(st)
			set, unset := map[string]string{}, []string{}
			var moved []string
			var closes []Open
			if r.ReadTier != "" {
				if r.ReadTier == ReadTierDefault || r.ReadTier == "none" {
					unset = append(unset, FieldReadTier, FieldReadTierReason)
					moved = append(moved, "read-tier the sprint's")
				} else {
					set[FieldReadTier] = r.ReadTier
					moved = append(moved, "read-tier "+r.ReadTier)
					if r.Reason != "" {
						set[FieldReadTierReason] = r.Reason
					}
					// a raise answers the stream's judgment that it should rise (readtier.go)
					for _, o := range s.Open {
						if o.Note.Type == NRaiseReadTier && o.Subject() == StreamSubject(st) {
							closes = append(closes, o)
						}
					}
				}
			}
			for _, f := range []struct{ field, v, word, off string }{
				{FieldLandProtected, r.LandProtected, "land-protected", "none"},
				{FieldRelease, r.Release, "release", "none"},
				{FieldDisplay, r.Display, "display", "none"},
				{FieldProse, r.Prose, "prose", "none"},
				{FieldDefaultTier, r.DefaultTier, "default-tier", "none"},
			} {
				switch f.v {
				case "":
				case ReadTierDefault, "none":
					unset, moved = append(unset, f.field), append(moved, f.word+" "+f.off)
				default:
					set[f.field], moved = f.v, append(moved, f.word+" "+f.v)
				}
			}
			switch r.SeatAccept {
			case SwitchOn:
				set[FieldSeatAccept], moved = SwitchOn, append(moved, "seat-accept on")
			case SwitchOff:
				unset, moved = append(unset, FieldSeatAccept), append(moved, "seat-accept off")
			}
			if r.Attempts != "" {
				if r.Attempts == ReadTierDefault {
					unset = append(unset, FieldAttempts)
					moved = append(moved, "attempts the sprint's")
				} else {
					set[FieldAttempts] = r.Attempts
					moved = append(moved, "attempts "+r.Attempts)
				}
			}
			// stream set --base rewrites the BASE line of every card of the stream not
			// yet landed, whatever its column (streamSetBaseCards), each a change of
			// the work table the store writes, with the brief revision the replacement
			// is (brief_attempt, as brief records one); a landed card keeps its base
			// (docs/SPEC-SPRINT.md section 11, stream set --base).
			if r.Base != "" {
				moved = append(moved, "base "+r.Base)
				for _, c := range streamSetBaseCards(s, st) {
					next := briefOnBase(c.F("brief"), r.Base)
					p.Units = append(p.Units, Unit{Key: c.ID, Stream: st,
						Changes: []Change{change(Work, setEntry(c, map[string]string{"brief": next, FieldBriefAttempt: c.F("attempt")}))},
						Moved:   fmt.Sprintf("%s base %s -> %s (%s)", c.ID, orDash(baseOfBrief(c.F("brief"))), r.Base, c.Col)})
				}
			}
			p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: st, Changes: []Change{change(Merge, setEntry(ctl, set, unset...))}, Moved: "stream " + st + " " + strings.Join(moved, ", "), Closes: closes})
		}
		answered(&p, s, r.Answers, r.Who)
		return p
	}
	// a property is written with its word, default included: the readers take
	// default for none (DealtMax, readTierSetting)
	var moved []string
	kvs := [][2]string{
		{PropReadTier, r.ReadTier},
		{PropDealtMax, r.DealtMax},
		{PropGoLanes, r.GoLanes},
		{PropAttempts, r.Attempts},
		{PropFriendIdle, r.FriendIdle},
		{PropLandingStallBound, r.LandingStallBound},
		{PropFriendStallAfter, r.FriendStallAfter},
		{PropFriendStallStep, r.FriendStallStep},
		{PropDriftCommits, r.DriftCommits},
		{PropDriftHours, r.DriftHours},
		{PropFleet, r.Fleet},
		{PropFriends, r.Friends},
		{PropFleetTiers, sideTiers[PropFleetTiers]},
		{PropFriendsTiers, sideTiers[PropFriendsTiers]},
		{PropReadCards, r.ReadCards},
		{PropReadsNeeded, r.Reads},
		{PropUnservedTierFallsTo, r.UnservedTierFallsTo},
		{PropReworkPriority, r.ReworkPriority},
	}
	for _, a := range alarmProps {
		kvs = append(kvs, [2]string{a.prop, alarms[a.prop]})
	}
	for _, kv := range kvs {
		if kv[1] == "" {
			continue
		}
		was, had := s.Work.Prop(kv[0])
		p.Props = append(p.Props, PropWrite{Table: Work, Name: kv[0], Value: kv[1], Was: was, WasAbsent: !had})
		moved = append(moved, strings.ReplaceAll(kv[0], "_", "-")+" "+orDefault(kv[1], kv[0]))
	}
	p.Units = append(p.Units, Unit{Key: "set", Moved: "sprint " + strings.Join(moved, ", ")})
	return p
}

// orDefault is a setting's value as the line says it: what default is.
func orDefault(v, name string) string {
	switch {
	case v != ReadTierDefault:
		return v
	case name == PropDealtMax:
		return fmt.Sprintf("default (%s, 3 times the take deadline)", DealtMaxDefault)
	case name == PropGoLanes:
		return fmt.Sprintf("default (%d a machine)", LaneWidthDefault)
	case name == PropAttempts:
		return fmt.Sprintf("default (%d attempts on one brief)", AttemptsDefault)
	case name == PropFriendIdle:
		return fmt.Sprintf("default (%s)", FriendIdleDefault)
	case name == PropLandingStallBound:
		return fmt.Sprintf("default (%s)", LandingStallBound)
	case name == PropFriendStallAfter:
		return fmt.Sprintf("default (%s)", FriendStallAfterDefault)
	case name == PropFriendStallStep:
		return fmt.Sprintf("default (%s)", FriendStallStepDefault)
	case name == PropDriftCommits:
		return fmt.Sprintf("default (%d commits)", DriftCommitsDefault)
	case name == PropDriftHours:
		return fmt.Sprintf("default (%d hours)", DriftHoursDefault)
	case name == PropReadCards:
		return "default (off: the readers table asks)"
	case name == PropReadsNeeded:
		return "default (one for a flash card, two above)"
	}
	return "default (each card's own tier)"
}

// alarms is the backlog alarms' thresholds the request sets, by property; none unset.
func (r SetReq) alarms() map[string]string {
	out := map[string]string{}
	for prop, v := range map[string]string{PropAlarmReview: r.AlarmReview, PropAlarmMerging: r.AlarmMerging, PropAlarmFleet: r.AlarmFleet, PropAlarmReady: r.AlarmReady} {
		if v != "" {
			out[prop] = v
		}
	}
	return out
}

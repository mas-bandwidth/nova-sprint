package sprint

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// A setting's home: where its value lives (docs/SPEC-SPRINT.md section 11, "Working
// defaults").
const (
	// HomeProp is a work-table property, epoch keyed: a clear leaves the new epoch with
	// none, and the clear's set-defaults step writes the row's Value into it
	// (store.Clear, SetDefaults; tla/MachineReady.tla SetDefaults).
	HomeProp = "prop"
	// HomeBar is a field of nova-config's sprint row (config.SprintKey), not epoch
	// keyed and never written by the sprint (nova-config apply would undo it): its
	// default is read-side, an empty field read as the row's Value where ReadFill says
	// so (store.Bars.WithDefaults).
	HomeBar = "bar"
	// HomeShape is the sprint's shape carried across a clear by restoreMembers (Shape):
	// its Value is "carried", the old epoch's own; a-cleared-sprint-reads-with-no-seat-action
	// owns what it holds.
	HomeShape = "shape"
)

// ShapeCarried is a shape row's Value: the old epoch's, carried by the clear.
const ShapeCarried = "carried"

// Default is one setting's working default: its name (the property, the sprint row's
// field, or the shape's JSON field), its home, its value, and why that value is the one
// the sprint works with. ReadFill, for a bar, says an empty field is read as Value (its
// consumer has no default of its own); a bar whose empty value is already its default
// (a decide bar "" is no bar; max_attempts "" is AttemptsDefault) leaves it false.
type Default struct {
	Name, Home, Value, Why string
	ReadFill               bool
}

// Defaults is every setting's working default, the single source (the owner, 2026-10-13:
// "you need a post epoch increment set defaults step for clear. you advance epoch, and then
// set defaults in the new epoch that need to be set. eg. reading true, git_identity etc.").
// Every value is the accessor's own constant, so no second literal exists; the class test
// TestEverySettingHasADeclaredDefault holds every field of SetReq, Bars and Shape to a row.
var Defaults = []Default{
	// the work switches and their tiers
	{Name: PropReadCards, Home: HomeProp, Value: ReadCardsOnWord, Why: "reads are dealt as read cards (the owner, 2026-10-13: \"default is TRUE\")"},
	{Name: PropFleet, Home: HomeProp, Value: SwitchOn, Why: "the fleet takes work (the owner, 2026-10-06: \"default enabled\")"},
	{Name: PropFriends, Home: HomeProp, Value: SwitchOn, Why: "the friends take work (the owner, 2026-10-06: \"default enabled\")"},
	{Name: PropFleetTiers, Home: HomeProp, Value: TiersAll, Why: "the fleet may take every tier"},
	{Name: PropFriendsTiers, Home: HomeProp, Value: TiersAll, Why: "the friends may take every tier"},
	{Name: PropReworkPriority, Home: HomeProp, Value: PriorityFix, Why: "a next attempt is dealt at fix priority"},
	{Name: PropUnservedTierFallsTo, Home: HomeProp, Value: SwitchOff, Why: "an unserved card waits for its tier (OPEN: the owner's to state)"},
	{Name: PropReadTier, Home: HomeProp, Value: ReadTierDefault, Why: "each card's reads run at its own tier"},
	{Name: PropReadsNeeded, Home: HomeProp, Value: ReadTierDefault, Why: "each card's own rule: one read for a flash card, two above"},
	// the bounds
	{Name: PropDealtMax, Home: HomeProp, Value: DealtMaxDefault.String(), Why: "three times the take deadline"},
	{Name: PropReadyMax, Home: HomeProp, Value: ReadyMaxDefault.String(), Why: "a card ready over a minute pushes the seat"},
	{Name: PropGoLanes, Home: HomeProp, Value: strconv.Itoa(LaneWidthDefault), Why: "one Go lane a machine"},
	{Name: PropFriendIdle, Home: HomeProp, Value: FriendIdleDefault.String(), Why: "a friend idle this long is an alarm"},
	{Name: PropLandingStallBound, Home: HomeProp, Value: LandingStallBound.String(), Why: "the owner's stall watch: one minute"},
	{Name: PropFriendStallAfter, Home: HomeProp, Value: FriendStallAfterDefault.String(), Why: "the friend stall ladder begins"},
	{Name: PropFriendStallStep, Home: HomeProp, Value: FriendStallStepDefault.String(), Why: "each rung of the friend stall ladder"},
	{Name: PropDriftCommits, Home: HomeProp, Value: strconv.Itoa(DriftCommitsDefault), Why: "the base may be this many commits ahead of dev"},
	{Name: PropDriftHours, Home: HomeProp, Value: strconv.Itoa(DriftHoursDefault), Why: "the base's oldest commit not on dev may be this old"},
	{Name: PropLowBalanceUsd, Home: HomeProp, Value: strconv.FormatFloat(LowBalanceUsdDefault, 'f', -1, 64), Why: "a provider below five dollars is low"},
	// the backlog alarms: off until the coordinator sets a threshold
	{Name: PropAlarmReview, Home: HomeProp, Value: AlarmOff, Why: "no review backlog alarm until set"},
	{Name: PropAlarmMerging, Home: HomeProp, Value: AlarmOff, Why: "no merging backlog alarm until set"},
	{Name: PropAlarmFleet, Home: HomeProp, Value: AlarmOff, Why: "no fleet-width alarm until set"},
	{Name: PropAlarmReady, Home: HomeProp, Value: AlarmOff, Why: "no nothing-ready alarm until set"},
	// nova-config's sprint row (store.Bars): read-side defaults, never written by clear
	{Name: config.FieldGitIdentity, Home: HomeBar, Value: config.DefaultGitIdentity, ReadFill: true, Why: "an unapplied git_identity commits as the machine identity, never the host's"},
	{Name: config.FieldMaxAttempts, Home: HomeBar, Value: strconv.Itoa(AttemptsDefault), Why: "attempts before the card is a judgment (empty reads AttemptsDefault)"},
	{Name: config.FieldDecideBounce, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideReview, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideAttemptNoResult, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideAttemptNothingToDo, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideGrade, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideScoreBar, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideGateFlaky, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideGatePreexisting, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldDecideJudgment, Home: HomeBar, Value: "", Why: "no bar: nova-config declares none"},
	{Name: config.FieldAnswerRulesOff, Home: HomeBar, Value: "", Why: "every rule answers"},
	// the shape (Shape), carried by the clear; a-cleared-sprint-reads-with-no-seat-action owns it
	{Name: "readers", Home: HomeShape, Value: ShapeCarried, Why: "the readers' rows"},
	{Name: "reader_tiers", Home: HomeShape, Value: ShapeCarried, Why: "each reader's tiers"},
	{Name: "members", Home: HomeShape, Value: ShapeCarried, Why: "the fleet's members"},
	{Name: "status", Home: HomeShape, Value: ShapeCarried, Why: "each member up or down"},
	{Name: "held", Home: HomeShape, Value: ShapeCarried, Why: "the members' holds"},
	{Name: "held_by", Home: HomeShape, Value: ShapeCarried, Why: "who made a hold"},
	{Name: "width", Home: HomeShape, Value: ShapeCarried, Why: "each member's width"},
}

// NotSettings is the fields of SetReq and Shape that hold no sprint-wide setting and so
// have no Defaults row: a stream's own fields (stream set), the request's own words, and
// the work a clear leaves behind (Shape.Streams). The class test refuses any other field
// without a row.
var NotSettings = []string{
	// SetReq: the streams and a stream's own fields (stream set <s> ...)
	"SetReq.Streams", "SetReq.Attempts", "SetReq.Reason", "SetReq.Answers", "SetReq.LandProtected",
	"SetReq.Release", "SetReq.Display", "SetReq.Prose", "SetReq.SeatAccept", "SetReq.DefaultTier",
	"SetReq.Base", "SetReq.BaseChecked", "SetReq.Who",
	// Shape: the streams are work, left behind by the clear
	"Shape.Streams",
}

// DefaultOf is the setting's row, ok false when it has none.
func DefaultOf(name string) (Default, bool) {
	i := slices.IndexFunc(Defaults, func(d Default) bool { return d.Name == name })
	if i < 0 {
		return Default{}, false
	}
	return Defaults[i], true
}

// defaultValue is the setting's Value; "" when it has no row.
func defaultValue(name string) string {
	d, _ := DefaultOf(name)
	return d.Value
}

// SetDefaults is the clear's set-defaults step (tla/MachineReady.tla SetDefaults): every
// HomeProp row's Value written into the new epoch's work table, so no setting is ever
// absent. A property already holding its Value is left as it is. old is the cleared
// epoch's properties: a value there away from its default is not carried (advance, then
// set defaults), and resets names each as "<name> was <v>, now <default>".
func SetDefaults(s *Snapshot, old map[string]string) (p Plan, resets []string) {
	var wrote []string
	for _, d := range Defaults {
		if d.Home != HomeProp {
			continue
		}
		if was, ok := old[d.Name]; ok && was != d.Value {
			resets = append(resets, d.Name+" was "+was+", now "+d.Value)
		}
		var was string
		var had bool
		if s != nil && s.Work != nil {
			was, had = s.Work.Prop(d.Name)
		}
		if had && was == d.Value {
			continue
		}
		p.Props = append(p.Props, PropWrite{Table: Work, Name: d.Name, Value: d.Value, Was: was, WasAbsent: !had})
		wrote = append(wrote, d.Name+" "+d.Value)
	}
	if len(wrote) > 0 {
		p.Units = append(p.Units, Unit{Key: "defaults", Moved: "sprint defaults " + strings.Join(wrote, ", ")})
	}
	return p, resets
}

// SettingsLine is the one line `where` prints of the sprint's settings: each property
// row's effective value, "(default)" when it holds its default, "(set)" when the
// coordinator set it away from it, "(absent)" when the epoch holds none (an epoch cleared
// before the set-defaults step existed).
func SettingsLine(s *Snapshot) string {
	var parts []string
	for _, d := range Defaults {
		if d.Home != HomeProp {
			continue
		}
		var v string
		var ok bool
		if s != nil && s.Work != nil {
			v, ok = s.Work.Prop(d.Name)
		}
		switch {
		case !ok:
			parts = append(parts, d.Name+"="+d.Value+" (absent)")
		case v == d.Value:
			parts = append(parts, d.Name+"="+v+" (default)")
		default:
			parts = append(parts, d.Name+"="+v+" (set)")
		}
	}
	return "SETTINGS " + strings.Join(parts, " ")
}

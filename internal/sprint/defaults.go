package sprint

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// A setting's home: where its value lives (docs/SPEC-SPRINT.md section 13, Epochs and
// clear, "Working defaults").
const (
	// HomeProp is a work-table property, epoch keyed: a clear leaves the new epoch with
	// none, and the clear's set-defaults step writes the row's Value into it where it is
	// still absent (store.Clear, SetDefaults; tla/MachineReady.tla SetDefaults).
	HomeProp = "prop"
	// HomeCarry is a work-table property that is the coordinator's choice, carried across
	// a clear as shape: the set-defaults step writes the old epoch's value into the new
	// one, and the row's Value only when the old epoch held none ("" is no default: the
	// property stays absent, as sprint_base does until set --base).
	HomeCarry = "carry"
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

// The backlog alarms' working thresholds, for a threshold alarm never set (a clear never
// turns an alarm off: the owner's standing rule, 2026-10-11, "never turn off alarms or
// warnings"). They are the thresholds the seat's adoption of the alarms named on
// 2026-10-07 (--alarm-review 120 --alarm-merging 60 --alarm-fleet 50 --alarm-ready on);
// alarm_fleet 50 is also the percent TestBacklogAlarmsPushOncePerEpisodeFromConfigThresholds
// pins.
const (
	AlarmReviewWorking  = "120"
	AlarmMergingWorking = "60"
	AlarmFleetWorking   = "50"
	AlarmReadyWorking   = "on"
)

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
// Every value is the accessor's own constant, so no second literal exists; the class tests
// hold every flag of `set` (TestEverySetFlagHasADeclaredDefault), every field of SetReq and
// Shape (TestEverySettingHasADeclaredDefault) and of Bars (TestEveryBarHasADeclaredDefault)
// to a row.
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
	{Name: PropFriendFinish, Home: HomeProp, Value: FriendFinishDefault.String(), Why: "a friend finishing nothing this long is judged idle by the pass"},
	{Name: PropFriendStartMax, Home: HomeProp, Value: FriendStartMaxDefault.String(), Why: "a friend's card unstarted this long is levelled"},
	{Name: PropPeggedBound, Home: HomeProp, Value: PeggedBoundDefault.String(), Why: "a member pegged with no card this long is a judgment"},
	{Name: PropLandingStallBound, Home: HomeProp, Value: LandingStallBound.String(), Why: "the owner's stall watch: one minute"},
	{Name: PropFriendStallAfter, Home: HomeProp, Value: FriendStallAfterDefault.String(), Why: "the friend stall ladder begins"},
	{Name: PropFriendStallStep, Home: HomeProp, Value: FriendStallStepDefault.String(), Why: "each rung of the friend stall ladder"},
	{Name: PropDriftCommits, Home: HomeProp, Value: strconv.Itoa(DriftCommitsDefault), Why: "the base may be this many commits ahead of dev"},
	{Name: PropDriftHours, Home: HomeProp, Value: strconv.Itoa(DriftHoursDefault), Why: "the base's oldest commit not on dev may be this old"},
	{Name: PropLowBalanceUsd, Home: HomeProp, Value: strconv.FormatFloat(LowBalanceUsdDefault, 'f', -1, 64), Why: "a provider below five dollars is low"},
	{Name: PropIdentical, Home: HomeProp, Value: strconv.Itoa(IdenticalDefault), Why: "two identical findings in a row stop a card"},
	// the backlog alarms: never off after a clear (the owner, 2026-10-11: "never turn off
	// alarms or warnings"); the feed alarm is on, the threshold alarms are the coordinator's
	// and carried, at their working threshold when never set
	{Name: PropAlarmReady, Home: HomeProp, Value: AlarmReadyWorking, Why: "nothing ready while cards wait is always told"},
	{Name: PropAlarmReview, Home: HomeCarry, Value: AlarmReviewWorking, Why: "the coordinator's threshold, carried; 120 when never set"},
	{Name: PropAlarmMerging, Home: HomeCarry, Value: AlarmMergingWorking, Why: "the coordinator's threshold, carried; 60 when never set"},
	{Name: PropAlarmFleet, Home: HomeCarry, Value: AlarmFleetWorking, Why: "the coordinator's threshold, carried; 50 percent when never set"},
	// the sprint's base: the coordinator's choice, carried; no default (start refuses
	// without one, naming set --base)
	{Name: PropSprintBase, Home: HomeCarry, Value: "", Why: "the branch every stream lands on, the coordinator's (set --base)"},
	// the twins' brief directory: the machine's choice, carried; none falls to the environment
	{Name: PropTwinBriefs, Home: HomeCarry, Value: "", Why: "where a twin's brief is written (else the environment, else the home)"},
	// nova-config's sprint row (store.Bars): read-side defaults, never written by clear. Each
	// Value is the default nova-config declares for the field (config.KindSprint), and an
	// empty field of a row with a declared default is read as it (ReadFill), so an unapplied
	// sprint row works as an applied default one (the cold reads of #190, 2026-10-13)
	barRow(config.FieldGitIdentity, "an unapplied git_identity commits as the machine identity, never the host's"),
	// max_attempts is not filled: its consumer reads empty as AttemptsDefault, the same 4, and
	// says so (AttemptsCapSource: "the default"), where a fill would claim nova-config set it
	barRowReadAsDefault(config.FieldMaxAttempts, "attempts before the card is a judgment; empty reads AttemptsDefault"),
	barRow(config.FieldDecideBounce, "the decide read's bounce bar"),
	barRow(config.FieldDecideReview, "the decide read's review bar"),
	barRow(config.FieldDecideAttemptNoResult, "empty routes nothing on the attempt decision"),
	barRow(config.FieldDecideAttemptNothingToDo, "empty routes nothing on the attempt decision"),
	barRow(config.FieldDecideGrade, "empty keeps the grade a hint"),
	barRow(config.FieldDecideScoreBar, "empty records the scores and raises none"),
	barRow(config.FieldDecideGateFlaky, "empty reruns nothing"),
	barRow(config.FieldDecideGatePreexisting, "empty reclassifies nothing"),
	barRow(config.FieldDecideJudgment, "empty applies nothing"),
	barRow(config.FieldDecideBriefBar, "empty asks and reports only (read by add, not a Bars field)"),
	barRow(config.FieldAnswerRulesOff, "empty answers by every rule"),
	// the shape (Shape), carried by the clear; a-cleared-sprint-reads-with-no-seat-action owns it
	{Name: "readers", Home: HomeShape, Value: ShapeCarried, Why: "the readers' rows"},
	{Name: "reader_tiers", Home: HomeShape, Value: ShapeCarried, Why: "each reader's tiers"},
	{Name: "members", Home: HomeShape, Value: ShapeCarried, Why: "the fleet's members"},
	{Name: "status", Home: HomeShape, Value: ShapeCarried, Why: "each member up or down"},
	{Name: "held", Home: HomeShape, Value: ShapeCarried, Why: "the members' holds"},
	{Name: "held_by", Home: HomeShape, Value: ShapeCarried, Why: "who made a hold"},
	{Name: "width", Home: HomeShape, Value: ShapeCarried, Why: "each member's width"},
}

// NotSettings is the fields of SetReq and Shape, and the flags of `set`, that hold no
// sprint-wide setting and so have no Defaults row: a stream's own fields (stream set), the
// request's own words, a retired flag, and the work a clear leaves behind (Shape.Streams).
// The class tests refuse any other field or flag without a row.
var NotSettings = []string{
	// SetReq: the streams and a stream's own fields (stream set <s> ...)
	"SetReq.Streams", "SetReq.Attempts", "SetReq.Reason", "SetReq.Answers", "SetReq.LandProtected",
	"SetReq.Release", "SetReq.Display", "SetReq.Prose", "SetReq.SeatAccept", "SetReq.DefaultTier",
	"SetReq.Base", "SetReq.BaseChecked", "SetReq.Who",
	// Shape: the streams are work, left behind by the clear
	"Shape.Streams",
	// set: retired 2026-10-12 (nova-config's sprint row max_attempts is the cap)
	"set --attempts",
}

// NotSettingProps is the work, fleet and merge tables' properties that are the machine's
// state, never a setting: indexes, episode stamps, records and the retired attempts cap. A
// clear starts the new epoch with none of them (each is rebuilt as the sprint runs), and
// TestEveryPropIsASettingOrState refuses a Prop constant in neither list.
var NotSettingProps = []string{
	PropAcceptStreamIndex, PropAskIndex, PropAskStreamIndex, PropDealIndex, PropStreamIndex,
	PropAttempts,
	PropDevSyncAt, PropDevSyncBaseLacks, PropDevSyncDevLacks, PropDevSyncSha,
	PropFriendSync, PropIdleSaid, PropIdleSince, PropLandRefusal, PropLandingStallSaid, PropLandingStallSince,
	PropLastLanding, PropMergeWindowReason, PropMergeWindowUntil, PropPromotedAt, PropPromotedSha,
	PropCarriedMedians, PropCostReconcilePrefix, PropReadsWindow, PropStatusSeen,
}

// SetFlagProp is the property a flag of `set` writes where it is not the flag's own name
// with '-' for '_'.
var SetFlagProp = map[string]string{"base": PropSprintBase, "reads": PropReadsNeeded}

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

// clearedDefault is the value a setting absent from a cleared epoch's work table reads as
// (epoch above 0): its row's Value, ok false when it has none or no Value. A cleared epoch
// holds every row once its set-defaults step ran; before it (a start that landed inside the
// clear, tla/MachineReady.tla Read, ReadersSeeWorking) the readers read the default, never
// an absent off. The first epoch, never cleared, keeps each accessor's own absent case.
func (s *Snapshot) clearedDefault(name string) (string, bool) {
	if s == nil || s.Epoch == 0 {
		return "", false
	}
	d, ok := DefaultOf(name)
	if !ok || d.Value == "" || (d.Home != HomeProp && d.Home != HomeCarry) {
		return "", false
	}
	return d.Value, true
}

// SetDefaults is the clear's set-defaults step (tla/MachineReady.tla SetDefaults): every
// HomeProp row's Value, and every HomeCarry row's old-epoch value (else its Value), written
// into the new epoch's work table where the property is still ABSENT, so no setting is ever
// absent. A property present in the new epoch was written there after the advance (a set
// that landed between the advance and this step) and is never overwritten
// (SetSurvives). old is the cleared epoch's properties: a HomeProp value there away from its
// default is not carried, and resets names each as "<name> was <v>, now <default>"; a
// HomeCarry value is carried and named in no reset.
func SetDefaults(s *Snapshot, old map[string]string) (p Plan, resets []string) {
	var wrote []string
	for _, d := range Defaults {
		var want string
		switch d.Home {
		case HomeProp:
			want = d.Value
			if was, ok := old[d.Name]; ok && was != d.Value {
				resets = append(resets, d.Name+" was "+was+", now "+d.Value)
			}
		case HomeCarry:
			want = d.Value
			if was, ok := old[d.Name]; ok && was != "" {
				want = was
			}
			if want == "" {
				continue
			}
		default:
			continue
		}
		if s != nil && s.Work != nil {
			if _, had := s.Work.Prop(d.Name); had {
				continue
			}
		}
		p.Props = append(p.Props, PropWrite{Table: Work, Name: d.Name, Value: want, WasAbsent: true})
		wrote = append(wrote, d.Name+" "+want)
	}
	if len(wrote) > 0 {
		p.Units = append(p.Units, Unit{Key: "defaults", Moved: "sprint defaults " + strings.Join(wrote, ", ")})
	}
	return p, resets
}

// barRow is a nova-config sprint row field's row: its declared default (config.KindSprint),
// filled on read when the field is empty and a default is declared.
func barRow(field, why string) Default {
	v := sprintRowDefault(field)
	return Default{Name: field, Home: HomeBar, Value: v, ReadFill: v != "", Why: why}
}

// barRowReadAsDefault is a sprint row field's row whose consumer reads its empty value as
// the declared default by itself, so it is not filled on read.
func barRowReadAsDefault(field, why string) Default {
	d := barRow(field, why)
	d.ReadFill = false
	return d
}

// sprintRowDefault is the default nova-config declares for the sprint row's field, "" when
// it declares none.
func sprintRowDefault(field string) string {
	k, ok := config.Lookup(config.KindSprint)
	if !ok {
		return ""
	}
	f, ok := k.Field(field)
	if !ok {
		return ""
	}
	return f.Default
}

package sprint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// fieldsOf is a struct type's fields, in order.
func fieldsOf(t reflect.Type) []reflect.StructField {
	out := make([]reflect.StructField, 0, t.NumField())
	for i := range t.NumField() {
		out = append(out, t.Field(i))
	}
	return out
}

// undeclaredSettings is every field of the struct (named typ) that is neither listed in
// NotSettings nor maps, by nameOf, to a Defaults row of the home: each named.
func undeclaredSettings(typ string, fields []reflect.StructField, home string, nameOf func(reflect.StructField) string) []string {
	var out []string
	for _, f := range fields {
		if slices.Contains(NotSettings, typ+"."+f.Name) {
			continue
		}
		name := nameOf(f)
		if name == "" {
			out = append(out, typ+"."+f.Name+" (no setting name and not in NotSettings)")
			continue
		}
		d, ok := DefaultOf(name)
		if !ok || (d.Home != home && !(home == HomeProp && d.Home == HomeCarry)) {
			out = append(out, typ+"."+f.Name+" ("+name+": no "+home+" row in Defaults)")
		}
	}
	return out
}

func propName(f reflect.StructField) string { return f.Tag.Get("prop") }

func jsonName(f reflect.StructField) string {
	n, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return n
}

// TestEverySettingHasADeclaredDefault is the class test of the working defaults: every
// field of SetReq (the coordinator's settings) and of Shape (what a clear carries) maps to
// a Defaults row or is a listed non-setting, so a new setting cannot be added without its
// default, and the clear's set-defaults step (SetDefaults; tla/MachineReady.tla
// SetDefaults) writes it. Bars is held to the table in internal/sprint/store
// (TestEveryBarHasADeclaredDefault). The reversed witness: an extra field with no row,
// tagged or not, is named.
func TestEverySettingHasADeclaredDefault(t *testing.T) {
	t.Parallel()
	assert.Empty(t, undeclaredSettings("SetReq", fieldsOf(reflect.TypeOf(SetReq{})), HomeProp, propName), "a SetReq field with no declared default")
	assert.Empty(t, undeclaredSettings("Shape", fieldsOf(reflect.TypeOf(Shape{})), HomeShape, jsonName), "a Shape field with no declared default")

	// every name is declared once, and every prop row a setting the accessors read
	seen := map[string]bool{}
	for _, d := range Defaults {
		assert.False(t, seen[d.Name], "%s declared twice", d.Name)
		seen[d.Name] = true
		assert.Contains(t, []string{HomeProp, HomeCarry, HomeBar, HomeShape}, d.Home, "%s: home", d.Name)
		assert.NotEmpty(t, d.Why, "%s: why", d.Name)
		if d.Home == HomeProp {
			assert.NotEmpty(t, d.Value, "%s: a property's default is never absent", d.Name)
		}
	}

	// reversed witness: one more field with no row is red, tagged or untagged
	extra := append(fieldsOf(reflect.TypeOf(SetReq{})),
		reflect.StructField{Name: "NewKnob", Tag: `json:",omitempty" prop:"new_knob"`},
		reflect.StructField{Name: "Untagged", Tag: `json:",omitempty"`})
	got := undeclaredSettings("SetReq", extra, HomeProp, propName)
	require.Len(t, got, 2, "the witness names both undeclared fields: %v", got)
	assert.Contains(t, got[0], "SetReq.NewKnob")
	assert.Contains(t, got[1], "SetReq.Untagged")
	shapeExtra := append(fieldsOf(reflect.TypeOf(Shape{})), reflect.StructField{Name: "Pinned", Tag: `json:"pinned"`})
	assert.Len(t, undeclaredSettings("Shape", shapeExtra, HomeShape, jsonName), 1, "a Shape field with no row is red")
}

// The accessors read their absent case as the row's default: a work table with no
// properties works as one holding every default. The backlog alarms are the exception the
// first epoch keeps (never cleared: off until set, as documented); a cleared epoch reads
// their working thresholds even before its set-defaults step ran (clearedDefault).
func TestEveryAccessorReadsItsDefaultWhenAbsent(t *testing.T) {
	t.Parallel()
	bare := &Snapshot{Work: NewTable(Work)}
	cleared := &Snapshot{Work: NewTable(Work), Epoch: 3}
	full := &Snapshot{Work: NewTable(Work)}
	props := map[string]string{}
	for _, d := range Defaults {
		if (d.Home == HomeProp || d.Home == HomeCarry) && d.Value != "" {
			props[d.Name] = d.Value
		}
	}
	full.Work.SetProps(props)
	for _, s := range []*Snapshot{bare, cleared, full} {
		assert.True(t, s.ReadCardsOn())
		assert.False(t, s.FleetOff())
		assert.False(t, s.FriendsOff())
		assert.True(t, s.FleetTakes("heavy"))
		assert.True(t, s.FriendsTake("flash"))
		assert.Equal(t, "", s.UnservedTierFallsTo())
		assert.Equal(t, DealtMaxDefault, s.DealtMax())
		assert.Equal(t, ReadyMaxDefault, s.ReadyMax())
		assert.Equal(t, FriendIdleDefault, s.FriendIdleAfter())
		assert.Equal(t, FriendFinishDefault, s.FriendFinishAfter())
		assert.Equal(t, FriendStartMaxDefault, s.FriendStartMax())
		assert.Equal(t, PeggedBoundDefault, s.PeggedBound())
		assert.Equal(t, FriendStallAfterDefault, s.FriendStallAfter())
		assert.Equal(t, FriendStallStepDefault, s.FriendStallStep())
		assert.Equal(t, DriftCommitsDefault, s.DriftCommits())
		assert.Equal(t, DriftHoursDefault, s.DriftHours())
		assert.Equal(t, "", s.readTierSetting("s1"))
		_, set := ReadsSetting(s.Work.Props())
		assert.False(t, set, "reads default is each card's own rule")
		assert.Equal(t, "", SprintBase(s), "no base until set --base")
	}
	for _, s := range []*Snapshot{cleared, full} {
		for prop, want := range map[string]int{PropAlarmReview: 120, PropAlarmMerging: 60, PropAlarmFleet: 50, PropAlarmReady: 0} {
			n, on := s.alarmSetting(prop)
			assert.True(t, on, "%s is on in a cleared epoch, never an absent off", prop)
			assert.Equal(t, want, n, "%s threshold", prop)
		}
	}
	for _, a := range alarmProps {
		_, on := bare.alarmSetting(a.prop)
		assert.False(t, on, "%s: the first epoch, never cleared, keeps off until set", a.prop)
	}
	assert.True(t, strings.HasPrefix(SettingsLine(full.Work.Props()), "SETTINGS read_cards=on (default)"), SettingsLine(full.Work.Props()))
	assert.Contains(t, SettingsLine(bare.Work.Props()), "read_cards=on (absent)")
	assert.Contains(t, SettingsLine(bare.Work.Props()), "sprint_base=none (absent)")
}

// SetDefaults writes every property row that is ABSENT, never one the new epoch already
// holds (a set that landed between the advance and the step survives); a HomeProp value of
// the old epoch away from its default is a reset, never carried; a HomeCarry value is
// carried, and a carried row the old epoch never held takes its working value (an alarm's
// threshold) or stays absent (sprint_base). No reset ever says an alarm is now off.
func TestSetDefaultsWritesOnlyWhatIsAbsent(t *testing.T) {
	t.Parallel()
	nRows := 0
	for _, d := range Defaults {
		if d.Home == HomeProp || (d.Home == HomeCarry && d.Value != "") {
			nRows++
		}
	}
	// an empty new epoch, an old epoch with read cards off, an alarm set and a base
	s := &Snapshot{Work: NewTable(Work)}
	old := map[string]string{PropReadCards: SwitchOff, PropFleet: SwitchOn, PropAlarmReview: "3", PropAlarmReady: SwitchOff, PropSprintBase: "sprint/v9"}
	p, resets := SetDefaults(s, old)
	require.Len(t, p.Props, nRows+1, "every row written into an empty epoch, the base carried")
	assert.ElementsMatch(t, []string{"read_cards was off, now on", "alarm_ready was off, now on"}, resets, "only a HomeProp value away from its default is a reset")
	got := map[string]string{}
	for _, pw := range p.Props {
		assert.True(t, pw.WasAbsent, "%s guarded on its absence", pw.Name)
		got[pw.Name] = pw.Value
	}
	assert.Equal(t, ReadCardsOnWord, got[PropReadCards])
	assert.Equal(t, "3", got[PropAlarmReview], "the coordinator's threshold is carried")
	assert.Equal(t, AlarmMergingWorking, got[PropAlarmMerging], "a threshold never set takes its working value")
	assert.Equal(t, AlarmReadyWorking, got[PropAlarmReady])
	assert.Equal(t, "sprint/v9", got[PropSprintBase], "the base is carried")
	for _, r := range resets {
		assert.False(t, strings.HasPrefix(r, "alarm_") && strings.HasSuffix(r, "now off"), "a clear never turns an alarm off: %s", r)
	}

	// no base in the old epoch: the base stays absent (start refuses, naming set --base)
	p, _ = SetDefaults(&Snapshot{Work: NewTable(Work)}, nil)
	for _, pw := range p.Props {
		assert.NotEqual(t, PropSprintBase, pw.Name, "no default base is invented")
	}

	// a set that landed in the new epoch before the step: never overwritten
	s.Work.SetProps(map[string]string{PropReadCards: SwitchOff, PropAlarmReview: "7"})
	p, _ = SetDefaults(s, old)
	assert.Len(t, p.Props, nRows+1-2, "the two properties the new epoch holds are not written")
	for _, pw := range p.Props {
		assert.NotContains(t, []string{PropReadCards, PropAlarmReview}, pw.Name, "%s written over the new epoch's own set", pw.Name)
	}
}

// The doctor can no longer FAIL an empty git_identity (it reads the machine identity): an
// unapplied sprint row is a WARN naming the default in use.
func TestDoctorWarnsOnAnUnappliedGitIdentity(t *testing.T) {
	t.Parallel()
	out := DoctorGitIdentity(DoctorInput{Snap: &Snapshot{GitIdentity: config.DefaultGitIdentity, GitIdentityUnapplied: true}})
	require.Len(t, out, 1)
	assert.Equal(t, DoctorWarn, out[0].Status)
	assert.Contains(t, out[0].Evidence, "git_identity default, sprint row unapplied")
	out = DoctorGitIdentity(DoctorInput{Snap: &Snapshot{GitIdentity: "Ops <ops@example.invalid>"}})
	require.Len(t, out, 1)
	assert.NotEqual(t, DoctorWarn, out[0].Status)
	assert.NotEqual(t, DoctorFail, out[0].Status)
}

// propConsts is every constant named Prop* with a string value declared in the package's
// own source (the test files excluded), by name.
func propConsts(t *testing.T) map[string]string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	require.NoError(t, err)
	out := map[string]string{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.CONST {
					continue
				}
				for _, sp := range g.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, n := range vs.Names {
						if !strings.HasPrefix(n.Name, "Prop") || i >= len(vs.Values) {
							continue
						}
						if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							out[n.Name], _ = strconv.Unquote(lit.Value)
						}
					}
				}
			}
		}
	}
	return out
}

// unclassifiedProps is every property neither a Defaults row nor a listed state property.
func unclassifiedProps(props map[string]string) []string {
	var out []string
	for name, v := range props {
		if _, ok := DefaultOf(v); ok || slices.Contains(NotSettingProps, v) {
			continue
		}
		out = append(out, name+" ("+v+")")
	}
	slices.Sort(out)
	return out
}

// TestEveryPropIsASettingOrState walks every Prop* constant of the package: each is a
// setting with a Defaults row (written or carried by the clear's set-defaults step) or a
// listed state property (NotSettingProps). ready_max, low_balance_usd and identical have no
// set flag and are held here. Reversed witness: a new Prop constant in neither is red.
func TestEveryPropIsASettingOrState(t *testing.T) {
	t.Parallel()
	props := propConsts(t)
	require.Contains(t, props, "PropReadyMax", "the package's constants were read")
	assert.Empty(t, unclassifiedProps(props), "a property that is neither a setting nor state")
	props["PropNewKnob"] = "new_knob"
	assert.Equal(t, []string{"PropNewKnob (new_knob)"}, unclassifiedProps(props), "the witness is red")
}

// The accessors read_cards does not cover: each absent reads its row's default.
func TestTheRemainingAccessorsReadTheirDefaultWhenAbsent(t *testing.T) {
	t.Parallel()
	bare := &Snapshot{Work: NewTable(Work)}
	assert.Equal(t, LaneWidthDefault, LaneWidth("", false))
	assert.Equal(t, defaultValue(PropGoLanes), strconv.Itoa(LaneWidth("", false)))
	assert.Equal(t, LandingStallBound, bare.landingStallBound())
	assert.Equal(t, defaultValue(PropLandingStallBound), bare.landingStallBound().String())
	usd, _ := lowBalanceUsdSetting(map[string]string{})
	assert.Equal(t, defaultValue(PropLowBalanceUsd), strconv.FormatFloat(usd, 'f', -1, 64))
	assert.Equal(t, IdenticalDefault, bare.IdenticalBound("s1"))
	set := map[string]string{}
	reworkPriority(bare, &Card{ID: "c1"}, set)
	assert.Equal(t, defaultValue(PropReworkPriority), set[FieldPriority], "rework_priority absent is fix")
}

package sprint

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		if !ok || d.Home != home {
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
		assert.Contains(t, []string{HomeProp, HomeBar, HomeShape}, d.Home, "%s: home", d.Name)
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

// The accessors read their absent case from Defaults: a work table with no properties
// works exactly as one holding every default.
func TestEveryAccessorReadsItsDefaultWhenAbsent(t *testing.T) {
	t.Parallel()
	bare := &Snapshot{Work: NewTable(Work)}
	full := &Snapshot{Work: NewTable(Work)}
	props := map[string]string{}
	for _, d := range Defaults {
		if d.Home == HomeProp {
			props[d.Name] = d.Value
		}
	}
	full.Work.SetProps(props)
	for _, s := range []*Snapshot{bare, full} {
		assert.True(t, s.ReadCardsOn())
		assert.False(t, s.FleetOff())
		assert.False(t, s.FriendsOff())
		assert.True(t, s.FleetTakes("heavy"))
		assert.True(t, s.FriendsTake("flash"))
		assert.Equal(t, "", s.UnservedTierFallsTo())
		assert.Equal(t, DealtMaxDefault, s.DealtMax())
		assert.Equal(t, ReadyMaxDefault, s.ReadyMax())
		assert.Equal(t, FriendIdleDefault, s.FriendIdleAfter())
		assert.Equal(t, FriendStallAfterDefault, s.FriendStallAfter())
		assert.Equal(t, FriendStallStepDefault, s.FriendStallStep())
		assert.Equal(t, DriftCommitsDefault, s.DriftCommits())
		assert.Equal(t, DriftHoursDefault, s.DriftHours())
		assert.Equal(t, "", s.readTierSetting("s1"))
		_, set := ReadsSetting(s.Work.Props())
		assert.False(t, set, "reads default is each card's own rule")
		for _, a := range alarmProps {
			_, on := s.alarmSetting(a.prop)
			assert.False(t, on, "%s is off by default", a.prop)
		}
	}
	assert.Equal(t, "SETTINGS read_cards=on (default)", SettingsLine(full)[:len("SETTINGS read_cards=on (default)")])
	assert.Contains(t, SettingsLine(bare), "read_cards=on (absent)")
}

// SetDefaults writes every property row absent or away from its default, names each old
// value away from its default as a reset, and writes nothing already at its default.
func TestSetDefaultsWritesEveryPropertyRow(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Work: NewTable(Work)}
	p, resets := SetDefaults(s, map[string]string{PropReadCards: SwitchOff, PropFleet: SwitchOn})
	n := 0
	for _, d := range Defaults {
		if d.Home == HomeProp {
			n++
		}
	}
	require.Len(t, p.Props, n, "every property row written into an empty epoch")
	assert.Equal(t, []string{"read_cards was off, now on"}, resets, "only an old value away from its default is a reset")
	for _, pw := range p.Props {
		assert.True(t, pw.WasAbsent, "%s guarded on its absence", pw.Name)
	}
	s.Work.SetProps(map[string]string{PropReadCards: ReadCardsOnWord})
	p, _ = SetDefaults(s, nil)
	assert.Len(t, p.Props, n-1, "a property at its default is not written again")
}

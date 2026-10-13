package store

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// awayFromDefault is a value of every property the coordinator sets, each away from its
// working default (sprint.Defaults).
var awayFromDefault = sprint.SetReq{Who: "tester",
	ReadCards: sprint.SwitchOff, Fleet: sprint.SwitchOff, Friends: sprint.SwitchOff,
	FleetTiers: "flash", FriendsTiers: "pro", ReworkPriority: sprint.ReworkKeep,
	UnservedTierFallsTo: "pro", ReadTier: "heavy", Reads: "3", DealtMax: "7h", GoLanes: "3",
	FriendIdle: "9m", LandingStallBound: "9m", FriendStallAfter: "9m", FriendStallStep: "9m",
	DriftCommits: "99", DriftHours: "99", AlarmReview: "3", AlarmMerging: "3", AlarmFleet: "50", AlarmReady: "on"}

// TestAClearSetsEveryWorkingDefaultInTheNewEpoch: every setting set away from its
// default, then a clear: the new epoch holds every property row at its default (no
// setting absent; the old epoch's values not carried), each reads its default through
// the tick's accessor (read cards on), the clear names each reset, and an unapplied
// git_identity reads the machine identity (tla/MachineReady.tla SetDefaults,
// ClearSetsDefaults). The owner, 2026-10-13: "you advance epoch, and then set defaults in
// the new epoch that need to be set. eg. reading true, git_identity etc."
func TestAClearSetsEveryWorkingDefaultInTheNewEpoch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(awayFromDefault))
	// the two properties set writes no flag for: by the table's own write
	s := h.snap()
	pinned, err := h.st.pin(h.ctx)
	require.NoError(t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: pinned.Names.Table(sprint.Work), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: "away-ready-max", Members: []ntable.BatchMemberEntry{},
		Props: map[string]string{sprint.PropReadyMax: "9m", sprint.PropLowBalanceUsd: "50"}})
	require.NoError(t, err)
	s = h.snap()
	for _, d := range sprint.Defaults {
		if d.Home != sprint.HomeProp {
			continue
		}
		v, ok := s.Work.Prop(d.Name)
		require.True(t, ok, "before the clear: %s set", d.Name)
		require.NotEqual(t, d.Value, v, "before the clear: %s away from its default", d.Name)
	}
	require.False(t, s.ReadCardsOn(), "before the clear: read cards off")

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	require.Equal(t, res.From+1, res.To)

	s = h.snap()
	for _, d := range sprint.Defaults {
		if d.Home != sprint.HomeProp {
			continue
		}
		v, ok := s.Work.Prop(d.Name)
		assert.True(t, ok, "after the clear: %s is absent in epoch %d", d.Name, res.To)
		assert.Equal(t, d.Value, v, "after the clear: %s holds its default", d.Name)
		assert.Contains(t, res.Defaults, fmt.Sprintf("%s was %s, now %s", d.Name, awayOf(awayFromDefault, d.Name, map[string]string{sprint.PropReadyMax: "9m", sprint.PropLowBalanceUsd: "50"}), d.Value), "the clear names the reset of %s", d.Name)
	}
	// the tick's accessors read the defaults
	assert.True(t, s.ReadCardsOn(), "read cards on after a clear")
	assert.False(t, s.FleetOff())
	assert.False(t, s.FriendsOff())
	assert.True(t, s.FleetTakes("heavy"))
	assert.True(t, s.FriendsTake("heavy"))
	assert.Equal(t, "", s.UnservedTierFallsTo())
	assert.Equal(t, sprint.DealtMaxDefault, s.DealtMax())
	assert.Equal(t, sprint.ReadyMaxDefault, s.ReadyMax())
	assert.Equal(t, sprint.FriendIdleDefault, s.FriendIdleAfter())
	assert.Equal(t, sprint.FriendStallAfterDefault, s.FriendStallAfter())
	assert.Equal(t, sprint.FriendStallStepDefault, s.FriendStallStep())
	assert.Equal(t, sprint.DriftCommitsDefault, s.DriftCommits())
	assert.Equal(t, sprint.DriftHoursDefault, s.DriftHours())
	_, set := sprint.ReadsSetting(s.Work.Props())
	assert.False(t, set, "reads: each card's own rule")
	w, err := h.st.LaneWidth(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, sprint.LaneWidthDefault, w)

	// git_identity: nova-config's, never written by the clear; an unapplied one (the
	// field absent on the store) reads the machine identity, so a landing never refuses
	// for it and never commits as the host
	assert.Equal(t, config.DefaultGitIdentity, Bars{}.WithDefaults().GitIdentity, "sprint:git_identity absent reads the machine identity")
	assert.Equal(t, "Ops <ops@example.invalid>", Bars{GitIdentity: "Ops <ops@example.invalid>"}.WithDefaults().GitIdentity, "an applied identity is kept")
	id, err := h.st.GitIdentity(h.ctx)
	require.NoError(t, err)
	_, ok := config.ParseGitIdentity(id)
	assert.True(t, ok, "the landing's identity reads after a clear: %q", id)

	// a second clear of an epoch at its defaults resets nothing and writes them again
	res2, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, res2.Defaults, "nothing away from its default: no reset named")
	assert.True(t, h.snap().ReadCardsOn())
}

// awayOf is the value awayFromDefault (or extra) gives the property.
func awayOf(r sprint.SetReq, name string, extra map[string]string) string {
	if v, ok := extra[name]; ok {
		return v
	}
	rv := reflect.ValueOf(r)
	for i := range rv.NumField() {
		if rv.Type().Field(i).Tag.Get("prop") == name {
			return rv.Field(i).String()
		}
	}
	return ""
}

// barNames is each field of Bars by the sprint row field fields() reads it from; a field
// fields() does not read is "".
func barNames(b *Bars, fields map[string]*string) map[string]string {
	out := map[string]string{}
	v := reflect.ValueOf(b).Elem()
	for i := range v.NumField() {
		p := v.Field(i).Addr().Interface().(*string)
		out[v.Type().Field(i).Name] = ""
		for name, q := range fields {
			if p == q {
				out[v.Type().Field(i).Name] = name
			}
		}
	}
	return out
}

// undeclaredBars is every Bars field with no sprint row field, or whose field has no bar
// row in sprint.Defaults.
func undeclaredBars(names map[string]string) []string {
	var out []string
	for f, name := range names {
		if d, ok := sprint.DefaultOf(name); name == "" || !ok || d.Home != sprint.HomeBar {
			out = append(out, f+" ("+name+")")
		}
	}
	return out
}

// TestEveryBarHasADeclaredDefault is the Bars half of the class test
// (sprint.TestEverySettingHasADeclaredDefault): every field of Bars is read from a sprint
// row field with a HomeBar row in sprint.Defaults. Reversed witness: a field naming a row
// that does not exist is red.
func TestEveryBarHasADeclaredDefault(t *testing.T) {
	t.Parallel()
	var b Bars
	names := barNames(&b, b.fields())
	assert.Empty(t, undeclaredBars(names), "a Bars field with no declared default")
	names["Witness"] = "decide_not_a_row"
	assert.Equal(t, []string{"Witness (decide_not_a_row)"}, undeclaredBars(names), "the witness is red")
	// the read-side default fills only the rows that say so
	full := Bars{}.WithDefaults()
	for name, p := range full.fields() {
		d, _ := sprint.DefaultOf(name)
		if d.ReadFill {
			assert.Equal(t, d.Value, *p, "%s filled", name)
		} else {
			assert.Equal(t, "", *p, "%s left empty: its consumer reads empty as its default", name)
		}
	}
}

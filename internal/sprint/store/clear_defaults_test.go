package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// awayFromDefault is a value of every property the coordinator sets with a SetReq field,
// each away from its working default (sprint.Defaults); awayProps the rest, written by the
// table's own write (the With* wrappers' and the unflagged settings).
var awayFromDefault = sprint.SetReq{Who: "tester",
	ReadCards: sprint.SwitchOff, Fleet: sprint.SwitchOff, Friends: sprint.SwitchOff,
	FleetTiers: "flash", FriendsTiers: "pro", ReworkPriority: sprint.ReworkKeep,
	UnservedTierFallsTo: "pro", ReadTier: "heavy", Reads: "3", DealtMax: "7h", GoLanes: "3",
	FriendIdle: "9m", LandingStallBound: "9m", FriendStallAfter: "9m", FriendStallStep: "9m",
	DriftCommits: "99", DriftHours: "99", AlarmReview: "3", AlarmMerging: "3", AlarmFleet: "70", AlarmReady: sprint.SwitchOff}

var awayProps = map[string]string{sprint.PropReadyMax: "9m", sprint.PropLowBalanceUsd: "50", sprint.PropFriendFinish: "9m",
	sprint.PropPeggedBound: "9m", sprint.PropFriendStartMax: "9m", sprint.PropIdentical: "5",
	sprint.PropSprintBase: "sprint/v9", sprint.PropTwinBriefs: "/srv/twins"}

// writeProps writes props on the work table at the harness's epoch, as a set would.
func writeProps(h *harness, st *Store, props map[string]string, op string) {
	h.t.Helper()
	pinned, err := st.pin(h.ctx)
	require.NoError(h.t, err)
	s, err := pinned.Load(h.ctx, []string{sprint.Work}, nil)
	require.NoError(h.t, err)
	_, err = pinned.B.Apply(h.ctx, ntable.BatchManifest{Schema: 1, Table: pinned.Names.Table(sprint.Work), Epoch: fmt.Sprint(pinned.epoch),
		ExpectedTableRevision: fmt.Sprint(s.Work.Revision), OperationID: op, Members: []ntable.BatchMemberEntry{}, Props: props})
	require.NoError(h.t, err)
}

// TestAClearSetsEveryWorkingDefaultInTheNewEpoch: every setting set away from its
// default, then a clear: the new epoch holds every HomeProp row at its default (none
// absent; the old epoch's values not carried) and every HomeCarry row at the old epoch's
// value (the coordinator's alarm thresholds and base), each reads through the tick's
// accessor (read cards on, the feed alarm on), the clear names each reset and never an
// alarm turned off, and an unapplied git_identity reads the machine identity
// (tla/ClearDefaults.tla SetDefaults, DefaultsAfterClear). The owner, 2026-10-13: "you
// advance epoch, and then set defaults in the new epoch that need to be set. eg. reading
// true, git_identity etc."
func TestAClearSetsEveryWorkingDefaultInTheNewEpoch(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(awayFromDefault))
	writeProps(h, h.st, awayProps, "away-props")
	s := h.snap()
	old := s.Work.Props()
	for _, d := range sprint.Defaults {
		if d.Home != sprint.HomeProp && d.Home != sprint.HomeCarry {
			continue
		}
		v, ok := old[d.Name]
		require.True(t, ok, "before the clear: %s set", d.Name)
		require.NotEqual(t, d.Value, v, "before the clear: %s away from its default", d.Name)
	}
	require.False(t, s.ReadCardsOn(), "before the clear: read cards off")

	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	require.Equal(t, res.From+1, res.To)

	s = h.snap()
	for _, d := range sprint.Defaults {
		v, ok := s.Work.Prop(d.Name)
		switch d.Home {
		case sprint.HomeProp:
			assert.True(t, ok, "after the clear: %s is absent in epoch %d", d.Name, res.To)
			assert.Equal(t, d.Value, v, "after the clear: %s holds its default", d.Name)
			assert.Contains(t, res.Defaults, fmt.Sprintf("%s was %s, now %s", d.Name, old[d.Name], d.Value), "the clear names the reset of %s", d.Name)
		case sprint.HomeCarry:
			assert.Equal(t, old[d.Name], v, "after the clear: %s is carried", d.Name)
		}
	}
	for _, r := range res.Defaults {
		assert.False(t, strings.HasPrefix(r, "alarm_") && strings.HasSuffix(r, "now off"), "a clear never turns an alarm off: %s", r)
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
	assert.Equal(t, sprint.FriendFinishDefault, s.FriendFinishAfter())
	assert.Equal(t, sprint.FriendStartMaxDefault, s.FriendStartMax())
	assert.Equal(t, sprint.PeggedBoundDefault, s.PeggedBound())
	assert.Equal(t, sprint.FriendStallAfterDefault, s.FriendStallAfter())
	assert.Equal(t, sprint.FriendStallStepDefault, s.FriendStallStep())
	assert.Equal(t, sprint.DriftCommitsDefault, s.DriftCommits())
	assert.Equal(t, sprint.DriftHoursDefault, s.DriftHours())
	assert.Equal(t, sprint.IdenticalDefault, s.IdenticalBound("s1"))
	assert.Equal(t, "sprint/v9", sprint.SprintBase(s), "the base is carried")
	_, set := sprint.ReadsSetting(s.Work.Props())
	assert.False(t, set, "reads: each card's own rule")
	w, err := h.st.LaneWidth(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, sprint.LaneWidthDefault, w)

	// git_identity: nova-config's, never written by the clear; an unapplied row (the field
	// empty on the store) reads the machine identity on Mem as on Redis, and says so
	h.m.SetGitIdentity("")
	id, err := h.st.GitIdentity(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, config.DefaultGitIdentity, id, "an unapplied git_identity reads the machine identity")
	rs := h.snap()
	require.NoError(t, h.st.RoutesInto(h.ctx, rs))
	assert.Equal(t, config.DefaultGitIdentity, rs.GitIdentity)
	assert.True(t, rs.GitIdentityUnapplied, "the snapshot says the row is unapplied (the doctor warns)")

	// a second clear of an epoch at its defaults resets nothing and writes them again
	res2, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.Empty(t, res2.Defaults, "nothing away from its default: no reset named")
	assert.True(t, h.snap().ReadCardsOn())
}

// A threshold alarm the coordinator never set takes its working threshold after a clear,
// never off; the feed alarm is on; no base is invented.
func TestAClearNeverLeavesAnAlarmOff(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	s := h.snap()
	for prop, want := range map[string]string{sprint.PropAlarmReview: sprint.AlarmReviewWorking, sprint.PropAlarmMerging: sprint.AlarmMergingWorking,
		sprint.PropAlarmFleet: sprint.AlarmFleetWorking, sprint.PropAlarmReady: sprint.AlarmReadyWorking} {
		v, ok := s.Work.Prop(prop)
		assert.True(t, ok, "%s written", prop)
		assert.Equal(t, want, v, "%s at its working threshold", prop)
	}
	_, ok := s.Work.Prop(sprint.PropSprintBase)
	assert.False(t, ok, "no base until set --base: start refuses, naming it")
	assert.Empty(t, res.Defaults, "nothing was away from its default")
}

// A set that lands between the clear's epoch advance and its set-defaults step survives:
// the step writes only what the new epoch does not hold (tla/ClearDefaults.tla SetSurvives;
// the cold read of #190, 2026-10-13).
func TestASetBetweenTheAdvanceAndTheDefaultsSurvives(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(sprint.SetReq{Who: "tester", AlarmReview: "9"}))
	ran := false
	h.st.beforeDefaults = func(next *Store) error {
		ran = true
		res, err := next.Run(h.ctx, SetStep(sprint.SetReq{Who: "tester", ReadCards: sprint.SwitchOff, AlarmReview: "7"}))
		require.NoError(t, err)
		require.Empty(t, res.Refused, "the set in the new epoch: %v", res.Refused)
		return nil
	}
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	require.True(t, ran, "the set ran between the advance and the step")
	s := h.snap()
	v, _ := s.Work.Prop(sprint.PropReadCards)
	assert.Equal(t, sprint.SwitchOff, v, "the new epoch's own set is never overwritten")
	assert.False(t, s.ReadCardsOn())
	v, _ = s.Work.Prop(sprint.PropAlarmReview)
	assert.Equal(t, "7", v, "the new epoch's threshold, not the old epoch's carried one")
	v, _ = s.Work.Prop(sprint.PropFleet)
	assert.Equal(t, sprint.SwitchOn, v, "the rest are written")
	assert.Empty(t, res.Defaults, "nothing the old epoch held was away from its default")
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

// TestEveryBarHasADeclaredDefault is the Bars half of the class test: every field of Bars
// is read from a sprint row field with a HomeBar row in sprint.Defaults, every field of
// nova-config's sprint row but the coordinator has a row, and each row's Value is the
// default nova-config declares, filled on read when the field is empty (git_identity,
// decide_bounce, decide_review; max_attempts reads its own default). Reversed witness: a field naming no row is
// red.
func TestEveryBarHasADeclaredDefault(t *testing.T) {
	t.Parallel()
	var b Bars
	names := barNames(&b, b.fields())
	assert.Empty(t, undeclaredBars(names), "a Bars field with no declared default")
	names["Witness"] = "decide_not_a_row"
	assert.Equal(t, []string{"Witness (decide_not_a_row)"}, undeclaredBars(names), "the witness is red")

	k, ok := config.Lookup(config.KindSprint)
	require.True(t, ok)
	var filled []string
	for _, name := range k.FieldNames() {
		if name == "coordinator" {
			continue
		}
		d, ok := sprint.DefaultOf(name)
		require.True(t, ok, "nova-config's sprint row field %s has no row", name)
		f, _ := k.Field(name)
		assert.Equal(t, f.Default, d.Value, "%s: the row is nova-config's declared default", name)
		assert.Equal(t, f.Default != "" && name != config.FieldMaxAttempts, d.ReadFill, "%s: filled on read exactly when a default is declared (max_attempts reads its own)", name)
		if d.ReadFill {
			filled = append(filled, name)
		}
	}
	assert.ElementsMatch(t, []string{config.FieldGitIdentity, config.FieldDecideBounce, config.FieldDecideReview}, filled)

	// the read-side default fills only the rows that say so
	full := Bars{}.WithDefaults()
	for name, p := range full.fields() {
		d, _ := sprint.DefaultOf(name)
		if d.ReadFill {
			assert.Equal(t, d.Value, *p, "%s filled", name)
		} else {
			assert.Equal(t, "", *p, "%s left empty: nova-config declares no default", name)
		}
	}
}

// A clear whose set-defaults step fails after the advance (a crash; the step's failure
// says run clear again) leaves the new epoch flagged owed. The clear run again finishes
// that step, carrying the coordinator's settings from the epoch the advance left, and
// advances nothing: the epoch is the one the crashed clear advanced to, the carried
// thresholds and base are the coordinator's, and the flag is cleared (the cold read of
// #190 at c4a5a3f; tla/ClearDefaults.tla CarrySurvives).
func TestAClearRunAgainAfterACrashFinishesTheOwedStepAndCarries(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	h.must(SetStep(sprint.SetReq{Who: "tester", AlarmReview: "3", AlarmFleet: "70", ReadCards: sprint.SwitchOff}))
	writeProps(h, h.st, map[string]string{sprint.PropSprintBase: "sprint/v9", sprint.PropTwinBriefs: "/srv/twins"}, "carry-props")
	// a first clear completes: epoch 1 carries the coordinator's settings and owes nothing
	res, err := h.st.Clear(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), res.To)
	es, err := h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	require.False(t, es.DefaultsOwed, "a completed clear clears its flag")

	// the second clear crashes between its advance and its step
	h.st.beforeDefaults = func(*Store) error { return errors.New("crashed before the set-defaults step") }
	_, err = h.st.Clear(h.ctx)
	require.Error(t, err)
	h.st.beforeDefaults = nil
	es, err = h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), es.N, "the crashed clear advanced to epoch 2")
	require.True(t, es.DefaultsOwed, "epoch 2 is flagged owed its step")

	// run clear again: it finishes epoch 2's step and advances nothing
	res, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.True(t, res.Owed, "the clear finished the owed step")
	assert.Equal(t, uint64(1), res.From, "carried from epoch 1, the epoch the advance left")
	assert.Equal(t, uint64(2), res.To, "no second advance")
	es, err = h.st.EpochNow(h.ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), es.N, "still epoch 2")
	assert.False(t, es.DefaultsOwed, "the flag is cleared")
	s := h.snap()
	for prop, want := range map[string]string{sprint.PropAlarmReview: "3", sprint.PropAlarmFleet: "70", sprint.PropSprintBase: "sprint/v9",
		sprint.PropTwinBriefs: "/srv/twins", sprint.PropAlarmMerging: sprint.AlarmMergingWorking, sprint.PropReadCards: sprint.ReadCardsOnWord} {
		v, _ := s.Work.Prop(prop)
		assert.Equal(t, want, v, "%s after the owed step", prop)
	}
	assert.True(t, s.ReadCardsOn())

	// and the next clear advances again as usual
	res, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	assert.False(t, res.Owed)
	assert.Equal(t, uint64(3), res.To)
	v, _ := h.snap().Work.Prop(sprint.PropAlarmReview)
	assert.Equal(t, "3", v, "still carried")
}

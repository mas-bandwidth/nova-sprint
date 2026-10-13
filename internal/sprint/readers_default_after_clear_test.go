package sprint

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cleared sprint reads with no seat action (epoch 20, v1.2.16): every
// working default holds fresh and after a clear, and every up member is a
// reader at every served tier. The table is every `set` flag, read from the
// prop tags Set itself reads (settings.go SetReq), so a setting added
// without a Defaults row fails here; plus the reader rows, their tiers and
// their up state. tla/ReaderTiers.tla Clear, EveryUpMemberIsAReader,
// ReadCardsStaysOn, EmptyCellReadsEveryServedTier.

// defaultsWorld is a sprint with two members up, one down, routes of every
// tier, and no reader rows for the members: the shape a members-only init
// leaves before its member readers are made, and the shape a clear must
// repair on its own.
func defaultsWorld(t *testing.T) *Snapshot {
	t.Helper()
	s := &Snapshot{Now: t0, Work: NewTable(Work), Readers: NewTable(Readers), Merge: NewTable(Merge), Fleet: NewTable(Fleet),
		Coordinator: "coordinator", Actor: "coordinator"}
	s.Fleet.SetRows([]string{"m1", "m2", "m3"})
	for _, m := range []string{"m1", "m2", "m3"} {
		status := Up
		if m == "m3" {
			status = Down
		}
		s.Fleet.Put(&Card{ID: CtlID(m), Row: m, Col: Ctl, Fields: map[string]string{"kind": "member", "status": status}})
	}
	for _, tier := range []string{"flash", "pro", "heavy", "frontier"} {
		s.Routes = append(s.Routes, Route{Name: tier + "-a", Tier: tier, Provider: "p", Model: tier + "-a", Tokens: 1000, Enabled: true, Deadline: 600})
	}
	return s
}

// defaultsFailures is every way s breaks the card: a setting away from its
// working default (read through the accessor the tick uses), or an up
// member with no reader row, a row that does not serve a served tier, or a
// row not up while its member is up.
func defaultsFailures(s *Snapshot) []string {
	var out []string
	// the settings, each through its accessor
	if !s.ReadCardsOn() {
		out = append(out, "read_cards reads off")
	}
	if s.FleetOff() {
		out = append(out, "fleet reads off")
	}
	if s.FriendsOff() {
		out = append(out, "friends reads off")
	}
	for _, tier := range []string{"flash", "pro", "heavy", "frontier"} {
		if !s.FleetTakes(tier) {
			out = append(out, "fleet_tiers leaves out "+tier)
		}
		if !s.FriendsTake(tier) {
			out = append(out, "friends_tiers leaves out "+tier)
		}
	}
	if got := s.UnservedTierFallsTo(); got != "" {
		out = append(out, "unserved_tier_falls_to reads "+got)
	}
	if got := s.DealtMax(); got != DealtMaxDefault {
		out = append(out, "dealt_max reads "+got.String())
	}
	if got := s.ReadyMax(); got != ReadyMaxDefault {
		out = append(out, "ready_max reads "+got.String())
	}
	if got := s.FriendIdleAfter(); got != FriendIdleDefault {
		out = append(out, "friend_idle reads "+got.String())
	}
	if got := s.FriendFinishAfter(); got != FriendFinishDefault {
		out = append(out, "friend_finish reads "+got.String())
	}
	if got := s.FriendStartMax(); got != FriendStartMaxDefault {
		out = append(out, "friend_start_max reads "+got.String())
	}
	if got := s.PeggedBound(); got != PeggedBoundDefault {
		out = append(out, "pegged_bound reads "+got.String())
	}
	if got := s.landingStallBound(); got != LandingStallBound {
		out = append(out, "landing_stall_bound reads "+got.String())
	}
	if got := s.FriendStallAfter(); got != FriendStallAfterDefault {
		out = append(out, "friend_stall_after reads "+got.String())
	}
	if got := s.FriendStallStep(); got != FriendStallStepDefault {
		out = append(out, "friend_stall_step reads "+got.String())
	}
	if got := s.DriftCommits(); got != DriftCommitsDefault {
		out = append(out, "drift_commits reads otherwise")
	}
	if got := s.DriftHours(); got != DriftHoursDefault {
		out = append(out, "drift_hours reads otherwise")
	}
	if got := s.readTierSetting("s1"); got != "" {
		out = append(out, "read_tier reads "+got)
	}
	if _, set := ReadsSetting(s.Work.Props()); set {
		out = append(out, "reads_needed reads a set count")
	}
	// every property row a clear writes holds its default value
	for _, d := range Defaults {
		if d.Home != HomeProp {
			continue
		}
		if v, ok := s.Work.Prop(d.Name); s.Epoch > 0 && ok && v != d.Value {
			out = append(out, d.Name+" holds "+v+", default "+d.Value)
		}
	}
	// the reader rows: every up member a reader of every served tier, up
	for _, m := range s.UpMembers() {
		rd := ReaderPrefix + m
		if s.Readers == nil || !s.Readers.HasRow(rd) {
			out = append(out, m+" has no reader row "+rd)
			continue
		}
		if !memberReads(s, m) {
			out = append(out, rd+" is held or retired while "+m+" is up")
		}
		for _, tier := range []string{"flash", "pro", "heavy", "frontier"} {
			if !s.readerServesTier(rd, tier) {
				out = append(out, rd+" does not serve "+tier)
			}
		}
		if !s.ReaderIsUp(rd) {
			out = append(out, rd+" is not up while "+m+" is up")
		}
	}
	return out
}

// applyDefaultsClear is the clear the store runs (store.Clear owedStep):
// the shape carried (ShapeOf, RestoreShape) and the working defaults
// written where absent (SetDefaults). It is the real code, applied to an
// in-memory snapshot as the world harness applies a plan.
func applyDefaultsClear(t *testing.T, old *Snapshot) *Snapshot {
	t.Helper()
	sh := ShapeOf(old)
	ns := &Snapshot{Now: old.Now, Epoch: old.Epoch + 1, Work: NewTable(Work), Readers: NewTable(Readers), Merge: NewTable(Merge), Fleet: NewTable(Fleet),
		Coordinator: old.Coordinator, Actor: old.Actor, Routes: old.Routes}
	ns.Readers.SetRows(sh.Readers)
	for rd, tiers := range sh.ReaderTiers {
		if ns.Readers.Texts == nil {
			ns.Readers.Texts = map[string]map[string]string{}
		}
		ns.Readers.Texts[rd] = map[string]string{ReaderTiers: tiers}
	}
	ns.Fleet.SetRows(sh.Members)
	w := &world{t: t, s: ns}
	w.do(RestoreShape(ns, sh))
	p, _, _ := SetDefaults(ns, old.Work.Props())
	for _, pw := range p.Props {
		ns.Work.SetProp(pw.Name, pw.Value)
	}
	return ns
}

func TestEverySettingHoldsItsWorkingDefaultFreshAndAfterAClear(t *testing.T) {
	t.Parallel()
	// the table is read from the same prop tags sprint.Set reads: a SetReq
	// field with no Defaults row fails, so a setting added without a row
	// breaks this test before it can break a sprint.
	rt := reflect.TypeOf(SetReq{})
	for i := range rt.NumField() {
		prop := rt.Field(i).Tag.Get("prop")
		if prop == "" {
			continue
		}
		d, ok := DefaultOf(prop)
		require.True(t, ok, "set flag %s (SetReq.%s) has no Defaults row", prop, rt.Field(i).Name)
		assert.Contains(t, []string{HomeProp, HomeCarry}, d.Home, "%s home", prop)
	}

	// fresh: members up, their reader rows made as init makes them, no
	// setting ever set: every default holds.
	fresh := defaultsWorld(t)
	for _, rd := range MemberReaderRows(fresh) {
		fresh.Readers.SetRows(append(fresh.Readers.Rows(), rd))
	}
	assert.Empty(t, defaultsFailures(fresh), "fresh: %v", defaultsFailures(fresh))

	// after a clear, with each setting first set away from its default:
	// every one reads its working default again, and the members read.
	old := defaultsWorld(t)
	away := map[string]string{}
	for _, d := range Defaults {
		if d.Home == HomeProp {
			away[d.Name] = "off"
		}
	}
	away[PropDealtMax] = "1ms"
	away[PropReadyMax] = "1ms"
	away[PropFriendIdle] = "1ms"
	away[PropFriendFinish] = "1ms"
	away[PropFriendStartMax] = "1ms"
	away[PropPeggedBound] = "1ms"
	away[PropLandingStallBound] = "1ms"
	away[PropFriendStallAfter] = "1ms"
	away[PropFriendStallStep] = "1ms"
	away[PropDriftCommits] = "999"
	away[PropDriftHours] = "999"
	away[PropGoLanes] = "9"
	away[PropLowBalanceUsd] = "999"
	away[PropIdentical] = "999"
	away[PropReadTier] = "heavy"
	away[PropReadsNeeded] = "3"
	away[PropReworkPriority] = ReworkKeep
	away[PropUnservedTierFallsTo] = "flash"
	away[PropFleetTiers] = "frontier"
	away[PropFriendsTiers] = "frontier"
	old.Work.SetProps(away)
	require.NotEmpty(t, defaultsFailures(old), "the away world must fail before the clear")
	cleared := applyDefaultsClear(t, old)
	assert.Empty(t, defaultsFailures(cleared), "after clear: %v", defaultsFailures(cleared))
	// the down member gained no reader row
	assert.False(t, cleared.Readers.HasRow(ReaderPrefix+"m3"), "a down member is no reader")
}

// The reversed witnesses: the clear is passed in as a function, and a clear
// that writes read_cards off, and one that drops the member reader rows,
// each make the check fail.
func TestAClearThatTurnsADefaultOffOrDropsMemberReadersIsRed(t *testing.T) {
	t.Parallel()
	old := defaultsWorld(t)
	old.Work.SetProp(PropReadCards, SwitchOff)

	offClear := func(o *Snapshot) *Snapshot {
		ns := applyDefaultsClear(t, o)
		ns.Work.SetProp(PropReadCards, SwitchOff)
		return ns
	}
	assert.NotEmpty(t, defaultsFailures(offClear(old)), "a clear that writes read_cards=off must fail")

	dropClear := func(o *Snapshot) *Snapshot {
		ns := applyDefaultsClear(t, o)
		var rows []string
		for _, r := range ns.Readers.Rows() {
			if !strings.HasPrefix(r, ReaderPrefix) {
				rows = append(rows, r)
			}
		}
		ns.Readers.SetRows(rows)
		return ns
	}
	assert.NotEmpty(t, defaultsFailures(dropClear(old)), "a clear that drops member reader rows must fail")
	assert.Empty(t, defaultsFailures(applyDefaultsClear(t, old)), "the real clear holds")
}

// TestReaderAddPrefixesABareMemberName lives in cmd/nova-sprint
// (release_accept_members_read_test.go): the verb is the CLI's.

package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Last night's live flash route had no work; only pro and heavy had ready cards.
func nothingCanWorkWorld(t *testing.T, work bool) *world {
	t.Helper()
	w := newWorld(t)
	w.readersTable()
	w.s.Routes = []Route{
		{Name: "flash-live", Tier: "flash", Provider: "flash", Enabled: true},
		{Name: "pro-credit", Tier: "pro", Provider: "credit", Enabled: true},
		{Name: "pro-auth", Tier: "pro", Provider: "key", Enabled: true},
		{Name: "heavy-seat", Tier: "heavy", Provider: "seat", Enabled: true},
	}
	w.s.Tiers = map[string][]string{"pro": {"pro-credit", "pro-auth"}, "heavy": {"heavy-seat"}}
	w.s.Providers = map[string]Provider{"flash": {Funded: true}, "credit": {Funded: true}, "key": {Funded: true}, "seat": {Funded: true}}
	for _, rest := range []RouteRest{
		{Provider: "credit", At: w.s.Now, Until: w.s.Now.Add(3 * time.Hour), Cause: RestCredit, Cards: []string{"old.w1"}, Why: "out of credit"},
		{Provider: "key", At: w.s.Now, Until: OpenUntil, Cause: RestAuth, Why: "auth refused"},
		{Provider: "seat", At: w.s.Now, Until: w.s.Now.Add(4 * time.Hour), Cause: RestCoordinator, Why: "rested by the seat"},
	} {
		w.s.Fleet.SetProp(PropProviderRest(rest.Provider), rest.value())
	}
	if work {
		w.must(Add(w.s, AddReq{Stream: "pro-stream", Count: 2, Brief: "RESULT: pro-work sha=000000000000 tier: pro\n\nThe task."}))
		w.must(Add(w.s, AddReq{Stream: "heavy-stream", Count: 1, Brief: "RESULT: heavy-work sha=000000000000 tier: heavy\n\nThe task."}))
	}
	return w
}

func TestEveryWorkedTierUnfundedStopsTheSprint(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	p, _ := TickDeal(w.s, TickReq{})
	require.Contains(t, p.Stop, "nothing funded can work", "live flash with no flash work cannot keep blocked pro/heavy running")
	for _, phrase := range []string{"pro:", "heavy:", "pro-credit", "out of credit", "pro-auth", "auth refused", "heavy-seat", "rested by the seat"} {
		assert.Contains(t, p.Stop, phrase)
	}
	var stops []Note
	for _, n := range p.Notes {
		if n.Type == "nothing funded can work" {
			stops = append(stops, n)
		}
	}
	require.Len(t, stops, 1, "one sprint-level judgment")
	assert.True(t, strings.HasPrefix(stops[0].What, "sprint stopped: nothing funded can work ("))
	assert.True(t, stops[0].SprintLevel)
	assert.False(t, stops[0].StreamLevel)
}

func TestALiveWorkedTierKeepsTheSprintRunning(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	w.s.Now = w.s.Now.Add(3 * time.Hour)
	p, _ := TickDeal(w.s, TickReq{})
	assert.Empty(t, p.Stop)
}

func TestNoWorkNoStop(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, false)
	rest := RouteRest{Provider: "flash", At: w.s.Now, Until: OpenUntil, Cause: RestCoordinator, Why: "rested by the seat"}
	w.s.Fleet.SetProp(PropProviderRest("flash"), rest.value())
	p, _ := TickDeal(w.s, TickReq{})
	assert.Empty(t, p.Stop, "an idle sprint is not stopped by the worked-tier rule")
}

func TestAnEnabledFundedRouteOutsideItsArrayCannotKeepWorkRunning(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	w.s.Routes = append(w.s.Routes, Route{Name: "pro-omitted", Tier: "pro", Provider: "paid", Enabled: true})
	w.s.Providers["paid"] = Provider{Funded: true}
	p, _ := TickDeal(w.s, TickReq{})
	require.Contains(t, p.Stop, "nothing funded can work")
	assert.Contains(t, p.Stop, "pro-omitted")
	assert.Contains(t, p.Stop, "array")
}

func TestAnEmptyArrayUsesItsNativeEnabledRoutesForTheStop(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	w.s.Tiers["pro"] = nil
	w.s.Now = w.s.Now.Add(3 * time.Hour)
	p, _ := TickDeal(w.s, TickReq{})
	assert.Empty(t, p.Stop, "empty array falls back to all enabled native-tier routes")
}

func TestOnlyWaitingReadsCanStopTheSprint(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, false)
	w.s.Work.SetRows([]string{"pro-stream"})
	w.s.Work.Put(&Card{ID: "read-me", Row: "pro-stream", Col: Review, Fields: map[string]string{"brief": "RESULT: read-me sha=000000000000 tier: pro\n", "attempt": "1", "result": "ok", "head": "abcdef", FieldTierNow: "pro"}})
	p, _ := TickDeal(w.s, TickReq{})
	require.Contains(t, p.Stop, "nothing funded can work", "reads waiting count as work without ready primaries")
	assert.Contains(t, p.Stop, "pro:")
	assert.NotContains(t, p.Stop, "heavy:")
}

func TestAWorkingFriendOfAnUnworkedTierDoesNotHideTheStop(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{{Name: "flash-friend", Status: Up, Class: "flash", Tiers: []string{"flash"}, Width: 1}}})
	assert.Contains(t, p.Stop, "nothing funded can work")
}

func TestAnUpFriendOfAWorkedTierPreventsTheStop(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	p, _ := TickDeal(w.s, TickReq{Friends: []FriendSeat{{Name: "pro-friend", Status: Up, Class: "pro", Tiers: []string{"pro"}, Width: 1}}})
	assert.Empty(t, p.Stop)
}

func TestTheExistingCreditStopWinsAndStillStopsWithoutWork(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	for _, r := range w.s.Routes {
		rest := RouteRest{Provider: r.Provider, At: w.s.Now, Until: OpenUntil, Cause: RestCredit, Cards: []string{"old.w1"}}
		w.s.Fleet.SetProp(PropProviderRest(r.Provider), rest.value())
	}
	p, _ := TickDeal(w.s, TickReq{})
	assert.True(t, strings.HasPrefix(p.Stop, FundsCause))
	w = nothingCanWorkWorld(t, false)
	for _, r := range w.s.Routes {
		rest := RouteRest{Provider: r.Provider, At: w.s.Now, Until: OpenUntil, Cause: RestCredit, Cards: []string{"old.w1"}}
		w.s.Fleet.SetProp(PropProviderRest(r.Provider), rest.value())
	}
	p, _ = TickDeal(w.s, TickReq{})
	assert.True(t, strings.HasPrefix(p.Stop, FundsCause), "legacy all-credit idle stop is retained")
}

func waitingReadWorld(t *testing.T, tier string) *world {
	t.Helper()
	w := nothingCanWorkWorld(t, false)
	w.s.Work.SetRows([]string{"read-stream"})
	w.s.Work.Put(&Card{ID: "read-me", Row: "read-stream", Col: Review, Fields: map[string]string{"brief": "RESULT: read-me sha=000000000000 tier: " + tier + "\n", "attempt": "1", "result": "ok", "head": "abcdef", FieldTierNow: tier}})
	return w
}

func TestAHeavyPrimaryWaitsOnItsActualProReadTier(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "heavy")
	p, _ := TickDeal(w.s, TickReq{})
	require.Contains(t, p.Stop, "pro:")
	assert.NotContains(t, p.Stop, "heavy:", "the heavy primary's read actually draws pro")
}

func TestAProPrimaryWithAFlashReadCanStillWork(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "pro")
	for i := range w.s.Routes {
		if w.s.Routes[i].Tier == "pro" {
			w.s.Routes[i].Enabled = false
		}
	}
	p, _ := TickDeal(w.s, TickReq{})
	assert.Empty(t, p.Stop, "pro's read draws live flash when no enabled pro route is selected")
}

func TestAnUpOwnModelReaderKeepsReadOnlyWorkRunning(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "pro")
	w.s.Readers.SetRows([]string{"reader-own"})
	w.s.Fleet.SetRows([]string{FriendRow("own")})
	// An own-model reader is up by its reader row, even without an up worker seat.
	w.s.ReaderStates = map[string]string{"reader-own": ReaderUp}
	p, _ := TickDeal(w.s, TickReq{})
	assert.Empty(t, p.Stop)
}

func TestDisabledAndForeignArrayEntriesCannotKeepWorkRunning(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	w.s.Routes = append(w.s.Routes, Route{Name: "pro-disabled", Tier: "pro", Provider: "disabled", Enabled: false})
	w.s.Tiers["pro"] = []string{"flash-live", "pro-disabled", "missing-route"}
	p, _ := TickDeal(w.s, TickReq{})
	require.Contains(t, p.Stop, "nothing funded can work")
	assert.Contains(t, p.Stop, "tier array pro")
	assert.Contains(t, p.Stop, "no enabled native-tier route")
}

func TestAnOwnModelReaderWhoWorkedThePrimaryCannotHideTheStop(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "pro")
	w.s.Readers.SetRows([]string{"reader-own"})
	w.s.Fleet.SetRows([]string{FriendRow("own")})
	w.s.ReaderStates = map[string]string{"reader-own": ReaderUp}
	w.s.Work.Card("read-me").Fields["member"] = FriendRow("own")
	p, _ := TickDeal(w.s, TickReq{})
	assert.Contains(t, p.Stop, "nothing funded can work", "the primary's worker cannot perform its missing read")
}

func TestAnOwnModelReaderAlreadySpentCannotHideTheStop(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "pro")
	w.s.Readers.SetRows([]string{"reader-own"})
	w.s.Fleet.SetRows([]string{FriendRow("own")})
	w.s.ReaderStates = map[string]string{"reader-own": ReaderUp}
	w.s.Readers.Put(&Card{ID: ReadCardID("read-me", 1, "reader-own"), Row: "reader-own", Col: "", Fields: map[string]string{"primary": "read-me", "attempt": "1", "verdict": "ok", "kind": "read"}})
	p, _ := TickDeal(w.s, TickReq{})
	assert.Contains(t, p.Stop, "nothing funded can work", "one reader cannot do both pro verdicts")
}

func TestRouteLessWorkWithoutAnEligibleReaderStops(t *testing.T) {
	t.Parallel()
	w := waitingReadWorld(t, "pro")
	w.s.Routes = nil
	p, _ := TickDeal(w.s, TickReq{})
	assert.Contains(t, p.Stop, "nothing funded can work")
}

func TestAnEmptyEffectiveArrayBlocksReadyWork(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, true)
	w.s.Routes = nil
	w.s.Tiers = nil
	p, _ := TickDeal(w.s, TickReq{})
	assert.Contains(t, p.Stop, "tier array pro [] has no enabled native-tier route")
}

func TestEveryActiveRestClassBlocksWorkedTiers(t *testing.T) {
	t.Parallel()
	for _, cause := range []string{RestRateBackoff, RestProvider} {
		t.Run(cause, func(t *testing.T) {
			t.Parallel()
			w := nothingCanWorkWorld(t, true)
			until := w.s.Now.Add(time.Hour)
			for _, r := range w.s.Routes {
				if r.Tier == "flash" {
					continue
				}
				rest := RouteRest{Route: r.Name, At: w.s.Now, Until: until, Cause: cause, Why: "temporary provider rest"}
				w.s.Fleet.SetProp(PropProviderRest(r.Provider), rest.value())
			}
			p, _ := TickDeal(w.s, TickReq{})
			require.Contains(t, p.Stop, "nothing funded can work")
			assert.Contains(t, p.Stop, cause)
			assert.Contains(t, p.Stop, until.UTC().Format(time.RFC3339))
		})
	}
}

func TestTheFundsPredicateUsesTheEscalatedExecutionTier(t *testing.T) {
	t.Parallel()
	w := nothingCanWorkWorld(t, false)
	w.s.Work.SetRows([]string{"pro-stream"})
	pr := &Card{ID: "escalating", Row: "pro-stream", Col: Ready, Fields: map[string]string{"brief": "RESULT: escalating sha=000000000000 tier: heavy\n\nThe task."}}
	w.s.Work.Put(pr)
	pr.Fields["attempt"] = "1"
	pr.Fields[FieldTierNow] = "pro"
	w.s.Fleet.SetRows([]string{"m1"})
	w.s.Fleet.Put(&Card{ID: WorkCardID(pr.ID, 1), Row: "m1", Col: Withdrawn, Fields: map[string]string{"primary": pr.ID, "attempt": "1", "redeals": "3", FieldTakeEnded: "failed"}})
	w.s.Now = w.s.Now.Add(3 * time.Hour)
	why := w.s.NothingFundedCanWork()
	require.Contains(t, why, "heavy:")
	assert.NotContains(t, why, "pro:", "the live original pro tier cannot hide the resting execution tier")
}

func TestExecutableQueuedWorkKeepsBlockedReadsRunning(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Snapshot, *Card, *Card)
		live   bool
	}{
		{name: "valid pin behind full width", live: true},
		{name: "live persisted route", live: true, change: func(_ *Snapshot, pr, wc *Card) {
			pr.Fields["brief"] = "tier: flash\n"
			wc.Fields[FieldRoute] = "flash-live"
			wc.Fields[FieldModel] = "flash/model"
		}},
		{name: "disabled persisted route", change: func(s *Snapshot, pr, wc *Card) {
			pr.Fields["brief"] = "tier: flash\n"
			wc.Fields[FieldRoute] = "flash-live"
			s.Routes[0].Enabled = false
		}},
		{name: "missing persisted route", change: func(_ *Snapshot, pr, wc *Card) { pr.Fields["brief"] = "tier: flash\n"; wc.Fields[FieldRoute] = "gone" }},
		{name: "friend provenance", change: func(_ *Snapshot, _, wc *Card) { wc.Row = FriendRow("amy") }},
		{name: "unlanded need", change: func(_ *Snapshot, pr, _ *Card) { pr.Fields["needs"] = "unlanded" }},
		{name: "invalid persisted deadline", change: func(_ *Snapshot, _, wc *Card) { wc.Fields[FieldDeadline] = "0" }},
		{name: "fleet off", change: func(s *Snapshot, _, _ *Card) { s.Fleet.SetProp(PropFleetTiers, SwitchOff) }},
		{name: "held stream", change: func(s *Snapshot, pr, _ *Card) {
			s.Merge.Put(&Card{ID: CtlID(pr.Row), Row: pr.Row, Col: Ctl, Fields: map[string]string{"kind": "stream", FieldHeld: "by coordinator"}})
		}},
		{name: "old attempt", change: func(_ *Snapshot, pr, _ *Card) { pr.Fields["attempt"] = "2" }},
		{name: "missing primary", change: func(_ *Snapshot, _, wc *Card) { wc.Fields["primary"] = "missing" }},
		{name: "down member", change: func(s *Snapshot, _, _ *Card) { s.MemberCtl("m1").Fields["status"] = Down }},
		{name: "zero width", change: func(s *Snapshot, _, _ *Card) { s.MemberCtl("m1").Fields["width"] = "0" }},
		{name: "invalid pin budget", change: func(_ *Snapshot, pr, _ *Card) {
			pr.Fields["brief"] = "tier: flash\nmodel: simulation/twin\ntokens: nope\ndeadline: 600"
		}},
		{name: "foreign pin model", change: func(_ *Snapshot, _, wc *Card) { wc.Fields[FieldModel] = "other/model" }},
		{name: "routed rest", change: func(_ *Snapshot, pr, wc *Card) {
			pr.Fields["brief"] = "tier: pro\n"
			pr.Fields[FieldTierNow] = "pro"
			wc.Fields[FieldRoute] = "pro-credit"
			wc.Fields[FieldModel] = "credit/model"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := nothingCanWorkWorld(t, true)
			w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "m1", Width: 1}))
			w.s.Work.SetRows(append(w.s.Work.Rows(), "queued"))
			pr := &Card{ID: "queued-1", Row: "queued", Col: Working, Fields: map[string]string{"brief": "tier: flash\nmodel: simulation/twin\ntokens: 1000\ndeadline: 600", "attempt": "1", "member": "m1"}}
			wc := &Card{ID: WorkCardID(pr.ID, 1), Row: "m1", Col: Ready, Fields: map[string]string{"kind": "work", "primary": pr.ID, "attempt": "1", FieldRoute: RoutePin, FieldModel: "simulation/twin", FieldTokens: "1000", FieldDeadline: "600"}}
			w.s.Work.Put(pr)
			w.s.Fleet.Put(wc)
			w.s.Fleet.Put(&Card{ID: "already-running.w1", Row: "m1", Col: Working, Fields: map[string]string{"kind": "work"}})
			if tc.change != nil {
				tc.change(w.s, pr, wc)
			}
			why := w.s.NothingFundedCanWork()
			if tc.live {
				assert.Empty(t, why, "already-dealt executable work counts while its slot is occupied")
				assert.True(t, w.s.HasLiveWorkedTier())
			} else {
				assert.Contains(t, why, NothingFundedCause, "ineligible queued work cannot conceal the resting worked tiers")
				assert.False(t, w.s.HasLiveWorkedTier())
			}
		})
	}
}

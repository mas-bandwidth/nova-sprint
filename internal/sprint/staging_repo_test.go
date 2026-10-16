package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// A staging refusal for want of a bench mirror is about the whole repository, not one
// card: the member cannot stage any card of that repository, so it becomes a capability,
// the way a harness the member cannot launch is one (Launches, notLaunching). The owner,
// 2026-10-11 ~19:05-19:20Z: "Please log cards to fix this deal and rebalance. It's shit.";
// "cards only may accrue per-fleet or friend, if there are not other machines/friends
// ready to do that work right now!" (mirrorRefusals).

const mirrorErr = "staging refused: no bench mirror for https://github.com/mas-bandwidth/nova-sprint.git: a card may not clone directly from github without a bench mirror"

// mirrorWorld is studio and b1 idle at width 4, a stream whose control card records the
// nova-sprint repository, and a withdrawn nova-sprint work card whose staging take studio
// refused for want of a bench mirror at finished.
func mirrorWorld(t *testing.T, err string, finished time.Time) *Snapshot {
	t.Helper()
	f := NewTable(Fleet)
	f.SetRows([]string{"studio", "b1"})
	f.Put(&Card{ID: CtlID("studio"), Row: "studio", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up}})
	f.Put(&Card{ID: CtlID("b1"), Row: "b1", Col: Ctl, Fields: map[string]string{"kind": "member", "status": Up}})
	f.Put(&Card{ID: "ns-1.w1", Row: "b1", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldStagingTake + "1": ProviderTake{Member: "studio", Finished: finished.UTC().Format(time.RFC3339), Error: err}.String(),
	}})
	m := NewTable(Merge)
	m.SetRows([]string{"ns"})
	m.Put(&Card{ID: CtlID("ns"), Row: "ns", Col: Ctl, Fields: map[string]string{FieldRepo: "https://github.com/mas-bandwidth/nova-sprint"}})
	w := NewTable(Work)
	w.Put(&Card{ID: "ns-1", Row: "ns", Col: Ready, Fields: map[string]string{"brief": "tier: flash\n"}})
	return &Snapshot{Now: t0, Work: w, Merge: m, Fleet: f,
		Routes: []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true}}}
}

func nsPrimary(id string) *Card {
	return &Card{ID: id, Row: "ns", Fields: map[string]string{"brief": "tier: flash\n"}}
}

// TestAMemberThatRefusedARepoAtStagingIsNotDealtIt: studio and b1 idle at width 4; a
// withdrawn nova-sprint work card with a staging take by studio "staging refused: no
// bench mirror for https://github.com/mas-bandwidth/nova-sprint.git: ..."; the deal of 4
// new nova-sprint primaries and 2 nova-tools primaries puts the 4 on b1 and may put the
// nova-tools ones on studio. At BASE studio gets the new nova-sprint cards because the
// refusal is remembered per card (StagingRefusers), so the b1 line fails there.
func TestAMemberThatRefusedARepoAtStagingIsNotDealtIt(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "studio", Width: 4}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "b1", Width: 4}))
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
	w.must(Add(w.s, AddReq{Stream: "ns", Count: 4, Repo: "https://github.com/mas-bandwidth/nova-sprint", Brief: "tier: flash\n"}))
	w.must(Add(w.s, AddReq{Stream: "nt", Count: 2, Repo: "https://github.com/mas-bandwidth/nova-tools", Brief: "tier: flash\n"}))
	// a withdrawn nova-sprint work card studio refused at staging for want of a bench
	// mirror; its repository is remembered on the card (FieldRepo).
	w.s.Fleet.Put(&Card{ID: "ns-0.w1", Row: "b1", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldRepo:              "https://github.com/mas-bandwidth/nova-sprint",
		FieldStagingTake + "1": ProviderTake{Member: "studio", Finished: t0.Add(-time.Minute).Format(time.RFC3339), Error: mirrorErr}.String(),
	}})
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"ns-1", "ns-2", "ns-3", "ns-4", "nt-1", "nt-2"}}}))
	for _, id := range []string{"ns-1", "ns-2", "ns-3", "ns-4"} {
		wc := w.s.Fleet.Card(WorkCardID(id, 1))
		require.NotNil(t, wc, id)
		assert.Equal(t, "b1", wc.Row, "%s: a nova-sprint card is not dealt to a member that refused nova-sprint at staging", id)
	}
}

// TestAMirrorRefusalKeepsTheLevelAndTheRedealOffTheMember: the level (notLaunching) and a
// hold's redeal of another member's nova-sprint card never place one on studio inside the
// window; the why names studio, the repository and what to do.
func TestAMirrorRefusalKeepsTheLevelAndTheRedealOffTheMember(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-time.Minute))
	dealt := &Card{ID: "ns-3.w2", Row: "studio", Col: Ready,
		Fields: map[string]string{FieldRepo: "https://github.com/mas-bandwidth/nova-sprint"}}
	assert.Equal(t, []string{"studio"}, notLaunching(s, []string{"studio", "b1"}, dealt),
		"the level never moves a refused repository's card onto the refuser")
	_, _, why, _ := s.routeOf(nsPrimary("ns-4"), nil, nil, "studio")
	require.NotEmpty(t, why)
	assert.Contains(t, why, "studio refused mas-bandwidth/nova-sprint at staging")
	assert.Contains(t, why, "no bench mirror")
	assert.Contains(t, why, "add the mirror on studio with the fleet play")
}

// TestAMirrorRefusalEndsPastTheWindowOrAfterAnOk: past StagingMirrorWindow, or after
// studio finishes ok a nova-sprint card, studio is dealt nova-sprint cards again.
func TestAMirrorRefusalEndsPastTheWindowOrAfterAnOk(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-2*StagingMirrorWindow))
	_, _, why, _ := s.routeOf(nsPrimary("ns-5"), nil, nil, "studio")
	assert.Empty(t, why, "the window has passed: the refusal lapses")

	s2 := mirrorWorld(t, mirrorErr, t0.Add(-time.Minute))
	s2.Fleet.Put(&Card{ID: "ns-6.w1", Row: "studio", Col: DoneOK, Fields: map[string]string{
		"ok": "yes", "member": "studio", "finished": t0.Format(time.RFC3339),
		FieldRepo: "https://github.com/mas-bandwidth/nova-sprint"}})
	_, _, why, _ = s2.routeOf(nsPrimary("ns-7"), nil, nil, "studio")
	assert.Empty(t, why, "studio finished a nova-sprint card ok: the refusal ends at once")
}

// TestANoLauncherJudgmentNamesTheMirrorRefusal: studio alone up refused nova-sprint at
// staging for want of a bench mirror, so no member the deal may give a new nova-sprint
// primary can launch it. The no-launcher judgment (noLauncher, route.go) and the deal's
// refusal (steps_work.go, next) must say the mirror sentence, never unlaunchableWhy's
// generic harness sentence: the coordinator reads what to add and where.
func TestANoLauncherJudgmentNamesTheMirrorRefusal(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "studio", Width: 4}))
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
	w.must(Add(w.s, AddReq{Stream: "ns", Count: 1, Repo: "https://github.com/mas-bandwidth/nova-sprint", Brief: "tier: flash\n"}))
	w.s.Fleet.Put(&Card{ID: "ns-0.w1", Row: "studio", Col: Withdrawn, Fields: map[string]string{
		"gen":                  "1",
		FieldRepo:              "https://github.com/mas-bandwidth/nova-sprint",
		FieldStagingTake + "1": ProviderTake{Member: "studio", Finished: t0.Add(-time.Minute).Format(time.RFC3339), Error: mirrorErr}.String(),
	}})
	why := w.s.noLauncher(nsPrimary("ns-1"), nsPrimary("ns-1"), cardhdr.RouteFlash, []string{"studio"})
	require.NotEmpty(t, why, "no member is left: the judgment is raised")
	assert.Contains(t, why, "studio refused mas-bandwidth/nova-sprint at staging")
	assert.Contains(t, why, "no bench mirror")
	assert.Contains(t, why, "add the mirror on studio with the fleet play")

	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"ns-1"}}})
	require.NotEmpty(t, p.Refused, "the deal refuses the card")
	assert.Contains(t, p.Refused[0].Why, "studio refused mas-bandwidth/nova-sprint at staging")
	assert.Contains(t, p.Refused[0].Why, "no bench mirror")
	assert.Contains(t, p.Refused[0].Why, "add the mirror on studio with the fleet play")
}

// TestAStagingRefusalOfAnotherCauseStaysPerCard: a staging refusal that names no bench
// mirror (the pushed head missing, a disk floor) is not a repository capability.
func TestAStagingRefusalOfAnotherCauseStaysPerCard(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, "staging refused: head 46cdb6 is in neither the mirror nor origin", t0.Add(-time.Minute))
	assert.Empty(t, mirrorRefusals(s), "no bench mirror was refused")
	_, _, why, _ := s.routeOf(nsPrimary("ns-8"), nil, nil, "studio")
	assert.Empty(t, why, "studio can be dealt a nova-sprint card again")
}

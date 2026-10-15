package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Synthetic staging errors only: no git, sockets, credentials or wall clock.
// The three refusals replay space's delta-audit incident; card four must go
// elsewhere. StagingRepoWindow's broken witness admits that fourth card.
func TestACredentialRefusalAtStagingBarsTheRepository(t *testing.T) {
	t.Parallel()
	for _, marker := range []string{"Write access to repository not granted", "Authentication failed", "Permission denied (publickey)"} {
		t.Run(marker, func(t *testing.T) {
			for _, scenario := range []struct {
				name   string
				now    time.Time
				okRepo string
				want   string
			}{
				{"within window", t0, "", "b1"},
				{"at window", t0.Add(StagingMirrorWindow - time.Minute), "", "b1"},
				{"past window", t0.Add(StagingMirrorWindow), "", "space"},
				{"matching ok", t0, "robotgamedevelopers/delta-audit", "space"},
				{"other repo ok", t0, "robotgamedevelopers/other", "b1"},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					w := newWorld(t)
					w.s.Now = scenario.now
					// Sorted fleet order is b1, space: make space next in the real deal.
					w.s.Fleet.SetProp(PropDealIndex, "1")
					w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "space", Width: 4}))
					w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "b1", Width: 4}))
					w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
					w.must(Add(w.s, AddReq{Stream: "delta", Count: 1, Repo: "https://github.com/robotgamedevelopers/delta-audit", Brief: "tier: flash\n"}))
					for i, id := range []string{"delta-refusal-1.w1", "delta-refusal-2.w1", "delta-refusal-3.w1"} {
						w.s.Fleet.Put(&Card{ID: id, Row: "b1", Col: Withdrawn, Fields: map[string]string{
							"gen": "1", FieldRepo: "https://github.com/robotgamedevelopers/delta-audit.git",
							FieldStagingTake + "1": ProviderTake{Member: "space", Finished: t0.Add(-time.Duration(3-i) * time.Minute).Format(time.RFC3339), Error: "staging refused: git fetch of the base branch sprint/delta-audit failed: remote: " + marker}.String(),
						}})
					}
					if scenario.okRepo != "" {
						w.s.Fleet.Put(&Card{ID: "finished.w1", Row: "space", Col: DoneOK, Fields: map[string]string{
							"ok": "yes", "member": "space", "finished": t0.Format(time.RFC3339), FieldRepo: scenario.okRepo,
						}})
					}
					assert.Empty(t, mirrorNeeds(w.s), "a credential refusal is not a request to install a missing mirror")
					if scenario.want == "b1" {
						why := w.s.stagingRepoWhy("space", w.s.Work.Card("delta-1"))
						assert.Contains(t, why, "credential")
						assert.NotContains(t, why, "add the mirror")
					}
					w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"delta-1"}}}))
					dealt := w.s.Fleet.Card(WorkCardID("delta-1", 1))
					require.NotNil(t, dealt)
					assert.Equal(t, scenario.want, dealt.Row, "a credential refusal bars the repository for the existing mirror window")
				})
			}
		})
	}
}

// A stream control card holds the union of its repositories, not the identity of
// each card. Real dealt cards can carry that identity only in their primary brief.
func TestACredentialRefusalAtStagingBarsTheRepositoryInAMixedStream(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "space", Width: 4}))
	w.must(FleetStep(w.s, FleetReq{Op: "up", Probe: EnvProbePassed, Member: "b1", Width: 4}))
	w.s.Routes = []Route{{Name: "flash-a", Tier: cardhdr.RouteFlash, Provider: "p", Model: "m", Enabled: true, Deadline: 600}}
	for _, repo := range []string{"a-allowed", "z-refused", "z-refused"} {
		w.must(Add(w.s, AddReq{Stream: "mixed", Count: 1, Repo: "https://github.com/example/" + repo,
			Brief: "REPO: https://github.com/example/" + repo + "\ntier: flash\n"}))
	}
	w.s.Fleet.Put(&Card{ID: "mixed-3.w1", Row: "b1", Col: Withdrawn, Fields: map[string]string{
		"gen": "1",
		FieldStagingTake + "1": ProviderTake{Member: "space", Finished: t0.Add(-time.Minute).Format(time.RFC3339),
			Error: "staging refused: git fetch failed: remote: Write access to repository not granted"}.String(),
	}})
	// Make space next for both independent admissions; neither member is full.
	for _, tc := range []struct{ id, member string }{{"mixed-1", "space"}, {"mixed-2", "b1"}} {
		w.s.Fleet.SetProp(PropDealIndex, "1")
		w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{tc.id}}}))
		dealt := w.s.Fleet.Card(WorkCardID(tc.id, 1))
		require.NotNil(t, dealt)
		assert.Equal(t, tc.member, dealt.Row, "only the refused repository is barred in a mixed stream")
	}
}

func TestACredentialRefusalAtStagingBarsTheRepositoryNamesTheLatestCause(t *testing.T) {
	t.Parallel()
	s := mirrorWorld(t, mirrorErr, t0.Add(-2*time.Minute))
	latest := t0.Add(-time.Minute).Format(time.RFC3339)
	s.Fleet.Put(&Card{ID: "ns-2.w1", Row: "b1", Col: Withdrawn, Fields: map[string]string{
		"gen": "1", FieldRepo: "https://github.com/mas-bandwidth/nova-sprint",
		FieldStagingTake + "1": ProviderTake{Member: "studio", Finished: latest,
			Error: "staging refused: Authentication failed"}.String(),
	}})
	why := s.stagingRepoWhy("studio", nsPrimary("ns-3"))
	assert.Contains(t, why, latest)
	assert.Contains(t, why, "credential")
	assert.NotContains(t, why, "add the mirror")
	assert.Len(t, mirrorNeeds(s), 1, "the older live mirror need is still reported separately")
}

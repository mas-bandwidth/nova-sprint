package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 2026-10-17 19:16:10 EDT the sprint server wedged (TICK WEDGED): 251 verbs waited
// on the control line while one ack held it inside TickDeadlines, the deal's check
// of every ready card on every member (readyWhy -> launchersOf -> routeServed ->
// stagingRepoWhy -> stagingRepoRefusals -> cardRepo) splitting a primary's whole
// brief with swarm.ReadCardBase once per fleet card per member per ready card. A
// brief is immutable on the table, so its repository is read once; the answer is
// unchanged. No real time: the clock is 1000 s, the snapshot is in memory.
func TestTheStagingRepoCheckReadsEachBriefOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	now := time.Unix(1000, 0).UTC()
	w.s.Now = now
	brief := "RESULT: rr sha=<sha12> tier: pro\nREPO: https://github.com/example/" + t.Name() + "\nBASE: main\n\nThe card's prose, never read for its repository.\n"
	for _, id := range []string{"rr-1", "rr-2", "rr-3"} {
		w.s.Work.Put(&Card{ID: id, Row: "rr", Col: Waiting, Fields: map[string]string{"brief": brief}})
	}
	// space refused the repository at staging a minute ago (a credential refusal: the
	// repository is read from the card's primary brief); b1 finished a card of it ok.
	w.s.Fleet.Put(&Card{ID: "rr-2.w1", Row: "space", Col: Withdrawn, Fields: map[string]string{
		"gen": "1",
		FieldStagingTake + "1": ProviderTake{Member: "space", Finished: now.Add(-time.Minute).Format(time.RFC3339),
			Error: "staging refused: git fetch failed: remote: Write access to repository not granted"}.String(),
	}})
	w.s.Fleet.Put(&Card{ID: "rr-1.w1", Row: "b1", Col: DoneOK, Fields: map[string]string{
		"ok": "yes", "member": "b1", "finished": now.Format(time.RFC3339),
	}})
	primary := w.s.Work.Card("rr-3")
	for range 10 {
		for _, m := range []string{"space", "b1", "b2"} {
			why := w.s.stagingRepoWhy(m, primary)
			if m == "space" {
				assert.Contains(t, why, "credential", "space refused the repository: the answer is unchanged")
			} else {
				assert.Empty(t, why, "%s refused nothing", m)
			}
		}
	}
	assert.Equal(t, int64(1), briefRepoReads(brief), "30 checks over three cards of one brief read that brief once")
}

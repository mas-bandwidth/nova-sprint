package sprint

import (
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConsumerRecordNeverWritesTheQueuedEntrysMap pins the v1.2.16 A14 race:
// the backing hands every reader of the work table's queue the same entry and
// the same Set map (Mem.QueueRead copies the slice, not the maps), and Drain
// keeps that map in the plan it builds. Recording a landing on that plan must
// write into a map the plan owns, never into the queued entry's: a second run
// loop planning on the same queue (WithQueue) reads that map at the same time,
// and under two loops that was a fatal concurrent map write. The witness is
// deterministic: two goroutines, one drains and records, one plans on the
// queue, no ordering between them; under -race the old code reports the race
// every run, and without -race the queued entry still shows the written field.
func TestConsumerRecordNeverWritesTheQueuedEntrysMap(t *testing.T) {
	t.Parallel()
	pr := &Card{ID: "p1", Row: "s1", Col: Merging, Fields: map[string]string{}}
	s := &Snapshot{Epoch: 7, Now: time.Date(2026, 10, 12, 20, 0, 0, 0, time.UTC), Work: logCoverTable(Work, []string{"s1"}, pr), Fleet: NewTable(Fleet), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	require.NotNil(t, s.Work.Card("p1")) // the table's lazy mutex is made before the loops start
	queued := map[string]string{"ci": "green", "landed": stamp(s.Now)}
	e := moveEntry(pr, "s1", Landed, queued)
	// e.Set is the queued map itself (moveEntry keeps the map it is given), so
	// the expectation is an independent copy taken before anything runs: a write
	// into the queued map changes e.Set and queued alike, and only this copy
	// can show it.
	want := maps.Clone(e.Set)
	q := []QueuedChange{{Entry: &e, Verb: "merge", Actor: "machine"}}

	var recorded Plan
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the first loop: the drain plan, held to the consumer record
		defer wg.Done()
		recorded = recordConsumerTransitions(s, Drain(s, q, "machine"))
	}()
	go func() { // the second loop: a step planned on the table as the drain will leave it
		defer wg.Done()
		WithQueue(s, q)
	}()
	wg.Wait()

	require.Equal(t, want, e.Set, "the queued entry's map was written by the consumer record")
	require.NotContains(t, queued, FieldConsumerPrefix+"7:land", "the consumer record reached the queued map")
	var set map[string]string
	for _, u := range recorded.Units {
		for _, ch := range u.Changes {
			if ch.Table == Work && ch.Entry.ID == "p1" {
				set = ch.Entry.Set
			}
		}
	}
	require.Equal(t, "green", set["ci"], "the drained change keeps what was queued")
	require.NotEmpty(t, set[FieldConsumerPrefix+"7:land"], "the drained change carries the landing record")
}

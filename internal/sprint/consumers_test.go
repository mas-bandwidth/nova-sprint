package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConsumerHistoryFollowsTheCommittedTakeAndFinish(t *testing.T) {
	pr := &Card{ID: "p1", Row: "s1", Col: Working, Fields: map[string]string{}}
	wc := &Card{ID: "p1.w1", Row: "member1", Col: Ready, Fields: map[string]string{"kind": "work", "primary": "p1", "gen": "1", "route": "sol"}}
	s := &Snapshot{Epoch: 7, Now: time.Date(2026, 10, 11, 20, 1, 0, 0, time.UTC), Work: logCoverTable(Work, []string{"s1"}, pr), Fleet: logCoverTable(Fleet, []string{"member1"}, wc), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	launch := Applied(s, Plan{Units: []Unit{{Key: wc.ID, Stream: "s1", Changes: []Change{change(Fleet, moveEntry(wc, wc.Row, Working, map[string]string{"taken": stamp(s.Now)}))}}}})
	for _, u := range launch.Units {
		for _, ch := range u.Changes {
			if ch.Table == Work {
				for k, v := range ch.Entry.Set {
					pr.Fields[k] = v
				}
			}
		}
	}
	started := ConsumersOf(pr)
	require.Equal(t, 1, started.Attempts)
	require.Equal(t, "member1", started.Consumers[0].Who)
	require.Empty(t, started.Consumers[0].Ended)
	wc.Col = Working
	wc.Fields["taken"] = stamp(s.Now)
	s.Now = s.Now.Add(time.Minute)
	end := Applied(s, Plan{Units: []Unit{{Key: wc.ID, Stream: "s1", Changes: []Change{change(Fleet, moveEntry(wc, wc.Row, Finished, map[string]string{"report": "done well\nmore detail", "result": "ok"}))}}}})
	for _, u := range end.Units {
		for _, ch := range u.Changes {
			if ch.Table == Work {
				for k, v := range ch.Entry.Set {
					pr.Fields[k] = v
				}
			}
		}
	}
	got := ConsumersOf(pr)
	require.Equal(t, 1, got.Attempts)
	require.Equal(t, "2026-10-11T20:02:00Z", got.Consumers[0].Ended)
	require.Equal(t, "done well", got.Consumers[0].FirstLine)
}

func TestConsumerRecordPreservesTimestampsAndWords(t *testing.T) {
	want := ConsumerRec{Key: "p1.w1#g1", Epoch: 7, Kind: "work", Taken: "2026-10-11T20:01:00Z", Ended: "2026-10-11T20:02:00Z", FirstLine: "provider refused a route"}
	got := parseConsumerRec(encodeConsumerRec(want))
	require.Equal(t, want.Taken, got.Taken)
	require.Equal(t, want.Ended, got.Ended)
	require.Equal(t, want.FirstLine, got.FirstLine)
}

func TestConsumersRecordedOnTheCardKeyedByEpoch(t *testing.T) {
	pr := &Card{ID: "c1", Fields: map[string]string{}}
	set := map[string]string{}

	AddConsumer(pr, set, ConsumerRec{Key: "w1", Epoch: 7, Kind: "work", Who: "member1", Route: "r1", Model: "m1", Taken: "t1"})
	AddConsumer(pr, set, ConsumerRec{Key: "r1", Epoch: 7, Kind: "read", Who: "reader1", Taken: "t2"})
	AddConsumer(pr, set, ConsumerRec{Key: "land", Epoch: 8, Kind: "land", Taken: "t3"})
	for k, val := range set {
		pr.Fields[k] = val
	}

	v := ConsumersOf(pr)
	require.Equal(t, 1, v.Attempts)
	require.Equal(t, 1, v.Reads)
	require.Equal(t, 1, v.Landings)
	require.Len(t, v.Consumers, 3)
	require.Equal(t, uint64(7), v.Consumers[0].Epoch)
	require.Equal(t, "work", v.Consumers[0].Kind)
	require.Equal(t, uint64(8), v.Consumers[2].Epoch)
	require.Equal(t, "land", v.Consumers[2].Kind)
}

func TestAddConsumerIsIdempotentPerKey(t *testing.T) {
	pr := &Card{ID: "c1", Fields: map[string]string{}}
	set := map[string]string{}
	rec := ConsumerRec{Key: "w1", Epoch: 1, Kind: "work", Who: "m", Taken: "t"}
	AddConsumer(pr, set, rec)
	AddConsumer(pr, set, rec)
	for k, val := range set {
		pr.Fields[k] = val
	}
	require.Equal(t, 1, ConsumersOf(pr).Attempts)
}

func TestStalledIsTheNewestConsumerEndedAndNoneOut(t *testing.T) {
	pr := &Card{ID: "c1", Fields: map[string]string{}}
	set := map[string]string{}
	AddConsumer(pr, set, ConsumerRec{Key: "w1", Epoch: 7, Kind: "work", Who: "member1", Taken: "t1", Ended: "e1", Outcome: "ok"})
	for k, val := range set {
		pr.Fields[k] = val
	}
	require.True(t, ConsumersOf(pr).Stalled(), "the only consumer ended: none is out")

	AddConsumer(pr, set, ConsumerRec{Key: "w2", Epoch: 8, Kind: "work", Who: "member1", Taken: "t2"})
	for k, val := range set {
		pr.Fields[k] = val
	}
	require.False(t, ConsumersOf(pr).Stalled(), "the newest consumer is out")

	k := consumerField(ConsumerRec{Key: "w2", Epoch: 8})
	set[k] = encodeConsumerRec(ConsumerRec{Key: "w2", Epoch: 8, Kind: "work", Who: "member1", Taken: "t2", Ended: "e2", Outcome: "ok"})
	pr.Fields[k] = set[k]
	require.True(t, ConsumersOf(pr).Stalled(), "the newest consumer ended again: none is out")
}

func TestCardWithNoConsumersIsNotStalled(t *testing.T) {
	pr := &Card{ID: "c1", Fields: map[string]string{}}
	require.False(t, ConsumersOf(pr).Stalled())
}

func TestViewCardsCarriesConsumerCounts(t *testing.T) {
	pr := &Card{ID: "p1", Row: "s1", Col: Working, Fields: map[string]string{}}
	set := map[string]string{}
	AddConsumer(pr, set, ConsumerRec{Key: "w1", Epoch: 7, Kind: "work", Who: "m1", Taken: "t1"})
	AddConsumer(pr, set, ConsumerRec{Key: "r1", Epoch: 7, Kind: "read", Who: "reader-a", Taken: "t2"})
	for k, val := range set {
		pr.Fields[k] = val
	}
	s := &Snapshot{Work: logCoverTable(Work, []string{"s1"}, pr), Fleet: NewTable(Fleet), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	rows := CardRows(s, CardsFilter{})
	require.Len(t, rows, 1)
	require.Equal(t, "p1", rows[0].ID)
	require.Equal(t, 1, rows[0].Attempts)
	require.Equal(t, 1, rows[0].Reads)
	require.Equal(t, 0, rows[0].Landings)
}

func TestProducerConsumerCheckNamesTheStall(t *testing.T) {
	pr := &Card{ID: "p1", Row: "s1", Col: Working, Fields: map[string]string{}}
	set := map[string]string{}
	AddConsumer(pr, set, ConsumerRec{Key: "w1", Epoch: 7, Kind: "work", Who: "m1", Taken: "t1", Ended: "e1", Outcome: "ok"})
	for k, val := range set {
		pr.Fields[k] = val
	}
	s := &Snapshot{Work: logCoverTable(Work, []string{"s1"}, pr), Fleet: NewTable(Fleet), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	var got []Violation
	for _, v := range Check(s, nil) {
		if v.Rule == 13 {
			got = append(got, v)
		}
	}
	require.Len(t, got, 1)
	require.Contains(t, got[0].Detail, "p1")
	require.Contains(t, got[0].Detail, "newest consumer has ended")
}

func TestConsumersBlockPrintsCountsThenNewestLast(t *testing.T) {
	pr := &Card{ID: "c1", Fields: map[string]string{}}
	set := map[string]string{}
	AddConsumer(pr, set, ConsumerRec{Key: "w1", Epoch: 7, Kind: "work", Who: "m1", Route: "r1", Model: "mdl", Taken: "t1"})
	AddConsumer(pr, set, ConsumerRec{Key: "r1", Epoch: 8, Kind: "read", Who: "reader-a", Taken: "t2"})
	for k, val := range set {
		pr.Fields[k] = val
	}
	lines := ConsumersOf(pr).ConsumerLines()
	require.Len(t, lines, 3)
	require.Contains(t, lines[0], "CONSUMERS attempts=1 reads=1 landings=0")
	require.Contains(t, lines[1], "epoch=7")
	require.Contains(t, lines[1], "kind=work")
	require.Contains(t, lines[2], "epoch=8")
	require.Contains(t, lines[2], "kind=read")
}

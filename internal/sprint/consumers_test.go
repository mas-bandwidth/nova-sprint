package sprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

	set["consumer:w2"] = encodeConsumerRec(ConsumerRec{Key: "w2", Epoch: 8, Kind: "work", Who: "member1", Taken: "t2", Ended: "e2", Outcome: "ok"})
	pr.Fields["consumer:w2"] = set["consumer:w2"]
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

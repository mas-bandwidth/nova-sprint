package run

import (
	"reflect"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type discardPushLog struct{}

func (discardPushLog) PushFailed(string) {}
func TestPushWorkerUsesLatestViewAndUniqueRecipients(t *testing.T) {
	worker := NewPushWorker(nil, "rowan", fakeDeadline{}, discardPushLog{})
	worker.Offer(store.Snapshot{Seq: 1}, time.Unix(1000, 0))
	worker.Offer(store.Snapshot{Seq: 2}, time.Unix(1001, 0))
	got := <-worker.updates
	if got.snapshot.Seq != 2 {
		t.Fatalf("seq=%d", got.snapshot.Seq)
	}
	names := pushRecipients(store.Snapshot{Rows: map[string]store.Row{"alice": {}, "rowan": {}, "sprint": {}}}, "rowan")
	if !reflect.DeepEqual(names, []string{"alice", "rowan"}) {
		t.Fatalf("names=%v", names)
	}
}

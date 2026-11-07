package verbs

import "github.com/mas-bandwidth/nova-sprint/store"

// eventFold is store.Rebuild applied one event at a time; memory is bounded by
// the current records and policy versions, not the length of the event log.
type eventFold struct {
	snap     store.Snapshot
	policies map[int]store.PolicyVersion
}

func newEventFold() *eventFold {
	return &eventFold{snap: emptyFoldSnapshot(1), policies: map[int]store.PolicyVersion{}}
}
func emptyFoldSnapshot(epoch int) store.Snapshot {
	return store.Snapshot{Epoch: epoch, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Rows: map[string]store.Row{}, Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}}
}
func (f *eventFold) add(e store.Event) {
	f.snap.Seq = e.Seq
	if !e.Accepted || e.After == nil {
		return
	}
	if e.After.Epoch != 0 {
		seq, cursors := f.snap.Seq, f.snap.Cursors
		f.snap = emptyFoldSnapshot(e.After.Epoch)
		f.snap.Seq, f.snap.Cursors = seq, cursors
	}
	foldMap(f.snap.Streams, e.After.Streams)
	foldMap(f.snap.Cards, e.After.Cards)
	foldMap(f.snap.Rows, e.After.Rows)
	foldMap(f.snap.Judgements, e.After.Judgements)
	foldMap(f.snap.Cursors, e.After.Cursors)
	foldMap(f.policies, e.After.Policies)
}
func foldMap[K comparable, V any](dst, src map[K]V) {
	for key, value := range src {
		dst[key] = value
	}
}
func (f *eventFold) result() store.Snapshot {
	for version, p := range f.policies {
		if p.Active {
			f.snap.Policy = version
		}
	}
	return f.snap
}

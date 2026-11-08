package verbs

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// eventFold is store.Rebuild applied one event at a time; memory is bounded by
// the current records, not the length of the event log. It starts from a
// fresh store (stopped, store.FreshStop) and lays each event with
// store.Fold, the one fold, so a backup's verification can never drift from
// the store's rebuild (v2.0.8's copy did: a card's model, which the store
// keeps from its cut, was taken from a pre-model binary's after-image).
type eventFold struct{ snap store.Snapshot }

func newEventFold() *eventFold              { return &eventFold{snap: store.Rebuild(nil)} }
func (f *eventFold) add(e store.Event)      { f.snap = store.Fold(f.snap, []store.Event{e}) }
func (f *eventFold) result() store.Snapshot { return f.snap }

// snapshotDifference names what differs between the rebuilt snapshot got
// and the stored one want: each differing top-level field, and for a card
// its id and fields with both values; "" when they are equal. A backup's
// verification says it, so a refusal names the card and field.
func snapshotDifference(got, want store.Snapshot) string {
	var out []string
	gv, wv := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := 0; i < gv.NumField(); i++ {
		name := gv.Type().Field(i).Name
		if name == "Cards" || reflect.DeepEqual(gv.Field(i).Interface(), wv.Field(i).Interface()) {
			continue
		}
		out = append(out, name)
	}
	for _, id := range slices.Sorted(maps.Keys(mergeKeys(got.Cards, want.Cards))) {
		g, inGot := got.Cards[id]
		w, inWant := want.Cards[id]
		switch {
		case !inGot || !inWant:
			out = append(out, fmt.Sprintf("card %s (in the rebuild %v, in the snapshot %v)", id, inGot, inWant))
		case !reflect.DeepEqual(g, w):
			cg, cw := reflect.ValueOf(g), reflect.ValueOf(w)
			for i := 0; i < cg.NumField(); i++ {
				if !reflect.DeepEqual(cg.Field(i).Interface(), cw.Field(i).Interface()) {
					out = append(out, fmt.Sprintf("card %s %s (rebuilt %.80q, snapshot %.80q)", id, cg.Type().Field(i).Name, fmt.Sprint(cg.Field(i).Interface()), fmt.Sprint(cw.Field(i).Interface())))
				}
			}
		}
		if len(out) >= 8 {
			out = append(out, "...")
			break
		}
	}
	return strings.Join(out, "; ")
}

func mergeKeys(a, b map[string]store.Card) map[string]bool {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	return keys
}

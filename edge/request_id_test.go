package edge

import (
	"reflect"
	"testing"
)

// Every edge fact is re-queryable by (epoch, kind, card, sha): a fact asked in
// one epoch must not share a key with the same (kind, card, sha) in another.
func TestRequestIDHasNoEpoch(t *testing.T) {
	t.Parallel()
	if _, ok := reflect.TypeOf(EdgeRequest{}).FieldByName("Epoch"); !ok {
		t.Errorf("EdgeRequest has no Epoch field: a fact asked in epoch 3 and in epoch 4 for the same (kind, card, sha) shares key %s", RequestID(0, "land", "p1.w1", "aaaa"))
	}
	if n := reflect.TypeOf(RequestID).NumIn(); n != 4 {
		t.Errorf("RequestID takes %d inputs, want 4 (epoch, kind, card, sha)", n)
	}
}

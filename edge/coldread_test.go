package edge

import (
	"reflect"
	"testing"
)

// Cold read, 2026-11-06, at 649d1b4c (probes by Rowan's cold-read reviewer).
// One failing probe per confirmed defect.

// Ruling: every edge fact is re-queryable by (epoch, kind, card, sha).
// edge/request.go:16-35 keys on (kind, card, sha) only and EdgeRequest has no epoch.
func TestColdreadDefectRequestIDHasNoEpoch(t *testing.T) {
	t.Parallel()
	if _, ok := reflect.TypeOf(EdgeRequest{}).FieldByName("Epoch"); !ok {
		t.Errorf("EdgeRequest has no Epoch field: a fact asked in epoch 3 and in epoch 4 for the same (kind, card, sha) shares key %s", RequestID(0, "land", "p1.w1", "aaaa"))
	}
	if n := reflect.TypeOf(RequestID).NumIn(); n != 4 {
		t.Errorf("RequestID takes %d inputs, want 4 (epoch, kind, card, sha)", n)
	}
}

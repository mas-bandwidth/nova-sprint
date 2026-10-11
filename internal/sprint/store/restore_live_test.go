package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func liveRow(at int, hidden bool, load, status string) string {
	b, _ := json.Marshal(map[string]any{"at": at, "hidden": hidden, "texts": map[string]string{sprint.Load: load, sprint.Status: status, "width": "16"}})
	return string(b)
}

func liveTable(rev uint64, sort string) string {
	b, _ := json.Marshal(map[string]any{"Name": "t", "Epoch": uint64(1), "Revision": rev, "Sort": sort})
	return string(b)
}

func liveState(rev uint64, at int, load, status string) SprintState {
	return SprintState{Parts: map[string]string{
		"table " + sprint.Fleet:       liveTable(rev, ""),
		"row " + sprint.Fleet + " m1": liveRow(at, false, load, status),
		"cursor":                      "7",
	}}
}

// What a beat rewrites is not a difference: the fleet table's revision, a row's
// place and load text, and an up/down status text.
func TestDiffLiveIgnoresWhatABeatRewrites(t *testing.T) {
	a := liveState(5, 0, "1.2", sprint.Up)
	b := liveState(9, 3, "0.4", sprint.Down)
	assert.NotEmpty(t, a.Diff(b), "the exact comparison sees the beat")
	assert.Empty(t, a.DiffLive(b))
	assert.Empty(t, a.DiffLive(a))
}

// Everything else still differs, so a real change mid-dump is still caught.
func TestDiffLiveStillSeesARealChange(t *testing.T) {
	base := liveState(5, 0, "1.2", sprint.Up)

	held := liveState(5, 0, "1.2", sprint.Held)
	assert.Equal(t, []string{"row " + sprint.Fleet + " m1"}, base.DiffLive(held), "up against held is a hold")
	assert.Equal(t, []string{"row " + sprint.Fleet + " m1"}, held.DiffLive(base), "held against up is a release")
	assert.Empty(t, held.DiffLive(liveState(8, 2, "9", sprint.Held)), "held on both sides, only the beat cells moved")

	other := liveState(5, 0, "1.2", sprint.Up)
	other.Parts["cursor"] = "8"
	assert.Equal(t, []string{"cursor"}, base.DiffLive(other), "a part outside the fleet is exact")

	missing := liveState(5, 0, "1.2", sprint.Up)
	delete(missing.Parts, "row "+sprint.Fleet+" m1")
	assert.Equal(t, []string{"row " + sprint.Fleet + " m1"}, base.DiffLive(missing), "a missing row")
	assert.Equal(t, []string{"row " + sprint.Fleet + " m1"}, missing.DiffLive(base), "an extra row")

	cell := liveState(5, 0, "1.2", sprint.Up)
	cell.Parts["row "+sprint.Fleet+" m1"] = strings.Replace(liveRow(0, false, "1.2", sprint.Up), `"16"`, `"32"`, 1)
	assert.NotEmpty(t, base.DiffLive(cell), "another cell of the row is exact")

	hid := liveState(5, 0, "1.2", sprint.Up)
	hid.Parts["row "+sprint.Fleet+" m1"] = liveRow(0, true, "1.2", sprint.Up)
	assert.NotEmpty(t, base.DiffLive(hid), "hidden is exact")

	tbl := liveState(5, 0, "1.2", sprint.Up)
	tbl.Parts["table "+sprint.Fleet] = liveTable(5, "name")
	assert.NotEmpty(t, base.DiffLive(tbl), "the table's shape is exact, only its revision is not")

	// another table's revision is exact
	a, b := liveState(5, 0, "1", sprint.Up), liveState(5, 0, "1", sprint.Up)
	a.Parts["table work"], b.Parts["table work"] = liveTable(1, ""), liveTable(2, "")
	assert.Equal(t, []string{"table work"}, a.DiffLive(b))
}

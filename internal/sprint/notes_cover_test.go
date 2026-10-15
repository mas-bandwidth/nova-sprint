package sprint

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Unit tests for the pure note helpers of internal/sprint/notes.go:
// SplitOpen, TickEndCounts, Due and Bound. Each covers the function's main
// path and one refusal, with no sleeps, no real time and no network.

func TestNotesCoverSplitOpen(t *testing.T) {
	t.Parallel()
	keys := func(os []Open) []string {
		if len(os) == 0 {
			return nil
		}
		ks := make([]string, len(os))
		for i, o := range os {
			ks[i] = o.Key
		}
		return ks
	}
	tests := []struct {
		name      string
		all       []Open
		judgments []string
		acked     []string
	}{
		{"judgment and acknowledged split into their halves", []Open{
			{Key: "n1|a", Note: Note{Kind: Judgment}}, {Key: "n2|b", Note: Note{Kind: Acknowledged}}, {Key: "n3|c", Note: Note{Kind: Judgment}},
		}, []string{"n1|a", "n3|c"}, []string{"n2|b"}},
		{"no acknowledged note: everything stays a judgment", []Open{
			{Key: "n1|a", Note: Note{Kind: Happened}}, {Key: "n2|b", Note: Note{Kind: Judgment}},
		}, []string{"n1|a", "n2|b"}, nil},
		{"only acknowledged notes: judgments half is empty", []Open{{Key: "n1|a", Note: Note{Kind: Acknowledged}}}, nil, []string{"n1|a"}},
		{"empty open set: both halves empty", nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			judgments, acked := SplitOpen(tt.all)
			assert.Equal(t, tt.judgments, keys(judgments))
			assert.Equal(t, tt.acked, keys(acked))
		})
	}
}

func TestNotesCoverTickEndCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		note  Note
		count bool
	}{
		{"a judgment is an item for the coordinator", Note{Kind: Judgment, Type: NCIRed}, true},
		{"a happened note addressed to someone is an item", Note{Kind: Happened, Type: NSprintDone, To: "coordinator"}, true},
		{"the tick-end note itself is never counted", Note{Kind: Happened, Type: NTickEnd, To: "coordinator"}, false},
		{"a happened note with no addressee is not counted", Note{Kind: Happened, Type: NBatchLanded}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.count, TickEndCounts(tt.note))
		})
	}
}

func TestNotesCoverDue(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	review := at.Add(2 * time.Hour)
	deadline := 4 * time.Hour
	tests := []struct {
		name string
		note Note
		want time.Time
	}{
		{"no review time: the time plus the deadline", Note{At: at}, at.Add(deadline)},
		{"a review time set: due then, not at the deadline", Note{At: at, Review: review, ReviewSet: at}, review},
		{"the sprint is done: no due time", Note{Type: NSprintDone, At: at, Review: review}, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.note.Due(deadline))
		})
	}
}

func TestNotesCoverBound(t *testing.T) {
	t.Parallel()
	all := make([]string, MaxListed+10)
	for i := range all {
		all[i] = "card-" + strconv.Itoa(i)
	}
	tests := []struct {
		name string
		note Note
		want []string
	}{
		{"over the bound: the list is cut and the count keeps the total", Note{Count: len(all), Primaries: all}, all[:MaxListed]},
		{"at the bound: the list is kept whole", Note{Count: MaxListed, Primaries: all[:MaxListed]}, all[:MaxListed]},
		{"under the bound and empty: nothing changes", Note{Count: 2, Primaries: []string{"a", "b"}}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bound := tt.note.Bound()
			assert.Equal(t, tt.want, bound.Primaries)
			assert.Equal(t, tt.note.Count, bound.Count, "Count keeps the total, the cut touches only Primaries")
		})
	}
}

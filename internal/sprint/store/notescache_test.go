package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/redis/go-redis/v9"
)

// fakeInbox is an append-only stream for notesCache.since: ids "<n>-0", each
// entry a note unless its id is in bad (an entry with no note, skipped).
type fakeInbox struct {
	msgs    []redis.XMessage
	fetches int // XRANGE calls
	read    int // entries returned over all fetches: the decode work
}

func (f *fakeInbox) add(id string, noteID string) {
	body, _ := json.Marshal(sprint.Note{ID: noteID, Kind: "happened"})
	f.msgs = append(f.msgs, redis.XMessage{ID: id, Values: map[string]any{"note": string(body)}})
}

func (f *fakeInbox) addBad(id string) {
	f.msgs = append(f.msgs, redis.XMessage{ID: id, Values: map[string]any{"other": "x"}})
}

func (f *fakeInbox) fetch(start string, n int) ([]redis.XMessage, error) {
	f.fetches++
	var out []redis.XMessage
	for _, m := range f.msgs {
		switch {
		case start == "-":
		case start[0] == '(' && !streamIDLess(start[1:], m.ID):
			continue // at or before an exclusive start
		case start[0] != '(' && streamIDLess(m.ID, start):
			continue
		}
		if len(out) == n {
			break
		}
		out = append(out, m)
	}
	f.read += len(out)
	return out, nil
}

func noteIDs(notes []sprint.Note) []string {
	var out []string
	for _, n := range notes {
		out = append(out, n.ID)
	}
	return out
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNotesCacheDecodesEachEntryOnce(t *testing.T) {
	in := &fakeInbox{}
	for i := 1; i <= 5; i++ {
		in.add(string(rune('0'+i))+"-0", "n"+string(rune('0'+i)))
	}
	c := &notesCache{}
	notes, got, err := c.since("", 100, in.fetch)
	if err != nil || !sameIDs(got, []string{"1-0", "2-0", "3-0", "4-0", "5-0"}) || !sameIDs(noteIDs(notes), []string{"n1", "n2", "n3", "n4", "n5"}) {
		t.Fatalf("first read: %v %v %v", err, got, noteIDs(notes))
	}
	if in.fetches != 1 || in.read != 5 {
		t.Fatalf("first read cost: fetches=%d read=%d, want 1 and 5", in.fetches, in.read)
	}
	// the same cursor again: one XRANGE past the tail, nothing to decode
	notes, got, _ = c.since("", 100, in.fetch)
	if !sameIDs(got, []string{"1-0", "2-0", "3-0", "4-0", "5-0"}) || in.fetches != 2 || in.read != 5 {
		t.Fatalf("repeat read: %v fetches=%d read=%d, want 2 and 5 (the fault: every read decoded the whole inbox)", got, in.fetches, in.read)
	}
	// two appended, one of them undecodable: only the new entries are read
	in.add("6-0", "n6")
	in.addBad("7-0")
	in.add("8-0", "n8")
	notes, got, _ = c.since("", 100, in.fetch)
	if !sameIDs(got, []string{"1-0", "2-0", "3-0", "4-0", "5-0", "6-0", "8-0"}) || in.read != 8 {
		t.Fatalf("appended read: %v read=%d, want 8", got, in.read)
	}
	if noteIDs(notes)[6] != "n8" {
		t.Fatalf("notes follow ids: %v", noteIDs(notes))
	}
}

func TestNotesCacheCursorMovesForwardInsideTheWindow(t *testing.T) {
	in := &fakeInbox{}
	for i := 1; i <= 6; i++ {
		in.add(string(rune('0'+i))+"-0", "n"+string(rune('0'+i)))
	}
	c := &notesCache{}
	c.since("", 100, in.fetch)
	before := in.read
	// the coordinator reads to 3-0: the window keeps 4..6 without a refetch of them
	_, got, _ := c.since("3-0", 100, in.fetch)
	if !sameIDs(got, []string{"4-0", "5-0", "6-0"}) || in.read != before {
		t.Fatalf("cursor forward: %v read=%d want %d", got, in.read, before)
	}
	// a cursor on the tail itself: an empty window, one XRANGE
	_, got, _ = c.since("6-0", 100, in.fetch)
	if len(got) != 0 || in.read != before {
		t.Fatalf("cursor at tail: %v read=%d", got, in.read)
	}
	// a cursor moving backward inside the window: served from the window
	_, got, _ = c.since("1-0", 100, in.fetch)
	if !sameIDs(got, []string{"2-0", "3-0", "4-0", "5-0", "6-0"}) || in.read != before {
		t.Fatalf("cursor backward: %v read=%d want %d", got, in.read, before)
	}
}

// The fault behind the second swap of 2026-10-17: the Inbox view, the tick-end
// mark and WaitTickEnd (every tenth of a second) read the inbox from three
// cursors in turn; a window for one cursor was rebuilt at every change.
func TestNotesCacheServesAlternatingCursors(t *testing.T) {
	in := &fakeInbox{}
	for i := 1; i <= 8; i++ {
		in.add(string(rune('0'+i))+"-0", "n"+string(rune('0'+i)))
	}
	c := &notesCache{}
	c.since("", 100, in.fetch) // the view, 20 hours back
	before := in.read
	for round := 0; round < 10; round++ {
		if _, got, _ := c.since("5-0", 100, in.fetch); !sameIDs(got, []string{"6-0", "7-0", "8-0"}) {
			t.Fatalf("tick-end mark: %v", got)
		}
		if _, got, _ := c.since("7-0", 100, in.fetch); !sameIDs(got, []string{"8-0"}) {
			t.Fatalf("wait cursor: %v", got)
		}
		if _, got, _ := c.since("", 100, in.fetch); len(got) != 8 {
			t.Fatalf("view: %v", got)
		}
	}
	if in.read != before {
		t.Fatalf("alternating cursors re-read the stream: read=%d want %d", in.read, before)
	}
	// a cursor past the tail reads only past the tail
	in.add("9-0", "n9")
	if _, got, _ := c.since("8-0", 100, in.fetch); !sameIDs(got, []string{"9-0"}) || in.read != before+1 {
		t.Fatalf("past tail: %v read=%d", got, in.read)
	}
	// a cursor before the window's start rebuilds from it, once
	c2 := &notesCache{}
	c2.since("4-0", 100, in.fetch)
	r := in.read
	if _, got, _ := c2.since("2-0", 100, in.fetch); !sameIDs(got, []string{"3-0", "4-0", "5-0", "6-0", "7-0", "8-0", "9-0"}) || in.read != r+7 {
		t.Fatalf("before window: %v read=%d want %d", got, in.read, r+7)
	}
}

func TestNotesCacheHonoursMax(t *testing.T) {
	in := &fakeInbox{}
	for i := 1; i <= 9; i++ {
		in.add(string(rune('0'+i))+"-0", "n"+string(rune('0'+i)))
	}
	c := &notesCache{}
	_, got, _ := c.since("", 3, in.fetch)
	if !sameIDs(got, []string{"1-0", "2-0", "3-0"}) || in.read != 3 {
		t.Fatalf("max 3: %v read=%d", got, in.read)
	}
	// a wider ask extends the window from its tail, not from the cursor
	_, got, _ = c.since("", 5, in.fetch)
	if !sameIDs(got, []string{"1-0", "2-0", "3-0", "4-0", "5-0"}) || in.read != 5 {
		t.Fatalf("max 5: %v read=%d", got, in.read)
	}
	// a narrower ask is the window's head
	_, got, _ = c.since("", 2, in.fetch)
	if !sameIDs(got, []string{"1-0", "2-0"}) || in.read != 5 {
		t.Fatalf("max 2: %v read=%d", got, in.read)
	}
}

func TestNotesCacheFetchErrorLeavesTheWindow(t *testing.T) {
	in := &fakeInbox{}
	in.add("1-0", "n1")
	c := &notesCache{}
	c.since("", 10, in.fetch)
	boom := errors.New("redis down")
	if _, _, err := c.since("", 10, func(string, int) ([]redis.XMessage, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("error not returned: %v", err)
	}
	_, got, err := c.since("", 10, in.fetch)
	if err != nil || !sameIDs(got, []string{"1-0"}) {
		t.Fatalf("after error: %v %v", err, got)
	}
}

func TestStreamIDLess(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"", "1-0", true}, {"1-0", "", false}, {"", "", false},
		{"1-0", "1-1", true}, {"1-1", "1-0", false}, {"1-9", "2-0", true},
		{"10-0", "9-0", false}, {"1760742000000-3", "1760742000000-3", false},
	} {
		if got := streamIDLess(tc.a, tc.b); got != tc.want {
			t.Errorf("streamIDLess(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

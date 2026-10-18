package store

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// countingDecode is decodeOpenNote counted: the decode work a read does.
type countingDecode struct{ n int }

func (c *countingDecode) decode(raw string) (sprint.Note, error) {
	c.n++
	return decodeOpenNote(raw)
}

func rawNote(t *testing.T, n sprint.Note) string {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The fault of 2026-10-17: every read of the open judgments decoded every
// note again. A note is decoded once, and again only when its text changes.
func TestOpenCacheDecodesEachNoteOnce(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	a := sprint.Note{ID: "n1", Kind: sprint.Judgment, Type: "t", Primaries: []string{"p1"}, At: at}
	b := sprint.Note{ID: "n2", Kind: sprint.Judgment, Type: "t", Primaries: []string{"p2"}, At: at}
	ids := []string{"n1", "n2", "n3"}
	vals := []any{rawNote(t, a), rawNote(t, b), nil} // n3 not found
	c := &openCache{}
	d := &countingDecode{}
	got, err := c.notesOf(ids, vals, d.decode)
	if err != nil {
		t.Fatal(err)
	}
	if d.n != 2 || len(got) != 2 || got["n1"].Primaries[0] != "p1" || got["n2"].ID != "n2" {
		t.Fatalf("first read: %d decodes, %v; want 2 decodes and n1, n2", d.n, got)
	}
	for range 3 {
		if got, err = c.notesOf(ids, vals, d.decode); err != nil || d.n != 2 || !reflect.DeepEqual(got["n1"], a) || !reflect.DeepEqual(got["n2"], b) {
			t.Fatalf("a repeat read: %d decodes (want 2, the fault: every read decoded every note), %v %v", d.n, got, err)
		}
	}
	// a note rewritten in place (SetReview) is decoded again, and only it
	b.Review, b.ReviewSet = at.Add(time.Hour), at
	vals[1] = rawNote(t, b)
	got, _ = c.notesOf(ids, vals, d.decode)
	if d.n != 3 || !got["n2"].Review.Equal(at.Add(time.Hour)) || !reflect.DeepEqual(got["n1"], a) {
		t.Fatalf("a rewritten note: %d decodes (want 3), n2's review %s", d.n, got["n2"].Review)
	}
	// a note the index no longer names leaves the cache; one named again is decoded again
	if _, err := c.notesOf(ids[1:2], vals[1:2], d.decode); err != nil || len(c.notes) != 1 {
		t.Fatalf("the cache holds %d notes after a read of one, want 1 (%v)", len(c.notes), err)
	}
	if _, _ = c.notesOf(ids, vals, d.decode); d.n != 4 {
		t.Fatalf("a note named again: %d decodes, want 4", d.n)
	}
	// an unreadable note is the read's error, as before the cache
	if _, err := c.notesOf([]string{"bad"}, []any{"{"}, d.decode); err == nil {
		t.Fatal("an unreadable note read without an error")
	}
}

// fillValue sets every field of v, reached through pointers and slices, to a
// value that is not its zero: the note a test copies with every part held.
func fillValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint64, reflect.Uint32:
		v.SetUint(7)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fillValue(p.Elem())
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillValue(s.Index(0))
		v.Set(s)
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(time.Unix(1000, 0).UTC()))
			return
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				fillValue(v.Field(i))
			}
		}
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		k, e := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fillValue(k)
		fillValue(e)
		m.SetMapIndex(k, e)
		v.Set(m)
	}
}

// spoil changes every string, number and flag reachable from v in place.
func spoil(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("spoiled")
	case reflect.Bool:
		v.SetBool(false)
	case reflect.Int, reflect.Int64, reflect.Int32:
		v.SetInt(-1)
	case reflect.Uint, reflect.Uint64, reflect.Uint32:
		v.SetUint(0)
	case reflect.Pointer:
		if !v.IsNil() {
			spoil(v.Elem())
		}
	case reflect.Slice:
		for i := range v.Len() {
			spoil(v.Index(i))
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				spoil(v.Field(i))
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			v.SetMapIndex(k, reflect.Zero(v.Type().Elem()))
		}
	}
}

// A note the cache gives is the caller's own: a caller that changes it in
// place, through any slice or pointer, changes nothing the next read gives.
// Every field of the note is filled, so a field added later that cloneNote
// does not copy fails here.
func TestOpenCacheGivesCopies(t *testing.T) {
	var n sprint.Note
	fillValue(reflect.ValueOf(&n).Elem())
	raw := rawNote(t, n)
	want, err := decodeOpenNote(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := &openCache{}
	d := &countingDecode{}
	got, err := c.notesOf([]string{"x"}, []any{raw}, d.decode)
	if err != nil {
		t.Fatal(err)
	}
	g := got["x"]
	spoil(reflect.ValueOf(&g).Elem())
	again, _ := c.notesOf([]string{"x"}, []any{raw}, d.decode)
	if d.n != 1 {
		t.Fatalf("%d decodes, want 1", d.n)
	}
	if !reflect.DeepEqual(again["x"], want) {
		t.Fatalf("a caller's change to its note reached the cache:\n got %+v\nwant %+v", again["x"], want)
	}
}

// The caches are bounded: a process that reads many epochs' notes keys (each clear moves the
// key) or many clients holds at most openCachesMax caches, not one per key for ever (cold read
// of PR #486, 2026-10-17). Not parallel: openCaches is the process's.
func TestOpenCachesAreBounded(t *testing.T) {
	first := openCacheFor(nil, "bounded-notes:1")
	if openCacheFor(nil, "bounded-notes:1") != first {
		t.Fatal("the same key gave another cache")
	}
	for e := 2; e <= 3*openCachesMax; e++ {
		openCacheFor(nil, fmt.Sprintf("bounded-notes:%d", e))
		openCaches.mu.Lock()
		n := len(openCaches.m)
		openCaches.mu.Unlock()
		if n > openCachesMax {
			t.Fatalf("%d caches after %d keys, want at most %d", n, e, openCachesMax)
		}
	}
}

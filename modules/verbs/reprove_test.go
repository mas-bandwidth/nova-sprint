package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestReproveStreamIsChecksAdmission (item 4): run's re-proof of one stream is
// check's admission of its waiting and ready producers, each refusal with
// its fix on one line; a read that could not be made (an I/O failure, not a
// refusal) fails the proof instead of refusing every card.
func TestReproveStreamIsChecksAdmission(t *testing.T) {
	t.Parallel()
	s := sprint()
	undealtSprint(&s)
	c := s.Cards["p1"]
	c.Paths = []string{"z/q.go"}
	s.Cards["p1"] = c
	w := worldFrom(t, s)
	healthy(t, w)
	snap := w.snap(t)
	got, err := w.e.ReproveStream(context.Background(), snap, "s")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.Contains(got["p1"], "card p1: PATHS z/q.go: neither it nor its directory z/ is there") || !strings.Contains(got["p1"], "then add the card again") {
		t.Fatalf("refused %q", got)
	}
	if got, err := w.e.ReproveStream(context.Background(), snap, "other"); err != nil || len(got) != 0 {
		t.Fatalf("a stream with no such card: %q %v", got, err)
	}
	w.origin.err = errors.New("network down")
	if got, err := w.e.ReproveStream(context.Background(), snap, "s"); err == nil {
		t.Fatalf("an unreadable tree refused the cards: %q", got)
	}
}

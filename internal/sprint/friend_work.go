package sprint

import (
	"log"
	"sync"
	"time"
)

// The evidence of a friend's work, reported as a fact and never a state: the sprint's
// friend states are up and down (the nova-hook record) and held (the coordinator's hold),
// and nothing here changes one or keeps a card from her (the owner, 2026-10-14, v1.3.6:
// "there is only up, down, held from the sprint POV").

// FriendWork is the evidence of a friend's work the tables do not hold: what her daemon's
// work report carries. Each is zero when there is none.
type FriendWork struct {
	// Active is the newest write under her working directory and outbox as her daemon's
	// last walk found it (FriendReport.Active). The walk is bounded in files and time, so
	// it can answer with an old file while her outbox holds new ones: never the only field.
	Active time.Time
	// Running is her beat's time while it names cards running (friend beat --running).
	Running time.Time
}

// FriendWorked is the newest evidence that friend f is at work, and what it is: her
// session's writes, her beat naming running cards, a card done on her row, a report of hers
// (FieldReported on a card of her row), and the coordinator's view of her row ("active").
// Card progress and takes are the cards', not hers (FriendCardMoved). Zero and "" when there
// is none. It is a fact the coordinator's view reports (the friend idle item), never a
// friend state: whether she is there is the nova-hook record's alone, up or down, and held
// is the coordinator's hold (FriendHookStatus). On 2026-10-06 one field, her daemon's walk
// of her working directory, read three days old while her outbox had reports that hour, so
// the newest of every field is read, never one alone.
func FriendWorked(s *Snapshot, f string, w FriendWork) (time.Time, string) {
	var at time.Time
	what := ""
	see := func(t time.Time, word string) {
		// A stamp after the server's clock is that clock. Zero now is not a
		// clock, and clamping to it would drop the evidence (see uses After).
		if s != nil && !s.Now.IsZero() && t.After(s.Now) {
			noteFutureEvidence(f, t, s.Now, word)
			t = s.Now
		}
		if t.After(at) {
			at, what = t, word
		}
	}
	see(w.Active, "session write")
	see(w.Running, "beat naming running cards")
	if s == nil || s.Fleet == nil {
		return at, what
	}
	row := FriendRow(f)
	for _, c := range append(append(append([]*Card(nil), s.Fleet.Cell(row, Finished)...), s.Fleet.Cell(row, DoneOK)...), s.Fleet.Cell(row, DoneFailed)...) {
		see(stampAt(c, "finished"), "finish")
	}
	for _, col := range []string{Ready, Working, DoneOK, DoneFailed} {
		for _, c := range s.Fleet.Cell(row, col) {
			see(stampAt(c, FieldReported), "report")
		}
	}
	if ctl := s.MemberCtl(row); ctl != nil {
		see(stampAt(ctl, "active"), "session write")
	}
	if texts, ok := s.Fleet.Texts[row]; ok {
		if t, err := time.Parse(time.RFC3339, texts[Active]); err == nil {
			see(t, "session write")
		}
	}
	return at, what
}

// FriendCardMoved is the newest move of a card of hers that she or her daemon made: a
// progress stamp, or a take, on a card ready or working on her row; zero when none.
func FriendCardMoved(s *Snapshot, f string) time.Time {
	var at time.Time
	if s == nil || s.Fleet == nil {
		return at
	}
	row := FriendRow(f)
	for _, c := range append(append([]*Card(nil), s.Fleet.Cell(row, Ready)...), s.Fleet.Cell(row, Working)...) {
		for _, k := range []string{FieldProgress, "taken"} {
			if t := stampAt(c, k); t.After(at) {
				at = t
			}
		}
	}
	return at
}

// futureEvidenceOnce is the log of a stamp dated after the server's clock,
// once per friend and stamp. The view calls FriendWorked every read, and a
// parallel test must not swap a process logger to count the line.
var futureEvidenceOnce sync.Map

func futureEvidenceKey(friend string, stamped time.Time) string {
	return friend + "\x00" + stamped.UTC().Format(time.RFC3339Nano)
}

// noteFutureEvidence counts a future stamp as the server's now and logs that
// once. stamped is the evidence time before the clamp.
func noteFutureEvidence(friend string, stamped, now time.Time, word string) {
	if _, loaded := futureEvidenceOnce.LoadOrStore(futureEvidenceKey(friend, stamped), 1); loaded {
		return
	}
	log.Printf("friend %s evidence %s dated %s, after the server's clock %s: counted as now",
		friend, word, stamped.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
}

package run

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Notice is one line for a recipient, under a subject, in a judgement group.
type Notice struct{ Subject, Group, Line string }

// pending is a notice and the seq of the event that raised it.
type pending struct {
	seq    int64
	notice Notice
}

// Pusher is the one pusher (drift row 6): each committed event that needs
// someone becomes a notice for that recipient as run folds it, and each
// tick each recipient gets at most one envelope of at most 4 KB with every
// notice since its cursor; past the bound, one line names `next`
// (overflow-to-next, the envelope model 4eecaadd). The cursor `push:<name>`
// moves to the last seq the envelope covers only after `nova-bus send`
// exits 0 and names the message (LAYER-6 guarantee 4); a failed send is not
// sent again within the tick, and the unmoved cursor is the only resend
// (drift row 19). Nothing is pushed for an event before its op commits:
// the notices come from the log. A nudge (steps.Nudge: a card in a row's
// hand it has not acknowledged) is the one notice not from the log: the
// tick derives it from the view, it rides in that tick's envelope and moves
// no cursor, and one not sent is derived again by the next tick.
type Pusher struct {
	queue  map[string][]pending
	nudges map[string][]Notice
}

func NewPusher() *Pusher {
	return &Pusher{queue: map[string][]pending{}, nudges: map[string][]Notice{}}
}

// Nudge queues this tick's nudges, replacing the last tick's.
func (p *Pusher) Nudge(ns []steps.Nudge) {
	p.nudges = map[string][]Notice{}
	for _, n := range ns {
		p.nudges[n.Row] = append(p.nudges[n.Row], Notice{Subject: "NUDGE", Line: n.Line})
	}
}

// Observe takes one folded event, with the snapshot after it, for every
// recipient whose cursor is behind it.
func (p *Pusher) Observe(e store.Event, after store.Snapshot) {
	notices := EventNotices{Coordinator: Coordinator(after)}
	for _, r := range recipients(after) {
		name, err := pushCursorName(r)
		if err != nil || after.Cursors[name] >= e.Seq {
			continue
		}
		ns, err := notices.ForEvent(e, after, r)
		if err != nil {
			continue
		}
		for _, n := range ns {
			p.queue[r] = append(p.queue[r], pending{e.Seq, n})
		}
	}
}

// Coordinator is the row the sprint hook recorded as coordinator (one per
// sprint; the hook refuses a second), or "".
func Coordinator(s store.Snapshot) string {
	for _, n := range slices.Sorted(maps.Keys(s.Rows)) {
		if r := s.Rows[n]; r.Session != "" && r.Role == "coordinator" {
			return n
		}
	}
	return ""
}

// CatchUp reads, once at run's cold start, the events between each
// recipient's cursor and the cold snapshot (bounded pages from the cursor,
// never from seq 0) and queues their notices against the cold snapshot.
func (p *Pusher) CatchUp(ctx context.Context, st store.Store, cold store.Snapshot) error {
	from := cold.Seq
	for _, r := range recipients(cold) {
		if name, err := pushCursorName(r); err == nil {
			from = min(from, cold.Cursors[name])
		}
	}
	for from < cold.Seq {
		events, err := st.Events(ctx, from, store.MaxEvents)
		if err != nil {
			return fmt.Errorf("run: push catch-up: %w", err)
		}
		if len(events) == 0 {
			return nil
		}
		for _, e := range events {
			if e.Seq <= cold.Seq {
				p.Observe(e, cold)
			}
		}
		from = events[len(events)-1].Seq
	}
	return nil
}

// PushFailure is a recipient whose envelope was not sent or whose cursor did
// not move, and why.
type PushFailure struct {
	To  string
	Err error
}

// Push sends each recipient's envelope and moves its cursor (SEAMS 6). It
// returns the recipients whose send failed, with the cause, for the PUSH
// FAILED lines.
func (p *Pusher) Push(ctx context.Context, b bus.Bus, st store.Store, epoch int, now time.Time) []PushFailure {
	var failed []PushFailure
	to := maps.Clone(p.queue)
	for r := range p.nudges {
		to[r] = p.queue[r]
	}
	for _, r := range slices.Sorted(maps.Keys(to)) {
		q := p.queue[r]
		for _, n := range p.nudges[r] {
			q = append(q, pending{0, n})
		}
		if len(q) == 0 {
			continue
		}
		subject, body := envelope(r, q)
		if _, err := b.Send(ctx, r, subject, body); err != nil {
			failed = append(failed, PushFailure{To: r, Err: err})
			continue
		}
		if events := p.queue[r]; len(events) > 0 {
			if err := setCursor(ctx, st, r, events[len(events)-1].seq, epoch, now); err != nil {
				failed = append(failed, PushFailure{To: r, Err: err})
				continue
			}
		}
		delete(p.queue, r)
	}
	p.nudges = map[string][]Notice{}
	return failed
}

// envelope is one message: the subject of its first notice (or SPRINT for
// several subjects), and the lines grouped by subject and judgement group,
// cut at 4 KB with `and N more: nova-sprint next --as <r>`.
func envelope(r string, q []pending) (string, string) {
	subject := q[0].notice.Subject
	var lines []string
	for _, x := range q {
		if x.notice.Subject != subject {
			subject = "SPRINT"
		}
		lines = append(lines, x.notice.Line)
	}
	more := func(n int) string { return fmt.Sprintf("and %d more: nova-sprint next --as %s", n, r) }
	reserve := len(more(len(lines))) + 1
	var b strings.Builder
	for i, l := range lines {
		if b.Len()+len(l)+1+reserve > bus.MaxMessageBytes {
			b.WriteString(more(len(lines) - i))
			return subject, b.String()
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return subject, strings.TrimSuffix(b.String(), "\n")
}

// setCursor moves push:<r> to seq in one op; its id is fixed by the
// recipient and seq, so a retry after an unknown outcome replays it.
func setCursor(ctx context.Context, st store.Store, r string, seq int64, epoch int, now time.Time) error {
	name, err := pushCursorName(r)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(r))
	op := store.Op{ID: fmt.Sprintf("push.%x.%d", sum[:8], seq), Epoch: epoch, Actor: "sprint", Kind: "push-cursor", At: now,
		Writes: []store.Write{store.SetCursor{Name: name, Seq: seq}}}
	rec, err := st.Apply(ctx, op)
	if err != nil {
		return err
	}
	if !rec.Accepted {
		return fmt.Errorf("run: push cursor %s refused: %s", name, rec.Reason)
	}
	return nil
}

// recipients are the rows hooked in (a session recorded); the coordinator
// is one of them.
func recipients(s store.Snapshot) []string {
	var names []string
	for _, n := range slices.Sorted(maps.Keys(s.Rows)) {
		if n != "sprint" && s.Rows[n].Session != "" {
			names = append(names, n)
		}
	}
	return names
}

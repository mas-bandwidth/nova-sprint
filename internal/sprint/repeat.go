package sprint

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// A repeated refusal is an alarm (docs/SPEC-SPRINT.md section 8, "A repeated refusal is
// an alarm"; the owner, 2026-10-05, after the server refused the work table's whole queued
// write 1,460 times in 3 minutes, and one land batch every few seconds for hours, each a
// log line and nothing more: "Why then does everything back up, and then suddenly you
// look, and go WHOOSP THIS THING HAPPENED 10000 TIMES"). A refusal or a failure the server
// sees is one occurrence of its cause: the verb (or the tick's part), the card, stream or
// friend it is about, and the reason, word for word. The third occurrence of one cause
// within RepeatWindow raises one judgment to the coordinator naming the cause, the count,
// the first and the last time and what it holds up; each occurrence after rewrites that
// judgment in place with the count risen; RepeatWindow with no occurrence is the cause
// stopped, and the judgment closes itself, with one happened note to the coordinator.
// Repeats is the count, kept by the server's run loop in memory; RepeatPlan is the step
// that writes what it says.

const (
	// NRepeated is the judgment of one cause refused or failing again and again.
	NRepeated = "the same refusal keeps repeating"
	// NRepeatStopped is the happened note, to the coordinator, that a repeated cause stopped.
	NRepeatStopped = "a repeated refusal stopped"
	// RepeatAt is the occurrences of one cause within RepeatWindow that raise its judgment.
	RepeatAt = 3
	// RepeatWindow is the window RepeatAt occurrences fall in, and the quiet that ends an
	// episode.
	RepeatWindow = 5 * time.Minute
	// RepeatVerb is the verb of the step that writes the repeats' judgments.
	RepeatVerb = "repeat alarm"
	// repeatMark separates a repeat judgment's cause from its facts in its what.
	repeatMark = "; it happened "
)

// RepeatDecisions are a repeat judgment's decisions. It is not the tick's (TickKept): ack
// answers it, and the count raises nothing again for that episode (RepeatState.Written);
// the cause stopping closes it.
var RepeatDecisions = []string{"look", "ack"}

// RepeatCause is what one refusal or failure is about: the same verb, the same subject
// and the same reason are the same cause.
type RepeatCause struct {
	Verb    string // the verb, or the tick's part ("tick drain", "land", "friend reconcile")
	Subject string // the card, stream or friend; "" for the verb as a whole
	Reason  string // the reason, word for word
}

// Text is the cause in words: the verb, its subject, and the reason.
func (c RepeatCause) Text() string {
	head := c.Verb
	if c.Subject != "" {
		head += " " + c.Subject
	}
	return head + ": " + oneLine(c.Reason)
}

// key is the cause as one string: what episodes are kept by.
func (c RepeatCause) key() string { return c.Verb + "\x00" + c.Subject + "\x00" + c.Reason }

// repeatEpisode is one cause's run of occurrences, none more than RepeatWindow after the
// one before.
type repeatEpisode struct {
	cause       RepeatCause
	recent      []time.Time // the last RepeatAt occurrences
	first, last time.Time
	count       int
	holds       []string // what it holds up, as the last occurrence said
	raised      bool
	written     int // the count its judgment last said (Done)
}

// Repeats counts each cause's occurrences, for the server's run loop: safe for the loop
// and the land loop to observe at once.
type Repeats struct {
	mu      sync.Mutex
	began   time.Time
	eps     map[string]*repeatEpisode
	settled bool // a plan made once the count had run RepeatWindow was written (Done)
	// renewed is the written episodes a new occurrence replaced before a plan saw them
	// stopped (Observe): their judgments are closed by the next plan (Due).
	renewed int
}

// NewRepeats is a count begun at now: an open judgment of a cause it has not seen is
// closed once RepeatWindow has passed since (the cause was not seen for that long).
func NewRepeats(now time.Time) *Repeats {
	return &Repeats{began: now, eps: map[string]*repeatEpisode{}}
}

// Observe is one occurrence of c at at, holding up holds (cards, streams; may be none).
func (r *Repeats) Observe(c RepeatCause, holds []string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.eps[c.key()]
	if e == nil || at.Sub(e.last) >= RepeatWindow {
		if e != nil && e.written > 0 {
			r.renewed++
		}
		e = &repeatEpisode{cause: c, first: at}
		r.eps[c.key()] = e
	}
	e.count++
	e.last = at
	e.recent = append(e.recent, at)
	if len(e.recent) > RepeatAt {
		e.recent = e.recent[len(e.recent)-RepeatAt:]
	}
	if len(holds) > 0 {
		e.holds = slices.Clone(holds)
	}
	if !e.raised && len(e.recent) == RepeatAt && at.Sub(e.recent[0]) <= RepeatWindow {
		e.raised = true
	}
}

// RepeatState is one raised cause as the count holds it.
type RepeatState struct {
	Cause       RepeatCause
	Count       int
	First, Last time.Time
	Holds       []string
	// Written says a judgment of this episode was written (Done): one not open now was
	// answered, and is not raised again until the cause stops and comes back.
	Written bool
}

// Raised is the causes raised and not stopped at now, the first time of every episode
// still live (raised or counting) by its cause's words, and whether the count has run
// RepeatWindow: what RepeatPlan writes.
func (r *Repeats) Raised(now time.Time) (raised []RepeatState, live map[string]time.Time, settled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	live = map[string]time.Time{}
	for _, e := range r.eps {
		if now.Sub(e.last) >= RepeatWindow {
			continue
		}
		live[e.cause.Text()] = e.first
		if e.raised {
			raised = append(raised, RepeatState{Cause: e.cause, Count: e.count, First: e.first, Last: e.last, Holds: slices.Clone(e.holds), Written: e.written > 0})
		}
	}
	slices.SortFunc(raised, func(a, b RepeatState) int { return strings.Compare(a.Cause.key(), b.Cause.key()) })
	return raised, live, now.Sub(r.began) >= RepeatWindow
}

// Due says a plan at now has something to write: a raised cause whose count its judgment
// does not yet say, a cause stopped, or the first plan once the count has run
// RepeatWindow (which closes what an earlier server left open). A loop that flushes only
// when it is due reads the store for it only then.
func (r *Repeats) Due(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.renewed > 0 || !r.settled && now.Sub(r.began) >= RepeatWindow {
		return true
	}
	for _, e := range r.eps {
		if now.Sub(e.last) >= RepeatWindow || e.raised && e.written != e.count {
			return true
		}
	}
	return false
}

// Done is a plan at now written: each raised cause's count said, each stopped cause
// forgotten, and the count settled once it has run RepeatWindow.
func (r *Repeats) Done(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, e := range r.eps {
		switch {
		case now.Sub(e.last) >= RepeatWindow:
			delete(r.eps, k)
		case e.raised:
			e.written = e.count
		}
	}
	r.settled = r.settled || now.Sub(r.began) >= RepeatWindow
	r.renewed = 0
}

// RepeatWhat is a raised cause's judgment: the cause, the count, the first and last time,
// and what it holds up.
func RepeatWhat(rs RepeatState) string {
	holds := "nothing it names"
	if len(rs.Holds) > 0 {
		holds = Preview(rs.Holds, ", ")
	}
	return fmt.Sprintf("%s%s%d times, first %s, last %s; it holds up %s; it is stuck: the same refusal again will not clear it; look at the cause, fix it, and this closes itself %s after the last",
		rs.Cause.Text(), repeatMark, rs.Count, stamp(rs.First), stamp(rs.Last), holds, RepeatWindow)
}

// repeatCauseOf is the cause's words a repeat judgment's what opens with.
func repeatCauseOf(what string) string {
	if i := strings.LastIndex(what, repeatMark); i >= 0 {
		return what[:i]
	}
	return what
}

// repeatLastOf is the last time a repeat judgment's what says; zero when it says none.
func repeatLastOf(what string) time.Time {
	i := strings.LastIndex(what, repeatMark)
	if i < 0 {
		return time.Time{}
	}
	_, rest, ok := strings.Cut(what[i:], ", last ")
	if !ok {
		return time.Time{}
	}
	at, _, _ := strings.Cut(rest, ";")
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return time.Time{}
	}
	return t
}

// RepeatPlan is the step that writes what the count says, on the open judgments s holds.
// First each open or acknowledged one whose episode has stopped closes, with one happened
// note to the coordinator: its cause not seen for RepeatWindow (or never seen by a count
// that has run RepeatWindow, for one an earlier server left), or seen again only after a
// quiet of RepeatWindow since the last it says (a new episode the loop had not yet seen
// the old one stop before). Then each raised cause with none open gets a judgment, unless
// its episode's was answered (ack); and one open says the count risen in place. Nothing
// to do is an empty plan.
func RepeatPlan(s *Snapshot, r *Repeats, who string) Plan {
	var p Plan
	raised, live, settled := r.Raised(s.Now)
	var held []Open
	held = append(held, s.Open...)
	held = append(held, s.Acked...)
	open := map[string]Open{}
	for _, o := range held {
		if o.Note.Type != NRepeated {
			continue
		}
		cause := repeatCauseOf(o.Note.What)
		first, seen := live[cause]
		said := repeatLastOf(o.Note.What)
		renewed := seen && !said.IsZero() && first.Sub(said) >= RepeatWindow
		if seen && !renewed || !seen && !settled {
			open[cause] = o
			continue
		}
		p.Closes = append(p.Closes, o)
		p.Notes = append(p.Notes, Note{Kind: Happened, Type: NRepeatStopped, Who: who, To: s.Coordinator, At: s.Now,
			What: fmt.Sprintf("%s: not seen for %s; %s closes itself", cause, RepeatWindow, o.Note.ID)})
	}
	for _, rs := range raised {
		what := RepeatWhat(rs)
		o, ok := open[rs.Cause.Text()]
		if !ok && rs.Written {
			continue // answered (ack): quiet until the cause stops
		}
		if !ok {
			n := Note{Kind: Judgment, Type: NRepeated, Primaries: slices.Clone(rs.Holds), Count: len(rs.Holds), What: what,
				Who: who, At: s.Now, SprintLevel: true, Marked: true, Before: rs.Count - 1,
				Decisions: slices.Clone(RepeatDecisions)}
			if len(n.Primaries) > MaxListed {
				n.Primaries = n.Primaries[:MaxListed]
			}
			p.Notes = append(p.Notes, n)
			continue
		}
		if o.Note.Kind == Judgment && o.Note.What != what {
			n := o.Note
			n.What, n.Before = what, rs.Count-1
			p.Updates = append(p.Updates, n)
		}
	}
	return p
}

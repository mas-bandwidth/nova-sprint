package sprint

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The landing stall (docs/SPEC-SPRINT.md, the judgments section; the owner,
// 2026-10-11: landed 68/294 +0/30m for half an hour while 76 cards sat in
// review and 68 in fix, and no judgment reached the seat). When cards wait in
// review or merging and no card has landed for LandingStallBound of running
// time, the tick end raises one judgment, NLandingStall, to the coordinator
// with a breakdown of why the cards wait; the episode lives on the work
// table's properties, is re-raised each bound it still holds, and clears on
// the next landing with a HAPPENED NLandingAgain. The model is its own card,
// tla-sprintrules-landing-stall-liveness.
const (
	// NLandingStall is the judgment that no card has landed while cards wait.
	NLandingStall = "landing stalled"
	// NLandingAgain is the happened note that a landing broke the stall.
	NLandingAgain = "landing again"
	// PartLandingStall is the alarm's display name, on the tick's part report.
	PartLandingStall = "landing-stall"

	// PropLastLanding is the work table's stamp of the last card that landed.
	PropLastLanding = "last_landing"
	// PropLandRefusal is the work table's record of the lander's last refusal,
	// the card and the reason.
	PropLandRefusal = "land_refusal"
	// PropLandingStallSince and PropLandingStallSaid are the episode's stamps
	// on the work table: when it began and when its judgment was last pushed.
	PropLandingStallSince = "landing_stall_since"
	PropLandingStallSaid  = "landing_stall_said"
	// PropLandingStallBound is the work table's property: the landing-stall
	// bound, a duration; none set is LandingStallBound, the default.
	PropLandingStallBound = "landing_stall_bound"
)

// LandingStallBound is how long, in running time, cards may wait with no
// landing before the judgment is raised; the owner's number ("10 minute stall
// watch is too long. 1 minute."), and the default.
const LandingStallBound = time.Minute

// landingStallBound is the landing-stall bound: the sprint's setting
// (PropLandingStallBound), else LandingStallBound.
func (s *Snapshot) landingStallBound() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropLandingStallBound); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return LandingStallBound
}

// NLanding is the happened note a landing pushes to the coordinator: "landed
// <card>", one line per card. The owner, 2026-10-11: "you should also get this
// push notification, so you as coordinator can read the card that landed and
// keep on top of things".
const NLanding = "landed"

// LandingTitleField and LandingTextField are the fields the member writes its
// ledger entry's one-line :title and :text on: what the coordinator reads in
// the landing push without opening the ledger.
const (
	LandingTitleField = "title"
	LandingTextField  = "text"
)

// landingPush is the coordinator's push for a batch of landings
// (docs/SPEC-SPRINT.md, the judgments section): one happened note, one line a
// card, "landed <card>" with its stream, its head, the one-line :title and
// :text of its ledger entry, and the attempts and reads it took. A batch in
// one tick is one push with one line per card, addressed to the coordinator,
// needing no ack beyond the hook's.
func landingPush(s *Snapshot, stream, who string, landing []*Card, ledger map[string]LandingLedgerEntry, now time.Time) Note {
	var lines []string
	for _, c := range landing {
		reads := 0
		if s.Readers != nil {
			reads = len(s.Readers.Of(c.ID))
		}
		title, text := c.F(LandingTitleField), c.F(LandingTextField)
		if entry, ok := ledger[c.ID]; ok {
			title, text = entry.Title, entry.Text
			if entry.Error != "" {
				title, text = "", "ledger entry unavailable: "+entry.Error
			}
		}
		lines = append(lines, fmt.Sprintf("landed %s (stream %s, head %s) %s %s; attempts %d, reads %d",
			c.ID, stream, orDash(shortSHA(c.F("head"))), oneLine(title), oneLine(text),
			c.Int("attempt"), reads))
	}
	n := happened(NLanding, stream, now)
	n.Who, n.To = who, s.Coordinator
	n.What = strings.Join(lines, "\n")
	return n
}

// openJudgmentOf is the open judgment of a type, if one is open.
func openJudgmentOf(s *Snapshot, typ string) (Open, bool) {
	for _, o := range s.Open {
		if o.Note.Kind == Judgment && o.Note.Type == typ {
			return o, true
		}
	}
	return Open{}, false
}

// landingStallWhat is the stall judgment's text: the reads out by reader with
// the oldest age, the cards waiting on the coordinator's judgment (the seat's
// own stall named as such when every review card waits on it), the accepted
// and not merged with each stream's merge state, the lander's last refusal,
// and the base's red CI.
func landingStallWhat(s *Snapshot, r TickReq, d time.Duration) string {
	type readerOut struct {
		n      int
		oldest time.Time
	}
	byReader := map[string]*readerOut{}
	for _, c := range s.Readers.Column(Asked) {
		rd := c.F("reader")
		g := byReader[rd]
		if g == nil {
			g = &readerOut{}
			byReader[rd] = g
		}
		g.n++
		if at := stampAt(c, "asked"); !at.IsZero() && (g.oldest.IsZero() || at.Before(g.oldest)) {
			g.oldest = at
		}
	}
	var readers []string
	for rd := range byReader {
		readers = append(readers, rd)
	}
	sort.Strings(readers)
	var rparts []string
	for _, rd := range readers {
		g := byReader[rd]
		age := ""
		if !g.oldest.IsZero() {
			age = ", oldest " + s.Now.Sub(g.oldest).Round(time.Second).String()
		}
		rparts = append(rparts, fmt.Sprintf("%s=%d%s", orDash(rd), g.n, age))
	}
	readsOut := "0"
	if len(rparts) > 0 {
		readsOut = strings.Join(rparts, " ")
	}
	review := s.Work.Column(Review)
	// cards waiting on the coordinator's judgment: open judgments other than
	// this one, and how many review cards wait on one.
	judged, onReview := 0, 0
	oldestJ, oldestAlias := time.Time{}, ""
	for _, o := range s.Open {
		if o.Note.Kind != Judgment || o.Note.Type == NLandingStall {
			continue
		}
		judged++
		onReview += len(o.Note.Primaries)
		if oldestJ.IsZero() || o.Note.At.Before(oldestJ) {
			oldestJ, oldestAlias = o.Note.At, o.Subject()
		}
	}
	seatStall := ""
	if len(review) > 0 && onReview >= len(review) {
		seatStall = "; the seat's stall: every review card waits on the coordinator's judgment"
	}
	// accepted and not merged, with each stream's merge state.
	merging := s.Work.Column(Merging)
	var states []string
	for _, c := range merging {
		st := "?"
		if ctl := s.StreamCtl(c.Row); ctl != nil {
			st = orDash(ctl.F("state"))
		}
		states = append(states, c.Row+":"+st)
	}
	sort.Strings(states)
	mergeState := strings.Join(states, " ")
	if mergeState == "" {
		mergeState = "none merging"
	}
	refusal := "none"
	if v, ok := s.Work.Prop(PropLandRefusal); ok && v != "" {
		refusal = v
	}
	ci := "green"
	var reds []string
	for _, f := range r.BaseCI {
		if f.Red {
			reds = append(reds, f.Repo+"@"+f.Base+" ("+Preview(f.Failed, ", ")+")")
		}
	}
	if len(reds) > 0 {
		ci = strings.Join(reds, ", ")
	}
	return fmt.Sprintf("no card has landed in %s with %d in review and %d merging; reads out: %s; waiting on the coordinator's judgment: %d (oldest %s%s); accepted and not merged: %d (%s); the lander's last refusal: %s; CI red: %s%s",
		d.Round(time.Second), len(review), len(merging), readsOut, judged, orDash(oldestAlias), seatStall, len(merging), mergeState, refusal, ci, "")
}

// TickLandingStall is the tick end's landing-stall alarm (docs/SPEC-SPRINT.md,
// the judgments section), run from the deadlines part (TickDeadlines) so it is
// watched whether or not the idle alarm is on: with cards waiting in review or
// merging and the work table's last landing the bound (LandingStallBound, the
// default, or the setting PropLandingStallBound) or more ago, it raises one
// NLandingStall judgment to the coordinator with the breakdown
// (landingStallWhat), re-raised each bound the condition still holds (the
// open judgment rewritten in place), and it clears the episode when a later
// landing breaks it, pushing NLandingAgain "landing again after <d>". The
// episode lives on the work table's properties so it survives ticks and run
// loops.
func TickLandingStall(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if s.Work == nil {
		return p, 0
	}
	review, merging := len(s.Work.Column(Review)), len(s.Work.Column(Merging))
	waiting := review + merging
	last, hasLast := s.Work.Prop(PropLastLanding)
	since, _ := s.Work.Prop(PropLandingStallSince)
	said, _ := s.Work.Prop(PropLandingStallSaid)
	write := func(name, value string) {
		was, ok := s.Work.Prop(name)
		p.Props = append(p.Props, PropWrite{Table: Work, Name: name, Value: value, Was: was, WasAbsent: !ok})
	}
	stalled := false
	var age time.Duration
	bound := s.landingStallBound()
	// With no prior landing, the first tick that sees waiting cards starts the
	// episode. Later ticks measure its running-time age just like a landing stamp.
	if waiting > 0 && !hasLast && since == "" {
		write(PropLandingStallSince, stamp(s.Now))
		return p, 0
	}
	if waiting > 0 && !hasLast {
		last, hasLast = since, true
	}
	if waiting > 0 && hasLast {
		if d, ok := r.running(s.Now, last); ok {
			age, stalled = d, d >= bound
		} else {
			stalled = true
		}
	}
	landedAgain := hasLast && since != "" && last > since
	switch {
	case stalled:
		if since == "" {
			write(PropLandingStallSince, stamp(s.Now))
		}
		due := said == ""
		if !due {
			if d, ok := r.running(s.Now, said); !ok || d >= bound {
				due = true
			}
		}
		if due {
			what := landingStallWhat(s, r, age)
			if o, ok := openJudgmentOf(s, NLandingStall); ok {
				n := o.Note
				n.What, n.At = what, s.Now
				p.Updates = append(p.Updates, n)
			} else {
				n := Note{Kind: Judgment, Type: NLandingStall, What: what, Who: r.who(), To: s.Coordinator, At: s.Now, SprintLevel: true}
				p.Notes = append(p.Notes, n)
			}
			p.Units = append(p.Units, Unit{Key: PartLandingStall, Moved: fmt.Sprintf("landing stalled: %d review, %d merging, no landing in %s", review, merging, age.Round(time.Second))})
			write(PropLandingStallSaid, stamp(s.Now))
		}
	case since != "":
		if said != "" && landedAgain {
			took := ""
			if d, ok := r.running(s.Now, since); ok {
				took = " after " + d.Round(time.Second).String()
			}
			n := happened(NLandingAgain, "", s.Now)
			n.What = "landing again" + took
			n.Who, n.To = r.who(), s.Coordinator
			p.Notes = append(p.Notes, n)
		}
		write(PropLandingStallSince, "")
		write(PropLandingStallSaid, "")
	}
	return p, 0
}

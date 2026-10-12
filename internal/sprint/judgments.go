package sprint

import (
	"encoding/json"
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
// the next landing with a HAPPENED NLandingAgain. The model is tla/SprintRules.tla
// Part "stall" (LsTick raises and re-raises at the tick end, LsLand clears on a
// landing, LsStop/LsStart the stopped time that does not count; the invariant
// StallNotYoung and the liveness StallLive), landed by this card,
// tla-sprintrules-landing-stall-judgment.
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

// landingStamp is a landing-stall stamp: RFC3339 to the nanosecond, since the
// bound may be under a second and a landing and its episode's start must order
// within one (two whole-second stamps of one second are equal). It parses as any
// stamp does (TickReq.running).
func landingStamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// stampAfter says the stamp a is strictly later than b; false when either does not parse.
func stampAfter(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339Nano, a)
	tb, errB := time.Parse(time.RFC3339Nano, b)
	return errA == nil && errB == nil && ta.After(tb)
}

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

// landingLineBytes bounds the ledger :title and :text each landing line carries
// (each; landingLines shares less among a batch too long for the field bound).
const landingLineBytes = 600

// LandingTitleField and LandingTextField are the fields the member writes its
// ledger entry's one-line :title and :text on: what the coordinator reads in
// the landing push without opening the ledger.
const (
	LandingTitleField = "title"
	LandingTextField  = "text"
	// FieldLandingReadHistory persists completed read-card identities on the
	// primary because fleet read verdicts retire their producer cards.
	FieldLandingReadHistory = "landing_read_history"
)

// landingPush is the coordinator's push for a batch of landings
// (docs/SPEC-SPRINT.md, the judgments section): one happened note, one line a
// card, "landed <card>" with its stream, its head, the one-line :title and
// :text of its ledger entry, and the attempts and reads it took. A batch in
// one tick is one push with one line per card, addressed to the coordinator,
// needing no ack beyond the hook's. The note stays inside the field bound (a
// push over it would refuse the landing it reports) and every card keeps its
// line: the ledger texts share what the bound leaves after the lines'
// facts, each at most landingLineBytes, and only past that are the lines
// themselves cut, ending "... and N more landed: <ids>" (landingLines).
func landingPush(s *Snapshot, stream, who string, landing []*Card, ledger map[string]LandingLedgerEntry, now time.Time) Note {
	var lines []landingLine
	for _, c := range landing {
		title, text := c.F(LandingTitleField), c.F(LandingTextField)
		if entry, ok := ledger[c.ID]; ok {
			title, text = entry.Title, entry.Text
			if entry.Error != "" {
				title, text = "", "ledger entry unavailable: "+entry.Error
			}
		}
		lines = append(lines, landingLine{id: c.ID, stream: stream, head: orDash(shortSHA(c.F("head"))),
			title: oneLine(title), text: oneLine(text), attempts: c.Int("attempt"), reads: len(landingReadConsumers(s, c.ID, true))})
	}
	n := happened(NLanding, stream, now)
	n.Who, n.To = who, s.Coordinator
	n.What = landingLines(lines, MaxCardTextBytes)
	return n
}

// landingLine is one landed card's line of the landing push.
type landingLine struct {
	id, stream, head, title, text string
	attempts, reads               int
}

// with is the line with its ledger title and text each cut to n bytes.
func (l landingLine) with(n int) string {
	return fmt.Sprintf("landed %s (stream %s, head %s) %s %s; attempts %d, reads %d",
		l.id, l.stream, l.head, cutTo(l.title, n), cutTo(l.text, n), l.attempts, l.reads)
}

// cutTo is cutText that never cuts below its ellipsis: under it, a text that
// does not fit is left out.
func cutTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 8 {
		return ""
	}
	return cutText(s, n)
}

// landingLines is the push's text within bound bytes (docs/SPEC-SPRINT.md, the
// judgments section): the ledger texts get an equal share of what the lines'
// facts leave, at most landingLineBytes each, so every card keeps its line.
// When even the facts do not fit, the lines that fit are kept and the last line
// counts every card left out, always in full: "... and N more landed: <ids>".
// Its ids are whole and as many as fit; when they do not all fit (ids that
// alone pass the bound) the line says so, "... and N more landed (partial list,
// M named): <ids>", and the count still names how many landed. The limit is the
// bound: no id is ever cut partway.
func landingLines(lines []landingLine, bound int) string {
	if len(lines) == 0 {
		return ""
	}
	base := len(lines) - 1 // the newlines
	for _, l := range lines {
		base += len(l.with(0))
	}
	share := min(landingLineBytes, (bound-base)/(2*len(lines)))
	if share >= 0 {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = l.with(share)
		}
		if what := strings.Join(out, "\n"); len(what) <= bound {
			return what
		}
	}
	// room always kept for the overflow line's count, at its longest
	reserve := len(landingOverflowHead(len(lines), len(lines), true))
	var kept []string
	used := 0
	i := 0
	for ; i < len(lines); i++ {
		line := lines[i].with(0)
		if used+len(line)+1+reserve > bound {
			break
		}
		kept = append(kept, line)
		used += len(line) + 1
	}
	if i == len(lines) {
		return strings.Join(kept, "\n")
	}
	return strings.Join(append(kept, landingOverflow(lines[i:], bound-used)), "\n")
}

// landingOverflowHead is the overflow line's count: n cards left out of the
// push, named of them when the list is partial.
func landingOverflowHead(n, named int, partial bool) string {
	if partial {
		return fmt.Sprintf("... and %d more landed (partial list, %d named):", n, named)
	}
	return fmt.Sprintf("... and %d more landed:", n)
}

// landingOverflow is the overflow line within room bytes: the count of the cards
// left out in full, then their whole ids, as many as fit.
func landingOverflow(rest []landingLine, room int) string {
	if line := landingOverflowHead(len(rest), len(rest), false) + " " + landingIDs(rest); len(line) <= room {
		return line
	}
	named, used := 0, len(landingOverflowHead(len(rest), len(rest), true))
	for _, r := range rest {
		if used+1+len(r.id) > room {
			break
		}
		used += 1 + len(r.id)
		named++
	}
	out := landingOverflowHead(len(rest), named, true)
	if named > 0 {
		out += " " + landingIDs(rest[:named])
	}
	return out
}

// landingIDs is the lines' card ids, space separated.
func landingIDs(lines []landingLine) string {
	ids := make([]string, len(lines))
	for i, l := range lines {
		ids[i] = l.id
	}
	return strings.Join(ids, " ")
}

type landingReadIdentity struct {
	Reader  string `json:"reader"`
	Attempt int    `json:"attempt"`
	Head    string `json:"head"`
	Verdict string `json:"verdict"`
}

func landingReadKey(reader string, attempt int, head string) string {
	return fmt.Sprintf("%s|%d|%s", reader, attempt, head)
}

// landingReadHistory appends a verdict's consumer identity to the primary's
// retained history (docs/SPEC-SPRINT.md, judgments: every landing push carries
// its read count). The identity makes replays and duplicate views one read, while
// attempt and head keep an old review from counting on a new one.
func landingReadHistory(pr, rc *Card, verdict string) string {
	var history []landingReadIdentity
	_ = json.Unmarshal([]byte(pr.F(FieldLandingReadHistory)), &history)
	id := landingReadIdentity{Reader: rc.F("reader"), Attempt: rc.Int("attempt"), Head: rc.F("head"), Verdict: verdict}
	if id.Attempt == 0 {
		id.Attempt = 1
	}
	attempt := pr.Int("attempt")
	if attempt == 0 {
		attempt = 1
	}
	// Retain just this primary's current review epoch, so history stays bounded
	// across rework and a stale report cannot inflate a later landing.
	current := history[:0]
	for _, old := range history {
		if old.Attempt == attempt && old.Head == pr.F("head") {
			current = append(current, old)
		}
	}
	history = current
	key := landingReadKey(id.Reader, id.Attempt, id.Head)
	for _, old := range history {
		if landingReadKey(old.Reader, old.Attempt, old.Head) == key {
			b, _ := json.Marshal(history)
			return string(b)
		}
	}
	history = append(history, id)
	b, _ := json.Marshal(history)
	return string(b)
}

// landingReadConsumers combines the legacy readers table and fleet read cards
// by their logical reader/primary/attempt/head identity. During migration a read
// can briefly appear in both tables; count it once.
func landingReadConsumers(s *Snapshot, primary string, includeCompleted bool) map[string]time.Time {
	consumers := map[string]time.Time{}
	pr := s.Work.Card(primary)
	if pr == nil {
		return consumers
	}
	attempt := pr.Int("attempt")
	if attempt == 0 {
		attempt = 1
	}
	readHead := pr.F("head")
	add := func(reader string, readAttempt int, head string, at time.Time) {
		if readAttempt == 0 {
			readAttempt = 1
		}
		if readAttempt != attempt || head != readHead {
			return
		}
		key := landingReadKey(reader, readAttempt, head)
		if old, ok := consumers[key]; !ok || (!at.IsZero() && (old.IsZero() || at.Before(old))) {
			consumers[key] = at
		}
	}
	if includeCompleted && pr.F(FieldLandingReadHistory) != "" {
		var history []landingReadIdentity
		if json.Unmarshal([]byte(pr.F(FieldLandingReadHistory)), &history) == nil {
			for _, id := range history {
				add(id.Reader, id.Attempt, id.Head, time.Time{})
			}
		}
	}
	if s.Readers != nil {
		for _, c := range s.Readers.Cards() {
			if c.F("primary") != primary {
				continue
			}
			active := c.Col == Asked || c.Col == Reading
			completed := c.Col == OK || c.Col == Broken
			if active || (includeCompleted && completed) {
				add(c.F("reader"), c.Int("attempt"), c.F("head"), stampAt(c, "asked"))
			}
		}
	}
	if s.Fleet != nil {
		for _, c := range s.Fleet.Cards() {
			if c.F("primary") != primary {
				continue
			}
			active := isRead(c) && (c.Col == Ready || c.Col == Working)
			completed := c.F(FieldReadCard) != "" && (c.Col == DoneOK || c.Col == DoneFailed)
			if active || (includeCompleted && completed) {
				add(c.F("reader"), c.Int("attempt"), c.F("head"), stampAt(c, "asked"))
			}
		}
	}
	return consumers
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
	for _, primary := range s.Work.Column(Review) {
		for key, at := range landingReadConsumers(s, primary.ID, false) {
			rd := strings.SplitN(key, "|", 2)[0]
			g := byReader[rd]
			if g == nil {
				g = &readerOut{}
				byReader[rd] = g
			}
			g.n++
			if !at.IsZero() && (g.oldest.IsZero() || at.Before(g.oldest)) {
				g.oldest = at
			}
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
	ciWord := "CI green"
	var reds []string
	for _, f := range r.BaseCI {
		if f.Red {
			reds = append(reds, f.Repo+"@"+f.Base+" ("+Preview(f.Failed, ", ")+")")
		}
	}
	if len(reds) > 0 {
		ciWord = "CI red: " + strings.Join(reds, ", ")
	}
	return fmt.Sprintf("no card has landed in %s with %d in review and %d merging; reads out: %s; waiting on the coordinator's judgment: %d (oldest %s%s); accepted and not merged: %d (%s); the lander's last refusal: %s; %s",
		d.Round(time.Second), len(review), len(merging), readsOut, judged, orDash(oldestAlias), seatStall, len(merging), mergeState, refusal, ciWord)
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
//
// tla/SprintRules.tla LsTick
func TickLandingStall(s *Snapshot, r TickReq) (Plan, int) {
	p, n := tickLandingStall(s, r)
	// an acknowledged rate or health judgment whose numbers have crossed a new
	// threshold or whose quiet period has passed is raised again, saying why
	// (reads_window.go): this deadlines part, run every tick, is its release
	rel := releaseAckedHealth(s, r)
	p.Closes = append(p.Closes, rel.Closes...)
	p.Notes = append(p.Notes, rel.Notes...)
	return p, n
}

// tickLandingStall is the landing-stall alarm alone (TickLandingStall).
func tickLandingStall(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	if s.Work == nil {
		return p, 0
	}
	review, merging := len(s.Work.Column(Review)), len(s.Work.Column(Merging))
	waiting := review + merging
	last, hasLast := s.Work.Prop(PropLastLanding)
	hasLast = hasLast && last != ""
	landing := last // the real last landing, before the no-landing case borrows since
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
		write(PropLandingStallSince, landingStamp(s.Now))
		return landingStallKept(p, fmt.Sprintf("landing-stall watch begins: %d review, %d merging, no landing yet", review, merging)), 0
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
	// a landing after the episode's start breaks it: the stamps are compared as
	// times to the nanosecond (landingStamp), never as text, so a sub-second
	// bound cannot read a landing and the episode it came before as one second
	landedAgain := landing != "" && since != "" && stampAfter(landing, since)
	switch {
	// The episode is cleared only when the condition is actually gone: nothing
	// is waiting, or a landing happened since it began. "The bound has not
	// elapsed yet" is not a clear -- keep since/said so the no-prior-landing
	// path keeps measuring the running-time age instead of restarting every
	// tick whose cadence is shorter than the bound. A landing is taken before a
	// stall that still holds after it, so the old episode says "landing again"
	// and a new one begins at the next tick.
	case since != "" && (waiting == 0 || landedAgain):
		if o, ok := openJudgmentOf(s, NLandingStall); ok {
			p.Closes = append(p.Closes, o)
		}
		if said != "" && landedAgain {
			took := ""
			if at, err := time.Parse(time.RFC3339, landing); err == nil {
				if d, ok := r.running(at, since); ok && d >= 0 {
					took = " after " + d.Round(time.Second).String()
				}
			}
			n := happened(NLandingAgain, "", s.Now)
			n.What = "landing again" + took
			n.Who, n.To = r.who(), s.Coordinator
			p.Notes = append(p.Notes, n)
		}
		write(PropLandingStallSince, "")
		write(PropLandingStallSaid, "")
	case stalled:
		if since == "" {
			write(PropLandingStallSince, landingStamp(s.Now))
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
			write(PropLandingStallSaid, landingStamp(s.Now))
		}
	}
	ends := "landing-stall watch ends: nothing waits"
	if landedAgain {
		ends = "landing-stall watch ends: a card landed"
	}
	return landingStallKept(p, ends), 0
}

// landingStallKept gives a plan that writes only the episode's stamps a unit of
// its own (docs/SPEC-SPRINT.md, the judgments section): the store's tick passes
// over a part whose plan is Empty, which counts no property write, so without
// the unit the episode's first stamp (a sprint that has not yet landed) or its
// clear would never be written and the stall would never be raised.
func landingStallKept(p Plan, moved string) Plan {
	if len(p.Props) > 0 && p.Empty() {
		// keyed by the property it writes: the stamp's own unit, as the store
		// reports it and the reference model words the property's move
		p.Units = append(p.Units, Unit{Key: PropLandingStallSince, Moved: moved})
	}
	return p
}

// The lane-broken bound (docs/SPEC-SPRINT.md, the bounds): an attempt that ended
// without a worker report (an idle reap, a harness fault, a lane refused, a push
// refused, a friend lane that never started) says nothing about the brief. It does
// not count toward the brief bound, which counts only attempts whose worker ran and
// reported (a verdict, a pushed head). Four such attempts raise "the lane is broken,
// not the brief" instead, naming the member or friend and the fault, with the offered
// actions hold <member>, rework --tier <other tier>, and drop. The attempt record and
// the lane-broken bound are in attempts.go and bounds.go.

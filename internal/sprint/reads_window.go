package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Broken reads outrunning ok reads (docs/SPEC-SPRINT.md section 8, "broken reads outrun ok
// reads"; the owner, 2026-10-06 8:30 PM ET: "We have to catch broken reads faster than this.
// You should get some notification."). Between 7:46 and 8:27 PM ET 48 reads came back ok and
// 66 broken, 54 of them one reader's while another read the same kind of work 36 ok to 8
// broken; every broken read sent its card to rework, and nobody was told. Every verdict is
// kept on a bounded ledger, a property of the table its read card is on (PropReadsWindow:
// the readers table's, the fleet table's for a read card on a fleet row), written by the step
// that records the verdict; the tick reads both and raises, once an episode:
//   - "broken reads outrun ok reads" when, over the last ReadsWindowSpan of running time,
//     broken verdicts exceed ok verdicts and there were at least ReadsWindowMin, naming each
//     reader's counts and the top three finding classes;
//   - "reader <r> breaks nearly everything", per reader, when its last ReaderWindowLen
//     verdicts are ReaderBreaksBar or more broken while another reader's last ReaderWindowLen
//     are under ReaderOtherBar.
// A reader is its machine (readerKey): one machine reading on its reader row and on its
// fleet row is one reader, and a friend is her name.
// Each closes when its condition no longer holds (notify), so the next episode raises again.

// PropReadsWindow is the table property holding the verdicts' ledger: one line a verdict,
// "<stamp>\t<reader>\t<verdict>\t<tier>\t<finding>", oldest first, at most
// MaxReadsWindowLines.
const PropReadsWindow = "reads_window"

// The bounds of the ledger and of the two notices.
const (
	MaxReadsWindowLines = 200
	ReadsWindowSpan     = 30 * time.Minute
	ReadsWindowMin      = 10
	ReaderWindowLen     = 20
	ReaderBreaksBar     = 0.8
	ReaderOtherBar      = 0.5
	ReaderOtherMin      = 5
	FindingClassLen     = 60
	// BrokenShareStep is how much the broken share of the window must rise for
	// an acknowledged "broken reads outrun" judgment to be raised again as a new
	// episode before its quiet period has passed (docs/SPEC-SPRINT.md section 8).
	BrokenShareStep = 0.1
	// HealthQuietPeriod is how long an acknowledged rate or health judgment stays
	// quiet while its condition still holds before the tick raises it again.
	HealthQuietPeriod = 30 * time.Minute
)

// The tick's two judgments.
const (
	NBrokenReadsOutrun = "broken reads outrun ok reads"
	NReaderBreaks      = "a reader breaks nearly everything"
)

// ReadVerdict is one verdict on the ledger.
type ReadVerdict struct {
	At      time.Time
	Reader  string
	Verdict string // ok or broken
	Tier    string
	Finding string // its class: the first FindingClassLen characters
}

// findingClass is a finding's class: its first FindingClassLen characters on one line.
func findingClass(f string) string {
	f = strings.Join(strings.Fields(f), " ")
	if r := []rune(f); len(r) > FindingClassLen {
		f = string(r[:FindingClassLen])
	}
	return f
}

func (v ReadVerdict) line() string {
	clean := func(x string) string { return strings.Join(strings.Fields(x), " ") }
	return strings.Join([]string{stamp(v.At), clean(v.Reader), v.Verdict, clean(v.Tier), findingClass(v.Finding)}, "\t")
}

// parseReadsWindow is a ledger's verdicts, oldest first; a line that does not read is skipped.
func parseReadsWindow(raw string) []ReadVerdict {
	var out []ReadVerdict
	for l := range strings.SplitSeq(raw, "\n") {
		f := strings.SplitN(l, "\t", 5)
		if len(f) < 4 {
			continue
		}
		at, err := time.Parse(time.RFC3339, f[0])
		if err != nil || (f[2] != "ok" && f[2] != "broken") {
			continue
		}
		v := ReadVerdict{At: at, Reader: f[1], Verdict: f[2], Tier: f[3]}
		if len(f) == 5 {
			v.Finding = f[4]
		}
		out = append(out, v)
	}
	return out
}

// verdictOf is the ledger's record of a verdict on read card c by the reader on row.
func verdictOf(s *Snapshot, c, pr *Card, row, verdict, finding string) ReadVerdict {
	tier := c.F(FieldTier)
	if tier == "" && pr != nil {
		tier = s.readTierOf(pr)
	}
	return ReadVerdict{At: s.Now, Reader: row, Verdict: verdict, Tier: tier, Finding: finding}
}

// readerKey is the reader a ledger row reads for, counted once: a friend's row her name, a
// reader row (ReaderPrefix) its machine, a member's fleet row the member, so one machine
// holding a reader row and a fleet row is one reader.
func readerKey(row string) string {
	if name, ok := FriendOfRow(row); ok {
		return name
	}
	return strings.TrimPrefix(row, ReaderPrefix)
}

// holdName is the name the hold verb takes for a ledger row: a friend's name, else the row.
func holdName(row string) string {
	if name, ok := FriendOfRow(row); ok {
		return name
	}
	return row
}

// windowWrite is the property write that adds vs to table t's ledger, the oldest dropped past
// MaxReadsWindowLines; false when there is nothing to add or no table.
func windowWrite(t *Table, table string, vs []ReadVerdict) (PropWrite, bool) {
	if t == nil || len(vs) == 0 {
		return PropWrite{}, false
	}
	was, had := t.Prop(PropReadsWindow)
	var lines []string
	if was != "" {
		lines = strings.Split(was, "\n")
	}
	for _, v := range vs {
		if v.Verdict == "ok" || v.Verdict == "broken" {
			lines = append(lines, v.line())
		}
	}
	if len(lines) > MaxReadsWindowLines {
		lines = lines[len(lines)-MaxReadsWindowLines:]
	}
	return PropWrite{Table: table, Name: PropReadsWindow, Value: strings.Join(lines, "\n"), Was: was, WasAbsent: !had}, true
}

// readsLedger is every verdict on the readers table's and the fleet table's ledgers, oldest
// first.
func readsLedger(s *Snapshot) []ReadVerdict {
	var out []ReadVerdict
	for _, t := range []*Table{s.Readers, s.Fleet} {
		if t == nil {
			continue
		}
		if raw, ok := t.Prop(PropReadsWindow); ok {
			out = append(out, parseReadsWindow(raw)...)
		}
	}
	slices.SortStableFunc(out, func(a, b ReadVerdict) int { return a.At.Compare(b.At) })
	return out
}

// ReadsWindowView is the window's counts, as where --json's reads_window carries them: the
// ok and broken verdicts over the last ReadsWindowSpan of running time, and when the window
// began by the clock.
type ReadsWindowView struct {
	OK     int       `json:"ok"`
	Broken int       `json:"broken"`
	Since  time.Time `json:"since"`
}

// windowSince is when the last ReadsWindowSpan of running time began: the clock's span
// back from now, widened by the time the machine was STOPPED within it.
func windowSince(now time.Time, stopped func(from, to time.Time) time.Duration) time.Time {
	since := now.Add(-ReadsWindowSpan)
	if stopped == nil {
		return since
	}
	for range 4 {
		next := now.Add(-ReadsWindowSpan - stopped(since, now))
		if next.Equal(since) {
			break
		}
		since = next
	}
	return since
}

// readsInWindow is the ledger's verdicts at or after since.
func readsInWindow(all []ReadVerdict, since time.Time) []ReadVerdict {
	var out []ReadVerdict
	for _, v := range all {
		if !v.At.Before(since) {
			out = append(out, v)
		}
	}
	return out
}

// ReadsWindowOf is the window's counts on the snapshot (where --json's reads_window).
func ReadsWindowOf(s *Snapshot, stopped func(from, to time.Time) time.Duration) ReadsWindowView {
	since := windowSince(s.Now, stopped)
	w := ReadsWindowView{Since: since}
	for _, v := range readsInWindow(readsLedger(s), since) {
		if v.Verdict == "ok" {
			w.OK++
		} else {
			w.Broken++
		}
	}
	return w
}

// tally is ok and broken counts.
type tally struct{ ok, broken int }

func (t tally) n() int { return t.ok + t.broken }

func (t tally) brokenShare() float64 {
	if t.n() == 0 {
		return 0
	}
	return float64(t.broken) / float64(t.n())
}

// topFindings is the top n finding classes of the broken verdicts, "<count>x \"<class>\"".
func topFindings(vs []ReadVerdict, n int) []string {
	count := map[string]int{}
	for _, v := range vs {
		if v.Verdict == "broken" {
			count[cmp.Or(v.Finding, "(no finding)")]++
		}
	}
	keys := slices.Collect(maps.Keys(count))
	slices.SortFunc(keys, func(a, b string) int { return cmp.Or(count[b]-count[a], strings.Compare(a, b)) })
	var out []string
	for _, k := range keys[:min(n, len(keys))] {
		out = append(out, fmt.Sprintf("%dx %q", count[k], k))
	}
	return out
}

// byReader is the verdicts' counts by reader (readerKey: a machine, a friend), and the
// readers in name order.
func byReader(vs []ReadVerdict) (map[string]tally, []string) {
	out := map[string]tally{}
	for _, v := range vs {
		k := readerKey(v.Reader)
		t := out[k]
		if v.Verdict == "ok" {
			t.ok++
		} else {
			t.broken++
		}
		out[k] = t
	}
	return out, slices.Sorted(maps.Keys(out))
}

// BrokenReadsOutrun is the "broken reads outrun ok reads" judgment's line, and whether it
// holds: over the window broken verdicts exceed ok ones, at least ReadsWindowMin in all.
func BrokenReadsOutrun(s *Snapshot, stopped func(from, to time.Time) time.Duration) (string, bool) {
	since := windowSince(s.Now, stopped)
	in := readsInWindow(readsLedger(s), since)
	readers, names := byReader(in)
	var all tally
	for _, t := range readers {
		all.ok, all.broken = all.ok+t.ok, all.broken+t.broken
	}
	if all.n() < ReadsWindowMin || all.broken <= all.ok {
		return "", false
	}
	var parts []string
	for _, r := range names {
		parts = append(parts, fmt.Sprintf("%s %d ok, %d broken", r, readers[r].ok, readers[r].broken))
	}
	return fmt.Sprintf("broken reads outrun ok reads: %d broken to %d ok in the last %s of running time; by reader: %s; top findings: %s",
		all.broken, all.ok, ReadsWindowSpan, strings.Join(parts, "; "), strings.Join(topFindings(in, 3), "; ")), true
}

// healthAckBase is the marker an acknowledged rate or health judgment carries in
// its text (Ack, steps_ack.go) with the numbers it had at ack time as
// "<ok>/<broken>", so the tick can tell when they have crossed a new threshold
// (readsCrossedStep). An acknowledgement shows in no inbox and is rewritten by no
// tick, so the marker is never read by a person.
const healthAckBase = "\x00ack="

// readsAckBase is the numbers of a reads-window judgment at ack time, as
// "<ok>/<broken>", so the tick can tell whether its numbers have crossed a new
// threshold since the acknowledgment. For a judgment with no countable window
// it is "" (never a threshold crossed).
func readsAckBase(s *Snapshot, n Note) string {
	if n.Type == NBrokenReadsOutrun {
		w := ReadsWindowOf(s, nil)
		return fmt.Sprintf("%d/%d", w.OK, w.Broken)
	}
	return ""
}

// ackBaseOf is the ack-time numbers an acknowledgement carries in its text, or ""
// when it carries none.
func ackBaseOf(n Note) string {
	i := strings.Index(n.What, healthAckBase)
	if i < 0 {
		return ""
	}
	return n.What[i+len(healthAckBase):]
}

// withAckBase is n.What with the ack-time numbers appended (healthAckBase), so a
// later tick can tell whether they have crossed a new threshold.
func withAckBase(what, base string) string {
	if base == "" {
		return what
	}
	return what + healthAckBase + base
}

// parseReadsBase reads a readsAckBase string "<ok>/<broken>"; nothing parses to
// zero counts.
func parseReadsBase(base string) (ok, broken int) {
	fmt.Sscanf(base, "%d/%d", &ok, &broken)
	return ok, broken
}

// readsCrossedStep says the window's numbers have crossed a new threshold since
// the acknowledgment: the broken share is up by BrokenShareStep or more.
func readsCrossedStep(base, now string) bool {
	if base == "" || now == "" {
		return false
	}
	okB, brB := parseReadsBase(base)
	okN, brN := parseReadsBase(now)
	var shareB, shareN float64
	if okB+brB > 0 {
		shareB = float64(brB) / float64(okB+brB)
	}
	if okN+brN > 0 {
		shareN = float64(brN) / float64(okN+brN)
	}
	return shareN >= shareB+BrokenShareStep
}

// readsReleaseReason is why an acknowledged rate or health judgment is raised
// again before its condition clears (readsWindowConds, releaseAckedHealth): its
// stated quiet period has passed, or its numbers have crossed a new threshold
// since the ack. "" keeps it quiet.
func readsReleaseReason(s *Snapshot, r TickReq, typ, subject string) string {
	for _, o := range s.Acked {
		if o.Note.Type != typ || o.Subject() != subject {
			continue
		}
		if !o.Note.At.IsZero() && DueNow(s.Now, o.Note.At.Add(HealthQuietPeriod), o.Note.At, r.Stopped) {
			return "its " + HealthQuietPeriod.String() + " quiet period has passed"
		}
		if typ == NBrokenReadsOutrun && readsCrossedStep(ackBaseOf(o.Note), readsAckBase(s, o.Note)) {
			return "the broken share has risen past a new threshold since you acknowledged it"
		}
	}
	return ""
}

// releaseAckedHealth is the tick's release of acknowledged rate or health
// judgments: one whose stated quiet period has passed or whose numbers have
// crossed a new threshold since the ack (readsReleaseReason) is closed and
// raised again, its text saying why it is back, while its condition still holds
// (docs/SPEC-SPRINT.md section 8; reads_window.go). A condition that has cleared
// is left to the tick's own close, and nothing is done when no such
// acknowledgement is held. It runs from the deadlines part (TickLandingStall,
// judgments.go), the part in this file's gift that runs every tick.
func releaseAckedHealth(s *Snapshot, r TickReq) Plan {
	var p Plan
	for _, o := range s.Acked {
		if o.Note.Type != NBrokenReadsOutrun && o.Note.Type != NReaderBreaks {
			continue
		}
		why := readsReleaseReason(s, r, o.Note.Type, o.Subject())
		if why == "" {
			continue
		}
		what, ok := healthCondition(s, r, o.Note.Type, o.Subject())
		if !ok {
			continue // the condition cleared: the tick closes the acknowledgement
		}
		p.Closes = append(p.Closes, o)
		n := Note{Kind: Judgment, Type: o.Note.Type, Stream: o.Note.Stream, StreamLevel: true,
			What: what + "; " + why, Who: r.who(), At: s.Now, Marked: true}
		if o.Note.Type == NBrokenReadsOutrun {
			n.Decisions = append([]string(nil), BrokenReadsDecisions...)
		} else {
			n.Decisions = append([]string(nil), ReaderBreaksDecisions...)
		}
		p.Notes = append(p.Notes, n)
	}
	return p
}

// healthCondition is the current line of the condition an acknowledgement holds:
// the broken-reads-outrun judgment, or the named reader's breaks, and whether it
// still holds.
func healthCondition(s *Snapshot, r TickReq, typ, subject string) (string, bool) {
	switch typ {
	case NBrokenReadsOutrun:
		return BrokenReadsOutrun(s, r.Stopped)
	case NReaderBreaks:
		for _, b := range ReadersBreaking(s) {
			if StreamSubject(ReaderSubject(b.Reader)) == subject {
				return b.What(), true
			}
		}
	}
	return "", false
}

// ReaderBreaking is a reader that breaks nearly everything: its last ReaderWindowLen
// verdicts, and the reader it is set beside, by that reader's own last ReaderWindowLen.
type ReaderBreaking struct {
	Reader       string
	Holds        []string // the names its rows are held by (holdName), the judgment's decisions
	Last         tally
	Other        string
	OtherTally   tally
	FindingsTop3 []string
}

// What is the judgment's line.
func (b ReaderBreaking) What() string {
	return fmt.Sprintf("reader %s breaks nearly everything: %d of its last %d verdicts broken, while %s broke %d of its last %d; top findings: %s",
		b.Reader, b.Last.broken, b.Last.n(), b.Other, b.OtherTally.broken, b.OtherTally.n(), strings.Join(b.FindingsTop3, "; "))
}

// ReadersBreaking is every reader whose last ReaderWindowLen verdicts are ReaderBreaksBar or
// more broken while another reader's own last ReaderWindowLen (at least ReaderOtherMin of
// them) are under ReaderOtherBar broken, ok against broken by reader, in reader order.
func ReadersBreaking(s *Snapshot) []ReaderBreaking {
	all := readsLedger(s)
	last := map[string][]ReadVerdict{}
	holds := map[string]map[string]bool{}
	for i := len(all) - 1; i >= 0; i-- {
		v := all[i]
		k := readerKey(v.Reader)
		if len(last[k]) < ReaderWindowLen {
			last[k] = append(last[k], v)
			if holds[k] == nil {
				holds[k] = map[string]bool{}
			}
			holds[k][holdName(v.Reader)] = true
		}
	}
	var out []ReaderBreaking
	for _, r := range slices.Sorted(maps.Keys(last)) {
		mine := last[r]
		t, _ := byReader(mine)
		if len(mine) < ReaderWindowLen || t[r].brokenShare() < ReaderBreaksBar {
			continue
		}
		for _, o := range slices.Sorted(maps.Keys(last)) {
			if o == r {
				continue
			}
			ot, _ := byReader(last[o])
			if len(last[o]) >= ReaderOtherMin && ot[o].brokenShare() < ReaderOtherBar {
				out = append(out, ReaderBreaking{Reader: r, Holds: slices.Sorted(maps.Keys(holds[r])), Last: t[r], Other: o, OtherTally: ot[o], FindingsTop3: topFindings(mine, 3)})
				break
			}
		}
	}
	return out
}

// ReaderSubject is the subject of a judgment about one reader.
func ReaderSubject(reader string) string { return "reader:" + reader }

// BrokenReadsDecisions and ReaderBreaksDecisions are the two judgments' decisions.
// Both are ackable: an acknowledgment (Ack) quiets them while their condition
// holds, until the numbers cross a new threshold or the quiet period passes
// (docs/SPEC-SPRINT.md section 8).
var (
	BrokenReadsDecisions  = []string{"look at the readers", "raise the read tier", "ack", "act"}
	ReaderBreaksDecisions = []string{"look at the reader", "ack", "act"}
)

// readsWindowConds is the tick's conditions of the verdicts' ledger (NBrokenReadsOutrun, and
// NReaderBreaks per reader).
func readsWindowConds(s *Snapshot, r TickReq) []cond {
	var out []cond
	if what, ok := BrokenReadsOutrun(s, r.Stopped); ok {
		if why := readsReleaseReason(s, r, NBrokenReadsOutrun, StreamSubject("")); why != "" {
			what += "; " + why // why an acknowledged rate judgment is back
		}
		out = append(out, cond{typ: NBrokenReadsOutrun, streamLevel: true, what: what, decisions: BrokenReadsDecisions})
	}
	for _, b := range ReadersBreaking(s) {
		var decisions []string
		for _, h := range b.Holds {
			decisions = append(decisions, "hold "+h)
		}
		what := b.What()
		if why := readsReleaseReason(s, r, NReaderBreaks, StreamSubject(ReaderSubject(b.Reader))); why != "" {
			what += "; " + why
		}
		out = append(out, cond{typ: NReaderBreaks, stream: ReaderSubject(b.Reader), streamLevel: true, what: what,
			decisions: append(decisions, ReaderBreaksDecisions...)})
	}
	return out
}

package sprint

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The coordinator bring-up (docs/SPEC-SPRINT.md, "The coordinator bring-up").
// start, coordinator, handover and check --bring-up print one line per thing
// the seat needs, each with the state just measured and the one nova verb
// that starts it. A thing that was running and is then missing is one
// judgment (NBringUpDown), once per transition.

const (
	// BringUpRunning, BringUpMissing and BringUpStale are the only states a
	// bring-up line carries. Missing is no evidence. Stale is evidence older
	// than the thing's window. Running is fresh evidence that it answers.
	BringUpRunning = "running"
	BringUpMissing = "missing"
	BringUpStale   = "stale"

	// BringUpFresh is how long a watch beat, a dashboard poll and a fleet
	// beat stand for running: the machine's silence and a reader's beat.
	BringUpFresh = 15 * time.Second
	// BringUpFriendFresh is how long a friend's beat stands for her daemon
	// and her beat both. FriendBeatEvery proves a daemon, not a session.
	BringUpFriendFresh = 3 * FriendBeatEvery

	// NBringUpDown is the judgment a line raises when it goes from running
	// to missing. Sprint-level: one subject, the sprint, and the command
	// that starts the thing is the decision.
	NBringUpDown = "bring-up down"
)

// Event kinds watch --events prints, in the order of the line. A kind with
// no line in the log is omitted. Repeats of one kind are one line and a count.
const (
	EventReadAsked      = "read-asked"
	EventReadVerdict    = "read-verdict"
	EventWorkFinished   = "work-finished"
	EventMergeQueued    = "merge-queued"
	EventLanding        = "landing"
	EventBlockedMerge   = "blocked-merge"
	EventRefusedDrain   = "refused-drain"
	EventPushedJudgment = "pushed-judgment"
)

// EventKinds is the order watch --events prints.
var EventKinds = []string{
	EventReadAsked,
	EventReadVerdict,
	EventWorkFinished,
	EventMergeQueued,
	EventLanding,
	EventBlockedMerge,
	EventRefusedDrain,
	EventPushedJudgment,
}

// BringUpItem is one measured thing.
type BringUpItem struct {
	Key     string // stable across passes; the judgment names it
	Words   string // the words after BRING-UP
	State   string
	Extra   string // held=, width= and tiers=, or last=
	Command string // the one nova verb that starts it
}

// Line is the bring-up line. The command is quoted so it pastes as one field.
func (it BringUpItem) Line() string {
	s := "BRING-UP " + it.Words + " " + it.State
	if it.Extra != "" {
		s += " " + it.Extra
	}
	return s + " command=" + strconv.Quote(it.Command)
}

// ReviewWarn is a tier that has cards in review and fewer than two readers
// up who can read it.
type ReviewWarn struct {
	Tier    string
	Review  int
	Readers int
}

// Line is the warning. It is not a state, and it raises no transition.
func (w ReviewWarn) Line() string {
	return "BRING-UP warning tier=" + w.Tier + " review=" + strconv.Itoa(w.Review) +
		" readers=" + strconv.Itoa(w.Readers) + " fewer than two readers can read it"
}

// BringUpFell is the keys that were running and are missing now, in order.
// An empty previous observation fell nothing: the first look records, and
// does not judge. A key the next look no longer carries was running and is
// gone, which is missing.
func BringUpFell(prev, next map[string]string) []string {
	if len(prev) == 0 {
		return nil
	}
	var out []string
	for key, was := range prev {
		if was != BringUpRunning {
			continue
		}
		now, ok := next[key]
		if !ok || now == BringUpMissing {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// ReaderLines is one line per reader row. Up is running, away or held is
// stale, down, retired or unknown is missing. Width is the fleet row's, or
// unbounded when the reader names no fleet row.
func ReaderLines(s *Snapshot) []BringUpItem {
	if s == nil || s.Readers == nil {
		return nil
	}
	var out []BringUpItem
	for _, name := range s.Readers.Rows() {
		tiers := strings.TrimSpace(s.readerTiersStored(name))
		if tiers == "" {
			tiers = "all"
		}
		width := "unbounded"
		if w := s.ReaderWidth(name); w != math.MaxInt {
			width = strconv.Itoa(w)
		}
		out = append(out, BringUpItem{
			Key:     "reader:" + name,
			Words:   "reader " + name,
			State:   readerBringState(s, name),
			Extra:   "width=" + width + " tiers=" + tiers,
			Command: "nova-sprint reader up " + name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func readerBringState(s *Snapshot, name string) string {
	st := ReaderUp
	if s.ReaderStates != nil {
		st = s.ReaderStates[name]
	}
	switch st {
	case ReaderUp:
		return BringUpRunning
	case ReaderAway, ReaderHeld:
		return BringUpStale
	default:
		return BringUpMissing
	}
}

// ReviewWarnings is one warning per tier that has cards in review and fewer
// than two readers up who read that tier. Tiers are named in order.
func ReviewWarnings(s *Snapshot) []ReviewWarn {
	if s == nil || s.Work == nil {
		return nil
	}
	counts := map[string]int{}
	for _, row := range s.Work.Rows() {
		for _, c := range s.Work.Cell(row, Review) {
			t := s.readTierOf(c)
			if t == "" {
				t = "flash"
			}
			counts[t]++
		}
	}
	tiers := make([]string, 0, len(counts))
	for t, n := range counts {
		if n > 0 {
			tiers = append(tiers, t)
		}
	}
	sort.Strings(tiers)
	var out []ReviewWarn
	for _, t := range tiers {
		n := 0
		if s.Readers != nil {
			for _, rd := range s.Readers.Rows() {
				if s.ReaderIsUp(rd) && s.readerReadsTier(rd, t) {
					n++
				}
			}
		}
		if n < 2 {
			out = append(out, ReviewWarn{Tier: t, Review: counts[t], Readers: n})
		}
	}
	return out
}

// FoldEvents counts the log's lines into the watch --events kinds. A line
// counts as one kind. A blocked-merge note is that kind, not also a pushed
// judgment. A landing is a land, or a move into landed, and is not also a
// merge queued.
func FoldEvents(lines []Line) map[string]int {
	out := map[string]int{}
	for _, l := range lines {
		if k := eventKind(l); k != "" {
			out[k]++
		}
	}
	return out
}

// FormatWatchEvents is one line per kind that occurred, in EventKinds order,
// repeats folded into a count. No kind is a blank string.
func FormatWatchEvents(lines []Line) string {
	counts := FoldEvents(lines)
	var b strings.Builder
	for _, k := range EventKinds {
		if counts[k] == 0 {
			continue
		}
		b.WriteString("EVENT " + k + " count=" + strconv.Itoa(counts[k]) + "\n")
	}
	return b.String()
}

func eventKind(l Line) string {
	if l.Note != nil && blockedMerge(l.Note.Type) {
		return EventBlockedMerge
	}
	if l.Note != nil && l.Note.Kind == Judgment {
		return EventPushedJudgment
	}
	if l.Verb == "drain" && strings.Contains(strings.ToLower(l.Cause), "refus") {
		return EventRefusedDrain
	}
	if l.Verb == "land" || strings.Contains(l.To, "landed") {
		return EventLanding
	}
	if l.Verb == "merge" || l.Table == Merge || strings.HasSuffix(l.To, ":merging") || strings.HasSuffix(l.To, ":queued") {
		return EventMergeQueued
	}
	if l.Verb == "finish" {
		return EventWorkFinished
	}
	if l.Verb == "read" {
		return EventReadVerdict
	}
	if l.Verb == "ask" || (l.Table == Readers && strings.Contains(l.To, ":asked")) {
		return EventReadAsked
	}
	return ""
}

func blockedMerge(typ string) bool {
	switch typ {
	case NConflict, NRed, NCross, NRejected, NBaseRed:
		return true
	}
	return false
}

// BeatState is running while the beat is within window, stale when it is
// older, missing when it never came.
func BeatState(b Beat, now time.Time, window time.Duration) string {
	if !b.Beaten() {
		return BringUpMissing
	}
	if now.Sub(b.At) <= window {
		return BringUpRunning
	}
	return BringUpStale
}

// NovaVerb says the command is one nova verb and not a shell script: no
// launchctl, redis-cli, a shell, a pipe or a substitution.
func NovaVerb(cmd string) bool {
	switch {
	case strings.Contains(cmd, "launchctl"), strings.Contains(cmd, "redis-cli"),
		strings.Contains(cmd, "sh -c"), strings.Contains(cmd, "|"), strings.Contains(cmd, "$("):
		return false
	}
	return strings.HasPrefix(cmd, "nova-sprint ") || strings.HasPrefix(cmd, "nova-bus ") || strings.HasPrefix(cmd, "nova-friend ")
}

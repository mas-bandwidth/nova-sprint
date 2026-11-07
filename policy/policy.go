// Package policy is layer 5 of nova-sprint v2 (docs/LAYER-5-POLICY.md): the
// rules the coordinator changes while the sprint runs, as a Starlark file
// stored as an immutable version in sprint.policy. It answers a closed set
// of questions the steps ask, checks every answer against its domain, and
// never writes the store during a tick and never raises a move. Pure Go over
// go.starlark.net: no bus, no socket; the one clock is a tick's deadline
// (TickTime), and the verbs (verbs.go) are the only code that touches the
// store, through its policy writes.
//
// The state machine of versions, activation and the per-tick fallback is
// modelled in tla/Policy.tla.
package policy

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The questions, the closed set: a policy's top-level name is one of these
// or starts with "_". Arity is the number of parameters each takes.
var Arity = map[string]int{
	"order": 1, "rows": 3, "reads": 1, "verdict": 2, "width": 1,
	"limits": 0, "landing": 1, "timeouts": 0, "stitch": 1, "prices": 0, "catalog": 0,
}

// params are each question's parameters as the design writes them.
var params = map[string]string{
	"order": "card", "rows": "card, kind, candidates", "reads": "card", "verdict": "card, reads", "width": "row",
	"limits": "", "landing": "stream", "timeouts": "", "stitch": "stream", "prices": "", "catalog": "",
}

// The kinds of a rows question.
const (
	KindWork = "work"
	KindRead = "read"
)

// The rungs a stream lands on (LANDING.md).
const (
	RungFlowing  = "flowing"
	RungBackedUp = "backed_up"
)

// Questions are the per-subject questions one tick asks. Width is asked for
// every row of the snapshot and limits, timeouts and prices once, always:
// the joint check needs every width, and the argument-free ones are cheap.
type Questions struct {
	Order   []string          // producer ids
	Rows    []RowsQuestion    // a producer and the kind of consumer to cut
	Reads   []string          // producers entering review
	Verdict []string          // producers whose reads are complete
	Landing []LandingQuestion // streams and the rung each is on now
	Stitch  []string          // streams at their sentinel
}

// RowsQuestion asks which rows take producer Card's next consumer of Kind.
type RowsQuestion struct{ Card, Kind string }

// LandingQuestion asks the rungs of stream Stream, now on Rung (layer 3's state).
type LandingQuestion struct{ Stream, Rung string }

// Answers are one tick's answers, every one checked against its domain.
// Version is the version that answered (0 is the built-in default). Each
// tick's Answers are the caller's own, except the Rows lists: equal lists
// share one slice (within a tick and with the answer cache), so they are
// read-only.
type Answers struct {
	Version  int
	Order    map[string][]any // each element an int64 or a string; see CompareOrder
	Rows     map[RowsQuestion][]string
	Reads    map[string]int
	Verdict  map[string]string // "yes" or "fix" (a faulted read is no read: none is asked over faults alone)
	Width    map[string]Width
	Limits   Limits
	Landing  map[string]Landing
	Timeouts Timeouts
	Stitch   map[string]bool
	Prices   map[string]Price
	Catalog  Catalog
	shown    map[string]string // question(subject) -> the answer as Starlark prints it, for check
}

// Width is how many consumers a row works and how many it holds ready ahead.
type Width struct{ Work, Ahead int }

// Limits are the counts the steps compare against, and the steering
// numbers layer 3 reads (round 5): EscalateAfter and FrontierAfter are the
// bounces at which work goes one rank up and to frontier, FlapLimit the
// flaps that open a flap judgement, IdleFloor the fraction of the open width
// working under which the idle alarm opens, LandTarget the first-time land
// rate under which the land-rate alarm opens.
// FleetWidthCap bounds a worker machine's width (its cores, at most this):
// the roster's cap, policy's number (drift row 26).
type Limits struct {
	Bounces, Faults, SameFinding, ReportsPerTick, RouteRestWindow, RouteRestAfter int
	EscalateAfter, FrontierAfter, FlapLimit, FleetWidthCap                        int
	IdleFloor, LandTarget                                                         float64
}

// Landing is the merging count at which a stream backs up (rung 2) and the
// one at which it flows again, and CIWait the seconds a gated sha may go
// with no CI before its landing is a judgement (0: the engine's default).
type Landing struct{ BackedUp, Flowing, CIWait int }

// Timeouts are seconds of running time; BaseRegate is a rising list. Tick
// is run's budget for one tick (drift row 26); Consumer is the deadline of a
// consumer by its tier, every tier of store.Tiers named (drift row 15: the
// worker reads it from the take receipt, never a Go literal).
type Timeouts struct {
	RowSilentGone, RowSilentWithdraw, LandingStall, Judgement, RouteRestFor, Tick int
	BaseRegate                                                                    []int
	Consumer                                                                      map[string]int
}

// Price is micro-dollars per million tokens in and out.
type Price struct{ In, Out int64 }

// Failure is one version's error or refused answer: none of that version's
// answers is used that tick. Line is the line of the question's def for a
// refusal, of the innermost frame for a runtime error, and the file's last
// line ("line N, end of file") for something the file does not define.
// Label is the version (v7, default) or, before storage, the file's name.
type Failure struct {
	Version  int    // 0 is the built-in or an unstored file
	Label    string // v7, default, or a file's name
	Question string // "" for a compile error
	Subject  string
	Line     int
	Msg      string
	eof      bool // Line is the file's last line: the thing refused is absent
}

func (f Failure) Error() string {
	at := f.Label
	if f.Line > 0 {
		at += fmt.Sprintf(" line %d", f.Line)
	}
	if f.eof {
		at += ", end of file"
	}
	if f.Question != "" {
		at += fmt.Sprintf(" %s(%s)", f.Question, f.Subject)
	}
	return "POLICY REFUSED " + at + ": " + f.Msg
}

// Judgement is the policy judgement a failure opens, if none is open for its
// version (the group key): its safe default, the fallback, is already in
// force. The deadline is the caller's (timeouts().judgement from now; the
// clock is layer 3's).
func (f Failure) Judgement(seq int64) store.PutJudgement {
	return store.PutJudgement{
		ID: fmt.Sprintf("policy-v%d-s%d", f.Version, seq), Kind: "policy", About: f.Label,
		Question: storable(f.Error()), Command: "nova-sprint policy check " + f.Label, Group: Group(f.Version),
	}
}

// maxQuestion caps a judgement's question text and every refusal's.
const maxQuestion = 4096

// storable is text the store accepts: invalid UTF-8 and NUL replaced, at
// most maxQuestion bytes, cut at a rune with a marker. It cuts first at four
// times that, so the work is bounded however long the text.
func storable(s string) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(cut(s, 4*maxQuestion), "�"), "\x00", "�")
	return cut(s, maxQuestion)
}

// cut is s at most n bytes, cut at a rune with a marker naming n.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	marker := fmt.Sprintf(" [cut at %d bytes]", n)
	i := max(n-len(marker), 0)
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + marker
}

// Group is the group key of version v's policy judgement.
func Group(v int) string { return fmt.Sprintf("policy:v%d", v) }

// OpenJudgements are the open policy judgements of version v in snap.
func OpenJudgements(snap store.Snapshot, v int) []string {
	var ids []string
	for id, j := range snap.Judgements {
		if j.Kind == "policy" && j.Group == Group(v) && j.State == "open" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// CompareOrder orders two order answers: element by element, an int before a
// string, a shorter prefix first. The engine sorts by (rung, answer, id).
func CompareOrder(a, b []any) int {
	for i := range min(len(a), len(b)) {
		if c := comparePart(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

func comparePart(a, b any) int {
	ai, aInt := a.(int64)
	bi, bInt := b.(int64)
	switch {
	case aInt && bInt:
		return cmp.Compare(ai, bi)
	case aInt:
		return -1
	case bInt:
		return 1
	}
	return cmp.Compare(a.(string), b.(string))
}

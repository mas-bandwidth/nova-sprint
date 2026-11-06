// Package policy is layer 5 of nova-sprint v2 (docs/LAYER-5-POLICY.md): the
// rules the coordinator changes while the sprint runs, as a Starlark file
// stored as an immutable version in sprint.policy. It answers a closed set
// of questions the steps ask, checks every answer against its domain, and
// never writes the store during a tick and never raises a move. Pure Go over
// go.starlark.net: no clock, no bus, no socket; the verbs (verbs.go) are the
// only code that touches the store, through its policy writes.
//
// The state machine of versions, activation and the per-tick fallback is
// modelled in tla/Policy.tla.
package policy

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// The questions, the closed set: a policy's top-level name is one of these
// or starts with "_". Arity is the number of parameters each takes.
var Arity = map[string]int{
	"order": 1, "rows": 3, "reads": 1, "verdict": 2, "width": 1,
	"limits": 0, "landing": 1, "timeouts": 0, "stitch": 1, "prices": 0,
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
// Version is the version that answered (0 is the built-in default).
type Answers struct {
	Version  int
	Order    map[string][]any // each element an int64 or a string; see CompareOrder
	Rows     map[RowsQuestion][]string
	Reads    map[string]int
	Verdict  map[string]string // "yes" or "fix"
	Width    map[string]Width
	Limits   Limits
	Landing  map[string]Landing
	Timeouts Timeouts
	Stitch   map[string]bool
	Prices   map[string]Price
	shown    map[string]string // question(subject) -> the answer as Starlark prints it, for check
}

// Width is how many consumers a row works and how many it holds ready ahead.
type Width struct{ Work, Ahead int }

// Limits are the counts the steps compare against.
type Limits struct {
	Bounces, Faults, SameFinding, ReportsPerTick, RouteRestWindow, RouteRestAfter int
}

// Landing is the merging count at which a stream backs up and the one at
// which it flows again.
type Landing struct{ BackedUp, Flowing int }

// Timeouts are seconds of running time; BaseRegate is a rising list.
type Timeouts struct {
	RowSilentGone, RowSilentWithdraw, LandingStall, Judgement, RouteRestFor int
	BaseRegate                                                              []int
}

// Price is micro-dollars per million tokens in and out.
type Price struct{ In, Out int64 }

// Failure is one version's error or refused answer: none of that version's
// answers is used that tick. Line is the line of the question's def for a
// refusal, of the innermost frame for a runtime error.
type Failure struct {
	Version  int    // 0 is the built-in or an unstored file
	Label    string // v7, default, or a file's name
	Question string // "" for a compile error
	Subject  string
	Line     int
	Msg      string
}

func (f Failure) Error() string {
	at := f.Label
	if f.Line > 0 {
		at += fmt.Sprintf(" line %d", f.Line)
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
		Question: f.Error(), Command: "nova-sprint policy check " + f.Label, Group: Group(f.Version),
	}
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

package sprint

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// Every attempt end is classed as the worker's own work or as coordination: everything the
// machine, the bench, the harness, the provider or the seat caused. Coordination ends never
// count against a worker, never consume an attempt toward the bound, and accumulate on a
// coordination-failure counter by cause. Unknown is coordination until classed: never blame
// by default (the owner, 2026-10-11: "I'm sick of seeing friends and fleet machines get
// blamed for coordinator fuckups.").

// EndClass is the class of an attempt's end.
type EndClass string

const (
	// EndWorker is the model's own work: a not-done verdict on its own work, its push
	// refused by the gate for its own code, a broken read of its head.
	EndWorker EndClass = "worker"
	// EndCoordination is everything the machine, bench, harness, provider or seat caused.
	EndCoordination EndClass = "coordination"
)

// CoordinationCause is the cause of a coordination-failure end, for the counter.
type CoordinationCause string

const (
	CauseHarnessCrash   CoordinationCause = "harness crash before first turn"
	CauseProviderLimit  CoordinationCause = "provider or account limit"
	CauseContentFilter  CoordinationCause = "provider content-filter or key refusal"
	CauseIdleReap       CoordinationCause = "idle-reap with no run"
	CauseMissingTool    CoordinationCause = "missing bench tool or jar"
	CauseStagingRefused CoordinationCause = "staging refused"
	CauseTakenBack      CoordinationCause = "tick or hold took the card back"
	CauseUnlandedNeeds  CoordinationCause = "card dealt with unlanded needs"
	CauseHookGate       CoordinationCause = "hook-gate refusal"
	CauseStoreFault     CoordinationCause = "server or store fault"
	CauseLandConflict   CoordinationCause = "landing refused on a conflict"
	CauseUnknown        CoordinationCause = "unknown"
)

// coordinationCauseREs is each coordination cause and the words that say it, in the order a
// report is matched: the first cause whose pattern matches names it, so a report that says
// more than one (a lane that died on a temp-directory crash, a usage limit) classes once.
var coordinationCauseREs = []struct {
	cause CoordinationCause
	re    *regexp.Regexp
}{
	{CauseHarnessCrash, regexp.MustCompile(`(?i)no result before the first turn|crash before the first turn|harness exit.*no result|died in \d+s on a temp-directory crash`)},
	{CauseProviderLimit, regexp.MustCompile(`(?i)provider.*(limit|out.of.credit|usage limit)|account limit|usage limit|rate.limit|out.of.credit`)},
	{CauseContentFilter, regexp.MustCompile(`(?i)content.filter|key refusal|api key|a key.*refused|safety (system|filter)`)},
	{CauseIdleReap, regexp.MustCompile(`(?i)idle.*reap|reaped at idle|ran=false.*idle=0s|idle=0s with no run`)},
	{CauseMissingTool, regexp.MustCompile(`(?i)TLC owed|no rustc on PATH|no jar on the bench|missing (tool|jar)|executable.*not found`)},
	{CauseStagingRefused, regexp.MustCompile(`(?i)staging refused|no bench mirror|mirror missing|refused at staging`)},
	{CauseTakenBack, regexp.MustCompile(`(?i)took the card back|card taken back|tick.*take.?back|a hold returned the card|no beat carried.*live set`)},
	{CauseUnlandedNeeds, regexp.MustCompile(`(?i)unlanded needs|needs.*not landed|dealt before.*needs|before its needs landed`)},
	{CauseHookGate, regexp.MustCompile(`(?i)hook.gate|the hook.*refused|coordinator hook|the gate.*the hook`)},
	{CauseStoreFault, regexp.MustCompile(`(?i)store fault|server fault|internal server error|database.*(unavail|error|down)|the store errored`)},
	{CauseLandConflict, regexp.MustCompile(`(?i)landing refused.*conflict|conflict with another card|refused.*conflict|a conflict with another card's landing`)},
}

// workerEnd says an end is the model's own work: a not-done verdict over a pushed head, a
// gate refusal on the worker's own diff, or a broken read verdict on its head.
func workerEnd(verdict, report string) bool {
	low := strings.ToLower(verdict + " " + report)
	switch {
	case strings.Contains(low, "not-done") && strings.Contains(report, "pushed="):
		return true
	case strings.Contains(low, "gate") && strings.Contains(low, "diff") && strings.Contains(low, "your"):
		return true
	case strings.Contains(low, "broken read") && strings.Contains(low, "head"):
		return true
	}
	return false
}

// ClassEnd classes an attempt's end by its verdict and report: the worker's own (EndWorker)
// when it is the model's work, else coordination (EndCoordination) with its cause. Unknown
// is coordination, never the worker by default.
func ClassEnd(verdict, report string) (EndClass, CoordinationCause) {
	if workerEnd(verdict, report) {
		return EndWorker, ""
	}
	for _, c := range coordinationCauseREs {
		if c.re.MatchString(report) {
			return EndCoordination, c.cause
		}
	}
	return EndCoordination, CauseUnknown
}

// CoordinationCounter is the epoch's tally of coordination-failure ends: the total, the
// count by cause, and the count per member or friend who suffered it. Coordination ends
// never count against a worker, never consume an attempt toward the bound, and accumulate
// here by cause (the owner, 2026-10-11: "failures not attributable to the model, are
// supposed to accumulate to a counter for coordination failures."; "I'm sick of seeing
// friends and fleet machines get blamed for coordinator fuckups."). A coordinator fault is
// the seat's to fix, so every new end is pushed to the coordinator's hook (hook.go).
type CoordinationCounter struct {
	Epoch   uint64                    `json:"epoch"`
	Total   int                       `json:"total"`
	ByCause map[CoordinationCause]int `json:"by_cause,omitempty"`
	ByWho   map[string]int            `json:"by_who,omitempty"`
}

// NewCoordinationCounter is an empty counter for the epoch.
func NewCoordinationCounter(epoch uint64) *CoordinationCounter {
	return &CoordinationCounter{Epoch: epoch, ByCause: map[CoordinationCause]int{}, ByWho: map[string]int{}}
}

// Record classes an attempt's end (ClassEnd) and, when it is coordination, adds it to the
// counter by cause and by the member or friend who suffered it; a worker's own end is left
// alone (it counts on the worker, not here). It returns the class and cause as ClassEnd
// does, so a caller pushes the same cause to the coordinator's hook.
func (c *CoordinationCounter) Record(verdict, report, who string) (EndClass, CoordinationCause) {
	class, cause := ClassEnd(verdict, report)
	if class == EndCoordination {
		c.Add(cause, who)
	}
	return class, cause
}

// Add is one coordination end of cause, suffered by who (a member or friend), added to the
// counter. The classes from a producer's record use it directly (CoordinationCauseOfEnd).
func (c *CoordinationCounter) Add(cause CoordinationCause, who string) {
	if c.ByCause == nil {
		c.ByCause = map[CoordinationCause]int{}
	}
	if c.ByWho == nil {
		c.ByWho = map[string]int{}
	}
	c.Total++
	c.ByCause[cause]++
	if who != "" {
		c.ByWho[who]++
	}
}

// CoordinationCauseOfEnd names the coordination cause of a take's end word as the sprint
// records it on the producer's cost record (cost.go, Consumer.End): the provider and
// no-result ends, a staging refusal and a stop on unlanded needs are coordination by
// construction, each at the card's nearest listed cause; a report cause is recorded by its
// own cause word (coordinationEnded). "" for every other end (ok, a plain failed take,
// which is the worker's). Unknown is never returned: a reason nothing here names arrives
// as a report and classes through ClassEnd.
func CoordinationCauseOfEnd(end string) CoordinationCause {
	switch {
	case strings.HasPrefix(end, cardhdr.EndProvider):
		return CauseProviderLimit
	case strings.HasPrefix(end, cardhdr.EndNoResult):
		return CauseHarnessCrash
	case strings.HasPrefix(end, cardhdr.EndStaging):
		return CauseStagingRefused
	case end == EndNeedsUnlanded:
		return CauseUnlandedNeeds
	}
	// a report cause is recorded by its own cause word, which the cost record's line
	// round-trip spells hyphens as spaces (cost.go, Consumer.line/parseConsumer): match both
	// spellings, so a hyphenated cause word survives the store.
	end = strings.ReplaceAll(end, "-", " ")
	for _, cause := range reportCauses {
		if end == strings.ReplaceAll(string(cause), "-", " ") {
			return cause
		}
	}
	return ""
}

// reportCauses are the coordination causes that arrive as a finish's report and are
// recorded by their own cause word (coordinationEnded): every cause the counter keeps but
// the four cost-record ends above, which carry their own word. Unknown is not one: a
// reason nothing names classes as unknown but is never recorded as a named cause.
var reportCauses = []CoordinationCause{
	CauseContentFilter, CauseIdleReap, CauseMissingTool, CauseTakenBack,
	CauseHookGate, CauseStoreFault, CauseLandConflict,
}

// reportCause is the coordination cause a failed finish's report names, when it is one of
// the report causes (reportCauses): false for the provider, no-result, staging and
// unlanded-needs ends (each routed by its own word), for a worker's own end, and for a
// reason nothing names, which is the worker's plain failed work at the finish, never
// coordination (ClassEnd's unknown is for the end, not for a worker's finish).
func reportCause(report string) (CoordinationCause, bool) {
	class, cause := ClassEnd("failed", report)
	if class != EndCoordination {
		return "", false
	}
	for _, rc := range reportCauses {
		if cause == rc {
			return cause, true
		}
	}
	return "", false
}

// coordinationEnded is the unit of a failed finish whose report names a coordination cause
// (ClassEnd), routed like a take that ended with no work to judge (takeEnded): the work
// card is withdrawn, its primary goes back to ready, no failed work is recorded and no
// brief-defect or bound judgment is raised, so a coordination end never counts against the
// worker, never spends an attempt toward the bound and never hits the row's ok%. The take's
// cost record carries the cause word, so CoordinationOf counts it by cause and by the
// member or friend who suffered it (the owner, 2026-10-11: "failures not attributable to
// the model ... accumulate to a counter for coordination failures.").
func coordinationEnded(s *Snapshot, c, pr *Card, r FinishReq, cause CoordinationCause) Unit {
	set := nextGen(c, "", s.Now)
	set["withdrawn"], set[FieldTakeEnded] = stamp(s.Now), stamp(s.Now)
	line := cutText(strings.TrimSpace(r.Report), MaxProviderErrorBytes)
	set[FieldProviderError] = line
	dealt, taken := takeStamps(c)
	rec := costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)
	usage := ""
	if r.Usage != "" {
		usage = rec
	}
	take := c.Int("redeals") + 1
	set[FieldProviderTake+itoa(take)] = ProviderTake{Route: c.F(FieldRoute), Model: c.F(FieldModel), Member: c.Row,
		Finished: stamp(s.Now), Usage: usage, Error: line, Taken: taken}.String()
	prSet := map[string]string{}
	addConsumer(pr, prSet, workConsumer(s, c, take, string(cause), rec))
	// each new coordination end is pushed to the coordinator's hook: a coordinator fault is
	// the seat's to fix, so the note is addressed to the coordinator (notes.go, NCoordinationFailure)
	n := happened(NCoordinationFailure, pr.Row, s.Now, pr.ID)
	n.Who, n.To = c.Row, s.Coordinator
	n.What = fmt.Sprintf("%s ended on a coordination failure (%s): %s", c.ID, cause, line)
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, moveEntry(c, c.Row, Withdrawn, set, "taken", "dealt")),
		change(Work, moveEntry(pr, pr.Row, Ready, prSet, "work")),
	}, Notes: []Note{n}, Moved: fmt.Sprintf("%s working -> withdrawn gen=%d, a coordination failure (%s); %s working -> ready", c.ID, c.Int("gen")+1, cause, pr.ID)}
}

// CauseTotal is the counter's ends of one cause.
func (c *CoordinationCounter) CauseTotal(cause CoordinationCause) int { return c.ByCause[cause] }

// WhoTotal is the counter's ends suffered by one member or friend.
func (c *CoordinationCounter) WhoTotal(who string) int { return c.ByWho[who] }

// Line is the counter's one line for the dashboard, under the fleet and friends tables:
// the total and each cause that has one, in name order, so the line is settled.
func (c *CoordinationCounter) Line() string {
	if c.Total == 0 {
		return "coordination failures: 0 total"
	}
	causes := make([]string, 0, len(c.ByCause))
	for cause, n := range c.ByCause {
		if n > 0 {
			causes = append(causes, fmt.Sprintf("%s: %d", cause, n))
		}
	}
	sort.Strings(causes)
	return fmt.Sprintf("coordination failures: %d total (%s)", c.Total, strings.Join(causes, "; "))
}

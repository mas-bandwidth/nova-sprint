package sprintreplay

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// AttemptRow is one row of the attempts table: an attempt of a card that began after another
// (a retry), why it began in the log's words, and the class and detail the table gives it.
type AttemptRow struct {
	Card    string
	Attempt int
	Why     string
	Class   string
	Detail  string
}

// Input is an epoch's log, in file order, and its attempts table.
type Input struct {
	Lines []sprint.Line
	Rows  []AttemptRow
}

// LoadDir reads dir/epoch21.jsonl.gz and dir/attempts.tsv.
func LoadDir(dir string) (Input, error) {
	lf, err := os.Open(filepath.Join(dir, "epoch21.jsonl.gz"))
	if err != nil {
		return Input{}, err
	}
	defer lf.Close()
	zr, err := gzip.NewReader(lf)
	if err != nil {
		return Input{}, err
	}
	tf, err := os.Open(filepath.Join(dir, "attempts.tsv"))
	if err != nil {
		return Input{}, err
	}
	defer tf.Close()
	return Load(zr, tf)
}

// Load reads a log dump (one sprint.Line JSON object per line) and an attempts table (card,
// attempt, why, class, detail; tab separated, one row a line).
func Load(log, tsv io.Reader) (Input, error) {
	var in Input
	sc := bufio.NewScanner(log)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for n := 1; sc.Scan(); n++ {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		var l sprint.Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return in, fmt.Errorf("log line %d: %w", n, err)
		}
		in.Lines = append(in.Lines, l)
	}
	if err := sc.Err(); err != nil {
		return in, err
	}
	sc = bufio.NewScanner(tsv)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for n := 1; sc.Scan(); n++ {
		f := append(strings.Split(sc.Text(), "\t"), "", "", "", "", "")
		a, err := strconv.Atoi(f[1])
		if err != nil {
			return in, fmt.Errorf("attempts row %d: attempt %q: %w", n, f[1], err)
		}
		in.Rows = append(in.Rows, AttemptRow{Card: f[0], Attempt: a, Why: f[2], Class: f[3], Detail: f[4]})
	}
	return in, sc.Err()
}

// The outcomes of an attempt.
const (
	Landed = "landed" // its primary landed with this attempt its last
	Failed = "failed" // another attempt began after it, or its finish recorded a failure
	Open   = "open"   // neither by the log's end (in review, merging, dropped, or still working)
)

// Event is one moment of an attempt's life after its deal: a take, a progress line, a take
// that ended with no result, a redeal, the finish.
type Event struct {
	At   time.Time `json:"at"`
	What string    `json:"what"`
}

// Attempt is one work attempt's record: who and what it was dealt as, every moment of its
// life up to its end, its outcome and, failed, its class.
type Attempt struct {
	Primary  string `json:"primary"`
	N        int    `json:"n"`
	Stream   string `json:"stream"`
	Priority string `json:"priority"`
	Tier     string `json:"tier"`
	Route    string `json:"route"`
	Provider string `json:"provider"`
	Harness  string `json:"harness"`
	Member   string `json:"member"`
	Kind     string `json:"kind"` // "friend" (a friend's lane) or "fleet" (a routed member)

	Dealt     time.Time `json:"dealt"`
	End       time.Time `json:"end"`        // zero while open
	OutcomeAt time.Time `json:"outcome_at"` // when the outcome was known
	Outcome   string    `json:"outcome"`
	Events    []Event   `json:"events"`

	// What the log says of its end: the class the table maps it to and the table phrase that
	// triggered it (ClassTable), sprint.FailureClass of the failed report, sprint.ClassifyEnd,
	// the replayed place of the work card at its end, and the evidence.
	Class        string `json:"class,omitempty"`
	Phrase       string `json:"phrase,omitempty"`
	FailureClass string `json:"failure_class,omitempty"`
	EndKind      string `json:"end_kind,omitempty"`
	EndPlace     string `json:"end_place,omitempty"`
	Ran          string `json:"ran,omitempty"`     // the primary's ran_attempts line for this attempt
	Failure      string `json:"failure,omitempty"` // the primary's failure field at this attempt (failureSet: FailureClass of the report)
	Report       string `json:"report,omitempty"`
	Why          string `json:"why,omitempty"` // why the next attempt began
	Joined       bool   `json:"joined"`        // the attempts table has the next attempt's row

	// Features at the deal (Deal) are filled by features(); they read only what the log held
	// at or before Dealt.
	Deal DealView `json:"deal"`

	// Line positions break ties when distinct log lines have the same At. They
	// are replay bookkeeping, not a feature visible in an attempt record.
	dealPos, outcomePos int
}

// Result is a replay: the attempt records in deal order, and how many lines it stepped.
type Result struct {
	Attempts []*Attempt
	Lines    int
	Places   map[string]sprint.Place // the places after the last line
}

var workCard = regexp.MustCompile(`^(.+)\.w(\d+)$`)
var briefSize = regexp.MustCompile(`^<brief: (\d+) bytes`)

func key(p string, n int) string { return p + "#" + strconv.Itoa(n) }

// Replay steps the input through the sprint's rule code, in At order then file order, and
// builds every work attempt dealt in it. It reads no clock.
func Replay(in Input) *Result {
	idx := make([]int, len(in.Lines))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return in.Lines[idx[a]].At.Before(in.Lines[idx[b]].At) })

	rows := map[string]AttemptRow{}
	for _, r := range in.Rows {
		rows[key(r.Card, r.Attempt)] = r
	}
	res := &Result{Places: map[string]sprint.Place{}}
	at := map[string]*Attempt{}
	latest := map[string]int{}      // primary -> its latest attempt dealt
	briefBytes := map[string]int{}  // primary -> its brief's size as the dump keeps it
	briefEdits := map[string]int{}  // primary -> brief lines so far
	ranField := map[string]string{} // primary -> its ran_attempts field
	var order []*Attempt

	end := func(a *Attempt, t time.Time, place string) {
		if a.End.IsZero() {
			a.End, a.EndPlace = t, place
		}
	}
	fail := func(a *Attempt, t time.Time, pos int) {
		if a.Outcome != Failed {
			a.Outcome, a.OutcomeAt = Failed, t
			a.outcomePos = pos
		}
	}
	for pos, i := range idx {
		l := in.Lines[i]
		res.Places = sprint.ReplayOnto(res.Places, l)
		res.Lines++
		if l.Kind != sprint.LineMove {
			continue
		}
		place := func(table, card string) string {
			p, ok := res.Places[table+"/"+card]
			if !ok {
				return "-"
			}
			return p.Row + ":" + p.Col
		}
		// ReplayOnto steps a grouped move once for every named card. Extract each
		// card's attempt evidence from that same move without counting the line
		// again: GroupSets keeps the shared fields on one line and puts the other
		// cards in Cards (docs/SPEC-SPRINT.md, the log).
		for _, card := range l.Names() {
			l := l
			l.Card = card
			switch l.Table {
			case "work":
				p := l.Card
				if b := briefSize.FindStringSubmatch(l.Text["brief"]); b != nil {
					briefBytes[p], _ = strconv.Atoi(b[1])
					briefEdits[p]++
				}
				if v, ok := l.Set[sprint.FieldRanAttempts]; ok {
					ranField[p] = v
				}
				if f := l.Set[sprint.FieldFailure]; f != "" {
					if a := at[key(p, atoi(l.Set[sprint.FieldFailureAt]))]; a != nil {
						a.Failure = f
					}
				}
				if lf := l.Set[sprint.FieldLastFinish]; lf != "" {
					n, how, _ := strings.Cut(lf, ":")
					if a := at[key(p, atoi(n))]; a != nil {
						end(a, l.At, place("fleet", a.card()))
						if how == "failed" {
							fail(a, l.At, pos)
						}
					}
				}
				if strings.HasSuffix(l.To, ":landed") && !strings.HasSuffix(l.From, ":landed") {
					if a := at[key(p, latest[p])]; a != nil && a.Outcome == "" {
						a.Outcome, a.OutcomeAt = Landed, l.At // the primary landed: docs/SPEC-SPRINT.md, the landing
						a.outcomePos = pos
					}
				}
				if l.Verb == "drop" {
					if a := at[key(p, latest[p])]; a != nil {
						end(a, l.At, "dropped")
					}
				}
			case "fleet":
				m := workCard.FindStringSubmatch(l.Card)
				if m == nil {
					continue
				}
				p, n := m[1], atoi(m[2])
				a := at[key(p, n)]
				dealt := strings.HasSuffix(l.To, ":ready") && l.Set["member"] != "" && !l.Removed
				if a == nil {
					if !dealt || l.From != "" {
						continue // an attempt dealt before the dump began
					}
					a = &Attempt{Primary: p, N: n, Dealt: l.At, dealPos: pos, Outcome: ""}
					at[key(p, n)] = a
					order = append(order, a)
					// the attempt before it ends here, failed: another attempt began
					if prev := at[key(p, n-1)]; prev != nil {
						end(prev, l.At, place("fleet", prev.card()))
						if prev.Outcome != Landed {
							fail(prev, l.At, pos)
						}
						prev.Why = l.Text["why"]
						if r, ok := rows[key(p, n)]; ok {
							prev.Joined = true
							if prev.Why == "" {
								prev.Why = r.Why
							}
						}
					}
					if n > latest[p] {
						latest[p] = n
					}
					a.Deal.BriefBytes, a.Deal.BriefEdits = briefBytes[p], briefEdits[p]
				}
				if !a.End.IsZero() {
					continue // lines after its end are not its life
				}
				if dealt {
					a.dealtAs(l)
					if len(a.Events) > 0 {
						a.Events = append(a.Events, Event{l.At, "redeal"})
					}
				}
				switch {
				case l.Verb == "take" && strings.HasSuffix(l.To, ":working"):
					a.Events = append(a.Events, Event{l.At, "take"})
				case l.Verb == "progress":
					a.Events = append(a.Events, Event{l.At, "progress"})
				case l.Verb == "finish" && strings.HasSuffix(l.To, ":withdrawn"):
					e := l.Set["provider_error"]
					if sprint.FailureClass(e) == "no result" || strings.Contains(l.Cause, "left no result") {
						a.Events = append(a.Events, Event{l.At, "no-result"})
					} else {
						a.Events = append(a.Events, Event{l.At, "take-ended"})
					}
				case l.Verb == "finish" && (strings.HasSuffix(l.To, ":finished") || strings.HasSuffix(l.To, ":defect")):
					a.Report = l.Text["report"]
					a.Events = append(a.Events, Event{l.At, "finish"})
					end(a, l.At, place("fleet", l.Card))
				case (strings.HasSuffix(l.From, ":ready") || strings.HasSuffix(l.From, ":working")) &&
					!strings.HasSuffix(l.To, ":ready") && !strings.HasSuffix(l.To, ":working"):
					// Holds, friend takes and idle reaps also withdraw fleet work.
					// They end its active assignment without being a new take.
					a.Events = append(a.Events, Event{l.At, "take-ended"})
				}
			}
		}
	}
	for _, a := range order {
		if a.Outcome == "" {
			a.Outcome = Open
		}
		a.Ran = ranLine(ranField[a.Primary], a.N)
		if a.Outcome == Failed {
			classify(a)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return order[i].Dealt.Before(order[j].Dealt) })
	res.Attempts = order
	features(res)
	return res
}

func (a *Attempt) card() string { return a.Primary + ".w" + strconv.Itoa(a.N) }

// dealtAs takes who and what a deal line places the attempt with; a later redeal replaces the
// member and route (the deal view keeps the first: what was known at the deal).
func (a *Attempt) dealtAs(l sprint.Line) {
	first := a.Member == ""
	set := func(dst *string, v string) {
		if v != "" && (first || *dst == "") {
			*dst = v
		}
	}
	set(&a.Member, l.Set["member"])
	set(&a.Stream, l.Set["stream"])
	set(&a.Priority, l.Set["priority"])
	set(&a.Tier, l.Set["tier"])
	set(&a.Route, l.Set["route"])
	set(&a.Harness, l.Set["harness"])
	if m := l.Set["model"]; m != "" {
		p, _, _ := strings.Cut(m, "/")
		set(&a.Provider, p)
	}
	if strings.HasPrefix(a.Member, "friend.") {
		a.Kind = "friend"
		set(&a.Route, "friend")
		set(&a.Provider, "friend")
		set(&a.Tier, "friend")
	} else if a.Member != "" {
		a.Kind = "fleet"
	}
}

// ranLine is attempt n's line of a ran_attempts field, its words after "attempt <n>: ".
func ranLine(field string, n int) string {
	pre := "attempt " + strconv.Itoa(n) + ":"
	for _, l := range strings.Split(field, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), pre) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), pre))
		}
	}
	return ""
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// Digest is a sha256 of every attempt record, in order: two replays of the same input give
// the same digest.
func (r *Result) Digest() string {
	h := sha256.New()
	for _, a := range r.Attempts {
		b, _ := json.Marshal(a)
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Labelled is how many attempts failed (the ones the class table maps).
func (r *Result) Labelled() int {
	n := 0
	for _, a := range r.Attempts {
		if a.Outcome == Failed {
			n++
		}
	}
	return n
}

// Other is how many failed attempts the class table maps to "other".
func (r *Result) Other() int {
	n := 0
	for _, a := range r.Attempts {
		if a.Outcome == Failed && a.Class == ClassOther {
			n++
		}
	}
	return n
}

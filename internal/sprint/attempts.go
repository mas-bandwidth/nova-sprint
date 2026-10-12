package sprint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// An environment exit is a child that ended 134 or 137 (the opencode harness aborting under
// the landlock sandbox: `Error: Unknown: ChildProcess.spawn (...)`), or a child that left no
// result at all (`why=no-result`), whatever its exit: it did not judge the card, it is an
// event of the member's environment, and it must not consume the card's attempts or spend
// its reader slot. The finish records it on the attempt as `ended=environment exit=<n>` and
// keeps the last HarnessTailLines of the harness output on the card, so the cause is on the
// card a reader reads (docs/SPEC-SPRINT.md, "An environment exit";
// a-card-carries-its-consumer-history).

// The doctor's thresholds over the attempt records, keyed by epoch (docs/SPEC-SPRINT.md,
// "An environment exit"): a member whose environment exits are over 10% of its children is
// a FAIL, 3% to 10% a WARN, below an OK; a rate over fewer than EnvExitMinN children is
// never read (a small sample says nothing).
const (
	EnvExitMinN    = 10
	EnvExitFailPct = 10
	EnvExitWarnPct = 3
)

// HarnessTailLines is how many lines of the harness output the finish keeps on the card.
const HarnessTailLines = 40

// HarnessTailBytes bounds the tail kept on a card, keeping its end: forty lines of a harness
// that prints long lines must not make one card field a poison for the tick (a step's text
// is bounded by MaxCardTextBytes; this is a quarter of it).
const HarnessTailBytes = 16 << 10

// envExitWord begins every environment exit's ended word (EnvironmentExit).
const envExitWord = "environment exit="

// EnvironmentExit is the attempt's word for an environment exit: `environment exit=<n>`.
func EnvironmentExit(exit int) string { return envExitWord + itoa(exit) }

// IsEnvironmentExit says an ended word (a card's FieldEnvExit, a consumer record's end) is
// an environment exit's: `environment exit=<n>`, or a no-result take's end that carries it
// (`no result environment exit=<n>`, takeEnded).
func IsEnvironmentExit(end string) bool { return strings.Contains(end, envExitWord) }

// envExitCode is the exit code an environment exit's end names, "" for another end.
func envExitCode(end string) string {
	_, code, ok := strings.Cut(end, envExitWord)
	if !ok {
		return ""
	}
	return code
}

// HardEnvExit says a child's exit is the harness dying (134, an abort; 137, a kill): the
// environment exit that spends nothing at all. A no-result child at another exit is an
// environment exit too, recorded and counted as one, but its take still counts as an ended
// take (takeEnded): the owner's rules for a take that left no result stand (2026-10-01, it
// is redealt counting the take; 2026-10-02, rule 2, the second identical end is the bound;
// 2026-10-03, flash first escalates), since a model that wrote nothing exits 0 too.
func HardEnvExit(exit int) bool { return exit == 134 || exit == 137 }

// EnvExit classifies a failed finish's child as an environment exit (docs/SPEC-SPRINT.md,
// "An environment exit"): the ended word (EnvironmentExit) and true when the child's exit is
// 134 or 137, or when its report says it left no result (IsNoResult, the member's
// `why=no-result`) at ANY exit, 0 included (0 is also the exit a member that does not pass
// one reports); "", false otherwise. A verdict at another exit is the child's own.
func EnvExit(report string, exit int) (ended string, env bool) {
	switch {
	case exit == 134, exit == 137, IsNoResult(report):
		return EnvironmentExit(exit), true
	}
	return "", false
}

// EnvExitStatus is the doctor's verdict for a member whose attempt records hold env
// environment exits out of n children: FAIL over EnvExitFailPct%, WARN from EnvExitWarnPct%
// to EnvExitFailPct%, OK below or when n is under EnvExitMinN (the counts are integers, so
// the rate is compared without rounding: env*100 against n*pct).
func EnvExitStatus(env, n int) string {
	if n < EnvExitMinN {
		return DoctorOK
	}
	switch {
	case env*100 > n*EnvExitFailPct:
		return DoctorFail
	case env*100 >= n*EnvExitWarnPct:
		return DoctorWarn
	}
	return DoctorOK
}

// EnvExitCheck is the member-env check of a member whose attempt records hold env
// environment exits out of n children in the window (docs/SPEC-SPRINT.md, "An environment
// exit"): `child exits: <k> of <n> children in the last hour ended 134/137 with no result
// (<rate>%), last: <first line of the harness tail>`, remedy `nova-sprint card <id>`, at
// EnvExitStatus. A member under EnvExitMinN children is no check (false): its rate says
// nothing. An OK names no card to read. last is the first line of the last environment
// exit's harness tail, id the card it ended on.
func EnvExitCheck(member string, env, n int, last, id string) (DoctorCheck, bool) {
	if n < EnvExitMinN {
		return DoctorCheck{}, false
	}
	status := EnvExitStatus(env, n)
	c := DoctorCheck{Area: AreaFleet, Check: CheckMemberEnv, Subject: member, Status: status,
		Evidence: fmt.Sprintf("child exits: %d of %d children in the last hour ended 134/137 with no result (%d%%), last: %s", env, n, env*100/n, orDash(last))}
	if status != DoctorOK && id != "" {
		c.Remedy = "nova-sprint card " + id
	}
	return c, true
}

// EnvExitRecord is the finish's whole record of an environment exit on the attempt: the
// ended word (`environment exit=<n>`) and the harness tail the finish keeps on the card
// (HarnessTailLines), so the cause rides the card a reader reads (docs/SPEC-SPRINT.md, "An
// environment exit"; a-card-carries-its-consumer-history).
type EnvExitRecord struct {
	Ended   string
	Harness string
}

// EnvironmentExitRecord is the record of an environment exit at exit with the child's whole
// harness output: the ended word EnvironmentExit(exit) and the last HarnessTailLines of the
// output, at most HarnessTailBytes of it. It is the one place the finish, the read's return
// and their tests build the pair, so the tail's bounds are never named twice.
func EnvironmentExitRecord(exit int, harnessOut string) EnvExitRecord {
	return EnvExitRecord{Ended: EnvironmentExit(exit), Harness: tailBytes(HarnessTail(harnessOut, HarnessTailLines), HarnessTailBytes)}
}

// envExitFields is the record's fields on a card (FieldEnvExit, FieldHarnessTail), and the
// fields it unsets: a record with no harness output clears an earlier tail rather than
// leave another exit's cause beside this one's word.
func (r EnvExitRecord) envExitFields() (set map[string]string, unset []string) {
	set = map[string]string{FieldEnvExit: r.Ended}
	if r.Harness != "" {
		set[FieldHarnessTail] = r.Harness
	} else {
		unset = []string{FieldHarnessTail}
	}
	return set, unset
}

// HarnessTail is the last n lines of a child's harness output, so the finish can keep the
// cause on the card (HarnessTailLines). Fewer than n lines keeps all; an empty output is
// empty; a trailing newline is preserved.
func HarnessTail(out string, n int) string {
	if out == "" || n <= 0 {
		return ""
	}
	lines := strings.Split(out, "\n")
	trailing := lines[len(lines)-1] == ""
	if trailing {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s := strings.Join(lines, "\n")
	if trailing {
		s += "\n"
	}
	return s
}

// tailBytes is s cut to its last n bytes at a rune boundary, beginning "..." when it was cut:
// the end of a harness's output is its cause.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - (n - len("..."))
	for cut < len(s) && !utf8.RuneStart(s[cut]) {
		cut++
	}
	return "..." + s[cut:]
}

// tailFirstLine is the first line of a harness tail, the doctor's `last:`.
func tailFirstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimLeft(s, "\n"), "\n")
	return strings.TrimSpace(line)
}

// EnvExitCounts is one member's children in the window, as the doctor names them
// (EnvExitCheck) and `nova-sprint stats --children` prints them: how many of the member's
// children ended 134 or 137 (Env, HardEnvExit: the doctor's "ended 134/137") out of all its
// children that ended in the window (N), the first line of the last such exit's harness tail
// (Last) and the primary it ended on (Card), and the histogram of every environment exit by
// child exit code (Exits, "134" -> count, a no-result at exit 0 in "0"); the N-Env other
// children include those no-results.
type EnvExitCounts struct {
	Member string
	Env    int
	N      int
	Last   string
	Card   string
	Exits  map[string]int
}

// EnvExitChildren counts each member's children over the attempt records of the snapshot
// (docs/SPEC-SPRINT.md, "An environment exit"): every take of a work card and every run of
// a read card that ended leaves one consumer record on its primary (cost.go, CardCostOf),
// with who ran it, how it ended and when; the snapshot is the epoch's, so the counts are
// keyed by the epoch (a clear starts them at nothing). A record whose end is an environment
// exit (IsEnvironmentExit) counts in Env and in its code's bucket; every record that ended
// in the window counts in N; an environment exit counts in its code's bucket, and in Env
// only at 134 or 137. A friend's runs are not a member's environment and are left out. Last
// and Card are the latest 134/137's: the first line of the harness tail on the card it ended
// on, else on its primary.
func EnvExitChildren(s *Snapshot, now time.Time, window time.Duration) []EnvExitCounts {
	if s == nil || s.Work == nil {
		return nil
	}
	since := now.Add(-window)
	by := map[string]*EnvExitCounts{}
	lastAt := map[string]time.Time{}
	for _, pr := range s.Work.Cards() {
		for _, con := range CardCostOf(pr).Consumers {
			member := s.consumerMember(con)
			if member == "" || strings.HasPrefix(con.End, cardhdr.EndStaging) {
				continue // a launch refused at staging ran no child
			}
			at, err := time.Parse(time.RFC3339, con.At)
			if err != nil || at.Before(since) || at.After(now) {
				continue
			}
			c := by[member]
			if c == nil {
				c = &EnvExitCounts{Member: member, Exits: map[string]int{}}
				by[member] = c
			}
			c.N++
			if !IsEnvironmentExit(con.End) {
				continue
			}
			code := envExitCode(con.End)
			c.Exits[code]++
			if n, err := strconv.Atoi(code); err != nil || !HardEnvExit(n) {
				continue // a no-result at another exit: in its code's bucket, not the 134/137 count
			}
			c.Env++
			if prev, seen := lastAt[member]; !seen || !at.Before(prev) {
				lastAt[member] = at
				tail := pr.F(FieldHarnessTail)
				if s.Fleet != nil {
					if run := s.Fleet.Card(con.Card); run != nil && run.F(FieldHarnessTail) != "" {
						tail = run.F(FieldHarnessTail)
					}
				}
				c.Last, c.Card = tailFirstLine(tail), pr.ID
			}
		}
	}
	out := make([]EnvExitCounts, 0, len(by))
	for _, m := range slices.Sorted(maps.Keys(by)) {
		out = append(out, *by[m])
	}
	return out
}

// consumerMember is the fleet member a consumer record's child ran on: a work take's member
// (its row), or a read's reader, reader-<m> naming the machine m (ReaderMachine); "" for a
// friend's run, or a name the fleet has no member row for.
func (s *Snapshot) consumerMember(con Consumer) string {
	who := con.Who
	if m, ok := ReaderMachine(who); ok {
		who = m
	}
	if who == "" || IsFriendRow(who) {
		return ""
	}
	if s.Fleet != nil && (!s.Fleet.HasRow(who) || IsFriendRow(who)) {
		return ""
	}
	return who
}

// RetiredByEnvironment is the retired_by of a read card its reader handed back because its
// child ended on the member's environment (EnvExit on the return's reason and exit): no
// verdict, and not in spentBy, so its reader is not spent at the attempt and the read-card
// deal may deal it the read again at a fresh generation (docs/SPEC-SPRINT.md, "An
// environment exit"; the seat's 2026-10-11 read: a card read once by every member ended
// `cannot ask`).
const RetiredByEnvironment = "environment"

// readEnvExitUnit retires the read card c on a fleet row whose return was an environment
// exit (ended), spending no reader: the ended word and the harness tail on the read card
// and on its primary, the run's cost record kept as a returned run's, and the primary's
// consumer record with that end (the attempt record the doctor counts, EnvExitChildren).
func readEnvExitUnit(s *Snapshot, c, pr *Card, r ReadReq, ended, name string) Unit {
	rec := EnvironmentExitRecord(r.Exit, r.Harness)
	run := nextTake(c, FieldReadTake)
	cost := readCostRecord(s, c, r.Usage, c.F("asked"), cmpOr(c.F("begun"), stamp(s.Now)))
	// the read identity's count (EnvReadCap): at the cap the read is retired as a return of
	// the reader's own, which spends it at the attempt (spentBy), and one bound judgment
	// holds the card
	history, capped := readEnvExitCount(pr, readAttempt(c), name, r.Exit)
	retiredBy := RetiredByEnvironment
	if capped {
		retiredBy = RetiredByReturned
	}
	set := map[string]string{"retired": stamp(s.Now), "retired_by": retiredBy, "reason": cutText(r.Reason, MaxCardTextBytes),
		FieldReadTake + itoa(run): cost}
	fields, _ := rec.envExitFields()
	maps.Copy(set, fields)
	if r.Usage != "" {
		set["usage"] = r.Usage
	}
	changes := []Change{change(Fleet, removeEntry(c, set))}
	if pr.Placed() {
		prSet := map[string]string{FieldEnvExitReads: history}
		addConsumer(pr, prSet, readConsumer(s, c, run, ended, cost))
		if rec.Harness != "" {
			prSet[FieldHarnessTail] = rec.Harness
		}
		changes = append(changes, change(Work, setEntry(pr, prSet)))
	}
	n := happened(NReadReturned, pr.Row, s.Now, pr.ID)
	n.Who, n.Attempt, n.What = name, c.Int("attempt"), name+" returned "+c.ID+": "+r.Reason+"; "+ended+": the child ended on the member's environment, its reader is not spent"
	notes := []Note{n}
	moved := c.ID + " " + c.Col + " -> " + RetiredByEnvironment + " (retired: " + ended + ", no verdict, its reader not spent)"
	if capped {
		n.What = name + " returned " + c.ID + ": " + r.Reason + "; " + ended + ": the read's environment cap is reached, its reader is spent"
		notes = []Note{n, envReadBound(s, pr, c.ID, name, history)}
		moved = c.ID + " " + c.Col + " -> " + RetiredByReturned + " (retired: " + ended + ", the read's environment cap reached)"
	}
	return Unit{Key: pr.ID, Stream: pr.Row, Changes: changes, Notes: notes, Moved: moved}
}

// EnvReadCap bounds a read identity's environment exits (one reader's read of one attempt of
// a primary, every generation of its read card): its children dying 134 or 137 this many
// times spends the reader at the attempt, so the read is dealt to it no more, and one bound
// judgment (NBound, a kind wait refuses) holds the card, naming the read, the reader and the
// exit codes (docs/SPEC-SPRINT.md, "An environment exit"; the cold reader on #129).
const EnvReadCap = 3

// FieldEnvExitReads is the primary's record of its reads' environment exits, one
// `<attempt>:<reader>=<exit>` per exit, comma separated, oldest first: the read card is
// retired at each one and dealt again at a fresh generation, so the count lives on the
// primary, keyed by the attempt (a rework's new attempt starts with none).
const FieldEnvExitReads = "env_exit_reads"

// readEnvExitCount is the primary's read history with one more environment exit of reader's
// read at attempt (exit), and whether that one reaches EnvReadCap for the read identity.
func readEnvExitCount(pr *Card, attempt int, reader string, exit int) (history string, capped bool) {
	key := itoa(attempt) + ":" + reader + "="
	h := append(Split(pr.F(FieldEnvExitReads)), key+itoa(exit))
	n := 0
	for _, e := range h {
		if strings.HasPrefix(e, key) {
			n++
		}
	}
	return strings.Join(h, ","), n >= EnvReadCap
}

// envReadBound is the bound judgment of a read identity at EnvReadCap: on the primary,
// naming the read card, the reader, the attempt and the exit codes.
func envReadBound(s *Snapshot, pr *Card, readCard, reader, history string) Note {
	attempt := readAttempt(pr)
	key := itoa(attempt) + ":" + reader + "="
	var codes []string
	for _, e := range Split(history) {
		if code, ok := strings.CutPrefix(e, key); ok {
			codes = append(codes, code)
		}
	}
	j := judgment(NBound, pr.Row, s.Now, 0, pr.ID)
	if len(j.Decisions) == 0 {
		j.Decisions = append([]string(nil), TickDecisions[NBound]...)
	}
	j.Who, j.Attempt, j.Card = reader, attempt, readCard
	j.What = fmt.Sprintf("%s: %s's read of attempt %d ended on the environment %d times (the cap is %d) and is not dealt to %s again: exits %s; rework it with a fix or drop it; its history: nova-sprint card %s",
		readCard, reader, attempt, len(codes), EnvReadCap, reader, strings.Join(codes, ", "), pr.ID)
	return j
}

// EnvExitCardCap bounds the environment exits that spend nothing (HardEnvExit) on one card at
// one attempt: a card whose children die 134 or 137 this many times is dealt no more, and the
// tick raises one bound judgment (NBound, a kind wait refuses: wait_refused.go) naming the
// card, the members and the exit codes, until the seat reworks or drops it
// (docs/SPEC-SPRINT.md, "An environment exit"; the coordinator, 2026-10-12: the 134/137 loop
// had no bound).
const EnvExitCardCap = 3

// FieldEnvExitHistory is the work card's record of its spending-nothing environment exits at
// its attempt, one `<member>=<exit>` per exit, comma separated, oldest first (envExited): the
// work card is the attempt's own, so a rework's new attempt starts with none.
const FieldEnvExitHistory = "env_exit_history"

// envExitHistory is the work card's environment exits at its attempt (FieldEnvExitHistory).
func envExitHistory(wc *Card) []string { return Split(wc.F(FieldEnvExitHistory)) }

// AtEnvExitBound is the primary c's work card at its attempt when it is withdrawn with
// EnvExitCardCap or more environment exits on it (FieldEnvExitHistory): the deal deals it to
// no machine and offers it to no friend, and the tick holds it with the bound judgment
// (envExitBoundWhat); nil otherwise.
func AtEnvExitBound(s *Snapshot, c *Card) *Card {
	if s == nil || s.Fleet == nil || c == nil {
		return nil
	}
	wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt")))
	if wc == nil || wc.Col != Withdrawn || len(envExitHistory(wc)) < EnvExitCardCap {
		return nil
	}
	return wc
}

// envExitBoundWhat is the bound judgment's text for a work card at the environment-exit cap:
// the card, how many times, the members, the exit codes, the first line of the last tail,
// and what answers it.
func envExitBoundWhat(wc *Card, primary string) string {
	h := envExitHistory(wc)
	var members, codes []string
	for _, e := range h {
		m, code, _ := strings.Cut(e, "=")
		if !slices.Contains(members, m) {
			members = append(members, m)
		}
		codes = append(codes, code)
	}
	return fmt.Sprintf("%s: attempt %s's child ended on the member's environment %d times (the cap is %d) and is not dealt again: members %s; exits %s; last: %s; rework it with a fix or drop it; its history: nova-sprint card %s",
		wc.ID, orDash(wc.F("attempt")), len(h), EnvExitCardCap, strings.Join(members, ", "), strings.Join(codes, ", "), orDash(tailFirstLine(wc.F(FieldHarnessTail))), primary)
}

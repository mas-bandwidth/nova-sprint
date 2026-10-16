package sprint

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The brief's bound (docs/SPEC-SPRINT.md, "The brief is wrong, not the worker"; the owner,
// 2026-10-03, after two gating cards were reworked to attempts 262 and 17 with the same reader
// finding every time: "These two gating cards getting rejected, should have been escalated to
// you, the coordinator, way sooner than this"). The same finding twice means the brief is
// wrong, not the worker, and no further attempt is possible without changing the brief: rework
// refuses such a card, whoever answers, and the judgment raised for it offers brief and drop,
// never rework. The decision is one pure function over the primary, AtBriefBound; a changed
// brief (Brief, FieldBriefAttempt) resets it.
//
// The attempt cap (the owner, 2026-10-04, after a night in which only a repeated finding
// bounded a card and one that failed differently each time ran for ever; and 2026-10-12,
// after epoch 16 ran one card to attempt 11 and another to 22: "11/22 attempts is tooooo
// many. max attempts should be configurable in nova-config, and default to 4"; "The max
// attempts 4 = max attempts before you do judgement and decide what to do"): a card runs at
// most AttemptsCap attempts between two coordinator decisions on it. The number is the
// stream's own `stream set --attempts`, else nova-config's sprint row max_attempts
// (Snapshot.MaxAttempts, read with the routes), else AttemptsDefault. Every attempt counts,
// whatever ended it (a failed finish, a reader's finding inside or outside PATHS, a
// deadline or harness fault, a push or lane refused, a landing refused, in the shared
// ledgers too): the count is the card's attempt less FieldDecidedAttempt, the attempt at the
// coordinator's last decision. Only a coordinator decision resets it (brief, brief --tier,
// rework by the seat, continue; a drop ends it): a rule's widen or rework, a stream's set
// --base and the machine's tier escalation never do. At the bound the card is the
// coordinator's as exactly one NBriefWrong judgment, "brief defect after N attempts, $X
// spent", with the findings of every attempt listed, and the decisions carry, brief, rework
// --tier (re-tier) and drop; it is dealt to no machine and no friend until a decision. The
// attempt cap's default answer of 2026-10-04 (a friend's card past the cap, AttemptCapDeal)
// is retired: a friend tier is reached by the coordinator's re-tier. tla/SprintRules.tla
// Part "reads" and "answers", NoAttemptPastCapWithoutDecision; tla/FriendEmptyRun.tla
// CapDeal; docs/SPEC-SPRINT.md, the bounds.

// LandRefusedConflict is the finding a landing's refusal for a merge conflict with the advanced
// base leaves on its card (LandRefusedFinding, RefusedConflict). Two of them are not the brief
// wrong: the base moved under a correct head (BriefBound.Conflict; tla/Land.tla, THE BOUND,
// ConflictBoundCarries).
const LandRefusedConflict = LandRefusedFinding + RefusedConflict

// FieldFindingAttempt is the attempt whose broken reads' finding the primary's `finding`
// carries (Rework writes both: the attempt it sends back and what its readers found). A
// primary with a finding and no attempt recorded (admitted before this field) was reworked
// from the attempt before its current one.
const FieldFindingAttempt = "finding_attempt"

// FieldBriefAttempt is the primary's attempt when its brief was last replaced (Brief); absent,
// the brief is the one add gave it, at attempt 0. The same-finding-twice bound counts from it;
// the attempt cap no longer does (FieldDecidedAttempt), since a rule's widen, the cap's old
// friend deal and stream set --base all replace the brief without a mind deciding anything.
const FieldBriefAttempt = "brief_attempt"

// FieldDecidedAttempt is the primary's attempt at the coordinator's last decision on it: a
// brief or brief --tier run by a person or the seat, a rework the seat answers (--tier and
// --carry included), a continue. Absent, no decision was made: 0. The attempt cap counts
// every attempt after it (AtBriefBound: attempt - decided), so a card never runs attempt
// cap+1 without a decision in between. A rule's act never writes it (the actor is told apart
// by ReworkCard.Rule and BriefReq.ByRule).
const FieldDecidedAttempt = "decided_attempt"

// FieldFindings is the primary's list of what each attempt sent back found, one line an
// attempt ("attempt <n>: <the finding's first sentence>"), appended by Rework and cut to
// MaxCardTextBytes from the front (the latest kept): what the cap's judgment lists, and
// kept by the card the cap's default answer deals to a friend.
const FieldFindings = "findings"

// PropAttempts is the work table's property that held the sprint's attempt cap until
// 2026-10-12 (set --attempts, init --attempts): retired, the number is nova-config's sprint
// row max_attempts (the owner: one source, no twin), and a value left in a store is ignored
// (AttemptsCap). FieldAttempts is a stream's control card field holding the stream's own cap
// (stream set --attempts), over the sprint's; AttemptsDefault is the cap when neither is set
// (config.DefaultMaxAttempts), and AttemptsMax the most a cap may be.
const (
	PropAttempts        = "attempts"
	FieldAttempts       = "attempts"
	AttemptsDefault     = 4
	AttemptsMax         = 100
	ReviewBreakerNew    = 4
	ReviewBreakerRounds = 6
)

// MaxAttemptsSetting is the nova-config verb that sets the sprint's attempt cap, as the
// refusals of set --attempts and init --attempts name it.
const MaxAttemptsSetting = "nova-config sprint set --max_attempts <n>"

// AttemptsCap is how many attempts one card may run in the stream between two coordinator
// decisions: the stream's setting (FieldAttempts), else nova-config's sprint row max_attempts
// as the routes read applied it (Snapshot.MaxAttempts), else AttemptsDefault. An absent,
// empty or unparseable max_attempts is the default, so the sprint works with a nova-tools not
// yet adopted. The work table's PropAttempts is read no more.
func (s *Snapshot) AttemptsCap(stream string) int {
	if s.Merge != nil {
		if n := s.StreamCtl(stream).Int(FieldAttempts); n > 0 {
			return n
		}
	}
	return s.SprintAttemptsCap()
}

// AttemptsCapOf is AttemptsCap memoized by stream, for a loop over many cards (the deal and
// the tick's friend offer): the stream's control card is looked up once a stream, so the
// deal at scale allocates nothing a card (TestTickDealAllocatesNoPerCardRouteMapAtScale).
func (s *Snapshot) AttemptsCapOf() func(stream string) int {
	caps := map[string]int{}
	return func(stream string) int {
		if n, ok := caps[stream]; ok {
			return n
		}
		n := s.AttemptsCap(stream)
		caps[stream] = n
		return n
	}
}

// SprintAttemptsCap is the sprint-wide attempt cap, nova-config's max_attempts, else
// AttemptsDefault: what every stream without its own setting runs under. It is asked once a
// card by the deal, so it allocates nothing: an absent value is the default without a parse.
func (s *Snapshot) SprintAttemptsCap() int {
	if n, ok := maxAttemptsOf(s.MaxAttempts); ok {
		return n
	}
	return AttemptsDefault
}

// maxAttemptsOf is the applied max_attempts as a cap: the whole number from 1 to AttemptsMax
// it holds, and false for "", a value that is no such number, or one above AttemptsMax
// (the default then).
func maxAttemptsOf(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > AttemptsMax || strconv.Itoa(n) != v {
		return 0, false
	}
	return n, true
}

// AttemptsCapSource says where the sprint-wide cap comes from, for the settings line:
// "nova-config sprint:max_attempts" when a parseable value is applied, else the default.
func (s *Snapshot) AttemptsCapSource() string {
	if n, ok := maxAttemptsOf(s.MaxAttempts); ok {
		return fmt.Sprintf("max-attempts %d (nova-config sprint:max_attempts)", n)
	}
	return fmt.Sprintf("max-attempts %d (the default; nova-config sprint:max_attempts %s)", AttemptsDefault, orDash(strings.TrimSpace(s.MaxAttempts)))
}

// ParseAttempts is an attempt cap as the verbs take it: a whole number from 1 to
// AttemptsMax, or ReadTierDefault (0: the default).
func ParseAttempts(text string) (int, error) {
	if text == ReadTierDefault {
		return 0, nil
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(text), "%d", &n); err != nil || n < 1 || n > AttemptsMax || fmt.Sprint(n) != strings.TrimSpace(text) {
		return 0, fmt.Errorf("--attempts wants a whole number from 1 to %d, or %s; found %q", AttemptsMax, ReadTierDefault, text)
	}
	return n, nil
}

// BriefBound is why a primary's brief is wrong: two attempts since the brief last changed
// whose readers found the same thing (Attempts and Finding), or the cap's attempts since the
// coordinator last decided on the card (Since and Cap, with Finding ""): then Spent is what
// the card has cost (MoneyText, "" when nothing of it was priced) and Findings what each
// attempt found, in order.
type BriefBound struct {
	ID       string
	Attempts [2]int // the two attempts that failed the same way, in order
	Finding  string // the finding's first sentence, as the first of the two said it
	Since    int    // the attempts since the coordinator last decided, when that is the bound
	Cap      int    // the cap those attempts reached (max_attempts)
	Spent    string
	Findings []string
	// Harness is true when the cap was reached mostly on the harness: more of the attempts
	// since the coordinator's last decision ended on a harness fault (classifyEnd says
	// EndHarness: no worker report, ran=false in FieldRanAttempts) than on a worker's or the
	// brief's end. A harness end counts toward max_attempts as an attempt and never as a vote
	// that the brief is wrong, so such a cap is worded "harness fault, not the brief", and the
	// card is not marked a brief defect. HarnessEnds is how many of the Since attempts ended
	// on the harness.
	Harness     bool
	HarnessEnds int
	// Conflict is true when the two same findings were both the landing's refusal for a merge
	// conflict with the advanced base (LandRefusedConflict): the head is right and the base moved
	// under it, so the bound is the base's, never the brief's. It is worded so, offers rework
	// --carry first (carry the head, merge the current base, rerun the gates), and the card is
	// taken for rework --carry however the brief-defect rule marked it (IsConflictBound,
	// reworkWhyCarry). A gate, checks or paths refusal twice is not one: the head itself is wrong.
	// tla/Land.tla, THE BOUND, ConflictBoundCarries; reversed witness
	// MCLandBoundBrokenConflictIsBriefDefect.
	Conflict bool
}

// String is the bound said in one line: what repeated and where, or the cap, the spend and
// the findings, then the verdict. A card that reached the cap mostly on harness ends is
// worded as the harness's, never as a vote that the brief is wrong.
func (b BriefBound) String() string {
	if b.Conflict {
		return fmt.Sprintf("%s: the landing refused its head twice only for a merge conflict with the advanced base (attempts %d and %d): the brief is not the cause",
			b.ID, b.Attempts[0], b.Attempts[1])
	}
	if b.Finding != "" {
		return fmt.Sprintf("%s has failed the same way twice (attempts %d and %d: %s); the brief is wrong, not the worker",
			b.ID, b.Attempts[0], b.Attempts[1], b.Finding)
	}
	if b.Harness {
		return fmt.Sprintf("%s: harness fault, %d attempts (max_attempts %d; %d ended on the harness, %d on a worker's report): the brief is not the cause",
			b.ID, b.Since, b.Cap, b.HarnessEnds, b.Since-b.HarnessEnds)
	}
	spent := "nothing priced"
	if b.Spent != "" {
		spent = b.Spent + " spent"
	}
	line := fmt.Sprintf("%s: brief defect after %d attempts (max_attempts %d) without a coordinator decision, %s; the brief is wrong, not the worker", b.ID, b.Since, b.Cap, spent)
	if len(b.Findings) > 0 {
		line += "; findings: " + strings.Join(b.Findings, "; ")
	}
	return line
}

// Remedy is what the coordinator decides at the bound, each a decision that opens a fresh
// window of attempts: the brief corrected in place, the card's next attempt staged from its
// last pushed head; reworked on another tier (re-tier); carried on from its last head; or
// dropped. The same-finding-twice bound offers no rework: a rework's --fix changes the brief
// not at all. A conflict bound (Conflict) is the exception, and offers rework --carry first:
// the base moved under a correct head (tla/Land.tla, THE BOUND, ConflictBoundCarries).
func (b BriefBound) Remedy() string {
	if b.Conflict {
		// the head is right and the base moved: carry the head, merge the current base and rerun
		// the gates first (tla/Land.tla, THE BOUND, ConflictBoundCarries)
		return "run: nova-sprint rework " + b.ID + " --carry --fix <text> (carry on from its last pushed head: merge the current base, rerun the gates), nova-sprint brief " + b.ID + " --brief-file <path> (the brief corrected in place), or nova-sprint drop " + b.ID + " --reason '<why>'"
	}
	if b.Finding != "" {
		return "run: nova-sprint brief " + b.ID + " --brief-file <path> (the brief corrected in place, its next attempt from its last pushed head), or nova-sprint drop " + b.ID + " --reason '<why>'"
	}
	if b.Harness {
		return "run: nova-sprint rework " + b.ID + " --carry --fix <text> (carry on from its last pushed head), or nova-sprint drop " + b.ID + " --reason '<why>'"
	}
	return "run: nova-sprint brief " + b.ID + " --brief-file <path> (the brief corrected in place, its next attempt from its last pushed head), nova-sprint rework " + b.ID + " --fix <text> --tier <a higher tier> (re-tier), nova-sprint rework " + b.ID + " --carry --fix <text>, or nova-sprint drop " + b.ID + " --reason '<why>'"
}

// Why is the rework's refusal of a card at the bound: the line, and the remedy.
func (b BriefBound) Why() string { return b.String() + "; " + b.Remedy() }

// FindingClass is what two findings are compared by: the whole finding, its whitespace
// collapsed and its case folded (two findings that share a first sentence, or a first line,
// and differ after it are two findings); and a finding of files outside the card's PATHS,
// however it is worded ("files outside PATHS", "outside its PATHS"), is the one class `files
// outside paths`. "" for an empty finding.
func FindingClass(finding string) string {
	text := strings.ToLower(strings.Join(strings.Fields(finding), " "))
	if strings.Contains(text, "outside") && strings.Contains(text, "paths") {
		return "files outside paths"
	}
	return text
}

// SameFinding says two findings are the same finding: each has a class and it is the same one.
func SameFinding(a, b string) bool {
	c := FindingClass(a)
	return c != "" && c == FindingClass(b)
}

// firstSentence is a finding's first sentence as said, for the line that names it.
func firstSentence(finding string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(finding), "\n")
	if i := strings.Index(line, ". "); i >= 0 {
		line = line[:i+1]
	}
	return cutText(strings.Join(strings.Fields(line), " "), MaxProviderErrorBytes)
}

// findingsOf is the primary's findings list (FieldFindings) with the finding of attempt
// n added, as the cap's judgment lists them; nothing added for an empty finding.
func findingsOf(c *Card, n int, finding string) []string {
	var out []string
	for _, l := range strings.Split(c.F(FieldFindings), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	if f := firstSentence(finding); f != "" {
		out = append(out, "attempt "+itoa(n)+": "+f)
	}
	return out
}

// findingsLine is the findings list as the primary keeps it, the latest kept under
// MaxCardTextBytes.
func findingsLine(findings []string) string {
	for len(findings) > 0 && len(strings.Join(findings, "\n")) > MaxCardTextBytes {
		findings = findings[1:]
	}
	return strings.Join(findings, "\n")
}

// conflictFinding says a finding is the landing's refusal for a merge conflict with the
// advanced base and nothing else (LandRefusedConflict, whatever follows it: the lander's note).
// The ledger-only conflict is another finding ("a conflict in the shared ledgers only"), and is
// never at the bound at all (landRefused).
func conflictFinding(finding string) bool {
	class := FindingClass(finding)
	conflict := FindingClass(LandRefusedConflict)
	return class == conflict || strings.HasPrefix(class, conflict+":")
}

// onlyConflicts says every finding the primary's findings list holds for the attempts since
// the brief last changed (FieldFindings, after briefAt and before attempt) is the landing's
// conflict refusal: a conflict, then a gate failure, then a conflict is not a conflict-only
// bound, for the head failed in between. A conflict bound is "only for a merge conflict with the advanced
// base" (tla/Land.tla, THE BOUND, ConflictBoundCarries).
func onlyConflicts(c *Card, briefAt, attempt int) bool {
	for _, l := range strings.Split(c.F(FieldFindings), "\n") {
		head, rest, ok := strings.Cut(strings.TrimSpace(l), ": ")
		var at int
		if !ok {
			continue
		}
		if _, err := fmt.Sscanf(head, "attempt %d", &at); err != nil || at <= briefAt || at >= attempt {
			continue
		}
		if !conflictFinding(rest) {
			return false
		}
	}
	return true
}

// IsConflictBound says the primary c is in review at the conflict bound: the landing refused
// its current head for a merge conflict with the advanced base at two attempts of its brief
// (BriefBound.Conflict), and nothing since. The bound's judgment is the base's, and whatever
// the brief-defect rule did with it, rework --carry takes the card (reworkWhyCarry): carry the
// head, merge the current base, rerun the gates. It is read off the card alone, from what
// landRefused leaves when it sends a card to review at the bound: the returned attempt and
// reason (a landing's conflict), the head the landing refused (FieldLandRefusedHead), and the
// finding of the attempt before, which the second refusal repeated. tla/Land.tla, THE BOUND,
// ConflictBoundCarries.
func IsConflictBound(c *Card) bool {
	if c == nil || !c.Placed() || c.Col != Review {
		return false
	}
	attempt, briefAt, at := c.Int("attempt"), c.Int(FieldBriefAttempt), c.Int(FieldFindingAttempt)
	return attempt > 0 && LandRefusedAtHead(c) && c.F("return_reason") == "conflict" && c.Int(FieldReturnedAttempt) == attempt &&
		at > briefAt && at < attempt && conflictFinding(c.F("finding")) && onlyConflicts(c, briefAt, attempt)
}

// AtBriefBound is whether the primary c is at its brief's bound, and why. finding is what
// its readers found at its current attempt, or the report of its failed work: the broken
// reads' findings the store holds (brokenFindings), or the one a read step is about to
// write; "" when none is known. cap is the attempts one card may run between two coordinator
// decisions (AttemptsCap). The finding before is the primary's own (`finding`, at
// FieldFindingAttempt), counted only when that attempt ran on the current brief. The cap's
// count is every attempt since the coordinator last decided on the card (AttemptsSince):
// an attempt that ended without a worker report counts as any other, since the owner's
// bound is on the machine's retries, not on what the attempts said about the brief (the
// owner, 2026-10-12). A card never dealt is at no bound.
func AtBriefBound(c *Card, finding string, cap int) (BriefBound, bool) {
	attempt := c.Int("attempt")
	briefAt := c.Int(FieldBriefAttempt)
	if attempt == 0 {
		return BriefBound{}, false
	}
	prev, prevAt := c.F("finding"), c.Int(FieldFindingAttempt)
	if prev != "" && prevAt == 0 {
		prevAt = attempt - 1
	}
	if prev != "" && prevAt > briefAt && prevAt < attempt && SameFinding(prev, finding) {
		return BriefBound{ID: c.ID, Attempts: [2]int{prevAt, attempt}, Finding: firstSentence(prev), Conflict: conflictFinding(prev) && onlyConflicts(c, briefAt, attempt)}, true
	}
	if strings.HasPrefix(finding, LandRefusedFinding) {
		for _, l := range strings.Split(c.F(FieldFindings), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			var at int
			var f string
			if head, rest, ok := strings.Cut(l, ": "); ok {
				if _, err := fmt.Sscanf(head, "attempt %d", &at); err == nil && at > briefAt && at < attempt {
					f = rest
				}
			}
			if f != "" && strings.HasPrefix(f, LandRefusedFinding) && (SameFinding(f, finding) || strings.HasPrefix(f, finding)) {
				return BriefBound{ID: c.ID, Attempts: [2]int{at, attempt}, Finding: firstSentence(finding), Conflict: conflictFinding(finding) && onlyConflicts(c, briefAt, attempt)}, true
			}
		}
	}
	if cap <= 0 {
		cap = AttemptsDefault
	}
	if since := AttemptsSince(c); since >= cap {
		harness := harnessEnds(c)
		spent := MoneyText(CardCostOf(c).Total.Charged)
		if spent == "-" {
			spent = ""
		}
		return BriefBound{ID: c.ID, Since: since, Cap: cap, Spent: spent, Findings: findingsOf(c, attempt, finding), Harness: harness > since-harness, HarnessEnds: harness}, true
	}
	return BriefBound{}, false
}

// harnessEnds is how many attempts since the coordinator's last decision ended on a harness
// fault (classifyEnd says EndHarness: no worker report, ran=false in FieldRanAttempts): the
// machine's retries, never a vote on the brief. An attempt with no record counts as having
// run (AtLaneBroken), so it is never a harness end. Only WORKER and BRIEF ends are votes:
// a card that reached the cap with more harness ends than votes is the harness's, and the
// brief-defect mark and the brief-wrong wording never come from it. tla/SprintRules.tla, Part "answers": a
// harness end is redealt and counts toward max_attempts as an attempt, never a vote
// (anvotes, AnMark, MarkOnlyOnVotes; reversed witness MCSprintRulesAnswersBrokenHarnessVotes).
func harnessEnds(c *Card) int {
	attempt, decided := c.Int("attempt"), c.Int(FieldDecidedAttempt)
	runs := ranAttempts(c)
	n := 0
	for at := decided + 1; at <= attempt; at++ {
		if r, ok := runs[at]; ok && classifyEnd(r.ran, "") == EndHarness {
			n++
		}
	}
	return n
}

// AttemptsSince is the attempts the primary has run since the coordinator last decided on
// it: its attempt less FieldDecidedAttempt (0 when no decision was made). Every attempt
// counts, whatever ended it; a redeal of one attempt (a new generation of the same work
// card, bounded by MaxRedeals on its own) is that one attempt. tla/SprintRules.tla
// NoAttemptPastCapWithoutDecision.
func AttemptsSince(c *Card) int {
	return max(0, c.Int("attempt")-c.Int(FieldDecidedAttempt))
}

// AtAttemptCap is whether the primary has run its cap's attempts since the coordinator last
// decided on it, by the count alone (never the same-finding bound), and why: what every
// deal of a ready card asks before it cuts the next attempt (steps_work.go, deal; the
// tick's friend offer), so a card at the cap is dealt to no machine and no friend until a
// decision. It is asked once a ready card by the deal, so under the cap it allocates
// nothing (TestTickDealAllocatesNoPerCardRouteMapAtScale).
func AtAttemptCap(c *Card, cap int) (BriefBound, bool) {
	attempt := c.Int("attempt")
	if attempt == 0 {
		return BriefBound{}, false
	}
	if cap <= 0 {
		cap = AttemptsDefault
	}
	since := AttemptsSince(c)
	if since < cap {
		return BriefBound{}, false
	}
	spent := MoneyText(CardCostOf(c).Total.Charged)
	if spent == "-" {
		spent = ""
	}
	return BriefBound{ID: c.ID, Since: since, Cap: cap, Spent: spent, Findings: findingsOf(c, attempt, "")}, true
}

// ReviewBreaker checks if the review circuit breaker should trip based on the
// current card state and the latest reading's finding. It returns a message
// describing why the breaker tripped and whether it did trip.
//
// The circuit breaker tracks:
// - consecutive: runs of attempts ending at current, each read broken with a different key
// - substantive: attempts since brief last changed that had findings
//
// It trips at consecutive >= ReviewBreakerNew (4) or substantive >= ReviewBreakerRounds (6).
func ReviewBreaker(c *Card, reader, finding string) (string, bool) {
	briefAt := c.Int(FieldBriefAttempt)

	// Parse existing findings
	keys := strings.Split(c.F(FieldFindingKeys), "\n")
	findings := strings.Split(c.F(FieldFindings), "\n")

	consecutive := 0
	consecutiveKey := ""
	substantive := 0

	// Count substantive (findings since brief last changed)
	for _, line := range findings {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format is "attempt N: finding text"
		attemptPrefix, _, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		if !strings.HasPrefix(attemptPrefix, "attempt ") {
			continue
		}
		attemptNumStr := strings.TrimPrefix(attemptPrefix, "attempt ")
		attemptNum, err := strconv.Atoi(attemptNumStr)
		if err != nil || attemptNum <= briefAt {
			continue
		}
		substantive++
	}

	// Count consecutive (distinct keys ending at current attempt)
	var attemptKeys []string
	for _, line := range keys {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		attemptPrefix, key, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		if !strings.HasPrefix(attemptPrefix, "attempt ") {
			continue
		}
		attemptNumStr := strings.TrimPrefix(attemptPrefix, "attempt ")
		attemptNum, err := strconv.Atoi(attemptNumStr)
		if err != nil || attemptNum <= briefAt {
			continue
		}
		attemptKeys = append(attemptKeys, key)
	}

	// Build consecutive count from oldest to newest
	for _, key := range attemptKeys {
		if consecutive == 0 {
			consecutive = 1
			consecutiveKey = key
			continue
		}
		if key != consecutiveKey {
			consecutive++
			consecutiveKey = key
		}
	}

	// Add current finding if it exists
	if finding != "" {
		currentKey := FindingKey(reader, finding)
		if consecutive == 0 || currentKey != consecutiveKey {
			consecutive++
			consecutiveKey = currentKey
		}
		substantive++
	}

	if consecutive >= ReviewBreakerNew || substantive >= ReviewBreakerRounds {
		var foundKeys []string
		for _, line := range keys {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			_, key, ok := strings.Cut(line, ": ")
			if !ok || key == "" {
				continue
			}
			foundKeys = append(foundKeys, key)
		}
		if finding != "" {
			foundKeys = append(foundKeys, FindingKey(reader, finding))
		}
		if len(foundKeys) > 10 {
			foundKeys = foundKeys[:10]
		}
		line := fmt.Sprintf("%s: review circuit breaker: %d rounds in a row with new blocking findings (bound %d), %d rounds with findings since its brief last changed (bound %d); the coordinator decides; findings: %s",
			c.ID, consecutive, ReviewBreakerNew, substantive, ReviewBreakerRounds, strings.Join(foundKeys, ", "))
		return line, true
	}

	return "", false
}

// A READ'S ENVIRONMENT IS NOT THE WORK'S (tla/DirtyTick.tla, READ ENVIRONMENT,
// EnvNeverRedoes; docs/SPEC-SPRINT.md, "An environment exit"). A read that ends
// with no verdict because the reader's own environment refused it (its wall or
// sandbox refused a write, staging could not fetch the head, its brief's pin is
// no longer the card's head, or its child died 134 or 137) says nothing about
// the work: it is asked again on the same head at the same attempt, spends no
// redeal, no attempt and no work generation, and counts toward no bound of the
// brief (a run of them is the read identity's own bound, EnvReadCap, one
// judgment naming the environment). ReadEnvRefusal is the ONE classifier of it;
// the read's return (steps_review.go Read, friend_read.go) is its only caller,
// and nothing here, in the deal or in the brief's bounds, takes a read's
// environment return for a reason to move a work card to a new generation.

// The kinds of a read's environment refusal (ReadEnvRefusal), and the ended word
// of one that is not an exit code.
const (
	EnvRefusalWall     = "wall"
	EnvRefusalStaging  = "staging"
	EnvRefusalStalePin = "stale-pin"
	envRefusalWord     = "environment refusal="
)

var (
	envWallRe    = regexp.MustCompile(`(?i)\bwall refused\b|\bsandbox refused\b|refused a write outside the job|\bOperation not permitted\b[^\n]*/`)
	envStagingRe = regexp.MustCompile(`(?i)\bstaging refused\b|\brefused at staging\b|is in neither the mirror`)
	envPinRe     = regexp.MustCompile(`(?i)\bpin no longer matches\b|\bbrief pin\b[^\n]*\b(stale|no longer)\b`)
)

// ReadEnvRefusal says a read's return is its environment's: the kind (EnvRefusalWall,
// EnvRefusalStaging, EnvRefusalStalePin) and true when the reason names a wall or sandbox
// refusal, a staging refusal, or a pin that no longer matches, or when readHead (the head the
// read card pins, "" when it pins none) differs from cardHead (the primary's); "", false
// otherwise. A child's exit 134 or 137 is the other half (HardEnvExit); it is not repeated here.
func ReadEnvRefusal(reason, readHead, cardHead string) (kind string, ok bool) {
	switch {
	case envWallRe.MatchString(reason):
		return EnvRefusalWall, true
	case envStagingRe.MatchString(reason):
		return EnvRefusalStaging, true
	case envPinRe.MatchString(reason), readHead != "" && cardHead != "" && readHead != cardHead:
		return EnvRefusalStalePin, true
	}
	return "", false
}

// ReadEnvEnd is a read return's environment class: the ended word and the code the read
// identity's count keeps (a hard exit's number, or a refusal's kind), true when the return is
// the environment's (a hard exit with no result, or ReadEnvRefusal).
func ReadEnvEnd(reason string, exit int, readHead, cardHead string) (ended, code string, ok bool) {
	if e, env := EnvExit(reason, exit); env && HardEnvExit(exit) {
		return e, itoa(exit), true
	}
	if kind, refused := ReadEnvRefusal(reason, readHead, cardHead); refused {
		return envRefusalWord + kind, kind, true
	}
	return "", "", false
}

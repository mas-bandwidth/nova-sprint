package sprint

import (
	"fmt"
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
	// Harness is true when every attempt since the coordinator's last decision ended on a
	// harness fault (no worker report: ran=false in FieldRanAttempts). Such a card reached
	// max_attempts on the machine's retries, never on a worker's or the brief's vote: the
	// bound is worded "harness fault, not the brief", and it is not marked a brief defect.
	Harness bool
}

// String is the bound said in one line: what repeated and where, or the cap, the spend and
// the findings, then the verdict. A card whose every attempt since the decision ended on a
// harness fault is worded as the harness's, never as a vote that the brief is wrong.
func (b BriefBound) String() string {
	if b.Finding != "" {
		return fmt.Sprintf("%s has failed the same way twice (attempts %d and %d: %s); the brief is wrong, not the worker",
			b.ID, b.Attempts[0], b.Attempts[1], b.Finding)
	}
	if b.Harness {
		return fmt.Sprintf("%s: harness fault, %d attempts (max_attempts %d): the brief is not the cause", b.ID, b.Since, b.Cap)
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
// not at all.
func (b BriefBound) Remedy() string {
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
		return BriefBound{ID: c.ID, Attempts: [2]int{prevAt, attempt}, Finding: firstSentence(prev)}, true
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
				return BriefBound{ID: c.ID, Attempts: [2]int{at, attempt}, Finding: firstSentence(finding)}, true
			}
		}
	}
	if cap <= 0 {
		cap = AttemptsDefault
	}
	if since := AttemptsSince(c); since >= cap {
		spent := MoneyText(CardCostOf(c).Total.Charged)
		if spent == "-" {
			spent = ""
		}
		return BriefBound{ID: c.ID, Since: since, Cap: cap, Spent: spent, Findings: findingsOf(c, attempt, finding), Harness: allHarness(c)}, true
	}
	return BriefBound{}, false
}

// allHarness says every attempt since the coordinator's last decision ended on a harness
// fault (classifyEnd says EndHarness: no worker report, ran=false in FieldRanAttempts): the
// machine's retries, never a vote on the brief. An attempt with no record counts as having
// run (AtLaneBroken), so it is never a harness fault. tla/SprintRules.tla, Part "answers":
// a harness end is redealt and counts toward max_attempts as an attempt, never a vote.
func allHarness(c *Card) bool {
	attempt, decided := c.Int("attempt"), c.Int(FieldDecidedAttempt)
	runs := ranAttempts(c)
	for n := decided + 1; n <= attempt; n++ {
		r, ok := runs[n]
		if !ok || classifyEnd(r.ran, "") != EndHarness {
			return false
		}
	}
	return attempt > decided
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

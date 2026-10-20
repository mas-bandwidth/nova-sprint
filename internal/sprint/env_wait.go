package sprint

import (
	"fmt"
	"strings"
	"time"
)

// NEEDS-FACT (docs/SPEC-CARD-CONTRACT.md; docs/SPEC-SPRINT.md section 5, the fleet):
// a worker that cannot run its card because the member lacks a tool writes
// `NEEDS-FACT: <fact>[,<fact>]` in its report and finishes HOLD. The words are the
// member's beat's fact words (FactWords, FieldFacts). The finish parks the CARD, never
// the member: the work card withdraws without FieldTakeEnded (no redeal, no attempt,
// no cap) and the primary stays ready, held to the facts by a field beside GATE-NEEDS.
// tla/WaitFor.tla FinishEnvMissing: the card waits in the wait-for graph, and
// AutoResumeCard (FactAppears) resumes it in the tick a member's beat carries the fact.
const (
	// FieldEnvFacts is the work card's facts a missing-tool HOLD put it on: a comma list of
	// fact words the member's beat would carry for the tool (FactWords). It is never a
	// brief edit; the deal reads it beside GATE-NEEDS.
	FieldEnvFacts = "env_facts"
	// FieldEnvWaitUntil is the running-time deadline of an environment wait: when no
	// member's beat carries the facts by then, one NEnvWait judgment names the card.
	FieldEnvWaitUntil = "env_wait_until"
	// EnvWaitFor is how long a card may wait on an environment fact before the judgment.
	EnvWaitFor = 24 * time.Hour
	// NEnvWait is the judgment at an environment wait's deadline: the card waits on the
	// environment, not on a decision, so wait is one of its decisions.
	NEnvWait = "a card waits on an environment fact"
	// NEnvWaited is the happened note of a finish that parked its card on a fact.
	NEnvWaited = "a card parked on an environment fact: its member lacks the tool"
)

// NeedsFact reads a finish report's NEEDS-FACT line: the fact words a worker says its
// member lacks, or nil when the report names none. A malformed word is dropped: a
// report that cannot name a fact word parks nothing (EnvFault and the ordinary failed
// finish answer it).
//
// docs/SPEC-CARD-CONTRACT.md (NEEDS-FACT); tla/WaitFor.tla FinishEnvMissing.
func NeedsFact(report string) []string {
	for line := range strings.SplitSeq(report, "\n") {
		word, ok := strings.CutPrefix(strings.TrimSpace(line), "NEEDS-FACT:")
		if !ok {
			continue
		}
		facts, err := FactWords(word)
		if err != nil {
			return nil
		}
		return facts
	}
	return nil
}

// EnvFacts is the facts a work card's primary waits on (FieldEnvFacts), Split: nil when
// it waits on none.
//
// tla/WaitFor.tla env-fact-wait (card(waiting) -> fact(env)).
func EnvFacts(c *Card) []string {
	if c == nil {
		return nil
	}
	return Split(c.F(FieldEnvFacts))
}

// envWaitDue says an environment wait has reached its deadline: the primary is ready,
// waits on facts still, and no NEnvWait judgment is open on it.
//
// tla/WaitFor.tla Overdue; docs/SPEC-SPRINT.md section 5 (the fleet): a fact no
// member's beat names waits to its deadline, then is the judgment.
func envWaitDue(s *Snapshot, c *Card, open []Open, now time.Time) bool {
	if c == nil || c.Col != Ready || len(EnvFacts(c)) == 0 || c.F(FieldEnvWaitUntil) == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, c.F(FieldEnvWaitUntil))
	if err != nil || now.Before(until) {
		return false
	}
	// dealt ready: a member whose facts carry it is about to take it, so no judgment
	if wc := s.Fleet.Placed(WorkCardID(c.ID, c.Int("attempt"))); wc != nil && (wc.Col == Ready || wc.Col == Working) {
		return false
	}
	for _, o := range open {
		if o.Note.Type == NEnvWait {
			return false
		}
	}
	return true
}

// envWaitJudgment is the one NEnvWait judgment at an environment wait's deadline: the
// card waits on a fact no member's beat names, and the coordinator may wait on it (the
// environment, not a decision: WaitAllowedOnACard), drop it, or bring the fact with a
// beat.
//
// tla/WaitFor.tla Overdue and ServerDefault; docs/SPEC-SPRINT.md section 5 (the fleet).
func envWaitJudgment(c *Card, now time.Time, who string) Note {
	n := judgment(NEnvWait, c.Row, now, 0, c.ID)
	n.Card = c.ID
	n.Who = who
	n.Decisions = append([]string(nil), TickDecisions[NEnvWait]...)
	n.What = fmt.Sprintf("%s waits on the environment fact %s: no member's beat carries it by %s; run: nova-sprint fleet beat <member> --facts <fact>, wait on it, or drop it",
		c.ID, strings.Join(EnvFacts(c), ","), c.F(FieldEnvWaitUntil))
	return n
}

// envWaited is the unit of a finish whose report names NEEDS-FACT (NeedsFact): the
// member lacks a tool, never the card's fault. The work card withdraws WITHOUT
// FieldTakeEnded, so no redeal and no attempt is spent and no bound or cap moves, and
// the member is NOT held (envFaulted's hold stays for a fault that is the member's); the
// primary stays ready, held to the facts by FieldEnvFacts beside GATE-NEEDS and given an
// environment deadline (FieldEnvWaitUntil, EnvWaitFor). The deal holds it to the facts
// (GateHostPool) and resumes it in the tick a member's beat carries them, with no
// coordinator step.
//
// tla/WaitFor.tla FinishEnvMissing (none: col=waiting, held=env, no attempt);
// AutoResumeCard; docs/SPEC-SPRINT.md section 5 (the fleet).
func envWaited(s *Snapshot, c, pr *Card, r FinishReq, facts []string, who string) Unit {
	set := nextGen(c, "", s.Now)
	set["withdrawn"] = stamp(s.Now)
	set["untaken_since"] = stamp(s.Now)
	words := strings.Join(facts, ",")
	prSet := map[string]string{
		FieldEnvFacts:     words,
		FieldEnvWaitUntil: stamp(s.Now.Add(EnvWaitFor)),
	}
	what := fmt.Sprintf("its member %s lacks %s: the card waits on the fact, no attempt spent and the member not held", c.Row, words)
	n := happened(NEnvWaited, pr.Row, s.Now, pr.ID)
	n.Who, n.What = who, what
	return Unit{Key: c.ID, Stream: pr.Row,
		Changes: []Change{
			change(Fleet, moveEntry(c, c.Row, Withdrawn, set, "taken", "dealt")),
			change(Work, moveEntry(pr, pr.Row, Ready, prSet, "work")),
		},
		Notes: []Note{n},
		Moved: fmt.Sprintf("%s working -> withdrawn gen=%d, %s working -> ready (waits on %s; no attempt spent)", c.ID, c.Int("gen")+1, pr.ID, words),
	}
}

// init registers the environment wait's judgment decisions and its wait, beside the
// tick's and the wait verb's tables (steps_tick.go TickDecisions, wait_refused.go
// WaitAllowedOnACard, which this card does not own otherwise): the card waits on the
// environment, not on a decision, so wait is one of its decisions and is accepted.
//
// tla/WaitFor.tla ServerDefault; docs/SPEC-SPRINT.md section 5 (the fleet).
func init() {
	TickDecisions[NEnvWait] = []string{"wait", "drop", "fleet beat <member> --facts <fact>"}
	WaitAllowedOnACard[NEnvWait] = "the card waits on the environment (a fact no member's beat carries), not on a decision: wait quiets it until the fact appears"
}

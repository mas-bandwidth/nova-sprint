package steps

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// A red base wakes the coordinator (2026-11-10: one repository's main was
// red for eleven days; check said so when run by hand and every land
// judgement said "the base itself is red at its tip", but nothing pushed
// one message saying the base is red, and the coordinator met it card by
// card). v1 raised one judgement per red (repo, base) when the red stretch
// began and closed it when a later tip was green (v1.6.29
// internal/sprint/base_ci.go TickBaseCI); this is that judgement back. The
// landing is unchanged: nothing is held, stopped or dealt differently
// (LANDING.md "A red base"); this is the push only.
//
// Layer 6 reads each watched base tip's checks (run.BaseWatch) and passes
// the last completed read of each repository and base in Inputs.Bases. A
// read that completed red, with live cards on its streams, has one open
// base-red judgement about <repo>@<base>; a red set that changes (another
// tip, other checks red) replaces it with the next episode in the same op,
// pushed again (never a second open one); a tip that completes green clears
// it. Pending checks, a rate-limited forge and a failed read are no fact:
// they neither open nor clear one. tla/BaseRed.tla is the model.

// BaseCI is one repository and base's CI as layer 6 last read it complete
// at a tip: red (a check failed and none is still running) or green.
type BaseCI struct {
	Repo, Base, Tip string
	Streams         []string  // the streams that land on it, by name
	Red             bool      // false: every check passed at Tip
	Failing         []string  // red: the failing checks
	Lines           []string  // red: "<check>: <its first failing lines>", at most three failing jobs
	FirstRed        string    // red: the first red tip the run saw in this red stretch
	Since           time.Time // red: when the run first saw it red
	SinceWatch      bool      // red: the first read the watch took of the base was red: it may be red for longer
}

// BaseAbout is what a base-red judgement is about: <repo>@<base>.
func BaseAbout(repo, base string) string { return repo + "@" + base }

// redNamed is how many of a red base's failing checks a line names.
const redNamed = 5

// RedChecks is n failing checks as a line says them: "<n> check(s) red
// (<the first five> and <m> more)".
func RedChecks(failing []string) string {
	named := strings.Join(failing[:min(len(failing), redNamed)], ", ")
	if more := len(failing) - redNamed; more > 0 {
		named += fmt.Sprintf(" and %d more", more)
	}
	checks := "checks"
	if len(failing) == 1 {
		checks = "check"
	}
	return fmt.Sprintf("%d %s red (%s)", len(failing), checks, named)
}

func shortSHA(sha string) string { return sha[:min(len(sha), 12)] }

// redSet is how a base-red question names its red set (the base and its
// failing checks, sorted); baseRed finds it in the latest episode to tell a
// changed red set from the one judged. Another tip with the same checks red
// is the same red set: the open judgement stands, and is not pushed again.
func redSet(f BaseCI) string {
	return fmt.Sprintf("%s %s: %s:", f.Repo, f.Base, RedChecks(slices.Sorted(slices.Values(f.Failing))))
}

// baseRed opens the base-red judgement of each repository and base whose
// last completed read is red while its streams have live cards. An open one
// with the same red set is detected again (so its deadline reopens it,
// escalated, never closes it); one whose red set changed is replaced by its
// next episode. One the coordinator answered stays quiet for the judgement
// timeout at the same red set (the answer says it is being fixed, and layer
// 6 reads the tip's checks again), then opens again saying so.
func (t *tick) baseRed() {
	for _, about := range sortedKeys(t.in.Bases) {
		f := t.in.Bases[about]
		if !f.Red || t.pending("base-red", about) {
			continue
		}
		merging, others := t.behind(f.Streams)
		if merging+others == 0 {
			continue
		}
		n := t.episodes("base-red", about)
		last := t.v.Judgements[fmt.Sprintf("base-red.%s.%d", about, n)]
		same := strings.Contains(last.Question, redSet(f))
		q := baseRedQuestion(f, merging, others)
		switch {
		case t.held("base-red", about) && same:
			continue
		case last.State == "open":
			t.expires[last.ID] = fmt.Sprintf("the red set changed (%s at %s): reopened as base-red.%s.%d", RedChecks(f.Failing), shortSHA(f.Tip), about, n+1)
		case last.State == "answered" && same:
			if t.in.Now.Before(last.ClosedAt.Add(seconds(t.tmo.Judgement))) {
				continue
			}
			q = fmt.Sprintf("STILL RED %s after your answer: %s", t.in.Now.Sub(last.ClosedAt).Truncate(time.Second), q)
		}
		t.addJudge(judgement{kind: "base-red", about: about, question: q})
	}
}

// behind is the live producers of streams: those merging (they land when
// the base is green) and the others not yet landed or failed.
func (t *tick) behind(streams []string) (merging, others int) {
	for _, state := range []string{"waiting", "ready", "working", "review", "merging"} {
		for id := range t.v.byState[state] {
			if c := t.v.Cards[id]; c.Kind != "producer" || !slices.Contains(streams, c.Stream) {
				continue
			}
			if state == "merging" {
				merging++
			} else {
				others++
			}
		}
	}
	return merging, others
}

// baseRedQuestion is the judgement's text, the instruction first (a push
// shows its first 300 bytes; judge and show show it whole): the base, its
// tip and red checks, what to do, the streams and cards behind it, since
// when, and the first failing lines.
func baseRedQuestion(f BaseCI, merging, others int) string {
	streams := "streams"
	if len(f.Streams) == 1 {
		streams = "stream"
	}
	q := fmt.Sprintf("BASE RED at %s, %s the base is red: fix it now, before anything else; nothing on %s %s lands by itself until it is green (%d merging, %d more not landed)",
		shortSHA(f.Tip), redSet(f), streams, firstIDs(f.Streams), merging, others)
	if f.FirstRed != "" {
		q += fmt.Sprintf("; red since %s, first seen %s", shortSHA(f.FirstRed), f.Since.UTC().Format("2006-01-02 15:04 UTC"))
		if f.SinceWatch {
			q += " (the first read since the run began watching this base: it may be red for longer)"
		}
	}
	if len(f.Lines) > 0 {
		q += "; first failing lines: " + strings.Join(f.Lines, " || ")
	}
	return q + "; fix it with a child of your own or a blocker card onto the base, then answer retry: the run reads its checks again, and this clears by itself when a tip is green"
}

// clearedBase is why an open base-red judgement's cause no longer holds: the
// last completed read of its repository and base is green. No read (pending
// checks, a rate-limited forge, a base no longer watched) is never a clear.
func (t *tick) clearedBase(about string) string {
	if f, ok := t.in.Bases[about]; ok && !f.Red {
		return fmt.Sprintf("cleared: %s %s is green at %s", f.Repo, f.Base, shortSHA(f.Tip))
	}
	return ""
}

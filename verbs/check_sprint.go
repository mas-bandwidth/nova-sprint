package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// checkSprint is check's health of the sprint, read from one snapshot: each
// stream's repository and base at origin, the open judgements by kind, who
// runs the sprint, the width and why nothing deals, and each landing's wait.
func (e *Engine) checkSprint(ctx context.Context, r *checkReport, who map[string]bus.Presence) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		r.red(err)
		return
	}
	heads := e.checkStreams(ctx, r, snap)
	e.checkPolicy(ctx, r, snap)
	e.checkCards(ctx, r, snap)
	checkWorking(r, snap, who)
	checkJudgements(r, snap)
	running := e.checkRun(ctx, r, who)
	e.checkWidth(ctx, r, snap, heads, running)
	e.checkLandings(ctx, r, snap, heads, running)
}

// checkStreams is one line per stream: stream add's admission again
// (admitStream: its repository reachable at its clone address and its base
// there, its card branches creatable, its forge able to act), every stream
// at once. It returns each readable stream's base tip.
func (e *Engine) checkStreams(ctx context.Context, r *checkReport, snap store.Snapshot) map[string]string {
	heads := map[string]string{}
	if len(snap.Streams) == 0 {
		r.ok("streams: none yet (nova-sprint stream add <s> --repo <org/repo> --forge github)")
		return heads
	}
	if e.Origin == nil {
		r.red(broken("the git edge is not configured, so no stream's repository can be read", "take it to the project's own tracker with this line"))
		return heads
	}
	ss := streamsInOrder(snap)
	shas, errs := make([]string, len(ss)), make([]error, len(ss))
	var wg sync.WaitGroup
	for i, s := range ss { // the streams' ls-remotes are independent: all at once
		wg.Go(func() { shas[i], errs[i] = e.admitStream(ctx, s, "nova-sprint check") })
	}
	wg.Wait()
	for i, s := range ss {
		sha, err := shas[i], errs[i]
		if err != nil {
			r.red(admissionWhat(s.Name, err))
			continue
		}
		heads[s.Name] = sha
		r.ok("stream %s: %s reachable, base %s at %s", s.Name, s.Repo, s.Base, shortSHA(sha))
	}
	return heads
}

// streamsInOrder is the streams by position, then name.
func streamsInOrder(snap store.Snapshot) []store.Stream {
	out := make([]store.Stream, 0, len(snap.Streams))
	for _, s := range snap.Streams {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b store.Stream) int {
		if a.Pos != b.Pos {
			return a.Pos - b.Pos
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

func shortSHA(sha string) string { return sha[:min(len(sha), 12)] }

// checkJudgements is one red line per kind of open judgement: how many,
// which, the oldest's question, and the command that answers them.
func checkJudgements(r *checkReport, snap store.Snapshot) {
	open := openJudgements(snap)
	if len(open) == 0 {
		r.ok("judgements: none open")
		return
	}
	byKind := map[string][]judgementLine{}
	var kinds []string
	for _, j := range open {
		k := orWord(j.Kind, "unkinded")
		if _, seen := byKind[k]; !seen {
			kinds = append(kinds, k)
		}
		byKind[k] = append(byKind[k], j)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		r.red(judgementsOpen(k, byKind[k]))
	}
}

// judgementsOpen is one kind's open judgements, oldest first, as one
// refusal: the ids (five, then a count), the oldest's question, and its
// command with the id left for the reader when there are several.
func judgementsOpen(kind string, js []judgementLine) *Fail {
	first := js[0]
	cmd := orWord(first.Command, "nova-sprint show "+first.ID)
	if len(js) == 1 {
		return refuse(fmt.Sprintf("judgement %s is open: %s", first.ID, clipN(first.Question, 200)), cmd)
	}
	var ids []string
	for _, j := range js[:min(len(js), 5)] {
		ids = append(ids, j.ID)
	}
	list := strings.Join(ids, ", ")
	if more := len(js) - len(ids); more > 0 {
		list += fmt.Sprintf(" and %d more", more)
	}
	return refuse(fmt.Sprintf("%d %s judgements are open (%s); the oldest: %s", len(js), kind, list, clipN(first.Question, 200)),
		strings.ReplaceAll(cmd, first.ID, "<id>")+" (each id; nova-sprint next lists them)")
}

// clipN is s on one line, at most n bytes.
func clipN(s string, n int) string {
	s = oneLine(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// checkRun is who holds the chooser lock: run on a host, and the sprint's
// name on the bus. It returns whether run serves the sprint.
func (e *Engine) checkRun(ctx context.Context, r *checkReport, who map[string]bus.Presence) bool {
	st, err := e.Doctor.Run(ctx)
	switch {
	case err != nil:
		r.red(RefusalOr(err, broken("who runs the sprint cannot be read ("+errText(err)+")", "nova-sprint show run")))
		return false
	case !st.Held:
		r.red(refuse("nothing runs the sprint: the chooser lock is free, so nothing is dealt or landed", runFix))
		return false
	case st.Operation != "run":
		r.red(refuse(fmt.Sprintf("the chooser lock is held by %s on %s, not run", orWord(st.Operation, "an unknown holder"), orWord(st.Host, "an unknown host")), "nova-sprint show run names it; when it ends, "+runFix))
		return false
	}
	if on, known := present(who, "sprint"); known && !on {
		r.red(refuse("run holds the lock on "+st.Host+" but the name sprint is not on the bus, so nothing is pushed", runFix))
		return true
	}
	r.ok("run on %s (pid %d)", st.Host, st.PID)
	return true
}

// runFix is how run is brought back: its service's log, then up.
const runFix = "read the run service's log (nova-sprint show run), then nova-sprint up --as <coordinator>"

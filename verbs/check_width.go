package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/steps"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// widthFacts are the deal's facts as check reads them: the open width (up,
// unheld, hooked rows, at the roster's width), the consumers working and
// dealt on those rows, the ready producers, and what each open row serves.
type widthFacts struct {
	open, working, dealt, rows int
	ready                      []store.Card
	serves                     map[string]bool // tier -> some open row serves it
}

// checkWidth is the width line: working against the open width, with the
// ready producers; when producers are ready and none is dealt it is red,
// naming why (the deal's own causes, and the steps' idle judgement); when
// the fleet works under its width with nothing ready it warns "nothing
// dealable" with the chains and overlaps the waiting producers sit on (ws,
// the steps' own Waits, as the idle judgement says them).
func (e *Engine) checkWidth(ctx context.Context, r *checkReport, snap store.Snapshot, heads map[string]string, running bool, ws []steps.StreamWaits) {
	lim, _, err := e.rules(ctx, snap)
	if err != nil {
		r.red(err)
		return
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		r.red(err)
		return
	}
	f := widthOf(snap, ro, e.Now())
	line := fmt.Sprintf("width: working %d (%d dealt) of %d open on %d rows; %d ready", f.working, f.dealt, f.open, f.rows, len(f.ready))
	causes := undealt(snap, f, heads, running)
	switch {
	case len(f.ready) > 0 && f.dealt == 0:
		what, fix := causesLine(causes, "nova-sprint show run, then the run service's log")
		r.red(refuse("nothing deals: "+line+"; why: "+what, fix))
	case len(f.ready) > 0 && f.dealt < f.open && len(causes) > 0:
		what, _ := causesLine(causes, "")
		r.ok("%s; not dealt: %s", line, what)
	case len(f.ready) == 0 && f.dealt < f.open:
		if what, fix := steps.IdleWhy(ws); what != "" {
			r.warn(line+"; nothing dealable: "+what, fix)
			return
		}
		r.ok("%s", line)
	default:
		r.ok("%s", line)
	}
}

// widthOf reads the facts through the steps' own view (Open, Working,
// Live), so a row counts as the deal counts it.
func widthOf(snap store.Snapshot, ro roster.Roster, now time.Time) widthFacts {
	v := steps.Build(snap, now)
	f := widthFacts{serves: map[string]bool{}}
	for name, row := range snap.Rows {
		if !v.Open(row) {
			continue
		}
		_, width, tiers, _ := ro.Row(name)
		f.rows++
		f.open += width
		f.working += v.Working(name)
		f.dealt += len(v.Live(name))
		for _, t := range tiers {
			f.serves[t] = width > 0 || f.serves[t]
		}
	}
	for _, id := range v.Producers("ready") {
		f.ready = append(f.ready, snap.Cards[id])
	}
	return f
}

// dealCause is one reason ready producers are not dealt, and its fix.
type dealCause struct{ what, fix string }

// undealt is why ready producers are not dealt, in the order the deal
// meets them: the machine (stopped, no run, no open row), then each ready
// producer's own cause (a held stream, a stream with no base head, a tier
// no open row serves), then the steps' own words (an open idle judgement).
func undealt(snap store.Snapshot, f widthFacts, heads map[string]string, running bool) []dealCause {
	var out []dealCause
	if snap.Stopped {
		out = append(out, dealCause{"the sprint is stopped (" + orWord(snap.StopReason, "no reason given") + ")", "nova-sprint start --reason \"<why>\""})
	}
	if !running {
		out = append(out, dealCause{"nothing runs the sprint", runFix})
	}
	if f.rows == 0 {
		out = append(out, dealCause{"no row is open (up, unheld and hooked)", "each worker hooks in: nova-sprint hook --as <row> --role worker; a held row: nova-sprint unhold <row> --reason \"<why>\""})
	}
	out = append(out, readyCauses(snap, f, heads, len(out) == 0)...)
	for _, j := range openJudgements(snap) {
		if j.Kind == "idle" {
			out = append(out, dealCause{"the steps say: " + clipN(j.Question, 200), j.Command})
		}
	}
	return out
}

// readyCauses counts the ready producers by their first cause, naming the
// streams or tiers; with unexplained (no machine cause above), it also
// counts the producers with none.
func readyCauses(snap store.Snapshot, f widthFacts, heads map[string]string, unexplained bool) []dealCause {
	held, headless, unserved := map[string]int{}, map[string]int{}, map[string]int{}
	for _, c := range f.ready {
		switch {
		case snap.Streams[c.Stream].Held:
			held[c.Stream]++
		case heads[c.Stream] == "" || engineJudged(snap, c.Stream):
			headless[c.Stream]++
		case !f.serves[c.Tier]:
			unserved[c.Tier]++
		}
	}
	var out []dealCause
	if n, names := tally(held); n > 0 {
		out = append(out, dealCause{fmt.Sprintf("%d ready in held streams (%s)", n, names), "nova-sprint unhold <stream> --reason \"<why>\""})
	}
	if n, names := tally(headless); n > 0 {
		out = append(out, dealCause{fmt.Sprintf("%d ready in streams with no base head (%s)", n, names), "fix each stream's red line above, then nova-sprint check"})
	}
	if n, names := tally(unserved); n > 0 {
		out = append(out, dealCause{fmt.Sprintf("%d ready at tiers no open row serves (%s)", n, names), "hook a row that serves the tier: nova-sprint hook --as <row> --role worker"})
	}
	if n := len(f.ready) - sum(held, headless, unserved); unexplained && n > 0 {
		out = append(out, dealCause{fmt.Sprintf("%d ready with no cause visible from here", n), "nova-sprint show run, then the run service's log"})
	}
	return out
}

// engineJudged is whether an open engine judgement is about stream (the
// steps open one when run passes no head for it).
func engineJudged(snap store.Snapshot, stream string) bool {
	for _, j := range snap.Judgements {
		if j.State == "open" && j.Kind == "engine" && j.About == stream {
			return true
		}
	}
	return false
}

// tally is the sum of counts and the names, sorted.
func tally(m map[string]int) (int, string) {
	n, names := 0, make([]string, 0, len(m))
	for k, c := range m {
		n += c
		names = append(names, k)
	}
	slices.Sort(names)
	return n, strings.Join(names, ", ")
}

// sum is the counts of every map.
func sum(ms ...map[string]int) int {
	n := 0
	for _, m := range ms {
		for _, c := range m {
			n += c
		}
	}
	return n
}

// causesLine is the causes on one line, and the first one's fix (dflt when
// there is none).
func causesLine(cs []dealCause, dflt string) (string, string) {
	if len(cs) == 0 {
		return "no cause is visible from here", dflt
	}
	whats := make([]string, len(cs))
	for i, c := range cs {
		whats[i] = c.what
	}
	return strings.Join(whats, "; "), cs[0].fix
}

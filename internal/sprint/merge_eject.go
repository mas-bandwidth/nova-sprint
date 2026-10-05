package sprint

import (
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The eject of a land batch (cmd/nova-sprint/land.go). tla/Land.tla EjectNext
// is this closure: NoLandedNeedsUnlanded, EjectedDependentsUnlanded, and
// LandsTheRest. A card that cannot merge is returned to review, and so is
// every card of the batch whose needs reach it. The third time the same
// reason comes up, the card stays and the stream stops: the brief is wrong.

const (
	FieldEjectLog        = "eject_log"
	FieldMergeIdleSince  = "merge_idle_since"
	FieldMergeIdleWhy    = "merge_idle_why"
	FieldMergeIdleJudged = "merge_idle_judged"

	NEjected    = "a card was ejected from its land batch"
	NMergeStuck = "merge stuck"

	// MergeStuckAfter is how long a batch may land nothing while it still
	// holds cards before one merge-stuck judgment. The clock is the store's.
	MergeStuckAfter = 15 * time.Minute
)

// Closure is the cards of order that are in bad or whose needs reach one,
// directly or through another card of order. A need that names an id already
// in bad counts, including an id from outside this batch.
func Closure(order []string, needs map[string][]string, bad map[string]bool) map[string]bool {
	out := map[string]bool{}
	for id := range bad {
		out[id] = true
	}
	for pass := 0; pass < len(order); pass++ {
		changed := false
		for _, id := range order {
			if out[id] {
				continue
			}
			for _, n := range needs[id] {
				if out[n] {
					out[id] = true
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	return out
}

// EjectCard is one card the batch will not land.
type EjectCard struct {
	ID     string
	Reason string
	Class  string // the reason class; a repeat is the same class, not the same git text
	Waited string // the ejected card this one needs, when it is not the root
}

// EjectReq is one batch's eject. Landed is what the same batch did land.
type EjectReq struct {
	Stream string
	Who    string
	Landed []string
	Cards  []EjectCard
}

type ejectMark struct {
	class string
	n     int
}

func parseEjectLog(s string) map[string]ejectMark {
	out := map[string]ejectMark{}
	for _, part := range strings.Split(s, ";") {
		f := strings.Fields(part)
		if len(f) != 3 {
			continue
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n < 1 {
			continue
		}
		out[f[0]] = ejectMark{class: f[1], n: n}
	}
	return out
}

func formatEjectLog(m map[string]ejectMark) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s %s %d", id, m[id].class, m[id].n))
	}
	return strings.Join(parts, ";")
}

func ejectClassOf(c EjectCard) string {
	if c.Waited != "" {
		return "waited:" + c.Waited
	}
	if c.Class != "" {
		return c.Class
	}
	return "head"
}

func ejectWords(c EjectCard) string {
	if c.Waited != "" && !strings.HasPrefix(c.Reason, "waited on ") {
		return "waited on " + c.Waited + ": " + c.Reason
	}
	return c.Reason
}

// Eject returns the cards a batch will not land and raises one judgment each,
// addressed to the coordinator. A card already ejected twice for the same
// class is not returned; the judgment says the brief is wrong and the stream
// stops. A dependent's need of an ejected card is waived, so review does not
// hold a need that has not landed (section 9, rule 11).
func Eject(s *Snapshot, r EjectReq) Plan {
	ctl := s.StreamCtl(r.Stream)
	if ctl == nil {
		var p Plan
		p.refuse(r.Stream, "no such stream")
		return p
	}
	if ctl.F("state") == StreamStopped {
		var p Plan
		p.refuse(r.Stream, "stopped ("+ctl.F("cause")+")")
		return p
	}
	log := parseEjectLog(ctl.F(FieldEjectLog))
	ejected := map[string]bool{}
	for _, c := range r.Cards {
		ejected[c.ID] = true
	}
	var returning, briefs []EjectCard
	for _, c := range r.Cards {
		class := ejectClassOf(c)
		c.Class = class
		mark := log[c.ID]
		if mark.class == class && mark.n >= 2 {
			briefs = append(briefs, c)
			continue
		}
		n := 1
		if mark.class == class {
			n = mark.n + 1
		}
		log[c.ID] = ejectMark{class: class, n: n}
		returning = append(returning, c)
	}
	var p Plan
	if len(returning) > 0 {
		ids := make([]string, len(returning))
		reasons := map[string]string{}
		for i, c := range returning {
			ids[i] = c.ID
			reasons[c.ID] = ejectWords(c)
		}
		p = Return(s, ReturnReq{Sel: Sel{IDs: ids}, Who: r.Who})
		if len(p.Units) == 0 {
			return p
		}
		patchReturnReasons(&p, reasons)
		waiveEjectedNeeds(&p, s, ejected, r.Who)
	}
	var notes []Note
	for _, c := range returning {
		notes = append(notes, ejectNote(s, r, c, false))
	}
	for _, c := range briefs {
		notes = append(notes, ejectNote(s, r, c, true))
	}
	set := map[string]string{}
	if formatted := formatEjectLog(log); formatted != ctl.F(FieldEjectLog) {
		set[FieldEjectLog] = formatted
	}
	if len(briefs) > 0 {
		set["state"] = StreamStopped
		set["since"] = stamp(s.Now)
		set["cause"] = "brief"
		p.Said = append(p.Said, "stream "+r.Stream+" stopped: the brief is wrong")
	}
	if len(set) > 0 || len(notes) > 0 {
		editStream(&p, s, r.Stream, set, nil, notes...)
	}
	// A second eject of the same card is its own judgment. OnePerCause would
	// drop it while the first stays open, so the first is closed here.
	var returningIDs []string
	for _, c := range returning {
		returningIDs = append(returningIDs, c.ID)
	}
	closePriorEjects(&p, s, returningIDs)
	return p
}

// closePriorEjects closes an earlier eject judgment on each id, so this
// eject's judgment is written (store.OnePerCause keeps one open per cause).
func closePriorEjects(p *Plan, s *Snapshot, ids []string) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, o := range s.Open {
		if o.Note.Type == NEjected && want[o.Subject()] {
			p.Closes = append(p.Closes, o)
		}
	}
}

func ejectNote(s *Snapshot, r EjectReq, c EjectCard, brief bool) Note {
	var deps []string
	for _, o := range r.Cards {
		if o.Waited == c.ID {
			deps = append(deps, o.ID)
		}
	}
	dep, landed := "none", "none"
	if len(deps) > 0 {
		dep = strings.Join(deps, ", ")
	}
	if len(r.Landed) > 0 {
		landed = strings.Join(r.Landed, ", ")
	}
	what := fmt.Sprintf("ejected %s: %s; dependents: %s; landed: %s", c.ID, ejectWords(c), dep, landed)
	typ := NEjected
	if brief {
		typ = NBriefWrong
		what = fmt.Sprintf("the brief is wrong: %s was ejected twice for the same reason (%s); dependents: %s; landed: %s", c.ID, ejectWords(c), dep, landed)
	}
	n := judgment(typ, r.Stream, s.Now, 0, c.ID)
	n.Who, n.What = r.Who, what
	n.To = s.Coordinator
	if n.To == "" {
		n.To = Coordinator
	}
	if brief {
		n.StreamLevel = true
	} else if len(n.Decisions) == 0 {
		n.Decisions = []string{"rework", "drop"}
	}
	return n
}

func patchReturnReasons(p *Plan, reasons map[string]string) {
	for i := range p.Units {
		reason := reasons[p.Units[i].Key]
		if reason == "" {
			continue
		}
		for j := range p.Units[i].Changes {
			if p.Units[i].Changes[j].Table != Work {
				continue
			}
			if p.Units[i].Changes[j].Entry.Set == nil {
				p.Units[i].Changes[j].Entry.Set = map[string]string{}
			}
			p.Units[i].Changes[j].Entry.Set["return_reason"] = reason
		}
		for j := range p.Units[i].Notes {
			if p.Units[i].Notes[j].Type == NReturned && p.Units[i].Notes[j].What == "" {
				p.Units[i].Notes[j].What = reason
			}
		}
	}
}

func waiveEjectedNeeds(p *Plan, s *Snapshot, ejected map[string]bool, who string) {
	for i := range p.Units {
		pr := s.Work.Placed(p.Units[i].Key)
		if pr == nil {
			continue
		}
		have := map[string]bool{}
		var waived []string
		for _, n := range Split(pr.F("waived")) {
			if !have[n] {
				have[n] = true
				waived = append(waived, n)
			}
		}
		added := false
		for _, n := range Split(pr.F("needs")) {
			if ejected[n] && !have[n] {
				have[n] = true
				waived = append(waived, n)
				added = true
			}
		}
		if !added {
			continue
		}
		for j := range p.Units[i].Changes {
			if p.Units[i].Changes[j].Table != Work {
				continue
			}
			if p.Units[i].Changes[j].Entry.Set == nil {
				p.Units[i].Changes[j].Entry.Set = map[string]string{}
			}
			p.Units[i].Changes[j].Entry.Set["waived"] = strings.Join(waived, ",")
			p.Units[i].Changes[j].Entry.Set["waived_by"] = who
			p.Units[i].Changes[j].Entry.Set["waived_at"] = stamp(s.Now)
		}
	}
}

// IdleReq is the batch's clock. Landed clears it. Held with nothing landed
// starts it, or raises merge stuck once MergeStuckAfter has passed on s.Now.
type IdleReq struct {
	Stream string
	Who    string
	Why    string
	Landed bool
	Held   bool
}

// MergeIdle keeps the stream's idle clock on its control card (tla/Land.tla
// does not model the clock; the 15 minutes are this store clock, which a test
// advances). One judgment, addressed to the coordinator.
func MergeIdle(s *Snapshot, r IdleReq) Plan {
	ctl := s.StreamCtl(r.Stream)
	if ctl == nil {
		return Plan{}
	}
	var p Plan
	if r.Landed || !r.Held {
		if ctl.F(FieldMergeIdleSince) == "" && ctl.F(FieldMergeIdleWhy) == "" && ctl.F(FieldMergeIdleJudged) == "" {
			return Plan{}
		}
		editStream(&p, s, r.Stream, nil, []string{FieldMergeIdleSince, FieldMergeIdleWhy, FieldMergeIdleJudged})
		return p
	}
	since := ctl.F(FieldMergeIdleSince)
	if since == "" {
		since = stamp(s.Now)
	}
	set := map[string]string{FieldMergeIdleSince: since, FieldMergeIdleWhy: r.Why}
	var notes []Note
	if ctl.F(FieldMergeIdleJudged) == "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil && !s.Now.Before(t.Add(MergeStuckAfter)) {
			n := judgment(NMergeStuck, r.Stream, s.Now, 0)
			n.StreamLevel = true
			n.Who = r.Who
			n.To = s.Coordinator
			if n.To == "" {
				n.To = Coordinator
			}
			n.What = "merge stuck: the batch has landed nothing for 15 minutes: " + r.Why
			n.Decisions = []string{"look", "wait"}
			notes = append(notes, n)
			set[FieldMergeIdleJudged] = stamp(s.Now)
		}
	}
	if ctl.F(FieldMergeIdleSince) == set[FieldMergeIdleSince] && ctl.F(FieldMergeIdleWhy) == r.Why && len(notes) == 0 {
		return Plan{}
	}
	editStream(&p, s, r.Stream, set, nil, notes...)
	return p
}

// editStream is setStream with fields cleared. The control card is changed
// once per step, so a second touch merges into the first.
func editStream(p *Plan, s *Snapshot, stream string, set map[string]string, unset []string, notes ...Note) {
	ctl := s.StreamCtl(stream)
	if ctl == nil || len(set) == 0 && len(unset) == 0 && len(notes) == 0 {
		return
	}
	first := -1
	for i := range p.Units {
		if p.Units[i].Stream != stream {
			continue
		}
		if first < 0 {
			first = i
		}
		for j, c := range p.Units[i].Changes {
			if c.Table == Merge && c.Entry.ID == ctl.ID {
				merged := map[string]string{}
				maps.Copy(merged, c.Entry.Set)
				maps.Copy(merged, set)
				p.Units[i].Changes[j].Entry.Set = nonEmpty(merged)
				p.Units[i].Changes[j].Entry.Unset = append(p.Units[i].Changes[j].Entry.Unset, unsetPresent(ctl, unset)...)
				p.Units[first].Notes = append(p.Units[first].Notes, notes...)
				return
			}
		}
	}
	if first < 0 {
		p.Units = append(p.Units, Unit{Key: ctl.ID, Stream: stream})
		first = len(p.Units) - 1
	}
	p.Units[first].Changes = append(p.Units[first].Changes, change(Merge, setEntry(ctl, set, unset...)))
	p.Units[first].Notes = append(p.Units[first].Notes, notes...)
}

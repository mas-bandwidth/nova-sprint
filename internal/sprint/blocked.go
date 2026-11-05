package sprint

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// BLOCKED: a primary that flaps stops (the owner, 2026-11-05: "I want there to be
// something that automatically catches flaps, and moves them into 'blocked' card state";
// "if a card flaps around more than n times, it goes into blocked primary card state";
// "blocked -> waiting -> ready -> etc."; "When a new card goes into blocked, it sits there
// until you review it and decide what to do."). The model is tla/Blocked.tla.
//
// A flap is one return without progress, counted once per occurrence (recordFlaps):
// a work or read card dealt to a member that ends with no verdict (not taken, an
// environment exit, taken off), a merge refused (merging back), and a rework (review
// back). A finish, a verdict, an accept, a landing and a deal to a new member never
// count. The primary keeps its flaps in FieldFlaps, one line each, pruned to the window
// whenever it is written. More than FlapLimit in FlapWindow, and the tick's block part
// (TickBlock) moves the primary from ready, working or review to blocked in one step:
// its work card is withdrawn (its attempt kept), its read cards retired, one judgment is
// raised and one push sent. Nothing leaves blocked but unblock (to waiting) and drop.

// Blocked is the state of a primary that flapped.
const Blocked State = "blocked"

const (
	// FieldFlaps is the primary's flaps in the window: "<RFC3339> <kind> <what>" a line.
	FieldFlaps = "flaps"
	// FieldBlockedFrom, FieldBlockedAt and FieldBlockedWhy are the block's record.
	FieldBlockedFrom = "blocked_from"
	FieldBlockedAt   = "blocked_at"
	FieldBlockedWhy  = "blocked_why"
	// PropFlapLimit and PropFlapWindow are the work table's properties (set --flap-limit,
	// --flap-window): more than the limit in the window blocks a primary.
	PropFlapLimit  = "flap_limit"
	PropFlapWindow = "flap_window"
	// NFlapBlocked is the judgment a block raises.
	NFlapBlocked = "a primary is blocked: it flapped"
	// RetiredByBlocked is a read card's retirement by a block.
	RetiredByBlocked = "blocked"
	// The kinds of flap.
	FlapNoVerdict = "no-verdict"
	FlapMerge     = "merge-refused"
	FlapRework    = "rework"
)

// FlapLimitDefault and FlapWindowDefault are the settings when none is set.
const (
	FlapLimitDefault  = 5
	FlapWindowDefault = 30 * time.Minute
)

// FlapLimit is the sprint's flap limit: a primary with more flaps than this in the
// window is blocked.
func (s *Snapshot) FlapLimit() int {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropFlapLimit); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 {
				return n
			}
		}
	}
	return FlapLimitDefault
}

// FlapWindow is how far back a flap counts.
func (s *Snapshot) FlapWindow() time.Duration {
	if s.Work != nil {
		if v, ok := s.Work.Prop(PropFlapWindow); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				return d
			}
		}
	}
	return FlapWindowDefault
}

// Flaps is a primary's flaps in the window: the total, by kind, and the last.
type Flaps struct {
	Total  int            `json:"total"`
	ByKind map[string]int `json:"by_kind"`
	Last   string         `json:"last,omitempty"`
}

// Kinds is the counts by kind, in a fixed order: "no-verdict=4 rework=2".
func (f Flaps) Kinds() string {
	var out []string
	for _, k := range []string{FlapNoVerdict, FlapMerge, FlapRework} {
		if n := f.ByKind[k]; n > 0 {
			out = append(out, k+"="+itoa(n))
		}
	}
	return strings.Join(out, " ")
}

// FlapsOf is the primary's flaps in the window ending at now.
func FlapsOf(c *Card, now time.Time, window time.Duration) Flaps {
	f := Flaps{ByKind: map[string]int{}}
	for _, l := range pruneFlaps(strings.Split(c.F(FieldFlaps), "\n"), now, window) {
		parts := strings.SplitN(l, " ", 3)
		f.Total++
		f.ByKind[parts[1]]++
		f.Last = l
	}
	return f
}

// pruneFlaps is the well-formed lines inside the window, each once, oldest first.
func pruneFlaps(lines []string, now time.Time, window time.Duration) []string {
	var out []string
	for _, l := range lines {
		parts := strings.SplitN(l, " ", 3)
		if len(parts) < 2 || slices.Contains(out, l) {
			continue
		}
		at, err := time.Parse(time.RFC3339, parts[0])
		if err != nil || !at.After(now.Add(-window)) {
			continue
		}
		out = append(out, l)
	}
	slices.SortStableFunc(out, func(a, b string) int { return strings.Compare(a[:20], b[:20]) })
	return out
}

// flapExcluded are the retirements that are no flap: a verdict, a step forward, the
// coordinator's own act, or a card moved to another member.
var flapExcluded = []string{"read", "rework", "accept", "brief", "continue", "escalation", RetiredByLevel, RetiredByHold,
	RetiredByCoordinator, RetiredByPrimary, RetiredByCards, RetiredByBlocked}

// recordFlaps adds every flap of the plan to its primary's FieldFlaps, in the plan
// (Applied): every step's plan passes here once. A work-table entry that carries
// FieldFlaps already was counted when it was planned (a change queued for the pump):
// its flaps are kept, with the ones the primary has since, and its move is not counted
// again.
func recordFlaps(s *Snapshot, p Plan) Plan {
	if s == nil || s.Work == nil {
		return p
	}
	now, window := s.Now, s.FlapWindow()
	gone, leavesReview := map[string]bool{}, map[string]bool{}
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			e := ch.Entry
			if ch.Table != Work {
				continue
			}
			if e.Remove || e.Move != nil && e.Move.Col == Blocked {
				gone[e.ID] = true
			}
			if pr := s.Work.Placed(e.ID); pr != nil && pr.Col == Review && e.Move != nil && e.Move.Col != Review {
				leavesReview[e.ID] = true
			}
		}
	}
	add, last := map[string][]string{}, map[string]int{}
	note := func(i int, primary, kind, what string) {
		add[primary] = append(add[primary], stamp(now)+" "+kind+" "+what)
		last[primary] = i
	}
	for i, u := range p.Units {
		for _, ch := range u.Changes {
			e := ch.Entry
			if ch.Table == Work {
				pr := s.Work.Placed(e.ID)
				if pr == nil || e.Move == nil || e.Set[FieldFlaps] != "" || IsSentinel(pr) {
					continue
				}
				back := e.Move.Col == Working || e.Move.Col == Ready
				switch {
				case pr.Col == Review && back:
					note(i, e.ID, FlapRework, e.ID+" review -> "+e.Move.Col)
				case pr.Col == Merging && (back || e.Move.Col == Review):
					note(i, e.ID, FlapMerge, e.ID+" merging -> "+e.Move.Col)
				}
				continue
			}
			var c *Card
			switch {
			case ch.Table == Fleet && s.Fleet != nil:
				c = s.Fleet.Card(e.ID)
			case ch.Table == Readers && s.Readers != nil:
				c = s.Readers.Card(e.ID)
			}
			if c == nil || e.Create != nil {
				continue
			}
			field := func(k string) string {
				if v, ok := e.Set[k]; ok {
					return v
				}
				return c.F(k)
			}
			primary, kind := c.F("primary"), c.F("kind")
			if ch.Table == Readers {
				kind = "read"
			}
			if primary == "" || gone[primary] || kind == "read" && leavesReview[primary] || e.Set["dropped"] != "" {
				continue
			}
			to := c.Col
			if e.Move != nil {
				to = e.Move.Col
			}
			if e.Remove {
				to = "retired"
			}
			out := c.Col == Ready || c.Col == Working || c.Col == Asked || c.Col == Reading
			back := to == "retired" || to == Withdrawn || to == Redealt || c.Col == Working && to == Ready || e.Set[FieldReturned] != ""
			if !out || !back || field("verdict") != "" || field("result") != "" || slices.Contains(flapExcluded, field("retired_by")) {
				continue
			}
			by := field("retired_by")
			if by == "" {
				by = c.Row
			}
			note(i, primary, FlapNoVerdict, e.ID+" "+c.Col+" -> "+to+" ("+by+")")
		}
	}
	// the primaries whose flaps the plan writes: the new ones, and an entry queued earlier
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Table == Work && ch.Entry.Set[FieldFlaps] != "" && add[ch.Entry.ID] == nil && !gone[ch.Entry.ID] {
				add[ch.Entry.ID] = []string{}
			}
		}
	}
	for primary, lines := range add {
		pr := s.Work.Placed(primary)
		if pr == nil {
			continue
		}
		ui, ci := -1, -1
		for i := range p.Units {
			for j, ch := range p.Units[i].Changes {
				if ch.Table == Work && ch.Entry.ID == primary && !ch.Entry.Remove {
					ui, ci = i, j
				}
			}
		}
		if ui < 0 {
			ui = last[primary]
			p.Units[ui].Changes = append(p.Units[ui].Changes, change(Work, setEntry(pr, nil)))
			ci = len(p.Units[ui].Changes) - 1
		}
		ch := &p.Units[ui].Changes[ci]
		all := append(strings.Split(pr.F(FieldFlaps), "\n"), strings.Split(ch.Entry.Set[FieldFlaps], "\n")...)
		value := strings.Join(pruneFlaps(append(all, lines...), now, window), "\n")
		set := make(map[string]string, len(ch.Entry.Set)+1)
		for k, v := range ch.Entry.Set {
			set[k] = v
		}
		set[FieldFlaps] = value
		if value == "" {
			delete(set, FieldFlaps)
		}
		ch.Entry.Set = nonEmpty(set)
	}
	return p
}

// PartBlock is the work pump's block part, after the drain and before the resolve
// and the deal: a flapping card is dealt nothing more in the tick it is caught.
const PartBlock = "block"

// WithFlapSettings adds set --flap-limit and --flap-window to a set step's plan, as
// WithPeggedBound adds its bound: "" leaves a setting, default takes it off.
func WithFlapSettings(p Plan, s *Snapshot, limit, window string) Plan {
	if limit == "" && window == "" {
		return p
	}
	for _, x := range p.Refused {
		if !strings.HasPrefix(x.Why, "nothing to set") {
			return p
		}
	}
	p.Refused = nil
	var words []string
	for _, w := range []struct{ prop, flag, v, def string }{
		{PropFlapLimit, "flap-limit", limit, itoa(FlapLimitDefault)},
		{PropFlapWindow, "flap-window", window, FlapWindowDefault.String()},
	} {
		if w.v == "" {
			continue
		}
		if w.v != ReadTierDefault {
			n, err := strconv.Atoi(w.v)
			d, derr := time.ParseDuration(w.v)
			if w.prop == PropFlapLimit && (err != nil || n < 1) || w.prop == PropFlapWindow && (derr != nil || d <= 0) {
				return Plan{Refused: []Refusal{{Key: "set", Why: "--" + w.flag + " wants a whole number from 1 (limit) or a duration above zero (window), or " + ReadTierDefault + " for " + w.def + "; found " + w.v}}}
			}
		}
		was, had := s.Work.Prop(w.prop)
		p.Props = append(p.Props, PropWrite{Table: Work, Name: w.prop, Value: w.v, Was: was, WasAbsent: !had})
		words = append(words, w.flag+" "+w.v)
	}
	for i := range p.Units {
		if p.Units[i].Key == "set" {
			if strings.TrimSpace(p.Units[i].Moved) == "sprint" {
				p.Units[i].Moved = "sprint " + strings.Join(words, ", ")
			} else {
				p.Units[i].Moved += ", " + strings.Join(words, ", ")
			}
			return p
		}
	}
	p.Units = append(p.Units, Unit{Key: "set", Moved: "sprint " + strings.Join(words, ", ")})
	return p
}

// TickBlock is the tick's block part: every primary in ready, working or review with
// more flaps than the limit in the window is blocked, each in one unit.
func TickBlock(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	p.on(s)
	p.blocking = true
	limit, window := s.FlapLimit(), s.FlapWindow()
	for _, c := range s.Work.Column(Ready, Working, Review) {
		if IsSentinel(c) || IsHeld(c) {
			continue
		}
		if f := FlapsOf(c, s.Now, window); f.Total > limit {
			p.Units = append(p.Units, blockUnit(s, c, f, window, r.who()))
		}
	}
	return Lawful(p), 0
}

// blockUnit blocks one primary: its work card withdrawn on its row (its attempt kept,
// no redeal spent), its read cards retired, the primary moved to blocked with where it
// was, and one judgment.
func blockUnit(s *Snapshot, c *Card, f Flaps, window time.Duration, who string) Unit {
	u := Unit{Key: c.ID, Stream: c.Row}
	now := stamp(s.Now)
	for _, fc := range s.Fleet.Of(c.ID) {
		if fc.Col != Ready && fc.Col != Working {
			continue
		}
		if fc.F("kind") == "read" {
			u.Changes = append(u.Changes, change(Fleet, removeEntry(fc, map[string]string{"retired": now, "retired_by": RetiredByBlocked})))
			continue
		}
		set := nextGen(fc, "", s.Now)
		set["withdrawn"], set["withdrawn_why"] = now, "blocked: flapping"
		u.Changes = append(u.Changes, change(Fleet, moveEntry(fc, fc.Row, Withdrawn, set, "taken", "dealt")))
	}
	if s.Readers != nil {
		for _, rc := range s.Readers.Of(c.ID) {
			if rc.Col == Asked || rc.Col == Reading {
				u.Changes = append(u.Changes, change(Readers, removeEntry(rc, map[string]string{"retired": now, "retired_by": RetiredByBlocked})))
			}
		}
	}
	what := BlockedWhat(c.ID, f, window)
	u.Changes = append(u.Changes, change(Work, moveEntry(c, c.Row, Blocked,
		map[string]string{FieldBlockedFrom: c.Col, FieldBlockedAt: now, FieldBlockedWhy: what}, "work")))
	n := judgment(NFlapBlocked, c.Row, s.Now, 0, c.ID)
	n.Card, n.What, n.Who = c.ID, what, who
	n.Hint = "nova-sprint unblock " + c.ID + " --reason '<what was fixed>' (to waiting), or brief it again first, or drop it"
	u.Notes = append(u.Notes, n)
	u.Moved = fmt.Sprintf("%s %s -> blocked (%d flaps in %s: %s)", c.ID, c.Col, f.Total, window, f.Kinds())
	return u
}

// BlockedWhat is the block's words: the judgment's and the log's.
func BlockedWhat(id string, f Flaps, window time.Duration) string {
	return fmt.Sprintf("blocked: %s flapped: %s in %s; last: %s", id, f.Kinds(), window, f.Last)
}

var blockedWhatRE = regexp.MustCompile(`^blocked: (\S+) flapped: (.*) in (\S+); last: (.*)$`)

// BlockedPush is the one push a block sends the coordinator, not folded into a digest:
// "BLOCKED <card>: <n> flaps in <window>", the counts by kind and the last bounce.
func BlockedPush(n Note) (subject, body string, ok bool) {
	m := blockedWhatRE.FindStringSubmatch(n.What)
	if n.Type != NFlapBlocked || m == nil {
		return "", "", false
	}
	total := 0
	for _, w := range strings.Fields(m[2]) {
		if _, v, ok := strings.Cut(w, "="); ok {
			k, _ := strconv.Atoi(v)
			total += k
		}
	}
	subject = fmt.Sprintf("BLOCKED %s: %d flaps in %s", m[1], total, m[3])
	body = subject + "\nflaps: " + m[2] + "\nlast: " + m[4] + "\nnext: " + n.Hint
	return subject, body, true
}

// withoutBlockedDeals refuses every unit that deals a blocked primary's card, on any
// path (Applied): a work or read card placed in, or moved into, ready, working, asked or
// reading (tla/Blocked.tla: a blocked card is never dealt).
func withoutBlockedDeals(s *Snapshot, p Plan) Plan {
	if s == nil || s.Work == nil || len(s.Work.Column(Blocked)) == 0 {
		return p
	}
	kept := p.Units[:0:0]
	for _, u := range p.Units {
		why := ""
		for _, c := range u.Changes {
			if why = dealsBlocked(s, c); why != "" {
				break
			}
		}
		if why != "" {
			p.refuse(u.Key, why)
			continue
		}
		kept = append(kept, u)
	}
	p.Units = kept
	return p
}

func dealsBlocked(s *Snapshot, c Change) string {
	e := c.Entry
	var to string
	switch {
	case c.Table != Fleet && c.Table != Readers:
		return ""
	case e.Create != nil:
		to = e.Create.Col
	case e.Move != nil:
		to = e.Move.Col
	}
	if to != Ready && to != Working && to != Asked && to != Reading {
		return ""
	}
	primary := e.Set["primary"]
	if primary == "" {
		var card *Card
		if c.Table == Fleet && s.Fleet != nil {
			card = s.Fleet.Card(e.ID)
		} else if c.Table == Readers && s.Readers != nil {
			card = s.Readers.Card(e.ID)
		}
		if card == nil || card.Col == to {
			return ""
		}
		primary = card.F("primary")
	}
	if pr := s.Work.Placed(primary); pr != nil && pr.Col == Blocked {
		return BlockedWhy(primary) + ": none of its cards is dealt"
	}
	return ""
}

// BlockedWhy is the refusal of a verb that would move a blocked primary.
func BlockedWhy(id string) string {
	return id + " is blocked (it flapped); nothing moves it but nova-sprint unblock " + id + " --reason '<what was fixed>' (to waiting), or drop; brief may edit it while it is blocked"
}

// UnblockReq is the coordinator's unblock of a blocked primary.
type UnblockReq struct {
	ID      string
	Reason  string
	Answers []string
	Who     string
}

// Unblock moves a blocked primary to waiting, its flaps cleared and its judgment closed:
// the resolve takes it to ready, and the deal on, as any waiting card.
func Unblock(s *Snapshot, r UnblockReq) Plan {
	var p Plan
	p.on(s)
	p.unblocking = true
	if why := notCoordinator(s, r.Who, "unblock"); why != "" {
		p.refuse(r.ID, why)
		return p
	}
	if strings.TrimSpace(r.Reason) == "" {
		p.refuse(r.ID, "unblock wants --reason <what was fixed>")
		return p
	}
	c := s.Work.Placed(r.ID)
	if c == nil || c.Col != Blocked {
		where := "not on the table"
		if c != nil {
			where = "it is " + c.Col
		}
		p.refuse(r.ID, r.ID+" is not blocked ("+where+"); nothing was changed")
		return p
	}
	u := Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, moveEntry(c, c.Row, Waiting,
		map[string]string{"unblocked_at": stamp(s.Now), "unblocked_by": r.Who, "unblock_reason": r.Reason},
		FieldFlaps, FieldBlockedFrom, FieldBlockedAt, FieldBlockedWhy))}}
	u.Closes = closesFor(s.Open, []string{NFlapBlocked}, c.ID)
	u.Moved = fmt.Sprintf("%s blocked -> waiting (unblocked: %s)", c.ID, r.Reason)
	p.Units = append(p.Units, u)
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

// BlockedCard is one blocked primary as `nova-sprint blocked` lists it.
type BlockedCard struct {
	ID     string `json:"id"`
	Stream string `json:"stream"`
	From   string `json:"from"`
	Flaps  Flaps  `json:"flaps"`
	Since  string `json:"since"`
	For    string `json:"for"`
}

// BlockedCards is every blocked primary, in work order, with its counts, its last flap
// and how long it has been blocked.
func BlockedCards(s *Snapshot) []BlockedCard {
	out := []BlockedCard{}
	for _, c := range s.Work.Column(Blocked) {
		at, _ := time.Parse(time.RFC3339, c.F(FieldBlockedAt))
		// the flaps as they stood at the block: the window is the block's, not now's
		out = append(out, BlockedCard{ID: c.ID, Stream: c.Row, From: c.F(FieldBlockedFrom), Flaps: FlapsOf(c, at.Add(time.Second), s.FlapWindow()),
			Since: c.F(FieldBlockedAt), For: s.Now.Sub(at).Truncate(time.Second).String()})
	}
	return out
}

// justUnblocked is whether the primary was unblocked and is waiting for the resolve.
func justUnblocked(s *Snapshot, id string) bool {
	pr := s.Work.Placed(id)
	return pr != nil && pr.Col == Waiting && pr.F("unblocked_at") != ""
}

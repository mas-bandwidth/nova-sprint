package sprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The read mix (the owner, 2026-10-20: "I would like to see 4 reads... 2 friends, 2 fleet.
// Any 2 friends. Any 2 fleet models (different)"; "Let's test going wide on the reads").
// nova-sprint set --read-mix friends=NF,fleet=NM switches it on (the owner, 2026-10-20:
// "increase reads to 5"; the working default a clear writes is ReadMixWorking, two friends
// and three fleet readers on three models; off takes it off): every card in review then
// needs NF+NM ok reads at its head (at most MaxReads, standing for --reads while it is on),
// and the read-card deal (readCardsAskSelected) asks them all at once, NF to distinct friends
// first, then NM to distinct fleet members, each fleet read drawn on a route whose model no
// fleet read of the attempt used (standing, or ended with no verdict). Short of a class, or of
// a distinct model, the deal fills from the readers up and never waits for the class, and one
// judgment on the primary (NReadMixShort) says so. A read that ends with no verdict spends its
// reader as before; the next deal counts the classes again, so its read goes to the same
// class first, and a fleet read to another model. The mix needs read cards on; the readers
// table's ask ignores it. tla/ReadMix.tla.

// PropReadMix is the work table's property holding the mix: "friends=<n>,fleet=<m>", default
// (ReadMixWorking), or off (no mix: the deal orders the readers as before). Absent in a
// cleared epoch it reads as its working default (clearedDefault); absent in the first epoch,
// never cleared, it is off.
const PropReadMix = "read_mix"

// ReadMixWorking is the sprint's working mix (the owner, 2026-10-20: "increase reads to 5",
// two friends and three fleet readers on three different models).
const ReadMixWorking = "friends=2,fleet=3"

// MaxReads is the most ok reads a card may need (set --reads, set --read-mix).
const MaxReads = 5

// NReadMixShort is the tick's judgment that a primary's reads were dealt short of the mix: a
// class had fewer readers who could read it than the mix names, or the fleet's routes fewer
// distinct models, and the deal filled from the readers up. One judgment for the primary while
// its reads stand so (condKey: its type and subject).
const NReadMixShort = "reads dealt short of the read mix"

// ReadMix is a mix: the friends' reads and the fleet's.
type ReadMix struct{ Friends, Fleet int }

// Total is the reads the mix needs.
func (m ReadMix) Total() int { return m.Friends + m.Fleet }

// String is the mix as set --read-mix takes it.
func (m ReadMix) String() string { return fmt.Sprintf("friends=%d,fleet=%d", m.Friends, m.Fleet) }

// parseReadMix reads a mix, "friends=<n>,fleet=<m>" in either order, a class not named 0; why
// is the refusal, "" for a mix of 1 to MaxReads reads.
func parseReadMix(v string) (m ReadMix, why string) {
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		k, n, ok := strings.Cut(strings.TrimSpace(part), "=")
		x, err := strconv.Atoi(n)
		if !ok || err != nil || x < 0 || seen[k] || k != "friends" && k != "fleet" {
			return ReadMix{}, "--read-mix wants friends=<n>,fleet=<m> (each a whole number, at most " + itoa(MaxReads) + " together), " + ReadTierDefault + " (" + ReadMixWorking + ") or " + SwitchOff + "; found " + v
		}
		seen[k] = true
		if k == "friends" {
			m.Friends = x
		} else {
			m.Fleet = x
		}
	}
	if t := m.Total(); t < 1 || t > MaxReads {
		return ReadMix{}, "--read-mix wants 1 to " + itoa(MaxReads) + " reads together; found " + v
	}
	return m, ""
}

// ReadMixSet is the sprint's mix, ok false while none is set (or the property is unreadable).
func (s *Snapshot) ReadMixSet() (ReadMix, bool) {
	if s == nil || s.Work == nil {
		return ReadMix{}, false
	}
	v, had := s.Work.Prop(PropReadMix)
	if !had || v == "" {
		v, _ = s.clearedDefault(PropReadMix)
	}
	if v == ReadTierDefault {
		v = ReadMixWorking
	}
	if v == "" || v == SwitchOff {
		return ReadMix{}, false
	}
	m, why := parseReadMix(v)
	return m, why == ""
}

// friendReader says the reader named is a friend (her seat, or her friend row on the fleet
// table): a read card's reader field names a friend or a member.
func (s *Snapshot) friendReader(name string) bool {
	if slices.ContainsFunc(s.Friends, func(f FriendSeat) bool { return f.Name == name }) {
		return true
	}
	return s.Fleet != nil && s.Fleet.HasRow(FriendRow(name))
}

// modelID is a model's id without its provider or namespace (provider/model, or a router's
// provider/vendor/model): its last part, so two providers of one model are one model. A read
// card's model and a route's are read alike (readMixCounts, readMixAvoid).
func modelID(providerModel string) string {
	return providerModel[strings.LastIndex(providerModel, "/")+1:]
}

// readMixCounts is the primary's reads that stand at its attempt (placed, or ok at its head)
// by class, and the fleet models they and the attempt's no-verdict fleet reads used.
func readMixCounts(s *Snapshot, pr *Card, cards []*Card) (friends, fleet int, used []string) {
	placed, oks, _ := fleetReadLiveOf(s, pr, cards)
	addModel := func(c *Card) {
		if m := modelID(c.F(FieldModel)); m != "" && !slices.Contains(used, m) {
			used = append(used, m)
		}
	}
	for _, c := range append(slices.Clone(placed), oks...) {
		if s.friendReader(c.F("reader")) {
			friends++
			continue
		}
		fleet++
		addModel(c)
	}
	// a fleet read handed back with no verdict (an environment or a provider failure), or
	// retired late with none: its
	// model is not drawn again while another serves (tla/ReadMix.tla, DistinctModelWhenServed)
	prefix := attemptReadPrefix(pr)
	for _, c := range cards {
		if c != nil && strings.HasPrefix(c.ID, prefix) && !c.Placed() && c.F("verdict") == "" &&
			(c.F("retired_by") == RetiredByReturned || c.F("retired_by") == RetiredByLate) && !s.friendReader(c.F("reader")) {
			addModel(c)
		}
	}
	return friends, fleet, used
}

// Read classes a phase of the deal takes (readMixPhases).
const (
	readAnyClass = iota
	readFriendClass
	readFleetClass
)

// readPhase is one phase of the deal of a primary's reads: the class it takes and the most
// reads it deals.
type readPhase struct{ class, n int }

// readMixPhases is the order the deal takes the reads the primary wants: with no mix, one
// phase of any class; with a mix, the friends it is short of, then the fleet reads, then any
// class for the rest (the fill: never waiting for a class; tla/ReadMix.tla ClassOK).
func readMixPhases(s *Snapshot, pr *Card, cards []*Card, want int) []readPhase {
	mix, on := s.ReadMixSet()
	if !on {
		return []readPhase{{readAnyClass, want}}
	}
	friends, fleet, _ := readMixCounts(s, pr, cards)
	f := min(max(0, mix.Friends-friends), want)
	m := min(max(0, mix.Fleet-fleet), want-f)
	return []readPhase{{readFriendClass, f}, {readFleetClass, m}, {readAnyClass, want - f - m}}
}

// classFits says the unit is of the phase's class.
func classFits(u readUnit, class int) bool {
	return class == readAnyClass || (class == readFriendClass) == u.friend
}

// readMixAvoid is the routes of the primary's read tier whose model is one of used: a fleet
// read under the mix draws another while one serves (readRouteOf falls back to them when
// none does).
func (s *Snapshot) readMixAvoid(pr *Card, used []string) []string {
	if len(used) == 0 {
		return nil
	}
	tier := s.readTierOf(pr)
	var out []string
	for _, r := range s.Routes {
		if r.Tier == tier && slices.Contains(used, modelID(r.Model)) {
			out = append(out, r.Name)
		}
	}
	return out
}

// readMixShortConds is the tick's NReadMixShort judgments: each primary in review whose reads
// at its attempt are all dealt (as many stand as it needs) and stand short of the mix: fewer
// friends or fleet reads than it names, or two fleet reads on one model.
func readMixShortConds(s *Snapshot) []cond {
	mix, on := s.ReadMixSet()
	if !on || s.Work == nil || s.Fleet == nil {
		return nil
	}
	idx := fleetReadIndex(s)
	var out []cond
	for _, pr := range s.Work.Column(Review) {
		if IsSentinel(pr) {
			continue
		}
		cards := idx[pr.ID]
		placed, oks, brk := fleetReadLiveOf(s, pr, cards)
		if len(brk) > 0 || len(placed)+len(oks) < ReadsNeededIn(s, pr) {
			continue
		}
		friends, fleet, _ := readMixCounts(s, pr, cards)
		var models []string
		dup := false
		for _, c := range append(slices.Clone(placed), oks...) {
			if s.friendReader(c.F("reader")) {
				continue
			}
			if m := modelID(c.F(FieldModel)); m != "" {
				dup = dup || slices.Contains(models, m)
				models = append(models, m)
			}
		}
		if friends >= mix.Friends && fleet >= mix.Fleet && !dup {
			continue
		}
		slices.Sort(models)
		what := fmt.Sprintf("%s: %s's reads are %d friends' and %d fleet reads (models %s); the mix wants %s: dealt from the readers up, as many as it needs",
			NReadMixShort, pr.ID, friends, fleet, orDash(strings.Join(models, ",")), mix)
		out = append(out, cond{typ: NReadMixShort, stream: pr.Row, primaries: []string{pr.ID}, what: what, decisions: TickDecisions[NReadMixShort]})
	}
	return out
}

// askAnotherReadCard is ask --another while read cards are on: one more read card of each
// primary chosen, dealt by the read-card deal's own rule (readCardsAskSelected with one
// extra; the read mix's classes first when it is on), never a readers-table read, which the
// tick's take-back would retire at once as "read cards" (the coordinator, 2026-10-20: each
// extra ask was withdrawn with outcome "read cards" and added nothing). When the deal can
// deal one of the primaries none (its readers are only on the readers table), the ask goes
// the readers table's way as before; one more reader never overrules a broken verdict
// (AskAnotherResolves leaves the broken judgment open).
// p is the ask's plan so far (pick's refusals). ok is false, and the ask goes on the readers
// table's way, when a primary chosen is dealt none.
func askAnotherReadCard(s *Snapshot, r AskReq, chosen []*Card, p Plan) (Plan, bool) {
	if len(chosen) == 0 {
		return p, true
	}
	only := map[string]bool{}
	for _, c := range chosen {
		only[c.ID] = true
	}
	var ri routeIndexes
	if len(s.Routes) > 0 {
		ri = routeIndexesOf(s)
	}
	dealt, _ := readCardsAskSelected(s, s.Friends, ri, nil, nil, only, 1)
	p.Rows = append(p.Rows, dealt.Rows...)
	p.Props = append(p.Props, dealt.Props...)
	p.Refused = append(p.Refused, dealt.Refused...)
	for _, u := range dealt.Units {
		if slices.ContainsFunc(u.Changes, func(ch Change) bool {
			return ch.Table == Fleet && ch.Entry.Create != nil && ch.Entry.Set["kind"] == "read"
		}) {
			for _, o := range closesFor(s.Open, AskAnotherResolves, u.Key) {
				if !slices.ContainsFunc(u.Closes, func(x Open) bool { return x.Key == o.Key }) {
					u.Closes = append(u.Closes, o)
				}
			}
			u.Moved += "; one more reader (ask --another)"
		}
		p.Units = append(p.Units, u)
	}
	dealtTo := 0
	for _, c := range chosen {
		if slices.ContainsFunc(p.Units, func(u Unit) bool {
			return u.Key == c.ID && slices.ContainsFunc(u.Changes, func(ch Change) bool { return ch.Entry.Create != nil })
		}) {
			dealtTo++
			continue
		}
		p.refuse(c.ID, "no reader card could be dealt one more read of "+c.ID+" (no friend or member up and free that may read it); ask "+c.ID+" --another alone to ask it of the readers table")
	}
	if dealtTo == 0 {
		return Plan{}, false // none dealt: the readers table's way, as before
	}
	if len(p.Refused) > 0 && len(r.Answers) > 0 {
		return Plan{Refused: p.Refused}, true
	}
	if ri != nil {
		ri.write(&p)
	}
	answered(&p, s, r.Answers, r.Who)
	return p, true
}

// readMixNeed is the ok reads a primary needs under the mix: its total, bounded by the
// readers the sprint has for the card at its attempt (readMixPool: a sprint of two readers is
// never held for five, the never-wait-forever rule; the short of it is judged,
// NReadMixShort), and never below its tier's rule or the mix's total, whichever is less (the
// floor a sprint with too few readers is judged at as before: cannot ask).
func readMixNeed(s *Snapshot, pr *Card, mix ReadMix) int {
	floor := min(ReadsNeeded(pr), mix.Total())
	return max(floor, min(mix.Total(), readMixPool(s, pr)))
}

// readMixPool is how many different readers the sprint has for the primary at its attempt,
// up or not, so the count never moves with a reader going down or quiet and a card accepted
// is never sent back for a reader who came back (ShortReadsBack): every friend seat whose
// roles name reader and whose tiers reach the read tier (not held), every member with a
// reader row neither held nor retired that serves it (not gates-only); never the attempt's
// worker, nor a reader spent at the attempt (it read or handed the read back) or that refused
// the card's staging, those counted only while a read of theirs stands (placed, or ok at its
// head). Within an attempt it only falls, as readers are spent; it rises only when the
// coordinator adds a reader (tla/ReadMix.tla, Pool and NeverStuckOnPool).
func readMixPool(s *Snapshot, pr *Card) int {
	if s == nil || s.Fleet == nil || pr == nil {
		return 0
	}
	attempt := readAttempt(pr)
	worker := attemptUnit(s, pr.ID, attempt)
	t := s.readTierOf(pr)
	cards := s.Fleet.WithPrefix(attemptReadPrefix(pr))
	names := map[string]bool{}
	usable := func(name string) bool {
		if name == worker {
			return false
		}
		mine := readerCardsAt(cards, pr.ID, attempt, name)
		if readSpent(mine) {
			return false
		}
		for _, c := range mine {
			if slices.Contains(StagingRefusers(c), name) {
				return false
			}
		}
		return true
	}
	for _, f := range s.Friends {
		if friendHasRole(f, RoleReader) && !f.Held && f.Status != Held && s.FriendsTake(t) && friendAtOrAbove(s, f, t) && usable(f.Name) {
			names[f.Name] = true
		}
	}
	for _, m := range s.Fleet.Rows() {
		if _, friend := FriendOfRow(m); friend || !memberReads(s, m) || s.MemberCtl(m).F(FieldGatesOnly) != "" {
			continue
		}
		if s.FleetTakes(t) && s.readerServesTier(ReaderPrefix+m, t) && usable(m) {
			names[m] = true
		}
	}
	placed, oks, _ := fleetReadLiveOf(s, pr, cards)
	for _, c := range append(placed, oks...) {
		if rd := c.F("reader"); rd != "" {
			names[rd] = true
		}
	}
	return len(names)
}

// roomFor says the unit has room for a read of the phase. A friend is dealt a read only while
// her row has a free slot (half > 0: her width less her work and read cards, ready and
// working), in every phase: a read never waits in the ready column of a friend whose row is at
// its width (the owner, 2026-10-24: four reads sat an hour on a friend at 8 of 8 while 18 cards
// waited in review; roadmap reads-never-wait-behind-a-full-row; tla/FriendReadSlot.tla,
// ReadOnlyToAFreeSlot). A member in the mix's fleet phase is dealt the read at its room too,
// queued on its row, and taken back unstarted past the deal bound as any read
// (RetiredByUnstarted); the fill and a deal with no mix keep the room rule (half > 0).
func roomFor(u readUnit, ph readPhase) bool {
	return u.half > 0 || (ph.class != readAnyClass && !u.friend)
}

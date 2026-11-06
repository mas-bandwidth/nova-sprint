package card

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// mover builds one plan: the conditions and writes, in the order the tables
// give them. A set write is added only when it changes its column, so layer 1
// never refuses a plan for a write that changes nothing.
type mover struct {
	plan  Plan
	p     Card   // the producer, or the consumer for MoveConsumer
	cons  []Card // the producer's consumers
	rows  map[string]Row
	s     Stream
	needs map[string]Card
}

func (m *mover) expect(e ...store.Expect) { m.plan.Expect = append(m.plan.Expect, e...) }
func (m *mover) write(w ...store.Write)   { m.plan.Writes = append(m.plan.Writes, w...) }

// state, reason and head are the producer's own columns, written only when
// they change.
func (m *mover) state(id, s string) { m.write(store.SetCardState{Card: id, State: s}) }

func (m *mover) reason(id, was, now string) {
	if now != "" && now != was { // "" leaves the reason as it is
		m.write(store.SetCardReason{Card: id, Reason: now})
	}
}

func (m *mover) head(id, was, now string) {
	if now != "" && now != was {
		m.write(store.SetCardHead{Card: id, Head: now})
	}
}

func no(format string, args ...any) *Refusal { return &Refusal{fmt.Sprintf(format, args...)} }

// name is an event's type name, for a reason: "Finished".
func name(e Event) string { return strings.TrimPrefix(fmt.Sprintf("%T", e), "card.") }

func final(kind, state string) bool {
	if consumer(kind) {
		return state == Done || state == Fail
	}
	return state == LandedState || state == Fail
}

func consumer(kind string) bool { return kind == "work" || kind == "read" }

func live(c Card) bool { return c.State == Ready || c.State == Working }

// from refuses a producer whose state is not one the event moves from:
// "producer X is in review, Finished needs working".
func (m *mover) from(e Event, states ...string) *Refusal {
	if slices.Contains(states, m.p.State) {
		return nil
	}
	return no("%s %s is in %s, %s needs %s", role(m.p.Kind), m.p.ID, m.p.State, name(e), orList(states))
}

func role(kind string) string {
	if consumer(kind) {
		return "consumer"
	}
	return kind
}

// orList is "ready, review or working".
func orList(ss []string) string {
	if len(ss) == 1 {
		return ss[0]
	}
	return strings.Join(ss[:len(ss)-1], ", ") + " or " + ss[len(ss)-1]
}

// isSHA is a full 40-hex commit sha.
func isSHA(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}

func sha(what, s string) *Refusal {
	if isSHA(s) {
		return nil
	}
	if s == "" {
		return no("%s is empty; it needs a full commit sha", what)
	}
	return no("%s %s is not a full commit sha", what, s)
}

// Move is a producer's (or sentinel's) move on event e. consumers are its
// consumers (every card whose of is p), rows the rows the event names, s its
// stream, needs every card reachable through p's needs and through the needs
// of a Relink target (for the landed, failed and cycle checks).
func Move(p Card, consumers []Card, rows map[string]Row, s Stream, needs map[string]Card, e Event) (Plan, *Refusal) {
	if consumer(p.Kind) {
		return Plan{}, no("card %s is a %s consumer; Move is for producers", p.ID, p.Kind)
	}
	if final(p.Kind, p.State) {
		return Plan{}, no("%s %s is %s; nothing moves a %s card", p.Kind, p.ID, p.State, p.State)
	}
	m := &mover{p: p, cons: consumers, rows: rows, s: s, needs: needs}
	m.expect(store.CardGen{Card: p.ID, Gen: p.Gen})
	if r := m.producerEvent(e); r != nil {
		return Plan{}, r
	}
	return m.plan, nil
}

func (m *mover) producerEvent(e Event) *Refusal {
	switch v := e.(type) {
	case Resolve:
		return m.resolve(v)
	case Park:
		return m.park(v)
	case Reached:
		return m.reached(v)
	case Start:
		return m.start(v)
	case Finished:
		return m.finished(v)
	case Broken:
		return m.returned(v, v.Consumer, "fail", true)
	case Faulted:
		return m.returned(v, v.Consumer, "fault", false)
	case AskRead:
		return m.askRead(v)
	case ReadYes:
		return m.readYes(v)
	case ReadFix:
		return m.readFix(v)
	case Landed:
		return m.landed(v)
	case Red:
		return m.red(v)
	case Released:
		return m.released(v)
	case Cancel:
		return m.fail(v.Reason)
	case Replace:
		return m.replace(v)
	case NeedFailed:
		return m.needFailed(v)
	case Waive:
		return m.waive(v)
	case Relink:
		return m.relink(v)
	case Edit:
		return m.edit(v)
	case Rescore:
		return m.rescore(v)
	case SetPriority:
		return m.setPriority(v)
	case Block:
		return m.block(v)
	case Unblock:
		return m.unblock(v)
	}
	return no("%s is not a producer event; Move takes the producer table's events", name(e))
}

// stream refuses an event on a stream other than p's, and, when free is
// set, a held one; it fences the hold either way.
func (m *mover) stream(free bool) *Refusal {
	switch {
	case m.s.Name != m.p.Stream:
		return no("stream %s is not producer %s's stream %s", m.s.Name, m.p.ID, m.p.Stream)
	case free && m.s.Held:
		return no("stream %s is held; no new consumers start in a held stream", m.s.Name)
	case !free && !m.s.Held:
		return no("stream %s is not held; Park is for a held stream", m.s.Name)
	}
	m.expect(store.StreamHeld{Stream: m.s.Name, Held: !free})
	return nil
}

// landedNeeds refuses a need that does not exist or has not landed, then
// fences them all with NeedsLanded.
func (m *mover) landedNeeds() *Refusal {
	for _, n := range m.p.Needs {
		need, ok := m.needs[n]
		switch {
		case !ok:
			return no("need %s of %s %s does not exist", n, role(m.p.Kind), m.p.ID)
		case need.State != LandedState:
			return no("need %s of %s %s is %s, not landed", n, role(m.p.Kind), m.p.ID, need.State)
		}
	}
	m.expect(store.NeedsLanded{Card: m.p.ID})
	return nil
}

func (m *mover) resolve(e Resolve) *Refusal {
	if m.p.Kind != "producer" {
		return no("sentinel %s is landed by Reached, never resolved", m.p.ID)
	}
	if r := m.from(e, Waiting); r != nil {
		return r
	}
	if r := m.stream(true); r != nil {
		return r
	}
	if r := m.landedNeeds(); r != nil {
		return r
	}
	m.state(m.p.ID, Ready)
	return nil
}

func (m *mover) park(e Park) *Refusal {
	if r := m.from(e, Ready); r != nil {
		return r
	}
	if r := m.stream(false); r != nil {
		return r
	}
	m.state(m.p.ID, Waiting)
	if e.Reason == "" {
		e.Reason = "stream " + m.s.Name + " is held"
	}
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

func (m *mover) reached(e Reached) *Refusal {
	if m.p.Kind != "sentinel" {
		return no("producer %s is not a sentinel; Reached is for sentinels", m.p.ID)
	}
	if r := m.from(e, Waiting); r != nil {
		return r
	}
	if r := m.landedNeeds(); r != nil {
		return r
	}
	m.state(m.p.ID, LandedState)
	return nil
}

// row refuses a row that does not exist, is down, is held or does not serve
// p's tier, then fences it: its gen, up, not held.
func (m *mover) row(name string) *Refusal {
	r, ok := m.rows[name]
	switch {
	case !ok:
		return no("row %s does not exist", name)
	case r.State != Up:
		return no("row %s is %s; a consumer is dealt only to a row that is up", name, r.State)
	case r.Held:
		return no("row %s is held; nothing is dealt to a held row", name)
	case !slices.Contains(r.Tiers, m.p.Tier):
		return no("row %s serves %s; consumer needs %s", name, tierList(r.Tiers), m.p.Tier)
	}
	m.expect(store.RowGen{Row: name, Gen: r.Gen}, store.RowIs{Row: name, State: Up}, store.RowHeld{Row: name, Held: false})
	return nil
}

func tierList(ts []string) string {
	if len(ts) == 0 {
		return "none"
	}
	return strings.Join(ts, ",")
}

// noLive refuses a producer with a live consumer and fences every consumer
// it read, and, in review, the next read's id (AskRead adds a consumer
// without writing the producer).
func (m *mover) noLive(refuse func(c Card) *Refusal) *Refusal {
	for _, c := range m.cons {
		if live(c) {
			return refuse(c)
		}
	}
	m.fenceConsumers()
	return nil
}

func (m *mover) fenceConsumers() {
	for _, c := range m.cons {
		m.expect(store.CardGen{Card: c.ID, Gen: c.Gen})
	}
	if m.p.State == Review {
		m.expect(store.CardAbsent{Card: m.readID()})
	}
}

func (m *mover) liveRefusal(c Card) *Refusal {
	return no("producer %s has live consumer %s on row %s; withdraw it first", m.p.ID, c.ID, c.Holder)
}

// workID and readID name the consumers a producer cuts: its work consumer
// of attempt n, and its next read of the current attempt.
func workID(p string, n int) string { return fmt.Sprintf("%s.w%d", p, n) }

func (m *mover) readID() string {
	n := 1
	for _, c := range m.cons {
		if c.Kind == "read" && c.Attempt == m.p.Attempt {
			n++
		}
	}
	return fmt.Sprintf("%s.r%d.%d", m.p.ID, m.p.Attempt, n)
}

// cut adds a consumer of p, held by row, in state ready: put card, then its
// own columns, then the state (layer 1 checks the row serves its tier).
func (m *mover) cut(id, kind, row string, attempt int, cols ...store.Write) *Refusal {
	if len(id) > store.MaxName {
		return no("producer %s's id is too long to name its consumer %s", m.p.ID, id)
	}
	if absent := (store.CardAbsent{Card: id}); !slices.Contains(m.plan.Expect, store.Expect(absent)) {
		m.expect(absent) // fenced already when noLive read the review's next read
	}
	m.write(store.PutCard{ID: id, Kind: kind, Of: m.p.ID, Stream: m.p.Stream, Holder: row, Tier: m.p.Tier, Attempt: attempt})
	m.write(cols...)
	m.state(id, Ready)
	return nil
}

func (m *mover) start(e Start) *Refusal {
	if m.p.Kind != "producer" {
		return no("sentinel %s never has a consumer", m.p.ID)
	}
	if r := m.from(e, Ready); r != nil {
		return r
	}
	if r := firstOf(m.stream(true), m.row(e.Row), sha("base", e.Base), m.noLive(m.liveRefusal)); r != nil {
		return r
	}
	if e.Branch == "" {
		return no("Start of producer %s needs a branch", m.p.ID)
	}
	n := m.p.Attempt + 1
	m.state(m.p.ID, Working)
	m.write(store.SetCardAttempt{Card: m.p.ID, Attempt: n})
	if m.p.Author != "" {
		m.write(store.SetCardAuthor{Card: m.p.ID, Author: ""})
	}
	id := workID(m.p.ID, n)
	cols := []store.Write{store.SetCardBranch{Card: id, Branch: e.Branch}, store.SetCardBaseSHA{Card: id, SHA: e.Base}}
	if m.p.Head != "" {
		cols = append(cols, store.SetCardPriorHead{Card: id, Head: m.p.Head})
	}
	if m.p.Reason != "" {
		cols = append(cols, store.SetCardReason{Card: id, Reason: m.p.Reason})
	}
	return m.cut(id, "work", e.Row, n, cols...)
}

// firstOf is the first refusal of checks run in order; a check that passed
// has already added its fences.
func firstOf(rs ...*Refusal) *Refusal {
	for _, r := range rs {
		if r != nil {
			return r
		}
	}
	return nil
}

// worker is p's work consumer of the current attempt, named id: the only
// consumer a producer event accepts. It is fenced by its gen.
func (m *mover) worker(id string) (Card, *Refusal) {
	i := slices.IndexFunc(m.cons, func(c Card) bool { return c.ID == id })
	if i < 0 || m.cons[i].Kind != "work" || m.cons[i].Of != m.p.ID || m.cons[i].Attempt != m.p.Attempt {
		return Card{}, no("card %s is not producer %s's work consumer of attempt %d", id, m.p.ID, m.p.Attempt)
	}
	m.expect(store.CardGen{Card: id, Gen: m.cons[i].Gen})
	return m.cons[i], nil
}

// reported is worker done with result.
func (m *mover) reported(e Event, id, result string) (Card, *Refusal) {
	c, r := m.worker(id)
	switch {
	case r != nil:
		return c, r
	case c.State != Done:
		return c, no("consumer %s is %s, not done", c.ID, c.State)
	case c.Result != result:
		return c, no("consumer %s reported %s, %s needs %s", c.ID, c.Result, name(e), result)
	}
	return c, nil
}

func (m *mover) finished(e Finished) *Refusal {
	if r := m.from(e, Working); r != nil {
		return r
	}
	c, r := m.reported(e, e.Consumer, "ok")
	if r != nil {
		return r
	}
	m.state(m.p.ID, Review)
	m.head(m.p.ID, m.p.Head, c.Head)
	if c.Holder != m.p.Author {
		m.write(store.SetCardAuthor{Card: m.p.ID, Author: c.Holder})
	}
	return nil
}

// returned is Broken (result fail, a bounce) and Faulted (result fault, no
// bounce): back to ready with the consumer's reason and pushed head.
func (m *mover) returned(e Event, id, result string, bounce bool) *Refusal {
	if r := m.from(e, Working); r != nil {
		return r
	}
	c, r := m.reported(e, id, result)
	if r != nil {
		return r
	}
	m.state(m.p.ID, Ready)
	m.reason(m.p.ID, m.p.Reason, c.Reason)
	if bounce {
		m.write(store.SetCardBounces{Card: m.p.ID, Bounces: m.p.Bounces + 1})
	}
	m.head(m.p.ID, m.p.Head, c.Head)
	return nil
}

func (m *mover) askRead(e AskRead) *Refusal {
	if r := m.from(e, Review); r != nil {
		return r
	}
	if e.Row == m.p.Author {
		return no("%s wrote producer %s's head and may not read it", e.Row, m.p.ID)
	}
	if r := firstOf(m.stream(true), m.row(e.Row), m.noLive(m.liveWork)); r != nil {
		return r
	}
	id := m.readID()
	cols := []store.Write{store.SetCardHead{Card: id, Head: m.p.Head}}
	if m.p.Author != "" {
		cols = append(cols, store.SetCardAuthor{Card: id, Author: m.p.Author})
	}
	return m.cut(id, "read", e.Row, m.p.Attempt, cols...)
}

// liveWork refuses only a live work consumer: reads of one head may be live
// together.
func (m *mover) liveWork(c Card) *Refusal {
	if c.Kind == "work" {
		return m.liveRefusal(c)
	}
	return nil
}

// reads checks what ReadYes and ReadFix need: no live consumer of p, every
// read of the current head done or fail; it returns how many said ok.
func (m *mover) reads() (int, *Refusal) {
	if r := m.noLive(m.liveRefusal); r != nil {
		return 0, r
	}
	ok := 0
	for _, c := range m.cons {
		if c.Kind == "read" && c.Attempt == m.p.Attempt && c.Head == m.p.Head && c.State == Done && c.Result == "ok" {
			ok++
		}
	}
	return ok, nil
}

func (m *mover) readYes(e ReadYes) *Refusal {
	if r := m.from(e, Review); r != nil {
		return r
	}
	ok, r := m.reads()
	if r != nil {
		return r
	}
	if ok == 0 {
		return no("producer %s has no read of head %s that said ok; ReadYes needs one", m.p.ID, m.p.Head)
	}
	m.state(m.p.ID, Merging)
	return nil
}

func (m *mover) readFix(e ReadFix) *Refusal {
	if r := m.from(e, Review); r != nil {
		return r
	}
	if _, r := m.reads(); r != nil {
		return r
	}
	m.state(m.p.ID, Ready)
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	m.write(store.SetCardBounces{Card: m.p.ID, Bounces: m.p.Bounces + 1})
	return nil
}

func (m *mover) atHead(e Event, h string) *Refusal {
	if r := m.from(e, Merging); r != nil {
		return r
	}
	if h != m.p.Head {
		return no("producer %s is at head %s, %s names %s", m.p.ID, m.p.Head, name(e), h)
	}
	return nil
}

func (m *mover) landed(e Landed) *Refusal {
	if r := m.atHead(e, e.Head); r != nil {
		return r
	}
	m.state(m.p.ID, LandedState)
	return nil
}

func (m *mover) red(e Red) *Refusal {
	if r := m.atHead(e, e.Head); r != nil {
		return r
	}
	m.state(m.p.ID, Review)
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

func (m *mover) released(e Released) *Refusal {
	if r := m.from(e, Working); r != nil {
		return r
	}
	c, r := m.worker(workID(m.p.ID, m.p.Attempt))
	if r != nil {
		return r
	}
	if c.State != Fail {
		return no("consumer %s is %s; Released needs it withdrawn (fail)", c.ID, c.State)
	}
	m.state(m.p.ID, Ready)
	if e.Reason == "" {
		e.Reason = c.Reason
	}
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	m.head(m.p.ID, m.p.Head, c.Head)
	return nil
}

// fail is Cancel, Replace and NeedFailed: to fail with the reason, never
// under a live consumer.
func (m *mover) fail(reason string) *Refusal {
	if r := m.noLive(m.liveRefusal); r != nil {
		return r
	}
	m.state(m.p.ID, Fail)
	m.reason(m.p.ID, m.p.Reason, reason)
	return nil
}

func (m *mover) replace(e Replace) *Refusal {
	if e.By == "" {
		return no("Replace of producer %s needs the card it is replaced by", m.p.ID)
	}
	return m.fail("replaced by " + e.By + ": " + e.Reason)
}

// failedNeed refuses a name that is not one of p's needs, or a need that
// has not failed, and fences its state.
func (m *mover) failedNeed(n string) *Refusal {
	if !slices.Contains(m.p.Needs, n) {
		return no("%s is not a need of producer %s", n, m.p.ID)
	}
	need, ok := m.needs[n]
	switch {
	case !ok:
		return no("need %s of producer %s does not exist", n, m.p.ID)
	case need.State != Fail:
		return no("need %s of producer %s is %s, not fail", n, m.p.ID, need.State)
	}
	m.expect(store.CardState{Card: n, State: Fail})
	return nil
}

func (m *mover) needFailed(e NeedFailed) *Refusal {
	if r := m.failedNeed(e.Need); r != nil {
		return r
	}
	return m.fail("need " + e.Need + " failed")
}

func (m *mover) waive(e Waive) *Refusal {
	if r := firstOf(m.from(e, Waiting), m.failedNeed(e.Need)); r != nil {
		return r
	}
	m.write(store.SetCardNeeds{Card: m.p.ID, Needs: slices.DeleteFunc(slices.Clone(m.p.Needs), func(n string) bool { return n == e.Need })})
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

func (m *mover) relink(e Relink) *Refusal {
	if r := firstOf(m.from(e, Waiting), m.failedNeed(e.Need)); r != nil {
		return r
	}
	to, ok := m.needs[e.To]
	switch {
	case !ok:
		return no("card %s does not exist", e.To)
	case consumer(to.Kind) || final(to.Kind, to.State):
		return no("card %s is a %s %s; a need is relinked only to a producer that is not final", e.To, to.State, to.Kind)
	case slices.Contains(m.p.Needs, e.To):
		return no("producer %s already needs %s", m.p.ID, e.To)
	}
	if loop := m.cycle(e.To); loop != nil {
		return no("%s", strings.Join(append([]string{m.p.ID}, loop...), " needs "))
	}
	needs := slices.Clone(m.p.Needs)
	needs[slices.Index(needs, e.Need)] = e.To
	m.expect(store.CardGen{Card: to.ID, Gen: to.Gen})
	m.write(store.SetCardNeeds{Card: m.p.ID, Needs: needs})
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

// cycle is the path of needs from id back to p ("c2", "c1" for c1 needs c2
// needs c1), or nil when p is not reachable from id.
func (m *mover) cycle(id string) []string {
	seen := map[string]bool{}
	var walk func(id string) []string
	walk = func(id string) []string {
		if id == m.p.ID {
			return []string{id}
		}
		if seen[id] {
			return nil
		}
		seen[id] = true
		for _, n := range m.needs[id].Needs {
			if path := walk(n); path != nil {
				return append([]string{id}, path...)
			}
		}
		return nil
	}
	return walk(id)
}

func (m *mover) edit(e Edit) *Refusal {
	if r := m.noLive(func(c Card) *Refusal {
		return no("producer %s is held by %s in consumer %s; message %s instead", m.p.ID, c.Holder, c.ID, c.Holder)
	}); r != nil {
		return r
	}
	if e.Prose != "" && e.Prose != m.p.Prose {
		m.write(store.SetCardProse{Card: m.p.ID, Prose: e.Prose})
	}
	if e.Paths != nil && !slices.Equal(e.Paths, m.p.Paths) {
		m.write(store.SetCardPaths{Card: m.p.ID, Paths: e.Paths})
	}
	if e.DoneWhen != nil && !slices.Equal(e.DoneWhen, m.p.DoneWhen) {
		m.write(store.SetCardDoneWhen{Card: m.p.ID, DoneWhen: e.DoneWhen})
	}
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	if len(m.plan.Writes) == 0 {
		return no("Edit of producer %s changes nothing", m.p.ID)
	}
	return nil
}

func (m *mover) rescore(e Rescore) *Refusal {
	switch {
	case e.Score < 0:
		return no("score %d is below 0", e.Score)
	case e.Score == m.p.Score:
		return no("producer %s already has score %d", m.p.ID, e.Score)
	}
	m.write(store.SetCardScore{Card: m.p.ID, Score: e.Score})
	return nil
}

func (m *mover) setPriority(e SetPriority) *Refusal {
	level := e.Level
	if level == "" {
		level = "normal"
	}
	switch {
	case !slices.Contains(Levels, level):
		return no("priority %s is not one of %s", level, orList(Levels))
	case level == m.p.Priority:
		return no("producer %s is already at priority %s", m.p.ID, level)
	}
	m.write(store.SetCardPriority{Card: m.p.ID, Priority: level})
	return nil
}

func (m *mover) block(e Block) *Refusal {
	if r := firstOf(m.from(e, Ready, Review, Working), m.noLive(m.liveRefusal)); r != nil {
		return r
	}
	m.state(m.p.ID, Blocked)
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

func (m *mover) unblock(e Unblock) *Refusal {
	if r := m.from(e, Blocked); r != nil {
		return r
	}
	m.state(m.p.ID, Waiting)
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

// MoveConsumer is a consumer's move on event e: c the consumer, p its
// producer, r its holder row.
func MoveConsumer(c Card, p Card, r Row, e Event) (Plan, *Refusal) {
	switch {
	case !consumer(c.Kind):
		return Plan{}, no("card %s is a %s; MoveConsumer is for consumers", c.ID, c.Kind)
	case c.Of != p.ID:
		return Plan{}, no("consumer %s belongs to %s, not %s", c.ID, c.Of, p.ID)
	case final(c.Kind, c.State):
		return Plan{}, no("consumer %s is %s; nothing moves a %s card", c.ID, c.State, c.State)
	}
	m := &mover{p: c}
	m.expect(store.CardGen{Card: c.ID, Gen: c.Gen})
	var refusal *Refusal
	switch v := e.(type) {
	case Take:
		refusal = m.take(v, r)
	case Report:
		refusal = m.report(v, p)
	case Withdraw:
		refusal = m.withdraw(v)
	default:
		refusal = no("%s is not a consumer event; MoveConsumer takes Take, Report and Withdraw", name(e))
	}
	if refusal != nil {
		return Plan{}, refusal
	}
	return m.plan, nil
}

// holder refuses a row that is not the consumer's holder.
func (m *mover) holder(row string) *Refusal {
	if row != m.p.Holder {
		return no("consumer %s is held by %s, not %s", m.p.ID, m.p.Holder, row)
	}
	return nil
}

// take moves a dealt consumer into working; layer 1's state write refuses it
// when the row already holds as many working consumers as its width.
func (m *mover) take(e Take, r Row) *Refusal {
	if rr := firstOf(m.from(e, Ready), m.holder(e.Row)); rr != nil {
		return rr
	}
	if r.Name != e.Row {
		return no("row %s is not the row the take names (%s)", r.Name, e.Row)
	}
	m.expect(store.RowGen{Row: r.Name, Gen: r.Gen})
	m.state(m.p.ID, Working)
	return nil
}

func (m *mover) report(e Report, p Card) *Refusal {
	c := m.p
	if r := firstOf(m.from(e, Working), m.holder(e.Row)); r != nil {
		return r
	}
	if r := m.reportHead(e, p); r != nil {
		return r
	}
	m.state(c.ID, Done)
	m.write(store.SetCardResult{Card: c.ID, Result: e.Result})
	m.head(c.ID, c.Head, e.Head)
	m.reason(c.ID, c.Reason, e.Reason)
	if e.Cost != c.Cost || e.Priced != c.Priced {
		m.write(store.SetCardCost{Card: c.ID, Cost: e.Cost, Priced: e.Priced})
	}
	return nil
}

// reportHead checks a report's result and head: one of the four results; an
// ok needs a full sha, a work consumer's other than its base, a read's the
// producer's head (fenced by the producer's gen).
func (m *mover) reportHead(e Report, p Card) *Refusal {
	c := m.p
	if !slices.Contains([]string{"ok", "fix", "fail", "fault"}, e.Result) {
		return no("result %q is not ok, fix, fail or fault", e.Result)
	}
	if e.Result == "ok" || e.Head != "" {
		if r := sha("head", e.Head); r != nil {
			return r
		}
	}
	switch {
	case c.Kind == "work" && e.Result == "ok" && e.Head == c.BaseSHA:
		return no("head %s is the base; nothing was done", e.Head)
	case c.Kind == "read" && e.Head != "" && e.Head != p.Head:
		return no("producer %s is at head %s, the read was of %s", p.ID, p.Head, e.Head)
	case c.Kind == "read":
		m.expect(store.CardGen{Card: p.ID, Gen: p.Gen})
	}
	return nil
}

func (m *mover) withdraw(e Withdraw) *Refusal {
	if r := m.from(e, Ready, Working); r != nil {
		return r
	}
	m.state(m.p.ID, Fail)
	m.reason(m.p.ID, m.p.Reason, e.Reason)
	return nil
}

// MoveRow is a row's move on event e.
func MoveRow(r Row, e Event) (Plan, *Refusal) {
	var p Plan
	p.Expect = []store.Expect{store.RowGen{Row: r.Name, Gen: r.Gen}}
	var w store.Write
	var refusal *Refusal
	switch v := e.(type) {
	case Seen:
		w, refusal = store.SetRowState{Row: r.Name, State: Up}, rowFrom(r, e, Down)
	case Gone:
		if refusal = rowFrom(r, e, Up); refusal == nil {
			p.Writes = append(p.Writes, store.SetRowState{Row: r.Name, State: Down})
			w = store.SetRowFlaps{Row: r.Name, Flaps: r.Flaps + 1}
		}
	case Width:
		w, refusal = store.SetRowWidth{Row: r.Name, Width: v.N}, rowWidth(r, v.N)
	case Hold:
		w, refusal = store.SetRowHeld{Row: r.Name, Held: true}, rowHeld(r, true)
	case Unhold:
		w, refusal = store.SetRowHeld{Row: r.Name, Held: false}, rowHeld(r, false)
	default:
		refusal = no("%s is not a row event; MoveRow takes Seen, Gone, Width, Hold and Unhold", name(e))
	}
	if refusal != nil {
		return Plan{}, refusal
	}
	p.Writes = append(p.Writes, w)
	return p, nil
}

func rowFrom(r Row, e Event, state string) *Refusal {
	if r.State != state {
		return no("row %s is %s, %s needs %s", r.Name, r.State, name(e), state)
	}
	return nil
}

func rowWidth(r Row, n int) *Refusal {
	switch {
	case n < 0:
		return no("width %d is below 0", n)
	case n == r.Width:
		return no("row %s already has width %d", r.Name, n)
	}
	return nil
}

func rowHeld(r Row, held bool) *Refusal {
	if r.Held == held {
		word := "held"
		if !held {
			word = "not held"
		}
		return no("row %s is already %s", r.Name, word)
	}
	return nil
}

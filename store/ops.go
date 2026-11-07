package store

import (
	"cmp"
	"math"
	"strings"
	"time"
)

// Expect is a condition: one of CardGen, CardState, CardHolder, CardRed,
// NeedsLanded, RowIs, RowGen, RowHeld, StreamHeld, CardAbsent, JudgementOpen,
// Running, AtSeq. There
// is no width condition and no tier condition: the writes are the checks (the
// state write into working for width, put card for the holder's tiers). The
// set is closed by the unexported method; any other value is ErrInvalid.
type Expect interface{ isExpect() }

// JudgementOpen holds when judgement ID is open: it fences a judgement plan
// (a judgement has no gen).
type JudgementOpen struct{ ID string }

// CardGen holds when card Card is at gen Gen.
type CardGen struct {
	Card string
	Gen  int
}

// CardState holds when card Card is in state State.
type CardState struct{ Card, State string }

// CardHolder holds when card Card has holder Holder ("" for none).
type CardHolder struct{ Card, Holder string }

// CardRed holds when card Card's red flag is Red: a plan that waives a red
// read fences on the producer being red.
type CardRed struct {
	Card string
	Red  bool
}

// NeedsLanded holds when every need of card Card, read from the card itself
// inside the transaction, is landed.
type NeedsLanded struct{ Card string }

// RowIs holds when row Row is in state State.
type RowIs struct{ Row, State string }

// RowGen holds when row Row is at gen Gen: it fences a row plan as CardGen
// fences a card plan.
type RowGen struct {
	Row string
	Gen int
}

// RowHeld holds when row Row's held flag is Held.
type RowHeld struct {
	Row  string
	Held bool
}

// StreamHeld holds when stream Stream's held flag is Held.
type StreamHeld struct {
	Stream string
	Held   bool
}

// CardAbsent holds when card Card does not exist in the current epoch.
type CardAbsent struct{ Card string }

// Running holds when the sprint is not stopped: run's tick ops carry it,
// so a stop that commits between a tick's fold and its apply refuses the
// tick's ops (Run.tla NoAcceptedTickWhileStopped; Store.tla s23).
type Running struct{}

// AtSeq holds when the last event's seq is Seq: nothing at all has entered
// the store since a snapshot read at Seq.
type AtSeq struct{ Seq int64 }

func (CardGen) isExpect()       {}
func (CardState) isExpect()     {}
func (CardHolder) isExpect()    {}
func (CardRed) isExpect()       {}
func (NeedsLanded) isExpect()   {}
func (RowIs) isExpect()         {}
func (RowGen) isExpect()        {}
func (RowHeld) isExpect()       {}
func (StreamHeld) isExpect()    {}
func (CardAbsent) isExpect()    {}
func (AtSeq) isExpect()         {}
func (Running) isExpect()       {}
func (JudgementOpen) isExpect() {}

// Write is one write. The set is closed by the unexported method; any other
// value is ErrInvalid. An op that writes a card raises its gen by one, once.
type Write interface{ isWrite() }

// PutStream inserts a stream or overwrites its repo, base, pos, mode and
// forge; held is kept on overwrite and false on insert. Pos is the order
// streams land in. Mode is one of StreamModes, "" for auto; Forge is the
// forge it lands through, "" for none.
type PutStream struct {
	Name, Repo, Base string
	Pos              int
	Mode, Forge      string
}

// SetStreamHeld sets a stream's held flag.
type SetStreamHeld struct {
	Stream string
	Held   bool
}

// SetStreamMode sets how a stream's producers land (one of StreamModes);
// refused when it is the stream's mode already.
type SetStreamMode struct{ Stream, Mode string }

// SetStreamForge sets the forge a stream lands through ("" for none);
// refused when it is the stream's forge already.
type SetStreamForge struct{ Stream, Forge string }

// PutCard adds a card of a kind (producer, sentinel, work or read): refused
// when it exists. It starts waiting, gen 0, attempt 0. Its stream must exist;
// every need, and every card it comes after, must name another card of the
// same epoch, once. Every card names its Tier (flash, pro, heavy, frontier).
// A producer or sentinel has no Of and no Holder; a sentinel has no Brief. A
// consumer (work or read) names its producer in Of (a producer of the same
// epoch), its producer's tier and stream, and its Holder (a row of the same
// epoch that serves its tier), has
// no needs and no after, and carries the Attempt it belongs to (a producer's
// starts at 0); its paths, done-when and prose are copies set here. Kind, of,
// stream, holder and tier never change after. A read consumer carries the
// Review it was cut at: its producer's current review, fixed; every other
// kind's is 0. A work consumer carries the Finding it was cut with: its
// producer's current finding, fixed; every other kind's is "". Prior head, bounces, cost and
// result are never set at add.
type PutCard struct {
	ID, Kind, Of, Stream string
	Score                int
	Attempt              int      // a consumer's attempt, set here and never after
	Review               int      // a read consumer's: its producer's review now; 0 on every other kind
	Finding              string   // a work consumer's: its producer's finding now; "" on every other kind
	Needs                []string // hard: cannot start until these landed
	After                []string // soft: may start any time; orders its landing only behind targets that are themselves queued to land, never behind one that is not
	Brief                string
	Holder               string // a consumer's row, fixed at add; none on a producer or sentinel
	Priority             string // a producer's or sentinel's level; "" is normal; a consumer takes only the default ("" or normal)
	Tier                 string
	Paths                []string
	DoneWhen             []string
	Prose                string
	Model                string // a consumer's: the model its deal named; "" on a producer or sentinel
}

// SetCardState sets a card's state; refused when the card is already in it,
// so nothing moves without a change. A consumer moved into ready or working
// is refused when its holder does not serve its tier, and one moved into
// working counts against its holder's width: refused when the row already
// holds as many working cards as its width, counted at the moment of the
// write (earlier writes of the op included). No other state counts. There is no holder
// write: a consumer's holder is fixed at add (a working card could otherwise
// drop its holder and keep the slot).
type SetCardState struct{ Card, State string }

// SetCardPaths replaces a producer's or sentinel's paths; refused on a
// consumer, which carries the copy it was added with.
type SetCardPaths struct {
	Card  string
	Paths []string
}

// SetCardDoneWhen replaces a producer's or sentinel's done-when list; refused
// on a consumer, as SetCardPaths.
type SetCardDoneWhen struct {
	Card     string
	DoneWhen []string
}

// SetCardProse sets a producer's or sentinel's prose; refused on a consumer,
// as SetCardPaths.
type SetCardProse struct{ Card, Prose string }

// SetCardResult sets a consumer's result: "", ok, fix, fail or fault; refused on
// a producer or sentinel.
type SetCardResult struct{ Card, Result string }

// SetCardAttempt sets a producer's or sentinel's attempt count; refused on a
// consumer, whose attempt is set at add.
type SetCardAttempt struct {
	Card    string
	Attempt int
}

// SetCardBranch sets a card's branch.
type SetCardBranch struct{ Card, Branch string }

// SetCardHead sets a card's head commit.
type SetCardHead struct{ Card, Head string }

// SetCardPR sets a card's pull request number.
type SetCardPR struct {
	Card string
	PR   int
}

// SetCardReason sets a card's reason.
type SetCardReason struct{ Card, Reason string }

// SetCardScore sets a card's score, the work order.
type SetCardScore struct {
	Card  string
	Score int
}

// SetCardAuthor sets a card's author: the row whose work its head is.
type SetCardAuthor struct{ Card, Author string }

// SetCardBaseSHA sets the base commit the card's attempt started from.
type SetCardBaseSHA struct{ Card, SHA string }

// SetCardPriorHead sets the head a work consumer's attempt continues from
// ("" on a first attempt); refused on any other kind.
type SetCardPriorHead struct{ Card, Head string }

// SetCardBounces sets a producer's bounce count: the returns that count
// toward blocked (a fault never raises it; layer 2 says when); refused on any
// other kind.
type SetCardBounces struct {
	Card    string
	Bounces int
}

// SetCardReview raises a producer's review by one: it entered review again
// (layer 2 says when); refused on any other kind. A read cut at an earlier
// review no longer counts for ReadYes.
type SetCardReview struct{ Card string }

// SetCardFinding sets a producer's finding: what the last read asked for
// (layer 2's ReadFix); a work consumer cut after carries a copy. Refused on
// any other kind.
type SetCardFinding struct{ Card, Finding string }

// SetCardRed records on a producer whether its last read's verdict was red,
// so a later waiver can require it. Refused on any other kind,
// naming the fix: a consumer's verdict is recorded on its producer, and a
// sentinel is never read.
type SetCardRed struct {
	Card string
	Red  bool
}

// SetCardCost sets what a consumer's run cost, in micro-dollars, and whether
// its route had a price at all: one field, so an unpriced run is never $0.
// Refused on a producer or sentinel.
type SetCardCost struct {
	Card   string
	Cost   int64
	Priced bool
}

// SetCardUsage sets a consumer's usage record: the words of package usage
// (tokens by class, model, billing, the harness's charged dollars, why it is
// unpriced), as the engine priced them. The store keeps the
// words; it reads none of them. Refused on a producer or sentinel.
type SetCardUsage struct{ Card, Usage string }

// SetCardPriority sets a producer's or sentinel's level (low, normal, high,
// critical, blocker); refused on a consumer for any value, normal included.
type SetCardPriority struct{ Card, Priority string }

// SetCardNeeds replaces a card's needs, under the rules of PutCard; refused
// on a consumer for any value, nil or empty included (as SetCardAfter).
type SetCardNeeds struct {
	Card  string
	Needs []string
}

// SetCardAfter replaces the cards a card lands after, under the rules of
// PutCard.
type SetCardAfter struct {
	Card  string
	After []string
}

// PutRow inserts a row or sets its hook, the one write that does: `nova-sprint
// hook --as <name> --role <r>` puts the bus session the hook recorded and
// the role (worker or coordinator), and unhook puts "" for both. Session
// and Role are both set or both "" (ErrInvalid otherwise, and for a role
// not in RowRoles). Hooking a row another session holds is refused: unhook
// it first. State, flaps and held are kept on overwrite, and down, 0 and
// false on insert. Its kind, width, tiers, model and harness are
// configuration, the roster's, never the store's.
type PutRow struct{ Name, Session, Role string }

// PutJudgement opens a judgement: refused when it exists. Its state is
// open, opened at the op's At; Deadline zero is none. It belongs to the
// epoch: a clear does not carry it.
type PutJudgement struct {
	ID, Kind, About, Question, Command, Group string
	Deadline                                  time.Time
}

// AnswerJudgement answers an open judgement: state answered, the answer, the
// actor ("" is the op's actor), closed at the op's At. Refused unless open,
// and when the op's At is before the judgement opened.
type AnswerJudgement struct{ ID, Answer, Actor string }

// ExpireJudgement expires an open judgement: state expired, the reason kept
// as its answer, the op's actor, closed at the op's At. Refused as
// AnswerJudgement is.
type ExpireJudgement struct{ ID, Reason string }

// SetCursor sets a subscriber's place in the event log (the last seq it has
// read), inserting it when absent (an absent cursor reads as 0); refused
// at its stored seq (0 for an absent one: "already at 0"), below it, and
// above the last event's seq before the op. Seq is
// global, so cursors are too: a clear carries them, and a subscriber resumes
// where it was.
type SetCursor struct {
	Name string
	Seq  int64
}

// SetRowState sets a row's state.
type SetRowState struct{ Row, State string }

// SetRowHeld sets a row's held flag and the reason it was held or released:
// the coordinator's hold, no deals to the row while true, whatever its state.
// Refused when the row is already held (or not held), whatever the reason.
type SetRowHeld struct {
	Row    string
	Held   bool
	Reason string
}

// SetRowFlaps sets a row's flap count.
type SetRowFlaps struct {
	Row   string
	Flaps int
}

// SetRowLoad sets a row's heartbeat: its 1-minute load average in hundredths,
// the logical cores it was measured over (at least 1), and the nova-reserve
// slots in use and in all (in use at most in all). It is telemetry: a write
// that changes nothing is refused like any set write, and it moves no gen,
// so a beat never fences out a deal made from an earlier snapshot.
type SetRowLoad struct {
	Row                                      string
	Load1Centi, Cores, SlotsUsed, SlotsTotal int
	Tiers                                    string // the tiers the worker serves, a comma list of Tiers ("" none advertised)
}

// PutPolicy adds the next policy version, inactive; actor and at come from the op.
type PutPolicy struct{ Kind, Name, Body string }

// SetStopped stops the sprint or starts it again, with the reason. Refused
// when the sprint is already in that state, like every set write.
type SetStopped struct {
	Stopped bool
	Reason  string
}

// SetIdentity sets the git identity the sprint commits as (Identity), both
// parts non-empty. Refused when it is already that identity, like every set
// write.
type SetIdentity struct{ Name, Email string }

// SetActivePolicy makes one existing version the active one and deactivates
// the one active before it.
type SetActivePolicy struct{ Version int }

func (PutStream) isWrite()        {}
func (SetStreamHeld) isWrite()    {}
func (SetStreamMode) isWrite()    {}
func (SetStreamForge) isWrite()   {}
func (PutCard) isWrite()          {}
func (SetCardState) isWrite()     {}
func (SetCardResult) isWrite()    {}
func (SetCardAttempt) isWrite()   {}
func (SetCardBranch) isWrite()    {}
func (SetCardHead) isWrite()      {}
func (SetCardPR) isWrite()        {}
func (SetCardReason) isWrite()    {}
func (SetCardScore) isWrite()     {}
func (SetCardAuthor) isWrite()    {}
func (SetCardBaseSHA) isWrite()   {}
func (SetCardNeeds) isWrite()     {}
func (SetCardAfter) isWrite()     {}
func (SetCardPriorHead) isWrite() {}
func (SetCardBounces) isWrite()   {}
func (SetCardReview) isWrite()    {}
func (SetCardFinding) isWrite()   {}
func (SetCardRed) isWrite()       {}
func (SetCardCost) isWrite()      {}
func (SetCardUsage) isWrite()     {}
func (SetCardPriority) isWrite()  {}
func (PutRow) isWrite()           {}
func (SetRowHeld) isWrite()       {}
func (SetRowState) isWrite()      {}
func (SetRowFlaps) isWrite()      {}
func (SetRowLoad) isWrite()       {}
func (PutPolicy) isWrite()        {}
func (SetActivePolicy) isWrite()  {}
func (SetStopped) isWrite()       {}
func (SetIdentity) isWrite()      {}
func (SetCardPaths) isWrite()     {}
func (SetCardDoneWhen) isWrite()  {}
func (SetCardProse) isWrite()     {}
func (PutJudgement) isWrite()     {}
func (AnswerJudgement) isWrite()  {}
func (ExpireJudgement) isWrite()  {}
func (SetCursor) isWrite()        {}

// field names one field one write sets; two writes to one field in an op are
// invalid.
type field struct{ table, key, name string }

func (f field) String() string {
	return strings.Join(strings.Fields(f.table+" "+f.key), " ") + " field " + f.name
}

// spec is one write's whole entry in the one table: the fields it sets (two
// writes to one field in an op are invalid), the check of its body (names,
// text, ranges and closed sets, before anything is read), and either do,
// the write itself, or for a set-card or set-row write the change and the
// rule it runs first (who may make it, what it must not break). A set write
// that would change nothing is refused: is says how ("is already up"), or
// the record "already has the given <column>". The zero spec is a value
// outside the closed set.
type spec struct {
	fields []field
	check  func(c *checker)
	do     func(w *work) (string, error)
	card   func(*Card)
	cardIf func(w *work, k Card) (string, error)
	row    func(*Row)
	rowIf  func(w *work, r Row) (string, error)
	is     string
}

// specOf is the one table of the writes, keyed by type; the zero spec for a
// value outside the closed set (a pointer or an embedding type included). A
// new write is one entry here.
func specOf(w Write) spec {
	switch v := w.(type) {
	case PutStream:
		return put("stream", v.Name, func(c *checker) {
			c.text(v.Repo, v.Base, v.Forge).num("pos", v.Pos).oneOf("stream mode", cmp.Or(v.Mode, StreamAuto), StreamModes)
		}, "repo", "base", "pos", "mode", "forge").then(func(w *work) (string, error) { return w.writePutStream(v) })
	case SetStreamHeld:
		return put("stream", v.Stream, nil, "held").then(func(w *work) (string, error) { return w.setStreamHeld(v) })
	case SetStreamMode:
		return put("stream", v.Stream, func(c *checker) { c.oneOf("stream mode", v.Mode, StreamModes) }, "mode").then(func(w *work) (string, error) {
			return w.setStream(v.Stream, "mode", v.Mode, func(s *Stream) *string { return &s.Mode })
		})
	case SetStreamForge:
		return put("stream", v.Stream, func(c *checker) { c.text(v.Forge) }, "forge").then(func(w *work) (string, error) {
			return w.setStream(v.Stream, "forge", v.Forge, func(s *Stream) *string { return &s.Forge })
		})
	case PutCard:
		return put("card", v.ID, func(c *checker) {
			c.name(v.Of, v.Holder).key("stream", v.Stream).keys("card", v.Needs...).keys("card", v.After...).text(v.Brief, v.Prose, v.Finding, v.Model).text(v.Paths...).text(v.DoneWhen...).num("attempt", v.Attempt).num("review", v.Review).num("score", v.Score).
				oneOf("card kind", v.Kind, CardKinds).oneOf("card tier", v.Tier, Tiers).oneOf("card priority", cmp.Or(v.Priority, "normal"), Priorities)
		}, "kind", "of", "stream", "attempt", "review", "finding", "score", "needs", "after", "brief", "holder", "priority", "tier", "paths", "done_when", "prose", "model").then(func(w *work) (string, error) { return w.addCard(v) })
	case PutRow:
		return put("row", v.Name, func(c *checker) { c.text(v.Session).hook(v.Session, v.Role) }, "session", "role").then(func(w *work) (string, error) { return w.writePutRow(v) })
	case PutJudgement:
		return put("judgement", v.ID, func(c *checker) {
			c.name(v.Kind, v.About, v.Group).text(v.Question, v.Command).time("deadline", v.Deadline, true)
		}, "state").then(func(w *work) (string, error) { return w.openJudgement(v) })
	case AnswerJudgement:
		return put("judgement", v.ID, func(c *checker) { c.name(v.Actor).text(v.Answer) }, "state").then(func(w *work) (string, error) {
			return w.closeJudgement(v.ID, "answered", v.Answer, cmp.Or(v.Actor, w.op.Actor))
		})
	case ExpireJudgement:
		return put("judgement", v.ID, func(c *checker) { c.text(v.Reason) }, "state").then(func(w *work) (string, error) {
			return w.closeJudgement(v.ID, "expired", v.Reason, w.op.Actor)
		})
	case SetCursor:
		return put("cursor", v.Name, func(c *checker) { c.num64("cursor seq", v.Seq) }, "seq").then(func(w *work) (string, error) { return w.writeCursor(v) })
	case PutPolicy:
		return spec{check: func(c *checker) { c.name(v.Name).text(v.Body).oneOf("policy kind", v.Kind, PolicyKinds) }}.then(func(w *work) (string, error) { return w.addPolicy(v) })
	case SetStopped:
		return spec{fields: []field{{"stop", "", "stopped"}}, check: func(c *checker) { c.text(v.Reason) }}.then(func(w *work) (string, error) { return w.writeStop(v) })
	case SetIdentity:
		return spec{fields: []field{{"identity", "", "identity"}}, check: func(c *checker) { c.name(v.Name, v.Email).given("an identity's name and email", v.Name, v.Email) }}.then(func(w *work) (string, error) { return w.writeIdentity(v) })
	case SetActivePolicy:
		return spec{fields: []field{{"policy", "", "active"}}, check: func(c *checker) { c.num("version", v.Version) }}.then(func(w *work) (string, error) { return w.activate(v) })
	}
	return setSpec(w)
}

// setSpec is specOf for the set-card and set-row writes: one column each,
// with its kind rule.
func setSpec(w Write) spec {
	switch v := w.(type) {
	case SetCardState:
		s := card(v.Card, "state", func(k *Card) { k.State = v.State }, func(c *checker) { c.oneOf("card state", v.State, CardStates) })
		s.is = "is already " + v.State
		return s
	case SetCardPaths:
		return card(v.Card, "paths", func(k *Card) { k.Paths = normList(v.Paths) }, func(c *checker) { c.text(v.Paths...) }).kinds("SetCardPaths", copied, holdless)
	case SetCardDoneWhen:
		return card(v.Card, "done_when", func(k *Card) { k.DoneWhen = normList(v.DoneWhen) }, func(c *checker) { c.text(v.DoneWhen...) }).kinds("SetCardDoneWhen", copied, holdless)
	case SetCardProse:
		return card(v.Card, "prose", func(k *Card) { k.Prose = v.Prose }, func(c *checker) { c.text(v.Prose) }).kinds("SetCardProse", copied, holdless)
	case SetCardResult:
		return card(v.Card, "result", func(k *Card) { k.Result = v.Result }, func(c *checker) { c.oneOf("card result", v.Result, CardResults) }).kinds("SetCardResult", "a consumer", consumer)
	case SetCardAttempt:
		return card(v.Card, "attempt", func(k *Card) { k.Attempt = v.Attempt }, func(c *checker) { c.num("attempt", v.Attempt) }).kinds("SetCardAttempt", "a producer or sentinel (a consumer's attempt is set at add)", holdless)
	case SetCardBranch:
		return card(v.Card, "branch", func(k *Card) { k.Branch = v.Branch }, func(c *checker) { c.text(v.Branch) })
	case SetCardHead:
		return card(v.Card, "head", func(k *Card) { k.Head = v.Head }, func(c *checker) { c.text(v.Head) })
	case SetCardPR:
		return card(v.Card, "pr", func(k *Card) { k.PR = v.PR }, func(c *checker) { c.num("pr", v.PR) })
	case SetCardReason:
		return card(v.Card, "reason", func(k *Card) { k.Reason = v.Reason }, func(c *checker) { c.text(v.Reason) })
	case SetCardScore:
		return card(v.Card, "score", func(k *Card) { k.Score = v.Score }, func(c *checker) { c.num("score", v.Score) })
	case SetCardAuthor:
		return card(v.Card, "author", func(k *Card) { k.Author = v.Author }, func(c *checker) { c.name(v.Author) })
	case SetCardBaseSHA:
		return card(v.Card, "base_sha", func(k *Card) { k.BaseSHA = v.SHA }, func(c *checker) { c.text(v.SHA) })
	case SetCardNeeds:
		s := card(v.Card, "needs", func(k *Card) { k.Needs = normSet(v.Needs) }, func(c *checker) { c.keys("card", v.Needs...) }).kinds("SetCardNeeds", "a producer or sentinel", holdless)
		return s.listed(needList, v.Needs)
	case SetCardAfter:
		s := card(v.Card, "after", func(k *Card) { k.After = normSet(v.After) }, func(c *checker) { c.keys("card", v.After...) }).kinds("SetCardAfter", "a producer or sentinel", holdless)
		return s.listed(afterList, v.After)
	}
	return kindSpec(w)
}

// kindSpec is setSpec for the set-card writes that one kind alone carries.
func kindSpec(w Write) spec {
	switch v := w.(type) {
	case SetCardPriorHead:
		return card(v.Card, "prior_head", func(k *Card) { k.Prior = v.Head }, func(c *checker) { c.text(v.Head) }).kinds("SetCardPriorHead", "a work consumer", func(k string) bool { return k == "work" })
	case SetCardBounces:
		return card(v.Card, "bounces", func(k *Card) { k.Bounces = v.Bounces }, func(c *checker) { c.num("bounces", v.Bounces) }).kinds("SetCardBounces", "a producer", func(k string) bool { return k == "producer" })
	case SetCardReview:
		s := card(v.Card, "review", func(k *Card) { k.Review++ }, nil).kinds("SetCardReview", "a producer", func(k string) bool { return k == "producer" })
		kind := s.cardIf
		s.cardIf = func(w *work, k Card) (string, error) {
			if r, err := kind(w, k); r != "" || err != nil {
				return r, err
			}
			return when(k.Review >= math.MaxInt32, "card %s is at review %d, the most there is", k.ID, k.Review), nil
		}
		return s
	case SetCardFinding:
		return card(v.Card, "finding", func(k *Card) { k.Finding = v.Finding }, func(c *checker) { c.text(v.Finding) }).kinds("SetCardFinding", "a producer", func(k string) bool { return k == "producer" })
	case SetCardRed:
		s := card(v.Card, "red", func(k *Card) { k.Red = v.Red }, nil)
		s.cardIf = func(_ *work, k Card) (string, error) { return redOnProducer(k), nil }
		return s
	case SetCardCost:
		return card(v.Card, "cost", func(k *Card) { k.Cost, k.Priced = v.Cost, v.Priced }, func(c *checker) { c.num64("cost", v.Cost) }).kinds("SetCardCost", "a consumer", consumer)
	case SetCardUsage:
		return card(v.Card, "usage", func(k *Card) { k.Usage = v.Usage }, func(c *checker) { c.text(v.Usage) }).kinds("SetCardUsage", "a consumer", consumer)
	case SetCardPriority:
		// "" is normal, as at put card: one rule for an absent priority.
		s := card(v.Card, "priority", func(k *Card) { k.Priority = cmp.Or(v.Priority, "normal") }, func(c *checker) { c.oneOf("card priority", cmp.Or(v.Priority, "normal"), Priorities) })
		s.cardIf = func(_ *work, k Card) (string, error) { return producersOnly(k), nil }
		return s
	}
	return rowSpec(w)
}

// rowSpec is specOf for the set-row writes.
func rowSpec(w Write) spec {
	switch v := w.(type) {
	case SetRowState:
		s := row(v.Row, "state", func(r *Row) { r.State = v.State }, func(c *checker) { c.oneOf("row state", v.State, RowStates) })
		s.is = "is already " + v.State
		return s
	case SetRowFlaps:
		return row(v.Row, "flaps", func(r *Row) { r.Flaps = v.Flaps }, func(c *checker) { c.num("flaps", v.Flaps) })
	case SetRowLoad:
		return put("row", v.Row, func(c *checker) {
			c.num("load1_centi", v.Load1Centi).num("cores", v.Cores).num("slots_used", v.SlotsUsed).num("slots_total", v.SlotsTotal)
			if v.Cores < 1 || v.SlotsUsed > v.SlotsTotal {
				c.fail("load needs cores of at least 1 and slots_used no more than slots_total, got cores %d, slots %d of %d", v.Cores, v.SlotsUsed, v.SlotsTotal)
			}
			if v.Tiers != JoinTiers(SplitTiers(v.Tiers)) {
				c.fail("load tiers %q are not a comma list of %s in that order, each once", v.Tiers, strings.Join(Tiers, ", "))
			}
		}, "load").then(func(w *work) (string, error) { return w.setRowLoad(v) })
	case SetRowHeld:
		s := row(v.Row, "held", func(r *Row) { r.Held, r.Reason = v.Held, v.Reason }, func(c *checker) { c.text(v.Reason) })
		s.rowIf = func(_ *work, r Row) (string, error) {
			return when(r.Held == v.Held, "row %s is already %s", r.Name, heldWord(v.Held)), nil
		}
		return s
	}
	return spec{}
}

// put is the spec of a write to the record key of table, setting columns;
// key is an id or a name, never empty.
func put(table, key string, check func(*checker), columns ...string) spec {
	s := spec{check: func(c *checker) {
		if c.key(table, key); check != nil {
			check(c)
		}
	}}
	for _, col := range columns {
		s.fields = append(s.fields, field{table, key, col})
	}
	return s
}

// then is s with the write itself.
func (s spec) then(do func(w *work) (string, error)) spec {
	s.do = do
	return s
}

// card is the spec of a set-card write: one column of card id.
func card(id, column string, set func(*Card), check func(*checker)) spec {
	s := put("card", id, check, column)
	s.card = set
	return s
}

// row is the spec of a set-row write: one column of row name.
func row(name, column string, set func(*Row), check func(*checker)) spec {
	s := put("row", name, check, column)
	s.row = set
	return s
}

// kinds is s refused on a card whose kind does not carry its column, before
// the value is looked at: write is only for kinds (the words), ok says which.
func (s spec) kinds(write, kinds string, ok func(kind string) bool) spec {
	s.cardIf = func(_ *work, k Card) (string, error) {
		return when(!ok(k.Kind), "card %s is of kind %s; %s is only for %s", k.ID, k.Kind, write, kinds), nil
	}
	return s
}

// listed is s with its list checked after its kind rule (checkList).
func (s spec) listed(l cardList, names []string) spec {
	kind := s.cardIf
	s.cardIf = func(w *work, k Card) (string, error) {
		if r, err := kind(w, k); r != "" || err != nil {
			return r, err
		}
		return w.checkList(k, l, names)
	}
	return s
}

// holdless is the kinds that are never held: producer and sentinel.
func holdless(kind string) bool { return !consumer(kind) }

// copied is the kinds whose contract fields are set after add: a consumer
// carries the copies it was added with.
const copied = "a producer or sentinel (a consumer's copy is set at add)"

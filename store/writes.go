package store

import (
	"bytes"
	"cmp"
	"fmt"
	"strings"
	"time"
)

// write performs one write (its entry in specOf), or returns the reason it is
// refused; check has already refused anything outside the closed sets.
func (w *work) write(wr Write) (string, error) {
	s := specOf(wr)
	switch {
	case s.do != nil:
		return s.do(w)
	case s.row != nil:
		return set(w, s, w.mustRow, s.rowIf, s.row, w.putRow)
	}
	return set(w, s, w.mustCard, s.cardIf, s.card, w.putCard)
}

// set is a set-card or set-row write on an existing record: its rule first
// (a write on the wrong kind is refused before its value is looked at), then
// a value that changes nothing is refused; put sets the gen, once per op.
func set[T any](w *work, s spec, must func(string) (T, string, error), rule func(*work, T) (string, error), change func(*T), put func(T, bool, T) error) (string, error) {
	old, r, err := must(s.fields[0].key)
	if r != "" || err != nil {
		return r, err
	}
	if rule != nil {
		if r, err := rule(w, old); r != "" || err != nil {
			return r, err
		}
	}
	next := old
	change(&next)
	if r := s.unchanged(old, next); r != "" {
		return r, nil
	}
	return "", put(old, true, next)
}

// unchanged is the refusal of a set write whose record would be the same
// after it as before: "row r1 is already up", "card c1 already has the given
// reason" (one form for one value or a list, so no word depends on the
// column). Nothing is logged as accepted and no gen moves.
func (s spec) unchanged(was, now any) string {
	if !bytes.Equal(encode(was), encode(now)) {
		return ""
	}
	f := s.fields[0]
	if s.is != "" {
		return fmt.Sprintf("%s %s %s", f.table, f.key, s.is)
	}
	return fmt.Sprintf("%s %s already has the given %s", f.table, f.key, strings.ReplaceAll(f.name, "_", " "))
}

func (w *work) setStreamHeld(v SetStreamHeld) (string, error) {
	s, r, err := w.mustStream(v.Stream)
	if r != "" || err != nil {
		return r, err
	}
	if s.Held == v.Held {
		return fmt.Sprintf("stream %s is already %s", v.Stream, heldWord(v.Held)), nil
	}
	next := s
	next.Held = v.Held
	return "", w.putStream(s, true, next)
}

// setStream sets one landing setting of a stream (field picks it), refused
// when the stream has that value already.
func (w *work) setStream(name, what, value string, field func(*Stream) *string) (string, error) {
	s, r, err := w.mustStream(name)
	if r != "" || err != nil {
		return r, err
	}
	if *field(&s) == value {
		return fmt.Sprintf("stream %s's %s is already %q", name, what, value), nil
	}
	next := s
	*field(&next) = value
	return "", w.putStream(s, true, next)
}

func (w *work) writePutStream(v PutStream) (string, error) {
	old, ok, err := w.tx.stream(v.Name)
	if err != nil {
		return "", err
	}
	next := Stream{Name: v.Name, Repo: v.Repo, Base: v.Base, Pos: v.Pos, Held: ok && old.Held, Mode: cmp.Or(v.Mode, StreamAuto), Forge: v.Forge}
	return "", w.putStream(old, ok, next)
}

func (w *work) writePutRow(v PutRow) (string, error) {
	old, ok, err := w.tx.row(v.Name)
	if err != nil {
		return "", err
	}
	if ok && old.Session != "" && v.Session != "" && old.Session != v.Session {
		return fmt.Sprintf("row %s is hooked by session %s; unhook it first", v.Name, old.Session), nil
	}
	next := Row{Name: v.Name, Session: v.Session, Role: v.Role, State: "down"}
	if ok {
		next.State, next.Flaps, next.Held, next.Reason = old.State, old.Flaps, old.Held, old.Reason
	}
	return "", w.putRow(old, ok, next)
}

// producersOnly refuses a priority write on a consumer, whatever the value: a
// consumer is ordered by its producer's level, never its own.
func producersOnly(card Card) string {
	return when(consumer(card.Kind), "card %s is of kind %s; priority is a producer's", card.ID, card.Kind)
}

// redOnProducer refuses a red write on any card but a producer, whatever
// the value, naming the fix: a read's verdict is recorded on the producer it
// read, and a sentinel is never read (round 17).
func redOnProducer(k Card) string {
	switch k.Kind {
	case "producer":
		return ""
	case "sentinel":
		return fmt.Sprintf("card %s is of kind sentinel; SetCardRed is only for a producer, and a sentinel is never read", k.ID)
	}
	return fmt.Sprintf("card %s is of kind %s; SetCardRed is only for a producer: record the verdict on its producer %s", k.ID, k.Kind, k.Of)
}

// consumer is true for the kinds a row is handed (work and read); the other
// kinds (producer and sentinel) are bookkeeping and are never held.
func consumer(kind string) bool { return kind == "work" || kind == "read" }

// checkHolder is the holder at add, the only time it is set: a producer or
// sentinel has none, and a consumer names a row of the epoch (read inside the
// transaction; the foreign key says the same). Whether that row serves the
// card's tier and has room for it is the roster's to say, at the deal (card
// package), never the store's: width and tiers are configuration (round 22).
func (w *work) checkHolder(c Card) (string, error) {
	switch {
	case !consumer(c.Kind):
		return when(c.Holder != "", "card %s is a %s; a %s never has a holder", c.ID, c.Kind, c.Kind), nil
	case c.Holder == "":
		return fmt.Sprintf("card %s is a consumer; a consumer names its holder row at add, and it never changes", c.ID), nil
	}
	_, r, err := w.mustRow(c.Holder)
	return r, err
}

// cardList is one of a card's lists of other cards: needs (hard) or after
// (soft).
type cardList struct{ plural, one, self string }

var (
	needList  = cardList{"needs", "need", "need itself"}
	afterList = cardList{"after", "after", "come after itself"}
)

// checkList refuses a list on a consumer, and a list that names the card
// itself, a card twice, or a card not in the epoch. Needs and after follow
// the same rule.
func (w *work) checkList(card Card, l cardList, names []string) (string, error) {
	id := card.ID
	if len(names) > 0 && consumer(card.Kind) {
		return fmt.Sprintf("card %s is a consumer; a consumer has no %s", id, l.plural), nil
	}
	seen := map[string]bool{}
	for _, n := range names {
		switch {
		case n == id:
			return fmt.Sprintf("card %s cannot %s", id, l.self), nil
		case seen[n]:
			return fmt.Sprintf("card %s names %s %s twice", id, l.one, n), nil
		}
		seen[n] = true
		if _, ok, err := w.tx.card(n); !ok || err != nil {
			return when(err == nil, "%s %s of card %s does not exist", l.one, n, id), err
		}
	}
	return "", nil
}

func (w *work) addCard(v PutCard) (string, error) {
	if _, ok, err := w.tx.card(v.ID); ok || err != nil {
		return when(err == nil, "card %s exists", v.ID), err
	}
	if _, r, err := w.mustStream(v.Stream); r != "" || err != nil {
		return r, err
	}
	if v.Kind == "sentinel" && v.Brief != "" {
		return fmt.Sprintf("card %s is a sentinel; a sentinel has no brief", v.ID), nil
	}
	c := Card{ID: v.ID, Kind: v.Kind, Of: v.Of, Stream: v.Stream, State: "waiting", Attempt: v.Attempt, Score: v.Score, Needs: normSet(v.Needs), After: normSet(v.After), Brief: v.Brief, Priority: cmp.Or(v.Priority, "normal"),
		Holder: v.Holder, Tier: v.Tier, Paths: normList(v.Paths), DoneWhen: normList(v.DoneWhen), Prose: v.Prose, Review: v.Review, Finding: v.Finding}
	if c.Priority != "normal" {
		if r := producersOnly(c); r != "" {
			return r, nil
		}
	}
	if v.Attempt != 0 && !consumer(v.Kind) {
		return fmt.Sprintf("card %s is a %s; its attempt starts at 0 and is raised by SetCardAttempt", v.ID, v.Kind), nil
	}
	if v.Finding != "" && v.Kind != "work" {
		return fmt.Sprintf("card %s is a %s; only a work consumer is cut with a finding (a producer's is set by SetCardFinding)", v.ID, v.Kind), nil
	}
	if v.Review != 0 && v.Kind != "read" {
		return fmt.Sprintf("card %s is a %s; only a read consumer is cut at a review (a producer's starts at 0 and is raised by SetCardReview)", v.ID, v.Kind), nil
	}
	if r, err := w.checkOf(c); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkList(c, needList, v.Needs); r != "" || err != nil { // as sent: a name twice is refused, not merged
		return r, err
	}
	if r, err := w.checkList(c, afterList, v.After); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkHolder(c); r != "" || err != nil {
		return r, err
	}
	return "", w.putCard(Card{}, false, c)
}

// checkOf refuses a producer or sentinel that names an of, and a consumer
// whose of is not a producer of the epoch (a sentinel has no consumers), or
// whose stream or tier is not its producer's.
func (w *work) checkOf(c Card) (string, error) {
	if !consumer(c.Kind) {
		return when(c.Of != "", "card %s is a %s; a %s has no of", c.ID, c.Kind, c.Kind), nil
	}
	if c.Of == "" {
		return fmt.Sprintf("card %s is a consumer; a consumer names its producer in of", c.ID), nil
	}
	p, ok, err := w.tx.card(c.Of)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return fmt.Sprintf("card %s is a consumer of %s, which does not exist", c.ID, c.Of), nil
	}
	if p.Kind != "producer" {
		return fmt.Sprintf("card %s is a consumer of %s, which is not a producer", c.ID, c.Of), nil
	}
	return cmp.Or(when(p.Stream != c.Stream, "card %s is in stream %s, its producer %s is in stream %s; a consumer takes its producer's stream", c.ID, c.Stream, c.Of, p.Stream),
		when(p.Tier != c.Tier, "card %s is tier %s, its producer %s is tier %s; a consumer takes its producer's tier", c.ID, c.Tier, c.Of, p.Tier),
		when(c.Kind == "work" && p.Finding != c.Finding, "card %s is cut with finding %q, its producer %s has %q; a work consumer takes its producer's finding", c.ID, c.Finding, c.Of, p.Finding),
		when(c.Kind == "read" && p.Review != c.Review, "card %s is cut at review %d, its producer %s is at review %d; a read takes its producer's review", c.ID, c.Review, c.Of, p.Review)), nil
}

func (w *work) openJudgement(v PutJudgement) (string, error) {
	if _, ok, err := w.tx.judgement(v.ID); ok || err != nil {
		return when(err == nil, "judgement %s exists", v.ID), err
	}
	j := Judgement{ID: v.ID, Kind: v.Kind, About: v.About, Question: v.Question, Command: v.Command, Group: v.Group, State: "open", Deadline: at(v.Deadline), OpenedAt: w.op.At}
	return "", w.putJudgement(Judgement{}, false, j)
}

// closeJudgement moves an open judgement to answered or expired, at the op's
// At, never before it opened.
func (w *work) closeJudgement(id, state, answer, actor string) (string, error) {
	j, r, err := w.mustJudgement(id)
	if r != "" || err != nil {
		return r, err
	}
	if j.State != "open" {
		return fmt.Sprintf("judgement %s is %s; only an open judgement can be %s", id, j.State, state), nil
	}
	if w.op.At.Before(j.OpenedAt) {
		return fmt.Sprintf("judgement %s opened at %s, cannot close at %s", id, j.OpenedAt.Format(time.RFC3339Nano), w.op.At.Format(time.RFC3339Nano)), nil
	}
	next := j
	next.State, next.Answer, next.Actor, next.ClosedAt = state, answer, actor, w.op.At
	return "", w.putJudgement(j, true, next)
}

// writeCursor moves a cursor forward, never back and never past the log; a
// cursor set to where it is already changes nothing and is refused, an
// absent cursor counting as 0 (SetCursor{c, 0} on a cursor never set is
// refused, as Store.tla has it; the store-cursor-zero fixture).
func (w *work) writeCursor(v SetCursor) (string, error) {
	seq, ok, err := w.tx.cursor(v.Name)
	if err != nil {
		return "", err
	}
	last, err := w.tx.lastSeq()
	switch {
	case err != nil:
		return "", err
	case v.Seq == seq:
		return fmt.Sprintf("cursor %s is already at %d", v.Name, seq), nil
	case v.Seq < seq:
		return fmt.Sprintf("cursor %s is at %d, cannot move back to %d", v.Name, seq, v.Seq), nil
	case v.Seq > last:
		return fmt.Sprintf("cursor %s cannot pass the log at %d", v.Name, last), nil
	}
	return "", w.putCursor(v.Name, seq, ok, v.Seq)
}

// addPolicy adds the next policy version, inactive.
func (w *work) addPolicy(v PutPolicy) (string, error) {
	last, _, err := w.tx.policies()
	if err != nil {
		return "", err
	}
	version, err := next("policy version", last)
	if err != nil {
		return "", err
	}
	p := PolicyVersion{Version: version, Kind: v.Kind, Name: v.Name, Body: v.Body, Actor: w.op.Actor, At: w.op.At}
	return "", w.putPolicy(PolicyVersion{}, false, p)
}

// writeStop stops or starts the sprint; one already in that state is
// refused.
func (w *work) writeStop(v SetStopped) (string, error) {
	old, err := w.tx.stop()
	if err != nil {
		return "", err
	}
	if old.Stopped == v.Stopped {
		return fmt.Sprintf("the sprint is already %s", map[bool]string{true: "stopped", false: "running"}[v.Stopped]), nil
	}
	return "", w.putStop(old, Stop{Stopped: v.Stopped, Reason: v.Reason})
}

// activate makes one version the active one and deactivates the others; the
// version already active is refused.
func (w *work) activate(v SetActivePolicy) (string, error) {
	_, active, err := w.tx.policies()
	if err != nil {
		return "", err
	}
	p, ok, err := w.tx.policy(v.Version)
	if err != nil || !ok {
		return when(err == nil, "policy version %d does not exist", v.Version), err
	}
	if p.Active {
		return fmt.Sprintf("policy version %d is already active", v.Version), nil
	}
	for _, a := range active {
		off := a
		off.Active = false
		if err := w.putPolicy(a, true, off); err != nil {
			return "", err
		}
	}
	on := p
	on.Active = true
	return "", w.putPolicy(p, true, on)
}

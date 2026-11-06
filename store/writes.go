package store

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
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

func (w *work) writePutStream(v PutStream) (string, error) {
	old, ok, err := w.tx.stream(v.Name)
	if err != nil {
		return "", err
	}
	next := Stream{Name: v.Name, Repo: v.Repo, Base: v.Base, Pos: v.Pos, Held: ok && old.Held}
	return "", w.putStream(old, ok, next)
}

func (w *work) writePutRow(v PutRow) (string, error) {
	old, ok, err := w.tx.row(v.Name)
	if err != nil {
		return "", err
	}
	next := Row{Name: v.Name, Kind: v.Kind, Width: v.Width, Session: v.Session, State: "down", Tiers: normSet(v.Tiers), Model: v.Model, Harness: v.Harness}
	if ok {
		if r, err := w.widthFits(v.Name, v.Width); r != "" || err != nil {
			return r, err
		}
		if r, err := w.tiersFit(v.Name, v.Tiers); r != "" || err != nil {
			return r, err
		}
		next.State, next.Flaps, next.Held = old.State, old.Flaps, old.Held
	}
	return "", w.putRow(old, ok, next)
}

// widthFits refuses a width below the working cards the row holds.
func (w *work) widthFits(row string, width int) (string, error) {
	n, err := w.tx.working(row)
	return when(err == nil && n > width, "row %s holds %s, more than width %d; wait for a card to finish before narrowing it", row, workingCards(n), width), err
}

// tiersFit refuses tiers that drop one a live (ready or working) consumer
// held by the row needs, as widthFits refuses narrowing below its working
// cards: a consumer's holder never changes, so its row keeps serving it.
func (w *work) tiersFit(row string, tiers []string) (string, error) {
	c, found, err := w.tx.needed(row, tiers)
	return when(err == nil && found, "row %s holds consumer %s (%s); cannot drop %s", row, c.ID, c.Tier, c.Tier), err
}

func workingCards(n int) string {
	if n == 1 {
		return "1 working card"
	}
	return fmt.Sprintf("%d working cards", n)
}

// move is the rule of SetCardState for a held card: moving into ready or
// working needs its row to serve its tier (rowServes), moving into working
// needs room on the row (rowFits).
func (w *work) move(card Card, state string) (string, error) {
	if card.Holder != "" && !live(card.State) && live(state) {
		if r, err := w.rowServes(card); r != "" || err != nil {
			return r, err
		}
	}
	if card.Holder != "" && card.State != "working" && state == "working" {
		return w.rowFits(card.Holder)
	}
	return "", nil
}

// producersOnly refuses a priority write on a consumer, whatever the value: a
// consumer is ordered by its producer's level, never its own.
func producersOnly(card Card) string {
	return when(consumer(card.Kind), "card %s is of kind %s; priority is a producer's", card.ID, card.Kind)
}

// consumer is true for the kinds a row is handed (work and read); the other
// kinds (producer and sentinel) are bookkeeping and are never held.
func consumer(kind string) bool { return kind == "work" || kind == "read" }

// checkHolder is the holder at add, the only time it is set: a producer or
// sentinel has none, and a consumer names a row of the epoch that serves its
// tier (the row's tiers, read inside the transaction). The card is added
// waiting, so it takes no room; width is checked when it moves into working.
func (w *work) checkHolder(c Card) (string, error) {
	switch {
	case !consumer(c.Kind):
		return when(c.Holder != "", "card %s is a %s; a %s never has a holder", c.ID, c.Kind, c.Kind), nil
	case c.Holder == "":
		return fmt.Sprintf("card %s is a consumer; a consumer names its holder row at add, and it never changes", c.ID), nil
	}
	return w.rowServes(c)
}

// rowServes refuses a consumer whose holder row does not exist or does not
// serve its tier, read now (earlier writes of the op included). It is checked
// at add and on every move into ready or working, and tiersFit keeps a live
// consumer's tier on its row, so a live consumer's row always serves it.
func (w *work) rowServes(c Card) (string, error) {
	row, r, err := w.mustRow(c.Holder)
	return cmp.Or(r, when(err == nil && !slices.Contains(row.Tiers, c.Tier), "row %s serves %s; consumer needs %s", c.Holder, tierList(row.Tiers), c.Tier)), err
}

// live is a consumer state that holds its row's tier: ready or working.
func live(state string) bool { return state == "ready" || state == "working" }

// tierList is a row's tiers for a reason: "flash,pro", or none.
func tierList(tiers []string) string { return cmp.Or(strings.Join(tiers, ","), "none") }

// rowFits refuses a row that does not exist or has no room for one more
// working card, counted now (earlier writes of the op included).
func (w *work) rowFits(name string) (string, error) {
	row, r, err := w.mustRow(name)
	if r != "" || err != nil {
		return r, err
	}
	n, err := w.tx.working(name)
	return when(err == nil && n >= row.Width, "row %s holds %s, its width is %d; raise the width with set row width, or wait for a card to finish", name, workingCards(n), row.Width), err
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
		Holder: v.Holder, Tier: v.Tier, Paths: normList(v.Paths), DoneWhen: normList(v.DoneWhen), Prose: v.Prose}
	if c.Priority != "normal" {
		if r := producersOnly(c); r != "" {
			return r, nil
		}
	}
	if v.Attempt != 0 && !consumer(v.Kind) {
		return fmt.Sprintf("card %s is a %s; its attempt starts at 0 and is raised by SetCardAttempt", v.ID, v.Kind), nil
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
		when(p.Tier != c.Tier, "card %s is tier %s, its producer %s is tier %s; a consumer takes its producer's tier", c.ID, c.Tier, c.Of, p.Tier)), nil
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
// cursor set to where it is already changes nothing and is refused.
func (w *work) writeCursor(v SetCursor) (string, error) {
	seq, ok, err := w.tx.cursor(v.Name)
	if err != nil {
		return "", err
	}
	last, err := w.tx.lastSeq()
	switch {
	case err != nil:
		return "", err
	case ok && v.Seq == seq:
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

package store

import (
	"cmp"
	"fmt"
)

// write performs one write, or returns the reason it is refused.
func (w *work) write(wr Write) (string, error) {
	switch v := wr.(type) {
	case PutStream:
		return w.writePutStream(v)
	case SetStreamHeld:
		s, r, err := w.mustStream(v.Stream)
		if r != "" || err != nil {
			return r, err
		}
		next := s
		next.Held = v.Held
		return "", w.putStream(s, true, next)
	case PutCard:
		return w.addCard(v)
	case PutRow:
		return w.writePutRow(v)
	case PutPolicy, SetActivePolicy:
		return w.writePolicy(wr)
	}
	if _, _, _, ok := rowSet(wr); ok {
		return w.writeRow(wr)
	}
	return w.writeCard(wr)
}

func (w *work) writePutStream(v PutStream) (string, error) {
	if v.Name == "" {
		return "a stream needs a name", nil
	}
	old, ok, err := w.tx.stream(v.Name)
	if err != nil {
		return "", err
	}
	next := Stream{Name: v.Name, Repo: v.Repo, Base: v.Base, Pos: v.Pos, Held: ok && old.Held}
	return "", w.putStream(old, ok, next)
}

func (w *work) writePutRow(v PutRow) (string, error) {
	if v.Name == "" {
		return "a row needs a name", nil
	}
	if r := oneOf("row kind", v.Kind, RowKinds); r != "" {
		return r, nil
	}
	old, ok, err := w.tx.row(v.Name)
	if err != nil {
		return "", err
	}
	next := Row{Name: v.Name, Kind: v.Kind, Width: v.Width, Session: v.Session, State: "down"}
	if ok {
		if r, err := w.widthFits(v.Name, v.Width); r != "" || err != nil {
			return r, err
		}
		next.State, next.Flaps, next.Held = old.State, old.Flaps, old.Held
	}
	return "", w.putRow(old, ok, next)
}

// widthFits refuses a width below the working cards the row holds.
func (w *work) widthFits(row string, width int) (string, error) {
	n, err := w.tx.held(row)
	return when(err == nil && n > width, "row %s holds %d working cards, more than width %d", row, n, width), err
}

// writeRow is the set-row writes (rowSet).
func (w *work) writeRow(wr Write) (string, error) {
	name, _, set, _ := rowSet(wr)
	if v, ok := wr.(SetRowState); ok {
		if r := oneOf("row state", v.State, RowStates); r != "" {
			return r, nil
		}
	}
	row, r, err := w.mustRow(name)
	if r != "" || err != nil {
		return r, err
	}
	if v, ok := wr.(SetRowWidth); ok {
		if r, err := w.widthFits(name, v.Width); r != "" || err != nil {
			return r, err
		}
	}
	next := row
	set(&next)
	return "", w.putRow(row, true, next)
}

// writeCard is the set-card writes (cardSet) on an existing card; putCard
// sets the gen, once per op.
func (w *work) writeCard(wr Write) (string, error) {
	id, _, set, _ := cardSet(wr)
	card, r, err := w.mustCard(id)
	if r != "" || err != nil {
		return r, err
	}
	switch v := wr.(type) {
	case SetCardState:
		r = oneOf("card state", v.State, CardStates)
		if r == "" && card.Holder != "" && card.State != "working" && v.State == "working" {
			r, err = w.rowFits(card.Holder)
		}
	case SetCardHolder:
		r, err = w.holderFits(card, v.Holder)
	case SetCardResult:
		r = oneOf("card result", v.Result, CardResults)
	case SetCardPriority:
		r = checkPriority(card, v.Priority)
	case SetCardNeeds:
		r, err = w.checkList(card, needList, v.Needs)
	case SetCardAfter:
		r, err = w.checkList(card, afterList, v.After)
	}
	if r != "" || err != nil {
		return r, err
	}
	next := card
	set(&next)
	return "", w.putCard(card, true, next)
}

// checkPriority refuses a level outside the five, and any level but normal
// on a consumer: a consumer is ordered by its producer's level, never its own.
func checkPriority(card Card, p string) string {
	if r := oneOf("card priority", p, Priorities); r != "" {
		return r
	}
	return when(consumer(card.Kind) && p != "normal", "card %s is a consumer; a consumer has no priority of its own", card.ID)
}

// consumer is true for the kinds a row is handed (work and read); the other
// kinds (producer and sentinel) are bookkeeping and are never held.
func consumer(kind string) bool { return kind == "work" || kind == "read" }

// holderFits refuses a holder on a producer or sentinel, and a holder row
// that does not exist. A card already in working also needs room on the row:
// refused when the row holds as many working cards as its width, counted now
// (earlier writes of the op included). A card in any other state takes no
// room: width caps work in progress, not what is queued. The count excludes
// the card being written: setting a card's holder to the row that holds it
// already is a no-op, never a refusal.
func (w *work) holderFits(card Card, holder string) (string, error) {
	switch {
	case holder == "" || holder == card.Holder:
		return "", nil
	case !consumer(card.Kind):
		return fmt.Sprintf("card %s is a %s; a %s never has a holder", card.ID, card.Kind, card.Kind), nil
	case card.State != "working":
		_, r, err := w.mustRow(holder)
		return r, err
	}
	return w.rowFits(holder)
}

// rowFits refuses a row that does not exist or has no room for one more
// working card.
func (w *work) rowFits(name string) (string, error) {
	row, r, err := w.mustRow(name)
	if r != "" || err != nil {
		return r, err
	}
	n, err := w.tx.held(name)
	return when(err == nil && n >= row.Width, "row %s holds %d working cards, its width is %d", name, n, row.Width), err
}

// list is one of a card's lists of other cards: needs (hard) or after (soft).
type list struct{ plural, one, self string }

var (
	needList  = list{"needs", "need", "need itself"}
	afterList = list{"after", "after", "come after itself"}
)

// checkList refuses a list on a consumer, and a list that names the card
// itself, a card twice, or a card not in the epoch. Needs and after follow
// the same rule.
func (w *work) checkList(card Card, l list, names []string) (string, error) {
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
	if v.ID == "" {
		return "a card needs an id", nil
	}
	if _, ok, err := w.tx.card(v.ID); ok || err != nil {
		return when(err == nil, "card %s exists", v.ID), err
	}
	if r := oneOf("card kind", v.Kind, CardKinds); r != "" {
		return r, nil
	}
	if _, r, err := w.mustStream(v.Stream); r != "" || err != nil {
		return r, err
	}
	if v.Kind == "sentinel" && v.Brief != "" {
		return fmt.Sprintf("card %s is a sentinel; a sentinel has no brief", v.ID), nil
	}
	c := Card{ID: v.ID, Kind: v.Kind, Of: v.Of, Stream: v.Stream, State: "waiting", Score: v.Score, Needs: v.Needs, After: v.After, Brief: v.Brief, Priority: cmp.Or(v.Priority, "normal")}
	if r := checkPriority(c, c.Priority); r != "" {
		return r, nil
	}
	if r, err := w.checkOf(c); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkList(c, needList, v.Needs); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkList(c, afterList, v.After); r != "" || err != nil {
		return r, err
	}
	if r, err := w.holderFits(c, v.Holder); r != "" || err != nil {
		return r, err
	}
	c.Holder = v.Holder
	return "", w.putCard(Card{}, false, c)
}

// checkOf refuses a producer or sentinel that names an of, and a consumer
// whose of is not a producer of the epoch (a sentinel has no consumers).
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
	return when(p.Kind != "producer", "card %s is a consumer of %s, which is not a producer", c.ID, c.Of), nil
}

func (w *work) writePolicy(wr Write) (string, error) {
	last, active, err := w.tx.policies()
	if err != nil {
		return "", err
	}
	if v, ok := wr.(PutPolicy); ok {
		if r := oneOf("policy kind", v.Kind, PolicyKinds); r != "" {
			return r, nil
		}
		p := PolicyVersion{Version: last + 1, Kind: v.Kind, Name: v.Name, Body: v.Body, Actor: w.op.Actor, At: w.op.At}
		return "", w.putPolicy(PolicyVersion{}, false, p)
	}
	v := wr.(SetActivePolicy)
	p, ok, err := w.tx.policy(v.Version)
	if err != nil || !ok {
		return when(err == nil, "policy version %d does not exist", v.Version), err
	}
	for _, a := range active {
		if a.Version != p.Version {
			off := a
			off.Active = false
			if err := w.putPolicy(a, true, off); err != nil {
				return "", err
			}
		}
	}
	on := p
	on.Active = true
	return "", w.putPolicy(p, true, on)
}

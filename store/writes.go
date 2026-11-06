package store

import (
	"fmt"
	"slices"
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
		next.State, next.Flaps = old.State, old.Flaps
	}
	return "", w.putRow(old, ok, next)
}

// widthFits refuses a width below the live cards the row holds.
func (w *work) widthFits(row string, width int) (string, error) {
	n, err := w.tx.held(row)
	return when(err == nil && n > width, "row %s holds %d live cards, more than width %d", row, n, width), err
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
		if r == "" && card.Holder != "" && !live(card.State) && live(v.State) {
			r, err = w.rowFits(card.Holder)
		}
	case SetCardHolder:
		r, err = w.holderFits(card, v.Holder)
	case SetCardResult:
		r = oneOf("card result", v.Result, CardResults)
	case SetCardNeeds:
		r, err = w.checkNeeds(card, v.Needs)
	}
	if r != "" || err != nil {
		return r, err
	}
	next := card
	set(&next)
	return "", w.putCard(card, true, next)
}

func live(state string) bool { return slices.Contains(LiveStates, state) }

// holderFits refuses a holder on a producer, a holder row that does not
// exist, or one that already holds as many live cards as its width, counted
// now (earlier writes of the op included). The count excludes the card being
// written: setting a card's holder to the row that holds it already is a
// no-op, never a refusal.
func (w *work) holderFits(card Card, holder string) (string, error) {
	switch {
	case holder == "" || holder == card.Holder:
		return "", nil
	case card.Kind == "producer":
		return fmt.Sprintf("card %s is a producer; a producer never has a holder", card.ID), nil
	}
	return w.rowFits(holder)
}

// rowFits refuses a row that does not exist or has no room for one more live
// card.
func (w *work) rowFits(name string) (string, error) {
	row, r, err := w.mustRow(name)
	if r != "" || err != nil {
		return r, err
	}
	n, err := w.tx.held(name)
	return when(err == nil && n >= row.Width, "row %s holds %d live cards, its width is %d", name, n, row.Width), err
}

// checkNeeds refuses needs on a consumer, and needs that name the card
// itself, a card twice, or a card not in the epoch.
func (w *work) checkNeeds(card Card, needs []string) (string, error) {
	id := card.ID
	if len(needs) > 0 && card.Kind != "producer" {
		return fmt.Sprintf("card %s is a consumer; a consumer has no needs", id), nil
	}
	seen := map[string]bool{}
	for _, n := range needs {
		switch {
		case n == id:
			return fmt.Sprintf("card %s cannot need itself", id), nil
		case seen[n]:
			return fmt.Sprintf("card %s names need %s twice", id, n), nil
		}
		seen[n] = true
		if _, ok, err := w.tx.card(n); !ok || err != nil {
			return when(err == nil, "need %s of card %s does not exist", n, id), err
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
	c := Card{ID: v.ID, Kind: v.Kind, Of: v.Of, Stream: v.Stream, State: "waiting", Score: v.Score, Needs: v.Needs, Brief: v.Brief}
	if r, err := w.checkOf(c); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkNeeds(c, v.Needs); r != "" || err != nil {
		return r, err
	}
	if r, err := w.holderFits(c, v.Holder); r != "" || err != nil {
		return r, err
	}
	c.Holder = v.Holder
	return "", w.putCard(Card{}, false, c)
}

// checkOf refuses a producer that names an of, and a consumer whose of is not
// a producer of the epoch.
func (w *work) checkOf(c Card) (string, error) {
	if c.Kind == "producer" {
		return when(c.Of != "", "card %s is a producer; a producer has no of", c.ID), nil
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

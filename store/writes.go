package store

import "fmt"

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
	case SetRowState, SetRowSession, SetRowFlaps:
		return w.writeRow(wr)
	case PutPolicy, SetActivePolicy:
		return w.writePolicy(wr)
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
		n, err := w.tx.held(v.Name)
		if n > v.Width || err != nil {
			return when(err == nil, "row %s holds %d cards, more than width %d", v.Name, n, v.Width), err
		}
		next.State, next.Flaps = old.State, old.Flaps
	}
	return "", w.putRow(old, ok, next)
}

func (w *work) writeRow(wr Write) (string, error) {
	var name string
	switch v := wr.(type) {
	case SetRowState:
		if r := oneOf("row state", v.State, RowStates); r != "" {
			return r, nil
		}
		name = v.Row
	case SetRowSession:
		name = v.Row
	case SetRowFlaps:
		name = v.Row
	}
	row, r, err := w.mustRow(name)
	if r != "" || err != nil {
		return r, err
	}
	next := row
	switch v := wr.(type) {
	case SetRowState:
		next.State = v.State
	case SetRowSession:
		next.Session = v.Session
	case SetRowFlaps:
		next.Flaps = v.Flaps
	}
	return "", w.putRow(row, true, next)
}

// cardTarget is the card a set-card write names.
func cardTarget(wr Write) string {
	switch v := wr.(type) {
	case SetCardState:
		return v.Card
	case SetCardHolder:
		return v.Card
	case SetCardAttempt:
		return v.Card
	case SetCardBranch:
		return v.Card
	case SetCardHead:
		return v.Card
	case SetCardPR:
		return v.Card
	case SetCardReason:
		return v.Card
	case SetCardScore:
		return v.Card
	case SetCardAuthor:
		return v.Card
	case SetCardBaseSHA:
		return v.Card
	case SetCardNeeds:
		return v.Card
	}
	return ""
}

// writeCard is the writes that set one field of an existing card; putCard
// sets the gen, once per op.
func (w *work) writeCard(wr Write) (string, error) {
	card, r, err := w.mustCard(cardTarget(wr))
	if r != "" || err != nil {
		return r, err
	}
	next := card
	switch v := wr.(type) {
	case SetCardState:
		r = oneOf("card state", v.State, CardStates)
		next.State = v.State
	case SetCardHolder:
		r, err = w.holderExists(v.Holder)
		next.Holder = v.Holder
	case SetCardAttempt:
		next.Attempt = v.Attempt
	case SetCardBranch:
		next.Branch = v.Branch
	case SetCardHead:
		next.Head = v.Head
	case SetCardPR:
		next.PR = v.PR
	case SetCardReason:
		next.Reason = v.Reason
	case SetCardScore:
		next.Score = v.Score
	case SetCardAuthor:
		next.Author = v.Author
	case SetCardBaseSHA:
		next.BaseSHA = v.SHA
	case SetCardNeeds:
		r, err = w.checkNeeds(card.ID, v.Needs)
		next.Needs = v.Needs
	}
	if r != "" || err != nil {
		return r, err
	}
	return "", w.putCard(card, true, next)
}

func (w *work) holderExists(holder string) (string, error) {
	if holder == "" {
		return "", nil
	}
	_, r, err := w.mustRow(holder)
	return r, err
}

// checkNeeds refuses needs that name the card itself, a card twice, or a card
// not in the epoch.
func (w *work) checkNeeds(id string, needs []string) (string, error) {
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
			return fmt.Sprintf("need %s of card %s does not exist", n, id), err
		}
	}
	return "", nil
}

func (w *work) addCard(v PutCard) (string, error) {
	if v.ID == "" {
		return "a card needs an id", nil
	}
	if _, ok, err := w.tx.card(v.ID); ok || err != nil {
		return fmt.Sprintf("card %s exists", v.ID), err
	}
	if _, r, err := w.mustStream(v.Stream); r != "" || err != nil {
		return r, err
	}
	if r, err := w.checkNeeds(v.ID, v.Needs); r != "" || err != nil {
		return r, err
	}
	c := Card{ID: v.ID, Stream: v.Stream, State: "waiting", Score: v.Score, Needs: v.Needs, Brief: v.Brief}
	return "", w.putCard(Card{}, false, c)
}

func (w *work) writePolicy(wr Write) (string, error) {
	last, active, err := w.tx.policies()
	if err != nil {
		return "", err
	}
	switch v := wr.(type) {
	case PutPolicy:
		if r := oneOf("policy kind", v.Kind, PolicyKinds); r != "" {
			return r, nil
		}
		p := PolicyVersion{Version: last + 1, Kind: v.Kind, Name: v.Name, Body: v.Body, Actor: w.op.Actor, At: w.op.At}
		return "", w.putPolicy(PolicyVersion{}, false, p)
	case SetActivePolicy:
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
	return "", nil
}

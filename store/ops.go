package store

// Expect is a condition: one of CardGen, CardState, CardHolder, NeedsLanded,
// RowIs, RowUnder, StreamHeld, CardAbsent, AtSeq. The set is closed by the
// unexported method; any other value is ErrInvalid.
type Expect interface{ isExpect() }

// CardGen holds when card Card is at gen Gen.
type CardGen struct {
	Card string
	Gen  int
}

// CardState holds when card Card is in state State.
type CardState struct{ Card, State string }

// CardHolder holds when card Card has holder Holder ("" for none).
type CardHolder struct{ Card, Holder string }

// NeedsLanded holds when every need of card Card, read from the card itself
// inside the transaction, is landed.
type NeedsLanded struct{ Card string }

// RowIs holds when row Row is in state State.
type RowIs struct{ Row, State string }

// RowUnder holds when row Row holds fewer than Width cards, counted inside the
// transaction.
type RowUnder struct {
	Row   string
	Width int
}

// StreamHeld holds when stream Stream's held flag is Held.
type StreamHeld struct {
	Stream string
	Held   bool
}

// CardAbsent holds when card Card does not exist in the current epoch.
type CardAbsent struct{ Card string }

// AtSeq holds when the last event's seq is Seq: nothing at all has entered
// the store since a snapshot read at Seq.
type AtSeq struct{ Seq int64 }

func (CardGen) isExpect()     {}
func (CardState) isExpect()   {}
func (CardHolder) isExpect()  {}
func (NeedsLanded) isExpect() {}
func (RowIs) isExpect()       {}
func (RowUnder) isExpect()    {}
func (StreamHeld) isExpect()  {}
func (CardAbsent) isExpect()  {}
func (AtSeq) isExpect()       {}

// Write is one write. The set is closed by the unexported method; any other
// value is ErrInvalid. Every card write raises the card's gen by one.
type Write interface{ isWrite() }

// PutStream inserts a stream or overwrites its repo, base and pos; held is
// kept on overwrite and false on insert. Pos is the order streams land in.
type PutStream struct {
	Name, Repo, Base string
	Pos              int
}

// SetStreamHeld sets a stream's held flag.
type SetStreamHeld struct {
	Stream string
	Held   bool
}

// PutCard adds a card: refused when it exists. It starts waiting, gen 0,
// attempt 0, no holder. Its stream must exist; every need must name another
// card of the same epoch, once.
type PutCard struct {
	ID, Stream string
	Score      int
	Needs      []string
	Brief      string
}

// SetCardState sets a card's state.
type SetCardState struct{ Card, State string }

// SetCardHolder sets a card's holder: a row's name, or "" for none.
type SetCardHolder struct{ Card, Holder string }

// SetCardAttempt sets the number of the attempt the holder is on.
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

// SetCardNeeds replaces a card's needs, under the rules of PutCard.
type SetCardNeeds struct {
	Card  string
	Needs []string
}

// PutRow inserts a row or overwrites its kind, width and session; state and
// flaps are kept on overwrite, and down and 0 on insert.
type PutRow struct {
	Name, Kind string
	Width      int
	Session    string
}

// SetRowState sets a row's state.
type SetRowState struct{ Row, State string }

// SetRowSession sets a row's session.
type SetRowSession struct{ Row, Session string }

// SetRowFlaps sets a row's flap count.
type SetRowFlaps struct {
	Row   string
	Flaps int
}

// PutPolicy adds the next policy version, inactive; actor and at come from the op.
type PutPolicy struct{ Kind, Name, Body string }

// SetActivePolicy makes one existing version the active one and deactivates
// the one active before it.
type SetActivePolicy struct{ Version int }

func (PutStream) isWrite()       {}
func (SetStreamHeld) isWrite()   {}
func (PutCard) isWrite()         {}
func (SetCardState) isWrite()    {}
func (SetCardHolder) isWrite()   {}
func (SetCardAttempt) isWrite()  {}
func (SetCardBranch) isWrite()   {}
func (SetCardHead) isWrite()     {}
func (SetCardPR) isWrite()       {}
func (SetCardReason) isWrite()   {}
func (SetCardScore) isWrite()    {}
func (SetCardNeeds) isWrite()    {}
func (PutRow) isWrite()          {}
func (SetRowState) isWrite()     {}
func (SetRowSession) isWrite()   {}
func (SetRowFlaps) isWrite()     {}
func (PutPolicy) isWrite()       {}
func (SetActivePolicy) isWrite() {}

// field names one field one write sets; two writes to one field in an op are
// invalid.
type field struct{ table, key, name string }

// fields are the fields a write sets; ok is false for a write outside the set.
func fields(w Write) (fs []field, ok bool) {
	card := func(id, name string) []field { return []field{{"card", id, name}} }
	row := func(name, f string) []field { return []field{{"row", name, f}} }
	switch v := w.(type) {
	case PutStream:
		return []field{{"stream", v.Name, "repo"}, {"stream", v.Name, "base"}, {"stream", v.Name, "pos"}}, true
	case SetStreamHeld:
		return []field{{"stream", v.Stream, "held"}}, true
	case PutCard:
		return append(append(card(v.ID, "stream"), card(v.ID, "score")...), append(card(v.ID, "needs"), card(v.ID, "brief")...)...), true
	case SetCardState:
		return card(v.Card, "state"), true
	case SetCardHolder:
		return card(v.Card, "holder"), true
	case SetCardAttempt:
		return card(v.Card, "attempt"), true
	case SetCardBranch:
		return card(v.Card, "branch"), true
	case SetCardHead:
		return card(v.Card, "head"), true
	case SetCardPR:
		return card(v.Card, "pr"), true
	case SetCardReason:
		return card(v.Card, "reason"), true
	case SetCardScore:
		return card(v.Card, "score"), true
	case SetCardNeeds:
		return card(v.Card, "needs"), true
	case PutRow:
		return append(append(row(v.Name, "kind"), row(v.Name, "width")...), row(v.Name, "session")...), true
	case SetRowState:
		return row(v.Row, "state"), true
	case SetRowSession:
		return row(v.Row, "session"), true
	case SetRowFlaps:
		return row(v.Row, "flaps"), true
	case PutPolicy:
		return nil, true
	case SetActivePolicy:
		return []field{{"policy", "", "active"}}, true
	}
	return nil, false
}

// normalize is an op with its times and lists as the store keeps them.
func normalize(op Op) Op {
	op.At = at(op.At)
	op.Writes = append([]Write(nil), op.Writes...)
	for i, w := range op.Writes {
		switch v := w.(type) {
		case PutCard:
			v.Needs = needs(v.Needs)
			op.Writes[i] = v
		case SetCardNeeds:
			v.Needs = needs(v.Needs)
			op.Writes[i] = v
		}
	}
	return op
}

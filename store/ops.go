package store

// Expect is a condition: one of CardGen, CardState, CardHolder, NeedsLanded,
// RowIs, StreamHeld, CardAbsent, AtSeq. There is no width condition: the
// holder write is the check. The set is closed by the
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
func (StreamHeld) isExpect()  {}
func (CardAbsent) isExpect()  {}
func (AtSeq) isExpect()       {}

// Write is one write. The set is closed by the unexported method; any other
// value is ErrInvalid. An op that writes a card raises its gen by one, once.
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

// SetCardHolder sets a card's holder: a row's name, or "" for none. Setting a
// name is refused when that row already holds as many cards as its width,
// counted at the moment of the write (earlier writes of the op included).
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

// SetCardAuthor sets a card's author: the row whose work its head is.
type SetCardAuthor struct{ Card, Author string }

// SetCardBaseSHA sets the base commit the card's attempt started from.
type SetCardBaseSHA struct{ Card, SHA string }

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
func (SetCardAuthor) isWrite()   {}
func (SetCardBaseSHA) isWrite()  {}
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
	if id, column, _, ok := cardSet(w); ok {
		return []field{{"card", id, column}}, true
	}
	if name, column, _, ok := rowSet(w); ok {
		return []field{{"row", name, column}}, true
	}
	of := func(table, key string, names ...string) []field {
		for _, n := range names {
			fs = append(fs, field{table, key, n})
		}
		return fs
	}
	switch v := w.(type) {
	case PutStream:
		return of("stream", v.Name, "repo", "base", "pos"), true
	case SetStreamHeld:
		return of("stream", v.Stream, "held"), true
	case PutCard:
		return of("card", v.ID, "stream", "score", "needs", "brief"), true
	case PutRow:
		return of("row", v.Name, "kind", "width", "session"), true
	case PutPolicy:
		return nil, true
	case SetActivePolicy:
		return of("policy", "", "active"), true
	}
	return nil, false
}

// cardSet is a set-card write as one field of one card: the card it names,
// the column it sets and the change. ok is false for any other write. It is
// the one table of the set-card writes.
func cardSet(w Write) (id, column string, set func(*Card), ok bool) {
	switch v := w.(type) {
	case SetCardState:
		return v.Card, "state", func(c *Card) { c.State = v.State }, true
	case SetCardHolder:
		return v.Card, "holder", func(c *Card) { c.Holder = v.Holder }, true
	case SetCardAttempt:
		return v.Card, "attempt", func(c *Card) { c.Attempt = v.Attempt }, true
	case SetCardBranch:
		return v.Card, "branch", func(c *Card) { c.Branch = v.Branch }, true
	case SetCardHead:
		return v.Card, "head", func(c *Card) { c.Head = v.Head }, true
	case SetCardPR:
		return v.Card, "pr", func(c *Card) { c.PR = v.PR }, true
	case SetCardReason:
		return v.Card, "reason", func(c *Card) { c.Reason = v.Reason }, true
	case SetCardScore:
		return v.Card, "score", func(c *Card) { c.Score = v.Score }, true
	case SetCardAuthor:
		return v.Card, "author", func(c *Card) { c.Author = v.Author }, true
	case SetCardBaseSHA:
		return v.Card, "base_sha", func(c *Card) { c.BaseSHA = v.SHA }, true
	case SetCardNeeds:
		return v.Card, "needs", func(c *Card) { c.Needs = v.Needs }, true
	}
	return "", "", nil, false
}

// rowSet is cardSet for the set-row writes.
func rowSet(w Write) (name, column string, set func(*Row), ok bool) {
	switch v := w.(type) {
	case SetRowState:
		return v.Row, "state", func(r *Row) { r.State = v.State }, true
	case SetRowSession:
		return v.Row, "session", func(r *Row) { r.Session = v.Session }, true
	case SetRowFlaps:
		return v.Row, "flaps", func(r *Row) { r.Flaps = v.Flaps }, true
	}
	return "", "", nil, false
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

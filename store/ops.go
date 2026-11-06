package store

// Expect is a condition: one of CardGen, CardState, CardHolder, NeedsLanded,
// RowIs, RowGen, RowHeld, StreamHeld, CardAbsent, AtSeq. There is no width condition: the
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

// AtSeq holds when the last event's seq is Seq: nothing at all has entered
// the store since a snapshot read at Seq.
type AtSeq struct{ Seq int64 }

func (CardGen) isExpect()     {}
func (CardState) isExpect()   {}
func (CardHolder) isExpect()  {}
func (NeedsLanded) isExpect() {}
func (RowIs) isExpect()       {}
func (RowGen) isExpect()      {}
func (RowHeld) isExpect()     {}
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

// PutCard adds a card of a kind (producer, sentinel, work or read): refused
// when it exists. It starts waiting, gen 0, attempt 0. Its stream must exist;
// every need, and every card it comes after, must name another card of the
// same epoch, once. A producer or sentinel has no Of and no Holder; a
// sentinel has no Brief. A consumer (work or read) names its producer in Of
// (a producer of the same epoch; Of never changes after), has no needs and no
// after, and may be added with its Holder, which is the holder write in the
// same op and with the Attempt it belongs to (a producer's starts at 0).
// Prior head, bounces, cost and result are never set at add.
type PutCard struct {
	ID, Kind, Of, Stream string
	Score                int
	Attempt              int      // a consumer's attempt, set here and never after
	Needs                []string // hard: cannot start until these landed
	After                []string // soft: may start any time; orders its landing only behind targets that are themselves queued to land, never behind one that is not
	Brief                string
	Holder               string
	Priority             string // a producer's or sentinel's level; "" is normal; a consumer takes only the default ("" or normal)
}

// SetCardState sets a card's state. A held card moved into working counts
// against its holder's width: refused when the row already holds as many
// working cards as its width. No other state counts.
type SetCardState struct{ Card, State string }

// SetCardHolder sets a card's holder: a row's name, or "" for none. Refused
// on a producer or sentinel whatever the value ("" included), and, for a card
// in working, when that row already holds as many working cards as its width,
// counted at the moment of the write (earlier writes of the op included) and
// not counting the card written.
type SetCardHolder struct{ Card, Holder string }

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

// SetCardCost sets what a consumer's run cost, in micro-dollars, and whether
// its route had a price at all: one field, so an unpriced run is never $0.
// Refused on a producer or sentinel.
type SetCardCost struct {
	Card   string
	Cost   int64
	Priced bool
}

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

// PutRow inserts a row or overwrites its kind, width and session; state,
// flaps and held are kept on overwrite, and down, 0 and false on insert.
type PutRow struct {
	Name, Kind string
	Width      int
	Session    string
}

// SetRowWidth sets a row's width; a width below the working cards it holds
// is refused.
type SetRowWidth struct {
	Row   string
	Width int
}

// SetRowState sets a row's state.
type SetRowState struct{ Row, State string }

// SetRowSession sets a row's session.
type SetRowSession struct{ Row, Session string }

// SetRowHeld sets a row's held flag: the coordinator's hold, no deals to the
// row while true, whatever its state.
type SetRowHeld struct {
	Row  string
	Held bool
}

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

func (PutStream) isWrite()        {}
func (SetStreamHeld) isWrite()    {}
func (PutCard) isWrite()          {}
func (SetCardState) isWrite()     {}
func (SetCardHolder) isWrite()    {}
func (SetCardResult) isWrite()    {}
func (SetRowWidth) isWrite()      {}
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
func (SetCardCost) isWrite()      {}
func (SetCardPriority) isWrite()  {}
func (PutRow) isWrite()           {}
func (SetRowHeld) isWrite()       {}
func (SetRowState) isWrite()      {}
func (SetRowSession) isWrite()    {}
func (SetRowFlaps) isWrite()      {}
func (PutPolicy) isWrite()        {}
func (SetActivePolicy) isWrite()  {}

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
		return of("card", v.ID, "kind", "of", "stream", "attempt", "score", "needs", "after", "brief", "holder", "priority"), true
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
	case SetCardResult:
		return v.Card, "result", func(c *Card) { c.Result = v.Result }, true
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
	case SetCardAfter:
		return v.Card, "after", func(c *Card) { c.After = v.After }, true
	case SetCardPriorHead:
		return v.Card, "prior_head", func(c *Card) { c.Prior = v.Head }, true
	case SetCardBounces:
		return v.Card, "bounces", func(c *Card) { c.Bounces = v.Bounces }, true
	case SetCardCost:
		return v.Card, "cost", func(c *Card) { c.Cost, c.Priced = v.Cost, v.Priced }, true
	case SetCardPriority:
		return v.Card, "priority", func(c *Card) { c.Priority = v.Priority }, true
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
	case SetRowWidth:
		return v.Row, "width", func(r *Row) { r.Width = v.Width }, true
	case SetRowHeld:
		return v.Row, "held", func(r *Row) { r.Held = v.Held }, true
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
			v.Needs, v.After = needs(v.Needs), needs(v.After)
			op.Writes[i] = v
		case SetCardNeeds:
			v.Needs = needs(v.Needs)
			op.Writes[i] = v
		case SetCardAfter:
			v.After = needs(v.After)
			op.Writes[i] = v
		}
	}
	return op
}

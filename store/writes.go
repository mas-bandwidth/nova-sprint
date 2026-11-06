package store

import (
	"cmp"
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
	case PutJudgement:
		return w.openJudgement(v)
	case AnswerJudgement:
		return w.closeJudgement(v.ID, "answered", v.Answer, cmp.Or(v.Actor, w.op.Actor))
	case ExpireJudgement:
		return w.closeJudgement(v.ID, "expired", v.Reason, w.op.Actor)
	case SetCursor:
		return w.writeCursor(v)
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
	if r := cmp.Or(oneOf("row kind", v.Kind, RowKinds), checkTiers(v.Name, v.Tiers)); r != "" {
		return r, nil
	}
	old, ok, err := w.tx.row(v.Name)
	if err != nil {
		return "", err
	}
	next := Row{Name: v.Name, Kind: v.Kind, Width: v.Width, Session: v.Session, State: "down", Tiers: v.Tiers, Model: v.Model, Harness: v.Harness}
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
	n, err := w.tx.working(row)
	return when(err == nil && n > width, "row %s holds %s, more than width %d; wait for a card to finish before narrowing it", row, workingCards(n), width), err
}

// checkTiers refuses a tier outside the four, or one named twice.
func checkTiers(row string, tiers []string) string {
	for i, t := range tiers {
		if r := oneOf("row tier", t, Tiers); r != "" {
			return r
		}
		if slices.Contains(tiers[:i], t) {
			return fmt.Sprintf("row %s names tier %s twice", row, t)
		}
	}
	return ""
}

func workingCards(n int) string {
	if n == 1 {
		return "1 working card"
	}
	return fmt.Sprintf("%d working cards", n)
}

// writeRow is the set-row writes (rowSet).
func (w *work) writeRow(wr Write) (string, error) {
	name, _, set, _ := rowSet(wr)
	switch v := wr.(type) {
	case SetRowState:
		if r := oneOf("row state", v.State, RowStates); r != "" {
			return r, nil
		}
	case SetRowTiers:
		if r := checkTiers(v.Row, v.Tiers); r != "" {
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
// sets the gen, once per op. A kind-specific write is refused on the wrong
// kind before its value is looked at, a no-op value included, so it never
// bumps a gen or logs an accepted event.
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
	case SetCardPaths:
		r = onlyFor(card, "SetCardPaths", "a producer or sentinel (a consumer's copy is set at add)", !consumer(card.Kind))
	case SetCardDoneWhen:
		r = onlyFor(card, "SetCardDoneWhen", "a producer or sentinel (a consumer's copy is set at add)", !consumer(card.Kind))
	case SetCardProse:
		r = onlyFor(card, "SetCardProse", "a producer or sentinel (a consumer's copy is set at add)", !consumer(card.Kind))
	case SetCardResult:
		r = cmp.Or(onlyFor(card, "SetCardResult", "a consumer", consumer(card.Kind)), oneOf("card result", v.Result, CardResults))
	case SetCardPriorHead:
		r = onlyFor(card, "SetCardPriorHead", "a work consumer", card.Kind == "work")
	case SetCardBounces:
		r = onlyFor(card, "SetCardBounces", "a producer", card.Kind == "producer")
	case SetCardCost:
		r = onlyFor(card, "SetCardCost", "a consumer", consumer(card.Kind))
	case SetCardAttempt:
		r = onlyFor(card, "SetCardAttempt", "a producer or sentinel (a consumer's attempt is set at add)", !consumer(card.Kind))
	case SetCardPriority:
		r = cmp.Or(producersOnly(card), oneOf("card priority", v.Priority, Priorities))
	case SetCardNeeds:
		if r = onlyFor(card, "SetCardNeeds", "a producer or sentinel", !consumer(card.Kind)); r == "" {
			r, err = w.checkList(card, needList, v.Needs)
		}
	case SetCardAfter:
		if r = onlyFor(card, "SetCardAfter", "a producer or sentinel", !consumer(card.Kind)); r == "" {
			r, err = w.checkList(card, afterList, v.After)
		}
	}
	if r != "" || err != nil {
		return r, err
	}
	next := card
	set(&next)
	return "", w.putCard(card, true, next)
}

// producersOnly refuses a priority write on a consumer, whatever the value: a
// consumer is ordered by its producer's level, never its own.
func producersOnly(card Card) string {
	return when(consumer(card.Kind), "card %s is of kind %s; priority is a producer's", card.ID, card.Kind)
}

// onlyFor refuses a write on a card whose kind does not carry the column,
// naming the card's kind and the write.
func onlyFor(card Card, write, kinds string, ok bool) string {
	return when(!ok, "card %s is of kind %s; %s is only for %s", card.ID, card.Kind, write, kinds)
}

// consumer is true for the kinds a row is handed (work and read); the other
// kinds (producer and sentinel) are bookkeeping and are never held.
func consumer(kind string) bool { return kind == "work" || kind == "read" }

// checkHolder is the holder at add, the only time it is set: a producer or
// sentinel has none, and a consumer names a row of the epoch. The card is
// added waiting, so it takes no room; width is checked when it moves into
// working.
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
	c := Card{ID: v.ID, Kind: v.Kind, Of: v.Of, Stream: v.Stream, State: "waiting", Attempt: v.Attempt, Score: v.Score, Needs: v.Needs, After: v.After, Brief: v.Brief, Priority: cmp.Or(v.Priority, "normal"),
		Holder: v.Holder, Tier: v.Tier, Paths: v.Paths, DoneWhen: v.DoneWhen, Prose: v.Prose}
	if r := cmp.Or(oneOf("card priority", c.Priority, Priorities), oneOf("card tier", c.Tier, Tiers)); r != "" {
		return r, nil
	}
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
	if r, err := w.checkList(c, needList, v.Needs); r != "" || err != nil {
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
	if p.Kind != "producer" {
		return fmt.Sprintf("card %s is a consumer of %s, which is not a producer", c.ID, c.Of), nil
	}
	return when(p.Tier != c.Tier, "card %s is tier %s, its producer %s is tier %s; a consumer takes its producer's tier", c.ID, c.Tier, c.Of, p.Tier), nil
}

func (w *work) openJudgement(v PutJudgement) (string, error) {
	if v.ID == "" {
		return "a judgement needs an id", nil
	}
	if _, ok, err := w.tx.judgement(v.ID); ok || err != nil {
		return when(err == nil, "judgement %s exists", v.ID), err
	}
	j := Judgement{ID: v.ID, Kind: v.Kind, About: v.About, Question: v.Question, Command: v.Command, Group: v.Group, State: "open", Deadline: v.Deadline, OpenedAt: w.op.At}
	return "", w.putJudgement(Judgement{}, false, j)
}

// closeJudgement moves an open judgement to answered or expired.
func (w *work) closeJudgement(id, state, answer, actor string) (string, error) {
	j, r, err := w.mustJudgement(id)
	if r != "" || err != nil {
		return r, err
	}
	if j.State != "open" {
		return fmt.Sprintf("judgement %s is %s; only an open judgement can be %s", id, j.State, state), nil
	}
	next := j
	next.State, next.Answer, next.Actor, next.ClosedAt = state, answer, actor, w.op.At
	return "", w.putJudgement(j, true, next)
}

func (w *work) writeCursor(v SetCursor) (string, error) {
	if v.Name == "" {
		return "a cursor needs a name", nil
	}
	seq, ok, err := w.tx.cursor(v.Name)
	switch {
	case err != nil:
		return "", err
	case ok && v.Seq < seq:
		return fmt.Sprintf("cursor %s is at %d, cannot move back to %d", v.Name, seq, v.Seq), nil
	}
	return "", w.putCursor(v.Name, seq, ok, v.Seq)
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

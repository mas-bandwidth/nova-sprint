package store

import (
	"fmt"
	"slices"
	"time"
)

// The walk's op generator: ops over the walk's names, most aimed at what
// exists so the walk gets deep, many refused, now and then a clear or an op
// planned against another epoch.

func (w *storeWalk) pick(ss []string) string { return ss[w.rng.IntN(len(ss))] }

// next is the next op of the walk, and whether it is a clear.
func (w *storeWalk) next() (Op, bool) {
	s := w.snap()
	w.n++
	epoch := s.Epoch
	if w.rng.IntN(12) == 0 {
		epoch = max(1, epoch+w.rng.IntN(3)-1) // planned against another epoch, now and then
	}
	op := Op{ID: fmt.Sprintf("o%d", w.n), Epoch: epoch, Actor: "walk", Kind: "walk", At: t0.Add(time.Duration(w.n) * time.Second)}
	if w.rng.IntN(80) == 0 {
		op.Kind = "clear"
		return op, true
	}
	if _, ok := s.Streams["s1"]; !ok { // a new epoch starts with its stream
		op.Writes = append(op.Writes, PutStream{Name: "s1", Repo: "git@example.com:r.git", Base: "main", Pos: 1})
		return op, false
	}
	if w.rng.IntN(6) == 0 { // a deal: a consumer added and moved in one op
		put := w.putConsumer(s).(PutCard)
		op.Writes = append(op.Writes, put, SetCardState{Card: put.ID, State: w.pick([]string{"ready", "working"})})
		return op, false
	}
	for range 1 + w.rng.IntN(5)/3 { // one write, two now and then
		op.Writes = append(op.Writes, w.write(s))
	}
	for range w.rng.IntN(5) / 2 { // most ops are unfenced
		op.Expect = append(op.Expect, w.expect(s))
	}
	sameTier(op.Writes)
	return op, false
}

// sameTier gives a consumer the tier of its producer when the op itself
// puts the producer first: a consumer's tier is its producer's (the
// abstraction in trace.go).
func sameTier(ws []Write) {
	tier := map[string]string{}
	for i, x := range ws {
		if p, ok := x.(PutCard); ok {
			if t, put := tier[p.Of]; put && p.Of != "" {
				p.Tier = t
				ws[i] = p
			}
			tier[p.ID] = p.Tier
		}
	}
}

func (w *storeWalk) write(s Snapshot) Write {
	switch w.rng.IntN(16) {
	case 0, 1:
		return w.putCard(s)
	case 2, 3, 4, 5:
		return w.setState(s)
	case 6:
		return SetCardRed{Card: w.some(s, twCards), Red: w.rng.IntN(2) == 0}
	case 7, 8, 9:
		return w.rowWrite(s)
	case 10, 11:
		return w.judgementWrite(s)
	case 12:
		return w.cursorWrite(s)
	case 13:
		if w.rng.IntN(3) == 0 {
			return SetStopped{Stopped: w.rng.IntN(2) == 0, Reason: "walk"}
		}
		return PutPolicy{Kind: "data", Name: "rules", Body: w.pick(twBodies)}
	}
	return w.putConsumer(s)
}

func (w *storeWalk) putCard(s Snapshot) Write {
	var needs []string
	for range w.rng.IntN(3) {
		if n := w.pick(twCards[:3]); !slices.Contains(needs, n) { // a set: the model has no list with a repeat
			needs = append(needs, n)
		}
	}
	if w.rng.IntN(4) == 0 {
		return PutCard{ID: "t1", Kind: "sentinel", Stream: "s1", Tier: "pro", Needs: needs}
	}
	return PutCard{ID: w.pick(twProducers), Kind: "producer", Stream: "s1", Tier: w.pick(twTiers), Needs: needs}
}

// putConsumer adds a consumer with its producer's tier (the store refuses
// another, and the model takes the producer's): the abstraction in trace.go.
func (w *storeWalk) putConsumer(s Snapshot) Write {
	of := w.pick(twCards[:3])
	tier := "pro"
	if p, ok := s.Cards[of]; ok {
		tier = p.Tier
	}
	holder := w.pick(twRows)
	if w.rng.IntN(10) == 0 {
		holder = ""
	}
	return PutCard{ID: w.pick(twCards[3:]), Kind: w.pick([]string{"work", "read"}), Of: of, Stream: "s1", Tier: tier, Holder: holder}
}

// setState moves a card, most often a consumer into ready, working or done.
func (w *storeWalk) setState(s Snapshot) Write {
	if w.rng.IntN(3) > 0 {
		return SetCardState{Card: w.some(s, twCards[3:]), State: w.pick([]string{"ready", "working", "working", "done"})}
	}
	return SetCardState{Card: w.some(s, twCards), State: w.pick(CardStates)}
}

// some is one of names, most often one that exists in s.
func (w *storeWalk) some(s Snapshot, names []string) string {
	var in []string
	for _, n := range names {
		if _, ok := s.Cards[n]; ok {
			in = append(in, n)
		}
	}
	if len(in) > 0 && w.rng.IntN(5) > 0 {
		return w.pick(in)
	}
	return w.pick(names)
}

func (w *storeWalk) rowWrite(s Snapshot) Write {
	r := w.pick(twRows)
	if _, ok := s.Rows[r]; !ok && w.rng.IntN(4) > 0 {
		return w.hook(r)
	}
	switch w.rng.IntN(4) {
	case 0:
		return w.hook(r)
	case 1, 2:
		return SetRowState{Row: r, State: w.pick(RowStates)}
	}
	return SetRowHeld{Row: r, Held: w.rng.IntN(2) == 0, Reason: "h"}
}

// hook is the sprint hook's write on row r: a session of the model's (x,
// y) with a role, naming no session it replaces, or x or y (the row's
// holder or not, as the pick falls), or unhook.
func (w *storeWalk) hook(r string) Write {
	if w.rng.IntN(3) == 0 {
		return PutRow{Name: r}
	}
	return PutRow{Name: r, Session: w.pick([]string{"x", "y"}), Role: w.pick(RowRoles), Replaces: w.pick([]string{"", "", "x", "y"})}
}

func (w *storeWalk) judgementWrite(s Snapshot) Write {
	j := w.pick(twJudgements)
	_, exists := s.Judgements[j]
	switch w.rng.IntN(3) {
	case 0:
		return PutJudgement{ID: j, Kind: "fix", About: "p1", Question: "q", Command: "c"}
	case 1:
		if !exists {
			return PutJudgement{ID: j, Kind: "fix", About: "p1", Question: "q", Command: "c"}
		}
		return AnswerJudgement{ID: j, Answer: "yes"}
	}
	return ExpireJudgement{ID: j, Reason: "late"}
}

// cursorWrite moves c1 around the log's end, 0 on a cursor never set
// included (refused as unchanged since round 22's ruling on cursor zero).
func (w *storeWalk) cursorWrite(s Snapshot) Write {
	seq := max(0, s.Cursors["c1"]+int64(w.rng.IntN(4))-1)
	if w.rng.IntN(2) == 0 {
		seq = max(0, s.Seq+int64(w.rng.IntN(3))-1)
	}
	return SetCursor{Name: "c1", Seq: seq}
}

// expect is a condition, most often one that holds now.
func (w *storeWalk) expect(s Snapshot) Expect {
	c, r := w.pick(twCards), w.pick(twRows)
	card, row := s.Cards[c], s.Rows[r]
	holds := w.rng.IntN(4) > 0
	bump := 0
	if !holds {
		bump = 1
	}
	switch w.rng.IntN(10) {
	case 0:
		return CardGen{Card: c, Gen: card.Gen + bump}
	case 1:
		return CardState{Card: c, State: map[bool]string{true: card.State, false: w.pick(CardStates)}[holds]}
	case 2:
		return CardHolder{Card: c, Holder: map[bool]string{true: card.Holder, false: w.pick(twRows)}[holds]}
	case 3:
		return NeedsLanded{Card: c}
	case 4:
		return CardAbsent{Card: c}
	case 5:
		return RowIs{Row: r, State: map[bool]string{true: row.State, false: w.pick(RowStates)}[holds]}
	case 6:
		return RowGen{Row: r, Gen: row.Gen + bump}
	case 7:
		return RowHeld{Row: r, Held: row.Held == holds}
	case 8:
		return JudgementOpen{ID: w.pick(twJudgements)}
	}
	return CardRed{Card: c, Red: card.Red == holds}
}

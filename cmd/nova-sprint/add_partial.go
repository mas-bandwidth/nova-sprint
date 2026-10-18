package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// addPartialReq is the many-brief add's own state cmdAddPartial admits from: the
// cards already read and their ids, the rules they are linted with, and the
// flags the write and the brief decision need.
type addPartialReq struct {
	stream        string
	cards         []sprint.CardAdd
	rules         ruleSet
	allowShared   bool
	allowPersonal bool
	score         string
	before, after string
	held          bool
	notDup        map[string]string
	decideRecord  string
	briefOps      []string
	replaces      []string
	c             *common
}

// cmdAddPartial is add --partial: it runs the brief check collector
// (brief_check.go) over every brief of the call, holds back every brief with a
// finding and every card that needs a held card through its Needs: or
// DEPENDS-ON: line or --needs, transitively, then runs the add step's own plan
// over the rest, holds back each card its Refused list names with the cards
// that need it, and plans the write again without them; the rounds are bounded
// by the number of cards, and nothing is written for a held card
// (docs/SPEC-SPRINT.md section 7, the add; docs/CLI.md). The rest are admitted
// in one store write, the brief decision asked for them only.
func (a *app) cmdAddPartial(q addPartialReq, stdout, stderr io.Writer) int {
	for i := range q.cards {
		q.cards[i].Rules = cardRules(q.cards[i].Brief, q.rules).held // each card names the rules the member injects into it
	}
	items := make([]briefItem, len(q.cards))
	for i, cd := range q.cards {
		items[i] = briefItem{file: cd.File, id: cd.ID, text: cd.Brief, card: cd}
	}
	st, err := a.store(*q.c)
	if err != nil {
		return refuse(stderr, "add", err.Error())
	}
	findings, err := a.partialFindings(st, items, q.stream, q.rules, q.allowShared, q.c)
	if err != nil {
		// the store did not answer every check: with a card's checks half run,
		// partial cannot say what may be admitted, so it writes nothing
		return refuse(stderr, "add", "the store did not answer the checks: "+err.Error())
	}
	// byFile maps a finding back to the card its file names: the lint findings
	// carry the file alone
	byFile := map[string]string{}
	for _, it := range items {
		byFile[it.file] = it.id
	}
	// held is the cards held and why, heldNeed the card each of them waits on:
	// the first finding a brief has, else a plan refusal, else its need
	held := map[string]string{}
	heldNeed := map[string]string{}
	for _, f := range findings {
		id := f.ID
		if id == "" {
			id = byFile[f.File]
		}
		if _, ok := held[id]; !ok {
			held[id] = f.Why
		}
	}
	// a brief that probable-duplicates an open card is held back as add holds
	// it, against the open cards of one read; the override add's flag takes is
	// honoured the same way, and its reason recorded on the card's note below
	var dupReasons map[string]string
	var wave []dupCard
	for _, cd := range q.cards {
		if cd.Sentinel || cd.Brief == "" {
			continue
		}
		wave = append(wave, dupCardOf(cd.ID, cd.Brief))
	}
	if len(wave) > 0 {
		snap, err := st.Load(context.Background(), []string{sprint.Work}, nil)
		if err != nil {
			return refuse(stderr, "add", "the store did not answer the duplicate check: "+err.Error())
		}
		dupHeldMap, reasons := dupHeld(wave, dupOpenCards(snap), q.notDup)
		for id, f := range dupHeldMap {
			if _, ok := held[id]; !ok {
				held[id] = f.String()
			}
		}
		dupReasons = reasons
	}
	heldClosure(q.cards, held, heldNeed)

	var at *float64
	if q.score != "" {
		f, err := strconv.ParseFloat(q.score, 64)
		if err != nil {
			return refuse(stderr, "add", "--score wants a number")
		}
		at = &f
	}
	// The add step's own plan is run over the rest; a card its Refused list
	// names is held with the cards that need it, and the write planned again
	// without them. Each round holds at least one more card, so the rounds are
	// bounded by the number of cards.
	for round := 0; round <= len(q.cards); round++ {
		admitted := notHeld(q.cards, held)
		if len(admitted) == 0 {
			break
		}
		r := partialReq(q, admitted, at)
		step := promotionGuard(store.AddStep(r), []sprint.AddReq{r}, "")
		snap, err := st.Load(context.Background(), step.Load, step.Extras)
		if err != nil {
			return refuse(stderr, "add", "the store did not answer the plan: "+err.Error())
		}
		refused := step.Plan(snap).Refused
		if len(refused) == 0 {
			break
		}
		added := false
		for _, ref := range refused {
			if _, ok := held[ref.Key]; !ok {
				held[ref.Key] = ref.Why
				added = true
			}
		}
		if !added {
			break
		}
		heldClosure(q.cards, held, heldNeed)
	}
	admitted := notHeld(q.cards, held)
	if len(admitted) == 0 {
		printHeld(q.cards, held, heldNeed, stdout)
		return 2
	}
	briefs := map[string]string{}
	for _, cd := range admitted {
		briefs[cd.ID] = cd.Brief
	}
	asked, code := a.briefGate(briefs, q.decideRecord, q.briefOps, q.c, stdout, stderr)
	if code != 0 {
		return code
	}
	if a.gateOnly != nil {
		return 0 // the served add's checks and brief decisions ran here; the server writes
	}
	r := partialReq(q, admitted, at)
	r.BriefOps, r.BriefRecord, r.BriefUsage = asked.ops, asked.record, asked.usage
	q.c.addStream = q.stream
	q.c.addBefore = q.before
	printHeld(q.cards, held, heldNeed, stdout)
	code = a.runStep("add", *q.c, st, dupNoteStep(promotionGuard(store.AddStep(r), []sprint.AddReq{r}, ""), dupReasons), stdout, stderr)
	if code == 0 && len(held) > 0 {
		return 1
	}
	return code
}

// partialReq is the add request of the admitted cards.
func partialReq(q addPartialReq, admitted []sprint.CardAdd, at *float64) sprint.AddReq {
	return sprint.AddReq{Stream: q.stream, Cards: admitted, Who: q.c.actor, Before: q.before, After: q.after, Held: q.held, Score: at, Replaces: q.replaces}
}

// heldClosure holds every card that needs a held card, transitively, naming the
// need it waits on (docs/SPEC-SPRINT.md section 7, the add).
func heldClosure(cards []sprint.CardAdd, held map[string]string, heldNeed map[string]string) {
	for changed := true; changed; {
		changed = false
		for _, cd := range cards {
			if _, ok := held[cd.ID]; ok {
				continue
			}
			for _, n := range cd.Needs {
				if _, ok := held[n]; ok {
					held[cd.ID], heldNeed[cd.ID] = "", n
					changed = true
					break
				}
			}
		}
	}
}

// notHeld is the cards of the call no held card holds back, in order.
func notHeld(cards []sprint.CardAdd, held map[string]string) []sprint.CardAdd {
	var out []sprint.CardAdd
	for _, cd := range cards {
		if _, ok := held[cd.ID]; !ok {
			out = append(out, cd)
		}
	}
	return out
}

// printHeld is one line per held card, in the call's order: its first finding
// or refusal, or the need it waits on.
func printHeld(cards []sprint.CardAdd, held, heldNeed map[string]string, stdout io.Writer) {
	for _, cd := range cards {
		why, ok := held[cd.ID]
		if !ok {
			continue
		}
		if n := heldNeed[cd.ID]; n != "" {
			fmt.Fprintf(stdout, "HELD %s: needs %s, held back\n", cd.ID, n)
			continue
		}
		fmt.Fprintf(stdout, "HELD %s: %s\n", cd.ID, why)
	}
}

// partialFindings runs the collector over every brief of the add: the shared
// PATHS refusal attributed to the first brief, the card lint per brief, then
// every store check add runs (briefStoreChecks, brief_check.go). It reads only.
func (a *app) partialFindings(st *store.Store, items []briefItem, stream string, rs ruleSet, allowShared bool, c *common) ([]briefFinding, error) {
	// the id and size bound first, as brief check runs them: each brief the
	// checks fault is held back, never refused whole (briefIDSizeFindings,
	// brief_check.go)
	var findings []briefFinding
	for _, it := range items {
		findings = append(findings, briefIDSizeFindings(it.file, it.id, it.text)...)
	}
	if !allowShared {
		if why := sharedPaths(briefCardsOf(items)); why != "" {
			findings = append(findings, briefFinding{File: items[0].file, ID: items[0].id, Check: "shared-paths", Why: why, order: chkShared})
		}
	}
	for _, it := range items {
		var lintBuf bytes.Buffer
		lintBriefFiles("add", []sprint.CardAdd{it.card}, rs, c.max, &lintBuf)
		findings = append(findings, lintFindings(lintBuf.String())...)
	}
	storeFindings, err := a.briefStoreChecks(st, items, stream, c)
	if err != nil {
		return nil, err
	}
	return append(findings, storeFindings...), nil
}

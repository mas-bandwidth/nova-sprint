package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// cmdAddPartial is add --partial over the many-brief form: it runs the brief
// check's collector over every brief, holds back every brief with a finding and
// every card that needs a held card (transitively), and admits the rest in one
// store write (docs/CLI.md; docs/SPEC-SPRINT.md section 7, the add). It prints
// one HELD line per held card and then add's own closing line for what it
// admitted; exit 0 when every card was admitted, 1 when some were held and the
// rest written, 2 when none could be admitted or for a usage refusal.
func (a *app) cmdAddPartial(stream string, files []string, extra []string, rs ruleSet, st *store.Store, score, before, after string, heldFlag, allowShared bool, decideRecord string, briefOps, replaces []string, c *common, stdout, stderr io.Writer) int {
	// brief items and the store-free findings the brief check names first: the
	// id, the size bound, then the card lint per brief (brief_check.go, the
	// collector cmdBriefCheck runs; the checks themselves are its functions,
	// not a second copy)
	var items []briefItem
	var findings []briefFinding
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".md")
		text, err := readTextFile(path, briefReadCap)
		if err != nil {
			return refuse(stderr, "add", fmt.Sprintf("%s: %v", path, err))
		}
		briefText := strings.TrimSuffix(text, "\n")
		if why := reservedCardID(id); why != "" {
			findings = append(findings, briefFinding{File: path, ID: id, Check: "id", Why: why, order: chkID})
		} else if !sprint.ValidID(id) {
			findings = append(findings, briefFinding{File: path, ID: id, Check: "id",
				Why: fmt.Sprintf("%q is not a card id (letters, digits, _ and -): the id is the file's base name without .md", id), order: chkID})
		}
		if len(briefText) > store.MaxBriefBytes {
			findings = append(findings, briefFinding{File: path, ID: id, Check: "size",
				Why: fmt.Sprintf("the brief is %d bytes, over the %d bytes a brief may be", len(briefText), store.MaxBriefBytes), order: chkSize})
		}
		cb := swarm.ReadCardBase([]byte(briefText))
		items = append(items, briefItem{file: path, id: id, text: briefText, card: sprint.CardAdd{
			ID: id, Brief: briefText, Needs: uniquify(append(briefNeeds(briefText), extra...)), File: path,
			Base: cb.Ref, Repo: cb.Named,
		}})
	}
	cards := briefCardsOf(items)
	if !allowShared {
		if why := sharedPaths(cards); why != "" {
			findings = append(findings, briefFinding{File: items[0].file, ID: items[0].id, Check: "shared-paths", Why: why, order: chkShared})
		}
	}
	for _, card := range cards {
		var lintBuf bytes.Buffer
		lintBriefFiles("add", []sprint.CardAdd{card}, rs, c.max, &lintBuf)
		findings = append(findings, lintFindings(lintBuf.String())...)
	}
	// the store checks and the add step's own plan, read-only, every finding
	// collected (briefStoreChecks; the step is never run here)
	storeFindings, err := a.briefStoreChecks(st, items, stream, c)
	if err != nil {
		return a.readFailed("add", err, stderr)
	}
	findings = append(findings, storeFindings...)
	for i := range cards {
		cards[i].Rules = cardRules(cards[i].Brief, rs).held
	}
	reason := map[string]string{} // id -> the line HELD prints for it
	for _, f := range findings {
		if f.ID == "" {
			continue
		}
		if _, ok := reason[f.ID]; !ok {
			reason[f.ID] = f.Why
		}
	}
	var at *float64
	if score != "" {
		f, err := strconv.ParseFloat(score, 64)
		if err != nil {
			return refuse(stderr, "add", "--score wants a number")
		}
		at = &f
	}
	// hold a card that needs a held card, transitively (docs/SPEC-SPRINT.md
	// section 7: a pass admits what passes; a need of a held card waits with it)
	markHeldNeeds := func() {
		for changed := true; changed; {
			changed = false
			for _, cd := range cards {
				if _, ok := reason[cd.ID]; ok {
					continue
				}
				for _, n := range cd.Needs {
					if _, ok := reason[n]; ok {
						reason[cd.ID] = "needs " + n + ", held back"
						changed = true
						break
					}
				}
			}
		}
	}
	markHeldNeeds()
	admitted := func() []sprint.CardAdd {
		var out []sprint.CardAdd
		for _, cd := range cards {
			if _, ok := reason[cd.ID]; !ok {
				out = append(out, cd)
			}
		}
		return out
	}
	// a card the add step's own plan refuses at the write is held the same way,
	// with the cards that need it, and the write is planned again without them;
	// the rounds are bounded by the number of cards
	for round := 0; round <= len(cards); round++ {
		adm := admitted()
		if len(adm) == 0 {
			break
		}
		r := sprint.AddReq{Stream: stream, Cards: adm, Who: c.actor, Before: before, After: after, Held: heldFlag, Score: at, Replaces: replaces}
		step := promotionGuard(store.AddStep(r), []sprint.AddReq{r}, "")
		snap, err := st.Load(context.Background(), step.Load, step.Extras)
		if err != nil {
			return a.readFailed("add", err, stderr)
		}
		refused := step.Plan(snap).Refused
		if len(refused) == 0 {
			break
		}
		changed := false
		for _, ref := range refused {
			if ref.Key == "" {
				continue
			}
			if _, ok := reason[ref.Key]; !ok {
				reason[ref.Key] = ref.Why
				changed = true
			}
		}
		if !changed {
			break
		}
		markHeldNeeds()
	}
	// one line per held card, in file order (docs/CLI.md: add --partial)
	held := 0
	for _, cd := range cards {
		if why, ok := reason[cd.ID]; ok {
			fmt.Fprintf(stdout, "HELD %s: %s\n", oneline.Field(cd.ID), oneline.Escape(why))
			held++
		}
	}
	adm := admitted()
	if len(adm) == 0 {
		return 2 // none could be admitted
	}
	briefs := map[string]string{}
	for _, cd := range adm {
		briefs[cd.ID] = cd.Brief
	}
	asked, code := a.briefGate(briefs, decideRecord, briefOps, c, stdout, stderr)
	if code != 0 || a.gateOnly != nil {
		return code // refused; or the served add's checks ran and the server writes
	}
	c.says = append(c.says, fmt.Sprintf("each card's id is its brief file's name without .md (%s is %s)", files[0], cards[0].ID))
	for _, cd := range adm {
		c.says = append(c.says, unfilledSays("the brief of "+cd.ID, cd.Brief)...)
	}
	c.addStream = stream
	c.addBefore = before
	r := sprint.AddReq{Stream: stream, Cards: adm, Who: c.actor, Before: before, After: after, Held: heldFlag, Score: at,
		BriefOps: asked.ops, BriefRecord: asked.record, BriefUsage: asked.usage, Replaces: replaces}
	if code := a.runStep("add", *c, st, promotionGuard(store.AddStep(r), []sprint.AddReq{r}, ""), stdout, stderr); code != 0 {
		return code
	}
	if held > 0 {
		return 1 // some were held and the rest written
	}
	return 0
}

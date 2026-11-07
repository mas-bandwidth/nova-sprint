package verbs

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// checkPolicy is the policy line: a version active, and it answering the
// questions the tick asks of the ready producers (order and rows) without
// a failure. With none active the tick runs on the built-in default, which
// is how a fleet once worked a night on defaults with nothing dealt
// : red, naming up, which stores and activates the default.
// It returns benchModelCheck's refusal, also a red line, for up to name.
func (e *Engine) checkPolicy(ctx context.Context, r *checkReport, snap store.Snapshot) *Fail {
	if snap.Policy == 0 {
		r.red(refuse("no policy is active: the tick runs on the built-in default", "nova-sprint up --as <coordinator> (its step 4 stores and activates the default), or nova-sprint policy activate <version> --reason \"<why>\""))
		return nil
	}
	if e.Answers == nil || e.Store == nil {
		r.red(broken("the policy engine is not configured", "take it to the project's own tracker with this line"))
		return nil
	}
	pv, err := e.Store.Policy(ctx, snap.Policy)
	if err != nil {
		r.red(RefusalOr(err, broken(fmt.Sprintf("policy v%d cannot be read (%s)", snap.Policy, errText(err)), "nova-sprint policy show --list")))
		return nil
	}
	lim, _, err := e.rules(ctx, snap)
	if err != nil {
		r.red(err)
		return nil
	}
	ro, err := e.roster(ctx, lim, snap)
	if err != nil {
		r.red(err)
		return nil
	}
	q := policy.Questions{}
	for _, c := range snap.Cards {
		if c.Kind == "producer" && c.State == "ready" {
			q.Order = append(q.Order, c.ID)
			q.Rows = append(q.Rows, policy.RowsQuestion{Card: c.ID, Kind: "work"})
		}
	}
	slices.Sort(q.Order)
	a, failures := e.Answers.AnswerContext(ctx, policy.Versions{Seq: snap.Seq, Active: &pv}, snap, ro, q)
	if len(failures) > 0 {
		r.red(refuse(fmt.Sprintf("policy v%d fails %d question(s); the first: %s", snap.Policy, len(failures), clipN(failures[0].Error(), 200)),
			fmt.Sprintf("nova-sprint policy check v%d, then fix the file and nova-sprint policy set <file> --reason \"<why>\"", snap.Policy)))
		return nil
	}
	r.ok("policy v%d active (answered by v%d; %d ready producers asked)", snap.Policy, a.Version, len(q.Order))
	f := benchModelCheck(snap.Policy, a.Catalog, ro)
	if f != nil {
		r.red(f)
	}
	return f
}

// benchModelCheck is red when a bench row serves a tier the policy's
// catalog names no bench model for: the deal names a model, so no card of
// that tier is ever dealt to a bench (a policy stored before the catalog
// had a bench). nil when every served tier has one.
func benchModelCheck(version int, cat policy.Catalog, ro roster.Roster) *Fail {
	var missing []string
	for _, w := range ro.Workers {
		for _, t := range w.Tiers {
			if cat.BenchModel(t) == "" && !slices.Contains(missing, t) {
				missing = append(missing, t)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return refuse(fmt.Sprintf("policy v%d names no bench model for tier %s, so no bench is dealt a card of it", version, strings.Join(missing, ", ")),
		"give the policy's catalog() a \"bench\": {\"<tier>\": \"<provider/model>\"} entry for each, then nova-sprint policy set <file> --reason \"<why>\"")
}

// checkCards re-proves each producer not yet dealt (waiting or ready)
// as add admitted it (admitCards): its stream live, tier and routes served,
// paths at the base, DONE-WHEN as one command, no retired mechanic, its work not
// at the base. A restore or an old add is held to today's rule here.
func (e *Engine) checkCards(ctx context.Context, r *checkReport, snap store.Snapshot) {
	var facts []cardFacts
	for _, c := range snap.Cards {
		if c.Kind == "producer" && (c.State == "waiting" || c.State == "ready") {
			facts = append(facts, factsOfCard(c))
		}
	}
	if len(facts) == 0 {
		r.ok("cards: none waiting or ready")
		return
	}
	refused, err := e.admitCards(ctx, snap, facts)
	if err != nil {
		r.red(err)
		return
	}
	ids := slices.Sorted(maps.Keys(refused))
	for _, id := range ids[:min(len(ids), 5)] {
		r.red(refused[id])
	}
	if len(ids) > 5 {
		r.red(refuse(fmt.Sprintf("%d more waiting or ready cards fail admission (%s)", len(ids)-5, firstN(ids[5:], 5)), "nova-sprint check lists five at a time: fix those, then check again"))
	}
	if len(ids) == 0 {
		r.ok("cards: %d waiting or ready, each admitted (stream live, tier served, paths at base, gates parse, not at base)", len(facts))
	}
}

// checkWorking is each working consumer's job: the store records the
// holder row, its hook and its state, and the bus its presence, so a card
// working on a row that is unhooked, down or off the bus has no live job
// and is red. What the store cannot tell: a job dead on a hooked, present
// bench (a worker holds a job on its disk; no per-job heartbeat reaches the
// store), which only the bench's worker log shows.
func checkWorking(r *checkReport, snap store.Snapshot, who map[string]bus.Presence) {
	var dead, n []string
	for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
		c := snap.Cards[id]
		if (c.Kind != "work" && c.Kind != "read") || c.State != "working" {
			continue
		}
		n = append(n, id)
		row := snap.Rows[c.Holder]
		on, known := present(who, c.Holder)
		switch {
		case row.Session == "":
			dead = append(dead, id+" on "+c.Holder+" (not hooked)")
		case row.State != "up":
			dead = append(dead, id+" on "+c.Holder+" (row "+orWord(row.State, "absent")+")")
		case known && !on:
			dead = append(dead, id+" on "+c.Holder+" (not on the bus)")
		}
	}
	if len(dead) > 0 {
		r.red(refuse(fmt.Sprintf("%d working cards have no live job: %s", len(dead), firstN(dead, 5)),
			"bring the row back (its worker's log on the bench), or nova-sprint release <card>@<gen> --reason \"<why>\" each"))
		return
	}
	r.ok("working: %d cards, each on a hooked row that is up and on the bus (a job dead on its bench is not visible here: no per-job heartbeat reaches the store)", len(n))
}

// admissionWhat is a stream's admission refusal as check's red line: the
// refusal, named for the stream.
func admissionWhat(name string, err error) *Fail {
	f := *classify(err)
	if !strings.HasPrefix(f.What, "stream "+name) {
		f.What = "stream " + name + ": " + f.What
	}
	return &f
}

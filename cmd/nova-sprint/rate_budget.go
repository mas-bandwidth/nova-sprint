package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/ratebudget"
)

// THE GLOBAL RATE LIMITER'S VERBS (sprint/rate_budget.go, pkg/ratebudget,
// tla/RateBudget.tla): the coordinator sets a budget (routes limit), and every caller of a
// limited route takes it before each model request through the server (budget take, then
// budget release when the request ends). A take never fails its caller: a budget that
// cannot be read is granted open, with the words why.

func init() {
	verbClasses["routes limit"] = classCoordinator
	verbClasses["budget take"] = classWorker
	verbClasses["budget release"] = classWorker
	verbClasses["budget renew"] = classWorker
	verbEffect["routes limit"] = "local write: sets a model's requests-per-minute budget (--rpm, on a route: its model) or a provider's concurrency budget (--concurrent, on a provider or a route: its provider) in the sprint's store, 0 clearing it; every member, reader and lane takes it before each model request, and the deal falls back past a full provider; --dry-run writes nothing"
	verbEffect["budget take"] = "local write: takes one grant of the model's global requests-per-minute window and one of its provider's concurrency slots for the holder, or says how long to wait; writes no table (a worker's, before each model request)"
	verbEffect["budget renew"] = "local write: renews the holder's concurrency slot of the model's provider while its request runs; takes no rpm grant; writes no table (a worker's, every LeaseTTL/3 of a long request)"
	verbEffect["budget release"] = "local write: frees the holder's concurrency slot of the model's provider; writes no table (a worker's, when a model request ends)"
	stepDryRun["routes limit"] = true
}

// cmdRoutesLimit is routes limit <provider|route> (--rpm <n> | --concurrent <n>) --reason
// <text>: the coordinator's budget, 0 clearing it.
func (a *app) cmdRoutesLimit(args []string, stdout, stderr io.Writer) int {
	const name = "routes limit"
	fs, c := a.verbSetup(name)
	rpm := fs.Int("rpm", -1, "the route's model's requests per minute across every machine (0 clears it)")
	concurrent := fs.Int("concurrent", -1, "the provider's requests and cards in flight across every machine (0 clears it)")
	reason := fs.String("reason", "", "why the budget is set, in a few words (required)")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 {
		return refuse(stderr, name, argErr("wants one word, a provider or a route as nova-sprint routes names it, ", err, pos...))
	}
	req := sprint.RouteLimitReq{Target: pos[0], Reason: *reason, Who: c.actor}
	switch {
	case *rpm >= 0 && *concurrent >= 0:
		return refuse(stderr, name, "give --rpm <n> or --concurrent <n>, not both: run it once for each")
	case *rpm >= 0:
		req.Set, req.RPM = "rpm", *rpm
	case *concurrent >= 0:
		req.Set, req.Concurrent = "concurrent", *concurrent
	default:
		return refuse(stderr, name, "give --rpm <n> (a route's model) or --concurrent <n> (a provider), 0 clearing it")
	}
	if strings.TrimSpace(*reason) == "" {
		return refuse(stderr, name, "--reason <text> is required: why the budget is set")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	return a.runStep(name, *c, st, store.RouteLimitStep(req), stdout, stderr)
}

// budgetWords is a budget take's or release's words: the model, --as <worker>, --holder
// <id>, and --json at most.
func (a *app) budgetWords(name string, args []string, stderr io.Writer) (model, holder string, c *common, ok bool) {
	fs, c := a.verbSetup(name)
	as := fs.String("as", "", "the worker taking: a member, a reader, or friend.<name> (required)")
	h := fs.String("holder", "", "the request's holder, unique to one model request (required)")
	pos, err := parse(fs, args)
	if err != nil || len(pos) != 1 || !strings.Contains(pos[0], "/") {
		refuse(stderr, name, argErr("wants one word, the provider/model the request is made to, ", err, pos...))
		return "", "", nil, false
	}
	if *as == "" || *h == "" {
		refuse(stderr, name, "--as <worker> and --holder <id> are required: who takes, and which request holds the slot")
		return "", "", nil, false
	}
	c.orActor(*as)
	return pos[0], *h, c, true
}

// cmdBudgetTake is budget take <provider/model> --as <worker> --holder <id>: one take of
// the model's global budget. It always exits 0 with an answer: granted, or the wait in
// milliseconds before asking again; a budget it cannot read is granted open (the owner,
// 2026-10-11: "i don't want this rate limiter causing errors").
func (a *app) cmdBudgetTake(args []string, stdout, stderr io.Writer) int {
	const name = "budget take"
	model, holder, c, ok := a.budgetWords(name, args, stderr)
	if !ok {
		return 2
	}
	answer := func(granted, open bool, wait time.Duration, rpm, concurrent int, why string) int {
		word := map[bool]string{true: "yes", false: "no"}
		line := fmt.Sprintf("BUDGET-TAKE OK model=%s granted=%s wait_ms=%d rpm=%d concurrent=%d open=%s", oneline.Field(model), word[granted], wait.Milliseconds(), rpm, concurrent, word[open])
		if why != "" {
			line += " why=" + oneline.Field(why)
		}
		sayOK(stdout, c.json, name, line, map[string]any{"model": model, "granted": granted, "wait_ms": wait.Milliseconds(), "rpm": rpm, "concurrent": concurrent, "open": open, "why": why})
		return 0
	}
	st, err := a.store(*c)
	if err != nil {
		return answer(true, true, 0, 0, 0, "the store could not be opened: "+err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), ratebudget.StoreTimeout)
	defer cancel()
	got, err := st.BudgetTake(ctx, model, holder, a.now())
	if err != nil {
		return answer(true, true, 0, got.RPM, got.Concurrent, "the budget could not be read: "+err.Error())
	}
	return answer(got.Grant.OK, false, got.Grant.Wait, got.RPM, got.Concurrent, "")
}

// cmdBudgetRenew is budget renew <provider/model> --as <worker> --holder <id>: the request
// still runs; its slot's lease is renewed. It always exits 0: a renewal not written is a
// lease that runs out, which frees a slot early and never strands one.
func (a *app) cmdBudgetRenew(args []string, stdout, stderr io.Writer) int {
	const name = "budget renew"
	model, holder, c, ok := a.budgetWords(name, args, stderr)
	if !ok {
		return 2
	}
	why, held := "", true
	if st, err := a.store(*c); err != nil {
		why = "the store could not be opened: " + err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), ratebudget.StoreTimeout)
		defer cancel()
		g, err := st.BudgetRenew(ctx, model, holder, a.now())
		switch {
		case err != nil:
			why = "not renewed (" + err.Error() + ")"
		case !g.OK:
			held, why = false, "the slot was not held and the provider is at its budget"
		}
	}
	word := map[bool]string{true: "yes", false: "no"}
	line := "BUDGET-RENEW OK model=" + oneline.Field(model) + " held=" + word[held]
	if why != "" {
		line += " why=" + oneline.Field(why)
	}
	sayOK(stdout, c.json, name, line, map[string]any{"model": model, "held": held, "why": why})
	return 0
}

// cmdBudgetRelease is budget release <provider/model> --as <worker> --holder <id>: the
// request ended; its slot is free. It always exits 0: a slot not freed expires by itself.
func (a *app) cmdBudgetRelease(args []string, stdout, stderr io.Writer) int {
	const name = "budget release"
	model, holder, c, ok := a.budgetWords(name, args, stderr)
	if !ok {
		return 2
	}
	why := ""
	if st, err := a.store(*c); err != nil {
		why = "the store could not be opened: " + err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), ratebudget.StoreTimeout)
		defer cancel()
		if err := st.BudgetRelease(ctx, model, holder); err != nil {
			why = "not released (" + err.Error() + "): it expires by itself"
		}
	}
	line := "BUDGET-RELEASE OK model=" + oneline.Field(model)
	if why != "" {
		line += " why=" + oneline.Field(why)
	}
	sayOK(stdout, c.json, name, line, map[string]any{"model": model, "why": why})
	return 0
}

// limitsText is the budgets as routes prints them: one LIMIT line each, the rpm's use read
// from its window.
func limitsText(ctx context.Context, st *store.Store, s *sprint.Snapshot, now time.Time) []sprint.LimitRow {
	rows := sprint.Limits(s)
	for i := range rows {
		if rows[i].Kind == "rpm" {
			if n, err := st.BudgetUse(ctx, rows[i].Target, now); err == nil {
				rows[i].InUse = n
			}
		}
	}
	return rows
}

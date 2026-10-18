package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// repair's effect line: what the verb changes. It is not one store step (it
// finishes the pending operation the fence holds, a step cut short, and it
// returns rule-6 merging cards to review), and its --dry-run is its own, so
// the effect is stated here rather than stepdry.go.
func init() {
	verbEffect["repair"] = "store write: finishes the pending operation the fence holds (a step cut short), and returns each merging card that entered merging with ok reads from fewer different readers at its head than it needs (check rule 6) back to review with the reason recorded, so it is read again; --skipped <op>.skip appends the move line of each skipped entry the store holds as applied and closes the judgment when none is refused; --dry-run prints each move it would make and writes nothing"
}

// cmdRepair finishes the pending operation (if any) and returns rule-6 merging
// cards to review (internal/sprint/check.go, Rule6Merging). When it can do
// neither it refuses, naming the check that shows what is broken.
// tla/Land.tla Repair
func (a *app) cmdRepair(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("repair")
	fs.BoolVar(&c.dry, "dry-run", false, "print each move repair would make and write nothing")
	skipped := fs.String("skipped", "", "reconcile a tick drain repair's skip judgment, <op>.skip: restore the move line of each entry the store holds as applied, refuse by name each it does not, and close the judgment when nothing is refused or unverified and the log replays to every card (the v1.5.4 hotfix)")
	acceptProps := fs.Bool("accept-props", false, "with --skipped: settle the table properties the judgment lists at the values the store holds (each is printed); without it they are unverified and the judgment stays open")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "repair", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "repair", err.Error())
	}
	ctx := context.Background()
	if *skipped != "" {
		return a.repairSkipped(ctx, *c, st, *skipped, *acceptProps, stdout, stderr)
	}
	// rule 6 first: a merging card short of its reads is returned to review
	// for its missing read, whatever else is pending.
	snap, err := st.Load(ctx, store.All, nil)
	if err != nil {
		return a.readFailed("repair", err, stderr)
	}
	rule6 := sprint.Rule6Merging(snap)
	if len(rule6) > 0 {
		ids := make([]string, 0, len(rule6))
		for _, pr := range rule6 {
			ids = append(ids, pr.ID)
		}
		step := store.ReturnStep(sprint.ReturnReq{
			Sel:    sprint.Sel{IDs: ids},
			Reason: "rule 6: ok reads at its head from fewer readers than it needs; returned to review for its missing read",
			For:    sprint.ReturnedForReads,
			Who:    c.actor,
		})
		return a.runStep("repair", *c, st, step, stdout, stderr)
	}
	// no rule 6: the pending operation, if any, is the repair. --dry-run
	// reads the fence and says what it would finish, never finishing it.
	if c.dry {
		f, err := st.B.ReadFence(ctx)
		if err != nil {
			return a.readFailed("repair", err, stderr)
		}
		if f.Pending == nil {
			return a.refuseNothing(stderr)
		}
		sayOK(stdout, c.json, "repair", fmt.Sprintf("WOULD finish OPERATION %s verb=%s", oneline.Escape(f.Pending.ID), oneline.Escape(f.Pending.Verb)),
			map[string]any{"dry_run": true, "op": f.Pending.ID, "verb": f.Pending.Verb})
		return 0
	}
	rr, err := st.Repair(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s repair: %s\n", prog, oneline.WithRemedy(err.Error(), prog+" repair -h"))
		return 2
	}
	if len(rr) == 0 {
		return a.refuseNothing(stderr)
	}
	if c.json {
		b, _ := json.Marshal(map[string]any{"repaired": rr})
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	code := 0
	for _, r := range rr {
		fmt.Fprintf(stdout, "OPERATION %s verb=%s done=%s detail=%s\n", oneline.Escape(r.Op), oneline.Escape(r.Verb), r.Done, oneline.Escape(r.Detail))
		if r.Done == "open" {
			code = 1
		}
	}
	status := "OK"
	if code != 0 {
		status = "FAILED"
	}
	fmt.Fprintf(stdout, "REPAIR %s operations=%d\n", status, len(rr))
	return code
}

// refuseNothing is repair's refusal when it can do nothing: no pending
// operation and no card merging with fewer ok reads at its head than it
// needs. It names the reason and the remedy, never a bare OK with nothing
// done.
func (a *app) refuseNothing(stderr io.Writer) int {
	fmt.Fprintf(stderr, "%s repair: nothing to repair: no pending operation and no card merging with fewer ok reads at its head than it needs; run: %s check (a rule 6 card is returned to review here, for its missing read)\n", prog, prog)
	return 1
}

// repairSkipped is repair --skipped <op>.skip (store.ReconcileSkipped): one
// line per entry (RESTORE, SETTLED or REFUSED, by name, with why), then the
// summary. Exit 0 when nothing is refused and the judgment is closed (or, dry,
// would be), 1 when an entry is refused, 2 when the store did not answer.
// tla/RepairLog.tla Reconcile
func (a *app) repairSkipped(ctx context.Context, c common, st *store.Store, id string, acceptProps bool, stdout, stderr io.Writer) int {
	res, err := st.ReconcileSkipped(ctx, id, c.op, c.dry, acceptProps)
	if err != nil {
		code := 1
		if errors.Is(err, store.ErrUnknown) {
			code = 2
		}
		fmt.Fprintf(stderr, "%s repair: %s\n", prog, oneline.WithRemedy(err.Error(), prog+" repair -h"))
		return code
	}
	code := 0
	if res.Counts[store.SkipRefused]+res.Counts[store.SkipUnverified] > 0 || len(res.Rule13) > 0 {
		code = 1
	}
	if c.json {
		b, _ := json.Marshal(map[string]any{"skipped": res, "exit": code})
		fmt.Fprintln(stdout, string(b))
		return code
	}
	would := ""
	if res.Dry {
		would = "WOULD "
	}
	for _, e := range res.Entries {
		name := "card=" + oneline.Field(e.Card)
		if e.Prop != "" {
			name = "prop=" + oneline.Field(e.Prop) + " value=" + oneline.Field(e.Value)
		}
		word := strings.ToUpper(e.Outcome)
		if e.Outcome == store.SkipRestore || e.Outcome == store.SkipSettled {
			word = would + word
		}
		fmt.Fprintf(stdout, "%s %s table=%s from=%s to=%s why=%s\n", word, name, oneline.Field(e.Table), oneline.Field(e.Expected), oneline.Field(e.Found), oneline.Escape(e.Why))
	}
	for _, v := range res.Rule13 {
		fmt.Fprintf(stdout, "RULE13 %s\n", oneline.Escape(v))
	}
	status := "OK"
	if code != 0 {
		status = "REFUSED"
	}
	closed := "no"
	if res.Closed {
		closed = "yes"
	}
	fmt.Fprintf(stdout, "REPAIR SKIPPED %s %sjudgment=%s op=%s restored=%d settled=%d unverified=%d refused=%d rule13_after=%d closed=%s%s%s\n", status, would, oneline.Field(res.Judgment), oneline.Field(res.Op),
		res.Counts[store.SkipRestore], res.Counts[store.SkipSettled], res.Counts[store.SkipUnverified], res.Counts[store.SkipRefused], len(res.Rule13), closed,
		map[bool]string{true: " replay=yes", false: ""}[res.Replay], map[bool]string{true: " written=" + oneline.Field(res.Written), false: ""}[res.Written != ""])
	return code
}

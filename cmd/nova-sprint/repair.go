package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// repair's effect line: what the verb changes. It is not one store step (it
// finishes the pending operation the fence holds, a step cut short, and it
// returns rule-6 merging cards to review), and its --dry-run is its own, so
// the effect is stated here rather than stepdry.go. The rule-6 return is done
// first: a call with both a cut operation and a short merging card returns the
// card and leaves the operation for the next repair, so the line says "when
// none", never "and".
func init() {
	verbEffect["repair"] = "store write: returns each merging card that entered merging with ok reads from fewer different readers at its head than it needs (check rule 6) back to review for its missing read, with the reason recorded; when none, finishes the pending operation the fence holds (a step cut short); --dry-run prints each move it would make and writes nothing"
}

// cmdRepair finishes the pending operation (if any) and returns rule-6 merging
// cards to review (internal/sprint/check.go, Rule6Merging). When it can do
// neither it refuses, naming the check that shows what is broken, or, for a
// landed card short of its reads (which it does not return), the card and the
// remedy that works for a landed one.
func (a *app) cmdRepair(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("repair")
	fs.BoolVar(&c.dry, "dry-run", false, "print each move repair would make and write nothing")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "repair", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "repair", err.Error())
	}
	ctx := context.Background()
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
			return a.refuseNothing(snap, stderr)
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
		return a.refuseNothing(snap, stderr)
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
// operation and no card merging with fewer ok reads at its head than it needs.
// A landed card short of its reads is named with the remedy that works for it
// (card <id>, look at it), never check, which reports the same rule and changes
// nothing. Otherwise it names check, which shows what is broken, never a bare
// OK with nothing done.
func (a *app) refuseNothing(snap *sprint.Snapshot, stderr io.Writer) int {
	if landed := sprint.Rule6Landed(snap); len(landed) > 0 {
		ids := make([]string, 0, len(landed))
		for _, pr := range landed {
			ids = append(ids, pr.ID)
		}
		remedy := prog + " card " + ids[0]
		if len(ids) > 1 {
			remedy = prog + " card (look at " + strings.Join(ids, ", ") + ")"
		}
		fmt.Fprintf(stderr, "%s repair: nothing to repair: no pending operation and no card merging with fewer ok reads at its head than it needs; %s landed with fewer ok reads at its head than it landed on, which repair does not return (a landed card is never judged again); run: %s (look at the card)\n",
			prog, strings.Join(ids, ", "), remedy)
		return 1
	}
	fmt.Fprintf(stderr, "%s repair: nothing to repair: no pending operation and no card merging with fewer ok reads at its head than it needs; run: %s check (a rule 6 card is returned to review here, for its missing read)\n", prog, prog)
	return 1
}

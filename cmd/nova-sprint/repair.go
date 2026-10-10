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

// repair's effect line: what the verb changes. It is the one write that is not
// a single store step: it finishes a pending operation (the fence held by a
// cut step) and returns rule-6 merging cards to review, and its --dry-run is
// its own, so the effect is stated here rather than in stepdry.go.
func init() {
	verbEffect["repair"] = "store write: finishes the pending operation the fence holds (a step cut short), and returns each merging card that entered merging with ok reads from fewer different readers at its head than it needs (check rule 6) back to review with the reason recorded, so it is read again; --dry-run prints each move it would make and writes nothing"
}

// cmdRepair finishes the pending operation (if any) and returns rule-6
// merging cards to review (internal/sprint/check.go, Rule6Merging). When it
// can do neither it refuses, naming the check that shows what is broken.
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
			Reason: fmt.Sprintf("rule 6: ok reads at its head from fewer readers than it needs; returned to review for its missing read"),
			Who:    c.actor,
		})
		return a.runStep("repair", *c, st, step, stdout, stderr)
	}
	// no rule 6: the pending operation, if any, is the repair.
	rr, err := st.Repair(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s repair: %s\n", prog, oneline.WithRemedy(err.Error(), prog+" repair -h"))
		return 2
	}
	if len(rr) == 0 {
		fmt.Fprintf(stderr, "%s repair: nothing to repair: no pending operation and no card merging with fewer ok reads at its head than it needs; run: %s check (a rule 6 card is returned to review here, for its missing read)\n", prog, prog)
		return 1
	}
	if c.dry {
		lines := make([]string, 0, len(rr))
		for _, r := range rr {
			lines = append(lines, fmt.Sprintf("OPERATION %s verb=%s done=%s", oneline.Escape(r.Op), oneline.Escape(r.Verb), r.Done))
		}
		sayOK(stdout, c.json, "repair", strings.Join(lines, "\n"), map[string]any{"dry_run": true, "operations": len(rr), "repaired": rr})
		return 0
	}
	if c.json {
		if rr == nil {
			rr = []store.RepairResult{}
		}
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

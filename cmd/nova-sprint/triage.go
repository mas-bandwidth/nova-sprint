package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// cmdPlanReview handles only the selected-card forms of check and promote;
// their existing audit/release forms are unchanged (docs/SPEC-SPRINT.md,
// plan and check columns).
func (a *app) cmdPlanReview(verb string, c common, r sprint.TriageReq, stdout, stderr io.Writer) int {
	if (len(r.IDs) > 0 && (r.Stream != "" || r.AllPlan)) || (r.Stream != "" && r.AllPlan) {
		return refuse(stderr, verb, "choose ids, --stream, or --all-plan")
	}
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, verb, err.Error())
	}
	if why, err := coordinatorsAlone(context.Background(), st, c); err != nil {
		return refuse(stderr, verb, err.Error())
	} else if why != "" {
		return refuse(stderr, verb, why)
	}
	gateCommon := c
	gateCommon.verb = "add"
	if why, err := hookedIn(context.Background(), st, gateCommon); err != nil {
		return refuse(stderr, verb, err.Error())
	} else if why != "" {
		return refuse(stderr, verb, why)
	}
	st,err=st.Pinned(context.Background()); if err!=nil { return a.readFailed(verb,err,stderr) }
    history,err:=st.PlanningHistory(context.Background()); if err!=nil { return a.readFailed(verb,err,stderr) }
    var rows []sprint.TriageRow
	step := store.Step{Verb: verb, Args: store.ArgsOf(r), Load: store.All, Mirrors: true, Plan: func(s *sprint.Snapshot) sprint.Plan {
		s.PlanHistory=history
		var p sprint.Plan
		if verb == "check" {
			p, rows = sprint.ReviewPlan(s, r)
		} else {
			rows = sprint.TriageSet(s, sprint.TriageIDs(s, r))
			p = sprint.PromotePlan(s, r)
		}
		raw, _ := json.Marshal(rows)
		p.Said = append(p.Said, "triage="+string(raw))
		return p
	}}
	step.CallerOp = c.op
	if c.epoch >= 0 {
		epoch := uint64(c.epoch)
		step.Epoch = &epoch
	}
	result, err := st.Run(context.Background(), step)
	if err != nil {
		return a.readFailed(verb, err, stderr)
	}
	for _, said := range result.Said {
		if raw, ok := strings.CutPrefix(said, "triage="); ok {
			if err := json.Unmarshal([]byte(raw), &rows); err != nil {
				return a.readFailed(verb, err, stderr)
			}
		}
	}
	result.Said = nil
	if c.json {
		b, _ := json.Marshal(struct {
			Triage []sprint.TriageRow `json:"triage"`
			Result store.Result       `json:"result"`
		}{rows, result})
		fmt.Fprintln(stdout, string(b))
	} else {
		for _, row := range rows {
			for _, v := range row.Checks {
				status := "PASS"
				if !v.Passed {
					status = "FAIL"
				}
				fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", row.ID, v.Name, status, v.Detail)
			}
			fmt.Fprintf(stdout, "%s\tbytes=%d\trepeated=%d\n", row.ID, row.Bytes, row.RepeatedBytes)
			for _, prior := range row.Precedents {
				fmt.Fprintf(stdout, "%s\tprecedent\t%s\n", row.ID, prior)
			}
		}
		for _, line := range result.Moved {
			fmt.Fprintln(stdout, line)
		}
		for _, r := range result.Refused {
			fmt.Fprintf(stderr, "REFUSED %s: %s\n", r.Key, r.Why)
		}
	}
	if len(result.Refused) > 0 {
		return 1
	}
	return 0
}

package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// JudgePlanner is layer 3's typed choices and fenced choice-plus-close plan.
type JudgePlanner interface {
	Judge(store.Snapshot, string, string, string, string, []string) (card.Plan, *card.Refusal)
}

type judgeInput struct {
	id, choice, reason, actor string
	expected                  []string
	hasExpect                 bool
}

func parseJudge(args []string, actor string) (judgeInput, bool) {
	in := judgeInput{actor: actor}
	if len(args) == 0 {
		return in, actor != ""
	}
	if len(args) == 2 && args[0] == "--as" {
		in.actor = args[1]
		return in, in.actor != ""
	}
	if len(args) < 2 {
		return in, false
	}
	in.id, in.choice = args[0], args[1]
	for i := 2; i < len(args); i++ {
		if i+1 >= len(args) || args[i+1] == "" {
			return in, false
		}
		switch args[i] {
		case "--reason":
			in.reason = args[i+1]
		case "--as":
			in.actor = args[i+1]
		case "--expect":
			in.expected = strings.Split(args[i+1], ",")
			in.hasExpect = true
		default:
			return in, false
		}
		i++
	}
	return in, in.actor != "" && in.reason != ""
}
func (e *Engine) judge(ctx context.Context, args []string) Result {
	in, ok := parseJudge(args, e.Actor)
	if !ok {
		return refused("judge", "invalid arguments", "nova-sprint help judge")
	}
	st, ok := e.Store.(ApplyStore)
	if !ok || e.Env == nil || e.Now == nil {
		return unavailable("judge")
	}
	coordinator, err := e.Env.Coordinator(ctx)
	if err != nil {
		return operationalFailure("judge", err)
	}
	if in.actor != coordinator {
		return refused("judge", "only the coordinator can judge", "ask "+coordinator+" over the bus")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, in.actor)
	if err != nil {
		return operationalFailure("judge", err)
	}
	if !hooked {
		return refused("judge", in.actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("judge", err)
	}
	if in.id == "" {
		return listJudgements(snap, in.actor, coordinator)
	}
	members, problem := judgeMembers(snap, in)
	if problem != "" {
		return refused("judge", problem, "nova-sprint judge")
	}
	for _, j := range members {
		if !validJudgementHeader(j) {
			return unavailable("judge")
		}
	}

	if e.Judges == nil {
		return unavailable("judge")
	}
	return e.applyJudge(ctx, st, snap, in, len(members))
}
func validJudgementHeader(j store.Judgement) bool {
	fields := strings.Fields(j.Question)
	return len(fields) >= 3 && fields[0] == "kind="+j.Kind && fields[1] == "card="+j.About && fields[2] == "group="+j.Group
}
func listJudgements(snap store.Snapshot, actor, coordinator string) Result {
	list := openJudgements(snap, actor, coordinator)
	if len(list) == 0 {
		return Result{"JUDGEMENTS none\nnext: nothing; wait for the bus\n", 0}
	}
	var out strings.Builder
	for _, j := range list {
		fmt.Fprintf(&out, "JUDGEMENT %s %s\n", j.ID, j.Question)
	}
	fmt.Fprintln(&out, "next: "+list[0].Command)
	return Result{out.String(), 0}
}
func (e *Engine) applyJudge(ctx context.Context, st ApplyStore, snap store.Snapshot, in judgeInput, memberCount int) Result {
	plan, why := e.Judges.Judge(snap, in.id, in.choice, in.reason, in.actor, in.expected)
	if why != nil {
		return refused("judge", why.Reason, "nova-sprint judge")
	}
	key, _ := json.Marshal([]string{"judge", in.actor, in.id, in.choice, in.reason})
	op := store.Op{ID: consumerOpID(string(key), snap.Epoch, in.id, int(snap.Seq)), Epoch: snap.Epoch, Actor: in.actor, Kind: "judge", At: e.Now(), Expect: append(plan.Expect, store.AtSeq{Seq: snap.Seq}), Writes: plan.Writes}
	rec, err := applyExact(ctx, st, op)
	if err != nil {
		return operationalFailure("judge", err)
	}
	if !rec.Accepted {
		return refused("judge", rec.Reason, "nova-sprint judge")
	}
	next := "nothing; wait for the bus"
	if len(openJudgements(snap, in.actor, in.actor)) > memberCount {
		next = "nova-sprint judge"
	}
	return Result{fmt.Sprintf("ANSWERED judgement=%s by=%s choice=%s\nnext: %s\n", in.id, in.actor, in.choice, next), 0}
}

func judgeMembers(s store.Snapshot, in judgeInput) ([]store.Judgement, string) {
	if j, ok := s.Judgements[in.id]; ok {
		if j.State != "open" {
			return nil, "judgement " + j.ID + " is " + j.State + " by " + j.Actor
		}
		if in.hasExpect {
			return nil, "--expect is for a group"
		}
		return []store.Judgement{j}, ""
	}
	members := []store.Judgement{}
	ids := []string{}
	for _, j := range s.Judgements {
		if j.Group == in.id && j.State == "open" {
			members = append(members, j)
			ids = append(ids, j.ID)
		}
	}
	if len(members) == 0 {
		return nil, "unknown judgement or group " + in.id
	}
	if !in.hasExpect {
		return nil, "group " + in.id + " needs --expect <member ids>"
	}
	expected := slices.Clone(in.expected)
	slices.Sort(expected)
	slices.Sort(ids)
	if !slices.Equal(expected, ids) {
		return nil, "judgement group membership changed"
	}
	return members, ""
}

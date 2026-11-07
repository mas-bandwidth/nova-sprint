package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type RunStatus struct {
	Held                             bool
	Operation, Host, ApplicationName string
	PID, BackendPID                  int
	Since                            time.Time
}
type RunInfo interface {
	Inspect(context.Context) (RunStatus, error)
}
type ShowEdges interface {
	StitchRange(context.Context, store.Stream, []store.Card) (string, error)
	CI(context.Context, store.Card) (string, error)
}
type showInput struct {
	name, actor string
	stitch, ci  bool
}

func parseShow(args []string, actor string) (showInput, bool) {
	in := showInput{actor: actor}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		k := args[i]
		if seen[k] {
			return in, false
		}
		seen[k] = true
		switch k {
		case "--as":
			if i+1 >= len(args) || args[i+1] == "" {
				return in, false
			}
			i++
			in.actor = args[i]
		case "--stitch":
			in.stitch = true
		case "--ci":
			in.ci = true
		default:
			if in.name != "" || k == "" || strings.HasPrefix(k, "-") {
				return in, false
			}
			in.name = k
		}
	}
	return in, !(in.ci && in.stitch) && (!in.ci && !in.stitch || in.name != "")
}
func (e *Engine) show(ctx context.Context, args []string) Result {
	in, ok := parseShow(args, e.Actor)
	if !ok {
		return refused("show", "invalid arguments", "nova-sprint help show")
	}
	if e.Store == nil || e.Env == nil {
		return unavailable("show")
	}
	snap, err := e.Store.Snapshot(ctx)
	if err != nil {
		return operationalFailure("show", err)
	}
	value, actor, errResult := e.showValue(ctx, snap, in)
	if errResult != nil {
		return *errResult
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return unavailable("show")
	}
	answer, err := e.findNext(ctx, actor)
	if err != nil {
		return operationalFailure("show", err)
	}
	return Result{fmt.Sprintf("%s\nnext: %s\n", data, answer.Next), 0}
}
func (e *Engine) showValue(ctx context.Context, s store.Snapshot, in showInput) (any, string, *Result) {
	if in.name == "run" || in.stitch || in.ci {
		return e.showEdgeValue(ctx, s, in)
	}
	if in.name == "" {
		return s, in.actor, nil
	}
	if c, ok := s.Cards[in.name]; ok {
		actor := in.actor
		if c.Holder != "" {
			actor = c.Holder
		}
		return c, actor, nil
	}
	if v, ok := s.Streams[in.name]; ok {
		return v, in.actor, nil
	}
	if v, ok := s.Rows[in.name]; ok {
		return v, in.name, nil
	}
	r := refused("show", "unknown name "+in.name, "nova-sprint show")
	return nil, "", &r
}

func (e *Engine) showEdgeValue(ctx context.Context, s store.Snapshot, in showInput) (any, string, *Result) {
	if in.name == "run" && !in.ci && !in.stitch {
		if e.RunInfo == nil {
			r := unavailable("show")
			return nil, "", &r
		}
		status, err := e.RunInfo.Inspect(ctx)
		if err != nil {
			r := operationalFailure("show", err)
			return nil, "", &r
		}
		return status, in.actor, nil
	}
	if in.stitch {
		stream, ok := s.Streams[in.name]
		if !ok {
			r := refused("show", "unknown stream "+in.name, "nova-sprint show "+in.name)
			return nil, "", &r
		}
		result, err := e.stitch(ctx, s, stream)
		if err != nil {
			r := operationalFailure("show", err)
			return nil, "", &r
		}
		return result, in.actor, nil
	}
	if in.ci {
		c, ok := s.Cards[in.name]
		if !ok {
			r := refused("show", "unknown card "+in.name, "nova-sprint show "+in.name)
			return nil, "", &r
		}
		if e.ShowEdges == nil {
			r := unavailable("show")
			return nil, "", &r
		}
		status, err := e.ShowEdges.CI(ctx, c)
		if err != nil {
			r := operationalFailure("show", err)
			return nil, "", &r
		}
		return struct {
			Card string `json:"card"`
			CI   string `json:"ci"`
		}{c.ID, status}, in.actor, nil
	}
	r := refused("show", "invalid arguments", "nova-sprint help show")
	return nil, "", &r
}

type stitchRead struct {
	ID      string `json:"id"`
	Row     string `json:"row"`
	Result  string `json:"result"`
	Finding string `json:"finding"`
	Cost    int64  `json:"cost_micro_usd"`
	Priced  bool   `json:"priced"`
}
type stitchCard struct {
	ID           string       `json:"id"`
	Head         string       `json:"head"`
	Author       string       `json:"author"`
	CurrentModel string       `json:"current_model"`
	Reads        []stitchRead `json:"reads"`
	WorkCosts    []stitchCost `json:"work_costs"`
}
type stitchCost struct {
	ID     string `json:"id"`
	Cost   int64  `json:"cost_micro_usd"`
	Priced bool   `json:"priced"`
}
type stitchView struct {
	Stream    string       `json:"stream"`
	DiffRange string       `json:"diff_range"`
	Cards     []stitchCard `json:"cards"`
}

func (e *Engine) stitch(ctx context.Context, s store.Snapshot, stream store.Stream) (stitchView, error) {
	if e.ShowEdges == nil {
		return stitchView{}, fmt.Errorf("edge unavailable")
	}
	producers := []store.Card{}
	for _, c := range s.Cards {
		if c.Stream == stream.Name && c.Kind == "producer" {
			producers = append(producers, c)
		}
	}
	slices.SortFunc(producers, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
	diff, err := e.ShowEdges.StitchRange(ctx, stream, producers)
	if err != nil {
		return stitchView{}, err
	}
	out := stitchView{Stream: stream.Name, DiffRange: diff, Cards: []stitchCard{}}
	for _, p := range producers {
		item := stitchCard{ID: p.ID, Head: p.Head, Author: p.Author, CurrentModel: s.Rows[p.Author].Model, Reads: []stitchRead{}, WorkCosts: []stitchCost{}}
		for _, c := range s.Cards {
			if c.Of != p.ID {
				continue
			}
			if c.Kind == "read" {
				item.Reads = append(item.Reads, stitchRead{ID: c.ID, Row: c.Holder, Result: c.Result, Finding: c.Reason, Cost: c.Cost, Priced: c.Priced})
			}
			if c.Kind == "work" {
				item.WorkCosts = append(item.WorkCosts, stitchCost{ID: c.ID, Cost: c.Cost, Priced: c.Priced})
			}
		}
		slices.SortFunc(item.Reads, func(a, b stitchRead) int { return strings.Compare(a.ID, b.ID) })
		slices.SortFunc(item.WorkCosts, func(a, b stitchCost) int { return strings.Compare(a.ID, b.ID) })
		out.Cards = append(out.Cards, item)
	}
	return out, nil
}

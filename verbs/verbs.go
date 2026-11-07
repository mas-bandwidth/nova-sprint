// Package verbs implements the client-facing sprint commands.
package verbs

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Result is the complete output and process status of a command.
type Result struct {
	Output string
	Exit   int
}

// Environment supplies external observations without granting verbs ownership
// of fleet, bus or setup operations. Implementations must not expose secrets.
type Environment interface {
	Fleet(context.Context) (bool, string, error)
	Schema(context.Context) (bool, error)
	Hooked(context.Context, string) (bool, string, error)
	Coordinator(context.Context) (string, error)
}

// Reader is the subset of the store used by next.
type Reader interface {
	Snapshot(context.Context) (store.Snapshot, error)
}

// Engine consumes its dependencies, keeping command parsing independent of CLI.
type Engine struct {
	Store              Reader
	Env                Environment
	Roles              RoleSource
	Roster             RosterSource
	Actor              string
	Setup              Setup
	SetupLock          SetupLock
	ReportContinuation func(context.Context, string, SetupInput) error
	SetupObserver      func(SetupObservation)
	SetupHook          func(context.Context, SetupInput) Result
	Now                Clock
	Reports            ReportPlanner
	Judges             JudgePlanner
	Heads              HeadVerifier
	Faults             FaultVerifier
	Health             Health
	History            HistoryVerifier
	Repositories       Repositories
	Briefs             Briefs
	Archive            Archive
	Preview            func(context.Context, string) error
	PolicyEvaluator    PolicyEvaluator
	PolicyFiles        PolicyFiles
	PolicyActivator    PolicyActivator
	Install            Installer
	RunInfo            RunInfo
	ShowEdges          ShowEdges
	Version            string
	ToolPin            string
}

type nextAnswer struct {
	Message     string            `json:"message"`
	Next        string            `json:"next"`
	Obligations []Obligation      `json:"obligations"`
	Judgements  []store.Judgement `json:"judgements"`
}

// Execute handles commands; it returns output rather than writing to a terminal.
func (e *Engine) Execute(ctx context.Context, args []string) Result {
	intro := len(args) == 0
	if intro {
		args = []string{"next"}
	}
	if need := requiredRole(args); need != "" {
		actor, actorOK := roleActor(args, e.Actor)
		if !actorOK {
			return refused(args[0], "invalid arguments", "nova-sprint help "+args[0])
		}
		if args[0] == "next" {
			if result, ok := e.nextPrerequisites(ctx, actor); !ok {
				return result
			}
		}
		ctx = context.WithValue(ctx, roleRequiredKey{}, true)
		var result Result
		var ok bool
		ctx, result, ok = e.authorizeRole(ctx, actor, need, args[0])
		if !ok {
			return result
		}
	}

	if result, ok := e.dispatchCore(ctx, args, intro); ok {
		return result
	}
	return e.dispatchOther(ctx, args)
}
func (e *Engine) dispatchCore(ctx context.Context, args []string, intro bool) (Result, bool) {
	switch args[0] {
	case "next":
		return e.next(ctx, args[1:], intro), true
	case "up":
		return e.up(ctx, args[1:]), true
	case "hook":
		name, role, ok := hookInput(args[1:])
		if !ok {
			return refused("hook", "invalid arguments", "nova-sprint help hook"), true
		}
		return e.Hook(ctx, name, role), true
	case "unhook":
		return e.unhook(ctx, args[1:]), true
	case "take":
		return e.take(ctx, args[1:]), true
	case "release":
		return e.release(ctx, args[1:]), true
	case "row":
		return e.row(ctx, args[1:]), true
	case "edit":
		return e.edit(ctx, args[1:]), true
	case "show":
		return e.show(ctx, args[1:]), true
	case "drop":
		return e.drop(ctx, args[1:]), true
	case "hold", "unhold":
		return e.hold(ctx, args[0], args[1:]), true
	case "help", "-h", "--help":
		return e.help(ctx, args[1:]), true
	}
	return Result{}, false
}
func (e *Engine) dispatchOther(ctx context.Context, args []string) Result {
	switch args[0] {
	case "priority":
		return e.priority(ctx, args[1:])
	case "policy":
		return e.policy(ctx, args[1:])
	case "judge":
		return e.judge(ctx, args[1:])
	case "stream":
		return e.stream(ctx, args[1:])
	case "add":
		return e.add(ctx, args[1:])
	case "install":
		return e.install(ctx, args[1:])
	case "backup":
		return e.backup(ctx, args[1:])
	case "restore":
		return e.restore(ctx, args[1:])
	case "clear":
		return e.clear(ctx, args[1:])
	case "stop", "start":
		return e.setStopped(ctx, args[0], args[1:])
	case "check":
		return e.check(ctx, args[1:])
	case "version":
		if len(args) != 1 {
			return refused("version", "invalid arguments", "nova-sprint help version")
		}
		if !validBuildVersion(e.Version) {
			return unavailable("version")
		}
		return Result{e.Version + "\n", 0}
	case "report", "finish", "read":
		return e.report(ctx, args[0], args[1:])
	default:
		return refused(args[0], "unknown verb", "nova-sprint help")
	}
}

func refused(verb, problem, fix string) Result {
	clean := strings.NewReplacer("\n", " ", "\r", " ")
	return Result{fmt.Sprintf("SPRINT REFUSED %s: %s: %s\n", clean.Replace(verb), clean.Replace(problem), clean.Replace(fix)), 1}
}
func unavailable(verb string) Result {
	return Result{fmt.Sprintf("SPRINT REFUSED %s: cannot look: try again\n", verb), 2}
}

func parseNext(args []string, actor string) (string, bool, bool) {
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--as":
			if i+1 == len(args) || args[i+1] == "" {
				return "", false, false
			}
			i++
			actor = args[i]
		case "--json":
			jsonOutput = true
		default:
			return "", false, false
		}
	}
	return actor, jsonOutput, true
}

func (e *Engine) next(ctx context.Context, args []string, intro bool) Result {
	actor, jsonOutput, ok := parseNext(args, e.Actor)
	if !ok {
		return refused("next", "invalid arguments", "nova-sprint help next")
	}
	if e.Env == nil {
		return unavailable("next")
	}
	if jsonOutput {
		return e.nextJSON(ctx, actor)
	}
	answer, err := e.findNext(ctx, actor)
	if err != nil {
		return operationalFailure("next", err)
	}

	prefix := ""
	if intro && strings.HasPrefix(answer.Message, "the fleet is not up") {
		prefix = "nova-sprint deals cards to AIs over nova-bus, reads them, and lands them. It stands on nova-tools.\n"
	}
	return Result{prefix + "NEXT " + answer.Message + "\nnext: " + answer.Next + "\n", 0}
}

func (e *Engine) findNext(ctx context.Context, actor string) (nextAnswer, error) {
	green, detail, err := e.Env.Fleet(ctx)
	if err != nil {
		return nextAnswer{}, err
	}
	if !green {
		return nextAnswer{"the fleet is not up: " + detail, "nova-fleet check", nil, nil}, nil
	}
	schema, err := e.Env.Schema(ctx)
	if err != nil {
		return nextAnswer{}, err
	}
	if !schema {
		coordinator := actor
		if coordinator == "" {
			coordinator = "<coordinator>"
		}
		return nextAnswer{"nova-tools is up; the sprint is not set up", "nova-sprint up --as " + coordinator, nil, nil}, nil
	}
	if actor == "" {
		return nextAnswer{"choose the name you use for the sprint", "nova-sprint next --as <name>", nil, nil}, nil
	}
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return nextAnswer{}, err
	}
	if !hooked {
		return nextAnswer{actor + " is not hooked in", recipe, nil, nil}, nil
	}
	if e.Store == nil {
		return nextAnswer{}, fmt.Errorf("store unavailable")
	}
	snap, err := e.Store.Snapshot(ctx)
	if err != nil {
		return nextAnswer{}, err
	}
	return nextFromSnapshot(snap, actor), nil
}
func nextFromSnapshot(snap store.Snapshot, actor string) nextAnswer {
	coordinator := ""
	if snap.Rows[actor].Role == "coordinator" {
		coordinator = actor
	}
	items := obligations(snap, actor)
	judgements := openJudgements(snap, actor, coordinator)
	var a nextAnswer
	if first, ok := cardNext(snap, actor); ok {
		a = first
	} else if first, ok := judgementNext(snap, actor, coordinator); ok {
		a = first
	} else {
		a = streamNext(snap)
	}
	if more := len(items) + len(judgements) - 1; more > 0 {
		a.Message += fmt.Sprintf(" (%d more)", more)
	}
	a.Obligations, a.Judgements = items, judgements
	return a
}

func cardNext(s store.Snapshot, actor string) (nextAnswer, bool) {
	var selected store.Card
	for _, c := range s.Cards {
		if c.Holder != actor || (c.State != "ready" && c.State != "working") {
			continue
		}
		if selected.ID == "" || (c.State == "working" && selected.State == "ready") || (c.State == selected.State && c.ID < selected.ID) {
			selected = c
		}
	}
	if selected.ID == "" {
		return nextAnswer{}, false
	}
	id := fmt.Sprintf("%s@%d", selected.ID, selected.Gen)
	if selected.State == "ready" {
		return nextAnswer{"card " + selected.ID + " is dealt to " + actor, "nova-sprint take " + id + " --as " + actor + fmt.Sprintf(" --epoch %d", s.Epoch), nil, nil}, true
	}
	if selected.Kind == "read" {
		return nextAnswer{"read " + selected.ID + " is in hand", "nova-sprint read " + id + " yes|fix --as " + actor + fmt.Sprintf(" --epoch %d", s.Epoch), nil, nil}, true
	}
	return nextAnswer{"card " + selected.ID + " is in hand", "nova-sprint finish " + id + " --head <sha> --as " + actor + fmt.Sprintf(" --epoch %d", s.Epoch), nil, nil}, true
}

func judgementNext(s store.Snapshot, actor, coordinator string) (nextAnswer, bool) {
	if actor != coordinator {
		return nextAnswer{}, false
	}
	var j store.Judgement
	for _, v := range s.Judgements {
		if v.State != "open" {
			continue
		}
		if j.ID == "" || v.OpenedAt.Before(j.OpenedAt) || (v.OpenedAt.Equal(j.OpenedAt) && v.ID < j.ID) {
			j = v
		}
	}
	if j.ID == "" {
		return nextAnswer{}, false
	}
	return nextAnswer{"judgement " + j.ID + " needs you", strings.TrimSpace(j.Command), nil, nil}, true
}

func streamNext(s store.Snapshot) nextAnswer {
	if len(s.Streams) == 0 {
		return nextAnswer{"no stream exists", "nova-sprint stream add <name> --repo <owner/name>", nil, nil}
	}
	names := make([]string, 0, len(s.Streams))
	for name := range s.Streams {
		names = append(names, name)
	}
	// Stable ordering matters when several streams need a first card.
	slices.Sort(names)
	allDone := true
	for _, name := range names {
		count := 0
		done := true
		for _, c := range s.Cards {
			if c.Stream == name && (c.Kind == "producer" || c.Kind == "sentinel") {
				count++
				if c.State != "landed" {
					done = false
				}
			}
		}
		if count == 0 {
			return nextAnswer{"stream " + name + " has no cards", "nova-sprint add --stream " + name + " --brief-file <file>", nil, nil}
		}
		allDone = allDone && done
	}
	if allDone {
		return nextAnswer{"everything is done", "nothing; wait for the bus", nil, nil}
	}
	return nextAnswer{"nothing needs you", "nothing; wait for the bus", nil, nil}
}

// Obligation is an actionable consumer assigned to the requested row.
type Obligation struct {
	Card    string `json:"card"`
	Gen     int    `json:"gen"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
	Command string `json:"command"`
}

func obligations(s store.Snapshot, actor string) []Obligation {
	result := []Obligation{}
	for _, c := range s.Cards {
		if c.Holder != actor || (c.State != "ready" && c.State != "working") {
			continue
		}
		a, ok := cardNext(store.Snapshot{Epoch: s.Epoch, Cards: map[string]store.Card{c.ID: c}}, actor)
		if ok {
			result = append(result, Obligation{c.ID, c.Gen, c.Kind, c.State, a.Next})
		}
	}
	slices.SortFunc(result, func(a, b Obligation) int { return strings.Compare(a.Card, b.Card) })
	return result
}

func openJudgements(s store.Snapshot, actor, coordinator string) []store.Judgement {
	result := []store.Judgement{}
	if actor != coordinator {
		return result
	}
	for _, j := range s.Judgements {
		if j.State == "open" {
			result = append(result, j)
		}
	}
	slices.SortFunc(result, func(a, b store.Judgement) int {
		if a.OpenedAt.Before(b.OpenedAt) {
			return -1
		}
		if a.OpenedAt.After(b.OpenedAt) {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result
}

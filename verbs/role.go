package verbs

import (
	"context"
	"fmt"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// RoleSource reads the role and generation of the stored hook for a name.
// A missing or sessionless hook returns an empty role. Bus presence is checked separately.
type RoleSource interface {
	HookRole(context.Context, string) (role string, gen int, err error)
}

// SnapshotRoles reads the stored role and its fencing generation.
type SnapshotRoles struct{ Store Reader }

func (s SnapshotRoles) HookRole(ctx context.Context, actor string) (string, int, error) {
	if s.Store == nil {
		return "", 0, fmt.Errorf("role store unavailable")
	}
	snap, err := s.Store.Snapshot(ctx)
	if err != nil {
		return "", 0, err
	}
	row, ok := snap.Rows[actor]
	if !ok || row.Session == "" {
		return "", 0, nil
	}
	return row.Role, row.Gen, nil
}

type roleClaim struct {
	actor string
	gen   int
}
type roleClaimKey struct{}
type roleRequiredKey struct{}

func requiredRole(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "take", "finish", "read", "report", "next", "show", "queue":
		return "worker"
	case "stream", "add", "row", "judge", "priority", "edit", "drop", "relink", "replace", "hold", "unhold", "release", "clear", "backup", "restore", "stop", "start":
		return "coordinator"
	case "policy":
		if len(args) > 1 && (args[1] == "set" || args[1] == "activate") {
			return "coordinator"
		}
		return "worker"
	}
	return ""
}
func roleActor(args []string, fallback string) (string, bool) {
	actor := fallback
	seen := false
	for i := 1; i < len(args); i++ {
		if args[i] == "--as" {
			if seen || i+1 >= len(args) || args[i+1] == "" {
				return "", false
			}
			seen = true
			i++
			actor = args[i]
			continue
		}
		switch args[i] {
		case "--dry-run", "--json", "--all", "--failed", "--list", "--pr", "--stitch", "--ci", "--waive":
		default:
			if len(args[i]) > 2 && args[i][:2] == "--" && i+1 < len(args) {
				i++
			}
		}
	}
	return actor, true
}
func (e *Engine) authorizeRole(ctx context.Context, actor, need, verb string) (context.Context, Result, bool) {
	if actor == "" {
		return ctx, refused(verb, "choose a name", "nova-sprint hook --as <you> --role "+need), false
	}
	if e.Env == nil || e.Roles == nil {
		return ctx, unavailable(verb), false
	}
	present, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return ctx, operationalFailure(verb, err), false
	}
	if !present {
		return ctx, busHookRefusal(verb, actor, recipe), false
	}
	role, gen, err := e.Roles.HookRole(ctx, actor)
	if err != nil {
		return ctx, operationalFailure(verb, err), false
	}
	if role != "worker" && role != "coordinator" {
		return ctx, refused(verb, "hook in first", "nova-sprint hook --as "+actor+" --role "+need), false
	}
	if need == "coordinator" && role != "coordinator" {
		return ctx, refused(verb, fmt.Sprintf("%s is hooked as worker", actor), "nova-sprint unhook --as "+actor+" then nova-sprint hook --as "+actor+" --role coordinator"), false
	}
	return context.WithValue(ctx, roleClaimKey{}, roleClaim{actor, gen}), Result{}, true
}
func fenceRole(ctx context.Context, op *store.Op) error {
	claim, ok := ctx.Value(roleClaimKey{}).(roleClaim)
	if !ok {
		if required, _ := ctx.Value(roleRequiredKey{}).(bool); required {
			return fmt.Errorf("required role claim is missing")
		}
		return nil // Internal setup operations have no name-acting preflight.
	}
	if claim.actor != op.Actor {
		return fmt.Errorf("role claim does not match operation actor")
	}
	op.Expect = append(op.Expect, store.RowGen{Row: claim.actor, Gen: claim.gen})
	return nil
}

func (e *Engine) nextPrerequisites(ctx context.Context, actor string) (Result, bool) {
	if e.Env == nil {
		return unavailable("next"), false
	}
	green, fix, err := e.Env.Fleet(ctx)
	if err != nil {
		return operationalFailure("next", err), false
	}
	if !green {
		return refused("next", "the fleet is not up", fix), false
	}
	ready, err := e.Env.Schema(ctx)
	if err != nil {
		return operationalFailure("next", err), false
	}
	if !ready {
		if actor == "" {
			actor = "<coordinator>"
		}
		return refused("next", "the sprint is not set up", "nova-sprint up --as "+actor), false
	}
	return Result{}, true
}

func roleMatchesSnapshot(ctx context.Context, actor string, snap store.Snapshot) bool {
	claim, ok := ctx.Value(roleClaimKey{}).(roleClaim)
	if !ok || claim.actor != actor {
		return false
	}
	row, ok := snap.Rows[actor]
	return ok && row.Gen == claim.gen
}

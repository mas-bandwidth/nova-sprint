package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/store"
	"os"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

func internalCommand(ctx context.Context, args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "--internal-up-phase1", "--internal-up-phase2":
		return executeUpPhase(ctx, args[0], args[1:]), true
	case "--internal-write-continuation":
		if len(args) != 1 {
			return 2, true
		}
		return writeContinuationChild(ctx), true
	case "--internal-model-probe":
		if len(args) != 2 {
			return 2, true
		}
		return (verbs.ModelListProbe{}).Run(ctx, args[1]), true
	case "--internal-setup-role-fresh":
		if len(args) != 1 {
			return 2, true
		}
		return verbs.InternalSprintRoleFresh(ctx), true
	case "--internal-setup-role":
		if len(args) != 1 {
			return 2, true
		}
		return verbs.InternalSprintRole(ctx), true
	case "--internal-role-exists":
		if len(args) != 1 {
			return 2, true
		}
		yes, err := verbs.InternalSprintRoleExists(ctx)
		return internalJSON("exists", yes, err), true
	case "--internal-store-probe", "--internal-store-setup":
		if len(args) != 1 {
			return 2, true
		}
		yes, err := verbs.SprintStoreChild(ctx, args[0] == "--internal-store-setup", versionString())
		return internalJSON("ready", yes, err), true
	}
	return 0, false
}
func internalJSON(key string, value bool, err error) int {
	if err != nil {
		return 1
	}
	if json.NewEncoder(os.Stdout).Encode(map[string]bool{key: value}) != nil {
		return 1
	}
	return 0
}

func executeUpPhase(ctx context.Context, mode string, args []string) int {
	in, ok := verbs.ParseSetup(args, os.Getenv("NOVA_SPRINT_AS"))
	if !ok {
		return 2
	}
	admin, sprint := os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	if mode == "--internal-up-phase1" {
		if admin == "" || sprint != "" || in.Continuation != "" || os.Getenv("NOVA_SPRINT_HANDOFF_FD") != "3" {
			return 2
		}
		return executeVerb(ctx, append([]string{"up"}, args...), "phase1")
	}
	if sprint == "" || admin != "" || in.Continuation == "" {
		return 2
	}
	return executeVerb(ctx, append([]string{"up"}, args...), "phase2")
}

func writeContinuationChild(parent context.Context) int {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	password := os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	dsn, err := setupRoleDSN(os.Getenv("NOVA_PG_DSN"), "nova_sprint")
	host, hostErr := os.Hostname()
	if err != nil || hostErr != nil || password == "" {
		return 1
	}
	host = strings.SplitN(host, ".", 2)[0]
	held, err := verbs.VerifySetupLeaseOwner(ctx, dsn, password, host, os.Getppid())
	if err != nil || !held {
		return 1
	}
	pg, err := store.Open(ctx, dsn, password)
	if err != nil {
		return 1
	}
	defer pg.Close()
	id, err := writeSetupContinuation(ctx, pg, os.Stdin, time.Now())
	if err != nil {
		return 1
	}
	if _, err = fmt.Fprintln(os.Stdout, id); err != nil {
		return 1
	}
	return 0
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"

	"github.com/mas-bandwidth/nova-sprint/verbs"
)

func internalCommand(ctx context.Context, args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	mode := args[0]
	switch mode {
	case "--internal-up-phase1", "--internal-up-phase2":
		return executeUpPhase(ctx, mode, args[1:]), true
	case "--internal-write-continuation":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		return writeContinuationChild(ctx), true
	case "--internal-model-probe":
		if code, bad := internalArgs(args, 2); bad {
			return code, true
		}
		return (verbs.ModelListProbe{}).Run(ctx, args[1]), true
	case "--internal-setup-role-fresh":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		return verbs.InternalSprintRoleFresh(ctx), true
	case "--internal-setup-role":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		return verbs.InternalSprintRole(ctx), true
	case "--internal-role-exists":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		yes, err := verbs.InternalSprintRoleExists(ctx)
		return internalJSON("exists", yes, err), true
	case "--internal-store-probe", "--internal-store-setup":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		yes, err := verbs.SprintStoreChild(ctx, mode == "--internal-store-setup", versionString())
		return internalJSON("ready", yes, err), true
	}
	return 0, false
}

// internalArgs refuses an internal mode called with the wrong number of
// words, on stderr (exit 2): an internal child never exits without saying
// why.
func internalArgs(args []string, want int) (int, bool) {
	if len(args) == want {
		return 0, false
	}
	return verbs.ChildFailed(fmt.Errorf("%s: takes %d word(s) after the binary, got %d (%q); it is run only by nova-sprint itself", args[0], want, len(args), edge.Redact(strings.Join(args, " "))), 2), true
}
func internalJSON(key string, value bool, err error) int {
	if err != nil {
		return verbs.ChildFailed(err, 1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]bool{key: value}); err != nil {
		return verbs.ChildFailed(edge.Cause("write result", err), 1)
	}
	return 0
}

func executeUpPhase(ctx context.Context, mode string, args []string) int {
	in, ok := verbs.ParseSetup(args, os.Getenv("NOVA_SPRINT_AS"))
	if !ok {
		return verbs.ChildFailed(fmt.Errorf("%s: setup arguments did not parse (%q): nova-sprint help up", mode, edge.Redact(strings.Join(args, " "))), 2)
	}
	admin, sprint := os.Getenv("NOVA_PG_ADMIN_PASSWORD"), os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	if mode == "--internal-up-phase1" {
		if admin == "" || sprint != "" || in.Continuation != "" || os.Getenv("NOVA_SPRINT_HANDOFF_FD") != "3" {
			return verbs.ChildFailed(fmt.Errorf("%s: wants NOVA_PG_ADMIN_PASSWORD set, NOVA_SPRINT_PG_PASSWORD unset, no --continuation and NOVA_SPRINT_HANDOFF_FD=3 (admin set %t, sprint set %t, continuation %t, handoff fd %q)", mode, admin != "", sprint != "", in.Continuation != "", edge.Redact(os.Getenv("NOVA_SPRINT_HANDOFF_FD"))), 2)
		}
		return executeVerb(ctx, append([]string{"up"}, args...), "phase1")
	}
	if sprint == "" || admin != "" || in.Continuation == "" {
		return verbs.ChildFailed(fmt.Errorf("%s: wants NOVA_SPRINT_PG_PASSWORD set, NOVA_PG_ADMIN_PASSWORD unset and a --continuation (sprint set %t, admin set %t, continuation %t)", mode, sprint != "", admin != "", in.Continuation != ""), 2)
	}
	return executeVerb(ctx, append([]string{"up"}, args...), "phase2")
}

func writeContinuationChild(parent context.Context) int {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	password := os.Getenv("NOVA_SPRINT_PG_PASSWORD")
	dsn, err := setupRoleDSN(os.Getenv("NOVA_PG_DSN"), "nova_sprint")
	if err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation: sprint role DSN", err), 1)
	}
	host, err := os.Hostname()
	if err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation: hostname", err), 1)
	}
	if password == "" {
		return verbs.ChildFailed(fmt.Errorf("write continuation: NOVA_SPRINT_PG_PASSWORD not set"), 1)
	}
	host = strings.SplitN(host, ".", 2)[0]
	held, err := verbs.VerifySetupLeaseOwner(ctx, dsn, password, host, os.Getppid())
	if err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation: setup lease", err), 1)
	}
	if !held {
		return verbs.ChildFailed(fmt.Errorf("write continuation: setup lease is not held by %s pid %d", host, os.Getppid()), 1)
	}
	pg, err := store.Open(ctx, dsn, password)
	if err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation: open store", err), 1)
	}
	defer pg.Close()
	id, err := writeSetupContinuation(ctx, pg, os.Stdin, time.Now())
	if err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation", err), 1)
	}
	if _, err = fmt.Fprintln(os.Stdout, id); err != nil {
		return verbs.ChildFailed(edge.Cause("write continuation: stdout", err), 1)
	}
	return 0
}

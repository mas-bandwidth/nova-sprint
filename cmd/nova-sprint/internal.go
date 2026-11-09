package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"

	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

func internalCommand(ctx context.Context, args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	mode := args[0]
	switch mode {
	case "--internal-up":
		return executeUpChild(ctx, args[1:]), true
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
	case "--internal-setup-role-password":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		yes, err := verbs.InternalSprintRolePassword(ctx)
		return internalJSON("ready", yes, err), true
	case "--internal-store-probe", "--internal-store-setup":
		if code, bad := internalArgs(args, 1); bad {
			return code, true
		}
		yes, err := verbs.SprintStoreChild(ctx, mode == "--internal-store-setup")
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

// executeUpChild is the walk under the sprint's credential, run by the up
// process that holds none (runUpWrapper) once steps 1 and 2 verify.
func executeUpChild(ctx context.Context, args []string) int {
	if _, ok := verbs.ParseSetup(args, os.Getenv("NOVA_SPRINT_AS")); !ok {
		return verbs.ChildFailed(fmt.Errorf("--internal-up: setup arguments did not parse (%q): nova-sprint help up", edge.Redact(strings.Join(args, " "))), 2)
	}
	if os.Getenv("NOVA_SPRINT_PG_PASSWORD") == "" || os.Getenv("NOVA_PG_ADMIN_PASSWORD") != "" {
		return verbs.ChildFailed(fmt.Errorf("--internal-up: wants NOVA_SPRINT_PG_PASSWORD set and NOVA_PG_ADMIN_PASSWORD unset (sprint set %t, admin set %t)", os.Getenv("NOVA_SPRINT_PG_PASSWORD") != "", os.Getenv("NOVA_PG_ADMIN_PASSWORD") != ""), 2)
	}
	return executeVerb(ctx, append([]string{"up"}, args...), "")
}

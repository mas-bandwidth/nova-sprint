package verbs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The sprint's commit identity (store.Identity, round 24). A repository's
// contributor agreement keys off the forge account, so every agent, friend
// and bench worker commits as ONE account on the agreement's allowlist (its
// git user.name and user.email, typically the account's noreply address);
// a contributor's own name goes only in a By: line and a Co-Authored-By:
// trailer. nova-sprint is public and generic, so the identity is the
// sprint's own state, set by the coordinator and held in the store (intent
// rules 5 and 8: PostgreSQL owns the state; nova-config holds the fleet and
// the friends, and which account a sprint's repositories accept is neither),
// never a literal in the code. The take receipt carries it to the worker;
// hook and take print it to a friend; finish refuses a head with a commit
// made as anyone else.

// identityFix is the command that sets the identity.
const identityFix = "nova-sprint identity --name \"<git user.name>\" --email \"<git user.email>\" --reason \"<why>\""

// runIdentity is `identity`: the coordinator sets the sprint's commit
// identity, one write, the same identity again changing nothing. With
// neither flag it prints the identity in force.
func runIdentity(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	name, email, reason := strings.TrimSpace(in.str("name")), strings.TrimSpace(in.str("email")), in.str("reason")
	if name == "" && email == "" {
		if !snap.Identity.Set() {
			return "", refuse("the sprint has no commit identity", identityFix)
		}
		return done("nova-sprint next --as "+in.actor, "IDENTITY "+commitIdentity(snap), worker.CommitRules(snap.Identity.Name, snap.Identity.Email)), nil
	}
	if why := worker.ValidIdentity(name, email); why != "" {
		return "", refuse(why, identityFix)
	}
	if reason == "" {
		return "", refuse("an identity change needs its reason", "add --reason \"<why>\"")
	}
	if snap.Identity == (store.Identity{Name: name, Email: email}) {
		return done("nova-sprint next --as "+in.actor, "IDENTITY already "+commitIdentity(snap)+"; nothing changed"), nil
	}
	op := e.newOp("identity", in.actor, snap, name, email)
	op.Expect = []store.Expect{store.UnchangedSince{Seq: snap.Seq, Identity: true, Hooks: true}} // no other identity set, and the caller still the coordinator it was read as
	op.Writes, op.Note = []store.Write{store.SetIdentity{Name: name, Email: email}}, reason
	r, err := e.commit(ctx, op, "nova-sprint identity --as "+in.actor)
	if err != nil {
		return "", err
	}
	return done("nova-sprint check", fmt.Sprintf("IDENTITY %s <%s> applied at seq %d; every take from now carries it", name, email, r.Seq),
		worker.CommitRules(name, email)), nil
}

// noIdentity is the refusal of a worker's hook or take on a sprint with no
// commit identity: nothing may commit until there is one.
func noIdentity() *Fail {
	return refuse("the sprint has no commit identity: every commit is made as one account on the repositories' contributor-agreement allowlist, and none is set",
		"ask the coordinator to run "+identityFix+", then run this again")
}

// identityLines is what hook prints under HOOKED: the identity rule, or
// for a coordinator on a sprint with none, the command that sets it.
func identityLines(snap store.Snapshot) string {
	if !snap.Identity.Set() {
		return "NO COMMIT IDENTITY: no worker can hook until one is set: " + identityFix
	}
	return "COMMIT AS " + commitIdentity(snap) + "\n" + strings.TrimRight(worker.CommitRules(snap.Identity.Name, snap.Identity.Email), "\n")
}

// identityCheck is check's line for the identity: red when none is set.
func identityCheck(snap store.Snapshot) (string, *Fail) {
	if !snap.Identity.Set() {
		return "", refuse("the sprint has no commit identity: no worker can hook and nothing can be committed", identityFix)
	}
	return "identity: every commit is made as " + commitIdentity(snap) + " (hook prints the two git config lines; finish refuses a commit by another email)", nil
}

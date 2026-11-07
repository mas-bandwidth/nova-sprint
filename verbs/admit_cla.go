package verbs

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// A repository's contributor agreement: a commit whose author
// has not signed fails the repository's agreement check, and the card sits
// red at the landing. A forge shows who has signed only where the agreement's
// bot exposes it, and most do not, so admission does the closest correct
// thing: it reads the checks the base branch requires and, when one looks like
// a contributor agreement, warns at stream add (and in check) with the
// identity every commit of the sprint is made as, for the human to sign.
// Nothing is refused: whether an identity has signed is not readable here.
var agreementCheck = regexp.MustCompile(`(?i)\b(cla|caa)\b|contributor|assignment agreement|licen[cs]e agreement`)

// agreementChecks are the names among checks that look like a contributor
// agreement, in the given order.
func agreementChecks(checks []string) []string {
	var out []string
	for _, c := range checks {
		if agreementCheck.MatchString(c) && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// agreementWarning is the warning for stream s: what is worth a look and
// what to do, or "" when its base requires no agreement check (or the stream
// has no forge). A forge read that fails is itself worth a look: the warning
// says the agreement could not be ruled out.
func (e *Engine) agreementWarning(ctx context.Context, snap store.Snapshot, s store.Stream) (what, fix string) {
	if s.Forge != "github" || e.Forge == nil {
		return "", ""
	}
	checks, err := e.Forge.RequiredChecks(ctx, s.Repo, s.Base)
	if err != nil {
		return "stream " + s.Name + ": the checks " + s.Repo + "'s " + s.Base + " requires cannot be read (" + errText(err) + ")",
			"if " + s.Repo + " asks contributors to sign an agreement, sign it for the identities nova-sprint check lists, and run check again"
	}
	names := agreementChecks(checks)
	if len(names) == 0 {
		return "", ""
	}
	return fmt.Sprintf("stream %s: %s's %s requires the check %s, which looks like a contributor agreement, and a commit whose author has not signed fails it",
			s.Name, s.Repo, s.Base, quoteList(names)),
		"sign it for " + commitIdentity(snap) + ", the identity every commit of the sprint is made as, before the first card lands"
}

// commitIdentity is the one git identity every agent, friend and bench
// worker commits as (store.Identity, set by nova-sprint identity), or what to
// do when the sprint has none yet.
func commitIdentity(snap store.Snapshot) string {
	if !snap.Identity.Set() {
		return "the sprint's commit identity, which is not set yet (" + identityFix + ")"
	}
	return snap.Identity.Name + " <" + snap.Identity.Email + ">"
}

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = `"` + n + `"`
	}
	return strings.Join(q, ", ")
}

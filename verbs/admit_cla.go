package verbs

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/worker"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// A repository's contributor agreement (directive 63): a commit whose author
// has not signed fails the repository's agreement check, and the card sits
// red at the landing. A forge shows who has signed only where the agreement's
// bot exposes it, and most do not, so admission does the closest correct
// thing: it reads the checks the base branch requires and, when one looks like
// a contributor agreement, warns at stream add (and in check) with the
// identities the benches will author commits as, for the human to sign.
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
		"sign it for " + e.commitIdentities(ctx, snap) + " before the first card lands; kin commit as their own git identity, which has to have signed too"
}

// commitIdentities are the git authors the benches commit as, from the
// roster (nova-config, the rows with width above 0) and the worker's author
// format <row> <row@domain>.
func (e *Engine) commitIdentities(ctx context.Context, snap store.Snapshot) string {
	lim, _, err := e.rules(ctx, snap)
	var ro roster.Roster
	if err == nil {
		ro, err = e.roster(ctx, lim, snap)
	}
	if err != nil {
		return "each bench's identity <row> <row@" + worker.CommitEmailDomain + "> (the roster could not be read: " + errText(err) + ")"
	}
	var ids []string
	for _, n := range slices.Sorted(maps.Keys(ro.Workers)) {
		if ro.Workers[n].Width > 0 {
			ids = append(ids, fmt.Sprintf("%s <%s@%s>", n, n, worker.CommitEmailDomain))
		}
	}
	if len(ids) == 0 {
		return "each bench's identity <row> <row@" + worker.CommitEmailDomain + "> (no bench in nova-config has cores yet)"
	}
	return strings.Join(ids, ", ")
}

func quoteList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = `"` + n + `"`
	}
	return strings.Join(q, ", ")
}

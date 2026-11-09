package verbs

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// Admission (intent rule 34, defect 49; CORRECT-USE "Admission"): what can
// be found when a stream, a card or a row is added is found then, each
// refusal naming its one fix, and check re-proves it on demand.

// Forge is the stream's forge as admission proves it, with the host's own
// gh identity (the identity the run does the forge work with): it can act
// on a repository, and a pull request's state.
type Forge interface {
	// CanAct is nil when the identity may push, open and merge pull
	// requests on repo; else a refusal naming its fix.
	CanAct(ctx context.Context, repo string) error
	// RequiredChecks are the names of the status checks the base branch
	// of repo requires to merge (branch protection and rulesets).
	RequiredChecks(ctx context.Context, repo, branch string) ([]string, error)
	// Checks is each sha's check rollup, read by the forge the landing reads
	// them with (forge.Forge.Checks); a sha with no checks has no entry.
	Checks(ctx context.Context, repo string, shas []string) (map[string]forge.CheckRollup, error)
}

// baseRedNamed is how many of a red base's failing checks a refusal names.
const baseRedNamed = 5

// baseRed refuses a github stream whose base tip is red by the landing's own
// rule (forge.CheckRed: a check completed in failure, other checks still
// running or not), naming the first red checks, their count and the fix:
// every landing onto it would be refused "the base itself is red", so a card
// added onto it can only wait (2026-11-08: 171 cards were admitted onto a
// base red on 155 of 205 jobs, and nothing landed for an hour). Pending
// checks, no checks, green and a gate fault are admitted: admission does not
// wait for CI. A stream with no forge has no checks to read. A read that
// fails is not red, as the landing reads it (baseRed_internal notes it and
// goes on): it admits with a warning (what, fix), never silently.
func (e *Engine) baseRed(ctx context.Context, s store.Stream, tip, again string) (string, error) {
	if s.Forge != "github" {
		return "", nil
	}
	if e.Forge == nil {
		return "", broken("the forge is not configured", "take it to the project's own tracker with this line")
	}
	at := shortSHA(tip)
	rollups, err := e.Forge.Checks(ctx, s.Repo, []string{tip})
	if err != nil {
		return "WARN stream " + s.Name + ": the checks of " + s.Repo + "'s " + s.Base + " at " + at + " cannot be read (" + oneLine(errText(err)) + "), so it is not known green: " +
			"if it is red, no card of it lands until it is green (gh auth status on this host shows who reads them)", nil
	}
	r := rollups[tip]
	if r.State != forge.CheckRed {
		return "", nil
	}
	named := strings.Join(r.Failing[:min(len(r.Failing), baseRedNamed)], ", ")
	if more := len(r.Failing) - baseRedNamed; more > 0 {
		named += fmt.Sprintf(" and %d more", more)
	}
	checks := "checks"
	if len(r.Failing) == 1 {
		checks = "check"
	}
	return "", refuse(fmt.Sprintf("stream %s: %s's %s is red at %s on %d %s (%s), so no card of it can land", s.Name, s.Repo, s.Base, at, len(r.Failing), checks, named),
		"make "+s.Repo+"'s "+s.Base+" green at "+at+", then "+again)
}

// admitStream is stream add's admission, and check's of each stream: the
// repository clones at its address and has the base (one ls-remote), the
// card branches sprint/<stream>/<card> are creatable there (git's rules for
// a ref name, and no branch sprint or sprint/<stream> at origin, which git's
// leaf/directory rule makes their parent), and with forge github the host's
// gh identity can act on it. It returns the base tip. The three reads at
// origin are independent and run at once.
func (e *Engine) admitStream(ctx context.Context, s store.Stream, again string) (string, error) {
	if why := cardBranchProblem(s); why != "" {
		return "", refuse("stream "+s.Name+": the card branches sprint/"+s.Name+"/<card> cannot be created: "+why, "name the stream or the base otherwise, then "+again)
	}
	var tip string
	errs := make([]error, 3)
	var wg sync.WaitGroup
	wg.Go(func() { tip, errs[0] = e.baseTip(ctx, s, again) })
	wg.Go(func() { errs[1] = e.noParentBranches(ctx, s, again) })
	wg.Go(func() { errs[2] = e.forgeActs(ctx, s, again) })
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return "", err
		}
	}
	return tip, nil
}

// cardBranchProblem is why sprint/<stream>/<card> cannot be a branch name
// (git check-ref-format, and a base that is its parent), or "".
func cardBranchProblem(s store.Stream) string {
	if why := refNameProblem("sprint/" + s.Name + "/card"); why != "" {
		return why
	}
	if s.Base == "sprint" || s.Base == "sprint/"+s.Name {
		return "the base " + s.Base + " is a branch, so git cannot create a branch under it"
	}
	return ""
}

// refNameProblem is why name breaks git's ref-name rules
// (git check-ref-format), or "".
func refNameProblem(name string) string {
	if name == "@" || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return name + " is not a git ref name (no .., @{, or trailing / or .)"
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return name + " is not a git ref name (a part " + clip(part) + " is empty, starts with . or ends with .lock)"
		}
	}
	if strings.ContainsAny(name, " ~^:?*[\\\x7f") || strings.IndexFunc(name, func(r rune) bool { return r < 0x20 }) >= 0 {
		return name + " is not a git ref name (no space, ~ ^ : ? * [ \\ or control character)"
	}
	return ""
}

// noParentBranches refuses a branch sprint or sprint/<stream> at origin:
// git keeps a branch as a file, so either makes sprint/<stream>/<card>
// uncreatable (the 2026-11-07 dogfood met a base named sprint/<stream>).
func (e *Engine) noParentBranches(ctx context.Context, s store.Stream, again string) error {
	url, err := streamURL(s)
	if err != nil {
		return err
	}
	if e.Origin == nil {
		return broken("the git edge is not configured", "nova-sprint check")
	}
	for _, b := range []string{"sprint", "sprint/" + s.Name} {
		_, ok, err := e.Origin.Tip(ctx, url, b)
		if err != nil {
			return RefusalOr(err, broken("git ls-remote of "+url+" failed ("+errText(err)+")", "make "+url+" readable from this host, then "+again))
		}
		if ok {
			return refuse("branch "+b+" is at "+s.Repo+"'s origin, so git cannot create the card branches sprint/"+s.Name+"/<card> under it",
				"rename or delete branch "+b+" at origin (git push origin --delete "+b+"), then "+again)
		}
	}
	return nil
}

// forgeActs is the host's gh identity able to act on a github stream's
// repository; a stream with no forge needs none.
func (e *Engine) forgeActs(ctx context.Context, s store.Stream, again string) error {
	if s.Forge != "github" {
		return nil
	}
	if e.Forge == nil {
		return broken("the forge is not configured", "take it to the project's own tracker with this line")
	}
	if err := e.Forge.CanAct(ctx, s.Repo); err != nil {
		return RefusalOr(err, broken("gh cannot act on "+s.Repo+" ("+errText(err)+")", "gh auth login on this host as an identity with write access to "+s.Repo+", then "+again))
	}
	return nil
}

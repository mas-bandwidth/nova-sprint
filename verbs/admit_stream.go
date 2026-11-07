package verbs

import (
	"context"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Admission (CORRECT-USE "Admission"): what can
// be found when a stream, a card or a row is added is found then, each
// refusal naming its one fix, and check re-proves it on demand.

// Forge is the stream's forge as admission proves it, with the host's own
// gh identity (the identity the run does the forge work with): it can act
// on a repository, and a pull request's state.
type Forge interface {
	// CanAct is nil when the identity may push, open and merge pull
	// requests on repo; else a refusal naming its fix.
	CanAct(ctx context.Context, repo string) error
	// PR is pull request n of repo: its head sha and whether it merged.
	PR(ctx context.Context, repo string, n int) (head string, merged bool, err error)
	// RequiredChecks are the names of the status checks the base branch
	// of repo requires to merge (branch protection and rulesets).
	RequiredChecks(ctx context.Context, repo, branch string) ([]string, error)
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
// uncreatable.
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

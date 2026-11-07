package verbs

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// headReady is finish's check of the head at origin (CORRECT-USE, "Finish
// is checked, not trusted"): a full sha, not the base or the prior head, and
// the tip of the dealt branch at origin. That is all finish needs (intent
// rule 32): no PR, no forge and no token on the worker; the landing opens
// the PR and reads its CI on the run's host.
func (e *Engine) headReady(ctx context.Context, snap store.Snapshot, c store.Card, head string) error {
	if !fullSHA(head) {
		return refuse("head "+head+" is not a full commit sha", "pass the 40-hex sha git ls-remote origin "+c.Branch+" prints")
	}
	if head == c.BaseSHA || head == c.Prior {
		return refuse("head "+head+" is where this attempt started; nothing was done", "commit the work on "+c.Branch+", push it, and finish with its head")
	}
	if e.Origin == nil {
		return broken("the git edge is not configured", "nova-sprint check")
	}
	s := snap.Streams[c.Stream]
	url, err := streamURL(s)
	if err != nil {
		return err
	}
	tip, ok, err := e.Origin.Tip(ctx, url, c.Branch)
	if err != nil {
		return RefusalOr(err, broken("git ls-remote of "+s.Repo+" failed ("+errText(err)+")", "check the repository is readable, then finish again"))
	}
	if !ok || tip != head {
		return refuse("head "+head+" is not the tip of "+c.Branch+" at origin", "push first: git push origin "+c.Branch)
	}
	return nil
}

// streamURL is the stream's repository as the git edge reaches it
// (git.CloneURL); a repository it cannot address is a refusal naming the
// form.
func streamURL(s store.Stream) (string, error) {
	url, err := git.CloneURL(s.Repo, s.Forge)
	if err != nil {
		return "", RefusalOr(err, refuse("stream "+s.Name+": "+err.Error(), "nova-sprint check"))
	}
	return url, nil
}

func fullSHA(s string) bool {
	return len(s) == 40 && strings.Trim(strings.ToLower(s), "0123456789abcdef") == ""
}

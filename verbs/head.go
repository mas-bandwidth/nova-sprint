package verbs

import (
	"context"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// headReady is finish's check of the head at origin (CORRECT-USE, "Finish
// is checked, not trusted"; INTENT rule 16, Glenn 2026-11-07): a full sha,
// not the base or the prior head, and the tip of the dealt branch at origin.
// Nothing else: no PR, no forge, no token on the worker's side; the landing
// opens the PR.
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
	tip, ok, err := e.Origin.Tip(ctx, s.Repo, c.Branch)
	if err != nil {
		return broken("git ls-remote of "+s.Repo+" failed ("+clip(err.Error())+")", "check the repository is readable, then finish again")
	}
	if !ok || tip != head {
		return refuse("head "+head+" is not the tip of "+c.Branch+" at origin", "push first: git push origin "+c.Branch)
	}
	return nil
}

func fullSHA(s string) bool {
	return len(s) == 40 && strings.Trim(strings.ToLower(s), "0123456789abcdef") == ""
}

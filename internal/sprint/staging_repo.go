package sprint

import (
	"strings"
	"time"
)

// StagingMirrorWindow is how long a member that refused a repository at staging for
// want of a bench mirror is not dealt any card of that repository (mirrorRefusals).
const StagingMirrorWindow = time.Hour

// stagingMirrorMarker is how a staging take's error names the refusal this rule is
// about: "no bench mirror for <url>". The error may carry the member's own
// "staging refused: " prefix (FieldStagingTake strips it in the replay path), so the
// marker is found, never anchored to the front.
const stagingMirrorMarker = "no bench mirror for "

// mirrorRefusals is, per member, the repositories it refused at staging for want of a
// bench mirror within the last StagingMirrorWindow, derived each tick from the fleet
// table: the staging takes on work cards (StagingTakes) whose error names a bench mirror
// ("no bench mirror for <url>"), the repository named by that url, at the latest take's
// time. A later ok finish by that member of a card of that repository ends it at once.
// The owner, 2026-10-11: "cards only may accrue per-fleet or friend, if there are not
// other machines/friends ready to do that work right now!" and "Please log cards to fix
// this deal and rebalance. It's shit." A refusal for any other cause is per card
// (StagingRefusers).
func mirrorRefusals(s *Snapshot) map[string]map[string]time.Time {
	out := map[string]map[string]time.Time{}
	if s == nil || s.Fleet == nil {
		return out
	}
	latest := map[string]time.Time{} // member\x00repo -> the latest take's time
	unknown := map[string]bool{}     // that time could not be read (no stamp)
	put := func(k string, at time.Time, known bool) {
		prev, seen := latest[k]
		switch {
		case !seen:
			latest[k], unknown[k] = at, !known
		case known && unknown[k]:
			latest[k], unknown[k] = at, false
		case known && at.After(prev):
			latest[k] = at
		}
	}
	for _, c := range s.Fleet.Cards() {
		takes, _ := StagingTakes(c)
		for _, t := range takes {
			repo := mirrorRepoOf(t.Error)
			if repo == "" || t.Member == "" {
				continue
			}
			at, err := time.Parse(time.RFC3339, t.Finished)
			put(t.Member+"\x00"+repo, at, err == nil)
		}
	}
	// a later ok finish by the member of a card of that repository ends it at once
	for _, c := range s.Fleet.Cards() {
		if c.F("ok") != "yes" || c.F("member") == "" {
			continue
		}
		repo := s.cardRepo(c)
		if repo == "" {
			continue
		}
		k := c.F("member") + "\x00" + repo
		at, err := time.Parse(time.RFC3339, c.F("finished"))
		if prev, seen := latest[k]; seen && (err != nil || !prev.After(at)) {
			delete(latest, k)
			delete(unknown, k)
		}
	}
	for k, at := range latest {
		if !unknown[k] && !s.Now.IsZero() && s.Now.Sub(at) > StagingMirrorWindow {
			continue
		}
		member, repo, _ := strings.Cut(k, "\x00")
		if out[member] == nil {
			out[member] = map[string]time.Time{}
		}
		out[member][repo] = at
	}
	return out
}

// mirrorRepoOf is the repository a staging take's error names: the url after "no bench
// mirror for ", cut at its first blank and stripped of a trailing colon, as owner/name
// (RepoName); "" when the error names no bench mirror.
func mirrorRepoOf(err string) string {
	i := strings.Index(err, stagingMirrorMarker)
	if i < 0 {
		return ""
	}
	url := err[i+len(stagingMirrorMarker):]
	if j := strings.IndexAny(url, " \t"); j >= 0 {
		url = url[:j]
	}
	url = strings.TrimSuffix(strings.TrimSpace(url), ":")
	if url == "" {
		return ""
	}
	return RepoName(url)
}

// cardRepo is the repository a card is of: the card's own repo field (FieldRepo; cards
// remember their repo), else its primary's, else its stream's. owner/name, "" when none.
func (s *Snapshot) cardRepo(c *Card) string {
	if c == nil {
		return ""
	}
	if v := firstRepo(c.F(FieldRepo)); v != "" {
		return v
	}
	id := c.ID
	if i := strings.Index(id, ".w"); i >= 0 {
		id = id[:i]
	}
	if p := s.Work.Card(id); p != nil {
		if v := firstRepo(p.F(FieldRepo)); v != "" {
			return v
		}
		if ctl := s.StreamCtl(p.Row); ctl != nil {
			if v := firstRepo(ctl.F(FieldRepo)); v != "" {
				return v
			}
		}
	}
	if ctl := s.StreamCtl(c.Row); ctl != nil {
		if v := firstRepo(ctl.F(FieldRepo)); v != "" {
			return v
		}
	}
	return ""
}

// firstRepo is the first repository a FieldRepo value names, as owner/name; "" when none.
func firstRepo(v string) string {
	for _, r := range Split(v) {
		if r != "" {
			return RepoName(r)
		}
	}
	return ""
}

// stagingRepoWhy is why member may not be dealt a card of c's repository: member refused
// that repository at staging for want of a bench mirror within StagingMirrorWindow, so it
// cannot stage any card of it, as a harness it cannot launch bars a route. "" when the
// member refused nothing of c's repository, or c's repository is unknown.
func (s *Snapshot) stagingRepoWhy(member string, c *Card) string {
	if member == "" || c == nil {
		return ""
	}
	repo := s.cardRepo(c)
	if repo == "" {
		return ""
	}
	at, refused := mirrorRefusals(s)[member][repo]
	if !refused {
		return ""
	}
	when := "an earlier tick"
	if !at.IsZero() {
		when = at.UTC().Format(time.RFC3339)
	}
	return member + " refused " + repo + " at staging at " + when + ": no bench mirror; add the mirror on " + member + " with the fleet play"
}

// stagingRefusalWhy is why the first member of pool that refused the primary c's repository
// at staging for want of a bench mirror (stagingRepoWhy) cannot take it: the sentence the
// deal's and the level's refusal says when no member is left, so the coordinator reads the
// mirror, never the routes the member cannot launch. "" when no member of pool refused c's
// repository that way. The owner, 2026-10-11: "cards only may accrue per-fleet or friend,
// if there are not other machines/friends ready to do that work right now!"
func (s *Snapshot) stagingRefusalWhy(c *Card, pool []string) string {
	for _, m := range pool {
		if w := s.stagingRepoWhy(m, c); w != "" {
			return w
		}
	}
	return ""
}

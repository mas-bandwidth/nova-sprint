package sprint

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// StagingMirrorWindow is how long a member that refused a repository at staging for
// want of a bench mirror or a credential is not dealt any card of that repository.
const StagingMirrorWindow = time.Hour

// stagingMirrorMarker is how a staging take's error names the refusal this rule is
// about: "no bench mirror for <url>". The error may carry the member's own
// "staging refused: " prefix (FieldStagingTake strips it in the replay path), so the
// marker is found, never anchored to the front.
const stagingMirrorMarker = "no bench mirror for "

// mirrorRefusals keeps the mirror-need judgments specific to missing mirrors.
// Eligibility uses the same window with credential failures included below.
func mirrorRefusals(s *Snapshot) map[string]map[string]time.Time {
	return stagingRepoRefusals(s, false)
}

// stagingRepoRefusals derives repository refusals from the staging takes on fleet
// cards. A mirror refusal names its repository in the error; a credential refusal
// concerns the card's recorded repository. Both use the latest take's time and
// StagingMirrorWindow, and a later ok finish by that member of that repository
// ends the refusal at once. Other staging failures remain per card.
func stagingRepoRefusals(s *Snapshot, credentials bool) map[string]map[string]time.Time {
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
			if repo == "" && credentials && stagingCredentialRefusal(t.Error) {
				repo = s.cardRepo(c)
			}
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

// stagingCredentialRefusal recognizes only the staging error markers whose
// failure applies to every card of a repository. It never inspects credentials.
func stagingCredentialRefusal(err string) bool {
	return strings.Contains(err, "Write access to repository not granted") ||
		strings.Contains(err, "Authentication failed") ||
		strings.Contains(err, "Permission denied (publickey)")
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

// briefRepoRead is one brief's repository and how many times it was actually read
// (a memo miss).
type briefRepoRead struct {
	repo  string
	reads atomic.Int64
}

// briefRepoCache is a brief's repository memoized by the brief: each parsed brief's
// repository, kept for the process (a brief never names a different repository once
// written), bounded by the number of distinct briefs the sprint has ever held, never
// by the number of checks that read them. 2026-10-17 19:16:10 EDT the sprint server
// wedged (TICK WEDGED): 251 verbs waited on the control line while one ack held it
// inside TickDeadlines, the deal's check of every ready card on every member
// (readyWhy -> launchersOf -> routeServed -> stagingRepoWhy -> stagingRepoRefusals ->
// cardRepo) splitting a primary's whole brief with swarm.ReadCardBase once per fleet
// card per member per ready card.
var briefRepoCache sync.Map // brief string -> *briefRepoRead

// briefRepo is a brief's repository, read once per brief: every later read of the same
// brief is the memo, so stagingRepoRefusals' walk of the fleet reads a primary's brief
// once, not once per fleet card per member per check. owner/name, "" when none; the
// answer is unchanged from reading the brief each time.
func briefRepo(brief string) string {
	if v, ok := briefRepoCache.Load(brief); ok {
		return v.(*briefRepoRead).repo
	}
	repo := ""
	cb := swarm.ReadCardBase([]byte(brief))
	if cb.Named != "" {
		repo = RepoName(cb.Named)
	} else if cb.Repo != "" {
		repo = RepoName(cb.Repo)
	}
	v, _ := briefRepoCache.LoadOrStore(brief, &briefRepoRead{repo: repo})
	v.(*briefRepoRead).reads.Add(1)
	return repo
}

// cardRepo is the repository a card is of: the card's own repo field (FieldRepo; cards
// remember their repo), else its primary's field or brief, else its stream's.
// owner/name, "" when none.
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
		// A stream may contain several repositories. Its control card holds
		// their union; the primary brief identifies this card's repository.
		brief := p.F("brief")
		// Most eligibility checks have no repository header. Avoid allocating
		// parser lines per ready card; the parser still validates every match.
		if strings.Contains(brief, "REPO:") || strings.Contains(brief, "base-repo:") {
			if repo := briefRepo(brief); repo != "" {
				return repo
			}
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
// that repository at staging for want of a mirror or credential within
// StagingMirrorWindow, so it cannot stage any card of it, as a harness it cannot
// launch bars a route. "" when the member refused nothing of c's repository, or
// c's repository is unknown. See tla/StagingRepoWindow.tla, NoBarredRepositoryDealt.
func (s *Snapshot) stagingRepoWhy(member string, c *Card) string {
	if member == "" || c == nil {
		return ""
	}
	repo := s.cardRepo(c)
	if repo == "" {
		return ""
	}
	at, refused := stagingRepoRefusals(s, true)[member][repo]
	if !refused {
		return ""
	}
	when := "an earlier tick"
	if !at.IsZero() {
		when = at.UTC().Format(time.RFC3339)
	}
	why := member + " refused " + repo + " at staging at " + when
	if mirrorAt, mirror := mirrorRefusals(s)[member][repo]; mirror && mirrorAt.Equal(at) {
		return why + ": no bench mirror; add the mirror on " + member + " with the fleet play"
	}
	return why + ": repository credential refused on " + member
}

// stagingRefusalWhy is why the first member of pool that refused the primary c's repository
// at staging for want of a mirror or credential (stagingRepoWhy) cannot take it:
// the sentence the deal's and the level's refusal says when no member is left, so
// the coordinator reads the repository refusal, never the routes the member cannot
// launch. "" when no member of pool refused c's repository that way. The owner,
// 2026-10-11: "cards only may accrue per-fleet or friend,
// if there are not other machines/friends ready to do that work right now!"
func (s *Snapshot) stagingRefusalWhy(c *Card, pool []string) string {
	for _, m := range pool {
		if w := s.stagingRepoWhy(m, c); w != "" {
			return w
		}
	}
	return ""
}

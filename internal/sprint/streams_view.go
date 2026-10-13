package sprint

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// FieldRepo is a stream's control card field: the repositories its cards name, in the
// order their briefs name them (docs/SPEC-SPRINT.md section 11, the streams verb).
const FieldRepo = "repo"

// FieldBase is a stream's control card field: the bases its cards name, from each
// brief's BASE: line, at admission.
const FieldBase = "base"

// StreamsReq is what a streams listing shows: only the streams recording a repository,
// only the streams of a release, and every card of each stream when Cards.
type StreamsReq struct {
	Repos   []string
	Release string
	Cards   bool
}

// StreamCard is one card of a stream in a streams listing: its state, tier, the first
// sentence of its THE TASK, and the ids it needs.
type StreamCard struct {
	ID    string   `json:"id"`
	State string   `json:"state"`
	Tier  string   `json:"tier,omitempty"`
	Title string   `json:"title,omitempty"`
	Needs []string `json:"needs"`
}

// StreamRow is one stream in a streams listing: the repositories and bases its cards
// record, its release, the name it is shown by (its display name; else "point-fix-"
// plus its release when one is set; else its key), the state it shows, its open and
// landed counts, and its cards when asked.
type StreamRow struct {
	Stream  string       `json:"stream"`
	Repos   []string     `json:"repos"`
	Bases   []string     `json:"bases"`
	Release string       `json:"release,omitempty"`
	Display string       `json:"display,omitempty"`
	State   string       `json:"state,omitempty"`
	Open    int          `json:"open"`
	Landed  int          `json:"landed"`
	Cards   []StreamCard `json:"cards,omitempty"`
}

// DisplayMax is a stream's display name's bound: one line of printable text, at most
// this many bytes (stream set --display, docs/SPEC-SPRINT.md section 11).
const DisplayMax = 60

// DisplayNameWhy says why a stream's display name is refused: empty is fine (it means
// none), a name is one line of printable text, at most DisplayMax bytes, and any other
// byte is refused naming the bound (stream set --display, docs/SPEC-SPRINT.md section 11).
func DisplayNameWhy(name string) string {
	if len(name) > DisplayMax {
		return fmt.Sprintf("--display wants one line of printable text, at most %d bytes; found %d bytes", DisplayMax, len(name))
	}
	if !utf8.ValidString(name) {
		return fmt.Sprintf("--display wants one line of printable text, at most %d bytes; found invalid UTF-8", DisplayMax)
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return fmt.Sprintf("--display wants one line of printable text, at most %d bytes; found a non-printable character", DisplayMax)
		}
	}
	return ""
}

// StreamDisplay is the name a stream is shown by (docs/SPEC-SPRINT-DASHBOARD.md, Work):
// its display name when set; else, when a release is set, "point-fix-" plus the release
// exactly as stored; else its key. A version is never parsed out of the key.
func StreamDisplay(stream, display, release string) string {
	if display != "" {
		return display
	}
	if release != "" {
		return "point-fix-" + release
	}
	return stream
}

// StreamsView is the whole listing: one row per stream, and the streams whose cards
// name more than one repository (the finding the verb prints).
type StreamsView struct {
	Streams []StreamRow `json:"streams"`
	Mixed   []string    `json:"mixed,omitempty"`
}

// StreamReposAndBases gives a stream's repos and bases: the union of its open
// cards' briefs' REPO and BASE as basesInUse reads them, the control card's
// fields only where no brief names one. streams and bases both call it; neither
// keeps its own reading (docs/SPEC-SPRINT.md section 11).
func StreamReposAndBases(s *Snapshot, stream string) (repos, bases []string) {
	var briefRepos, briefBases []string
	seenRepo := map[string]bool{}
	seenBase := map[string]bool{}
	for _, col := range States {
		if !IsOpen(col) {
			continue
		}
		for _, c := range s.Work.Cell(stream, col) {
			if c.F("kind") == "sentinel" {
				continue
			}
			cb := swarm.ReadCardBase([]byte(c.F("brief")))
			r := cb.Named
			if r == "" {
				r = cb.Repo
			}
			if r != "" && !seenRepo[r] {
				seenRepo[r] = true
				briefRepos = append(briefRepos, r)
			}
			if cb.Ref != "" && !seenBase[cb.Ref] {
				seenBase[cb.Ref] = true
				briefBases = append(briefBases, cb.Ref)
			}
		}
	}
	if len(briefRepos) > 0 {
		repos = briefRepos
	} else {
		if ctl := s.StreamCtl(stream); ctl != nil {
			for _, r := range Split(ctl.F(FieldRepo)) {
				if !seenRepo[r] {
					seenRepo[r] = true
					repos = append(repos, r)
				}
			}
		}
		for _, c := range s.Work.Cards() {
			if c.Placed() && c.Row == stream {
				for _, r := range Split(c.F(FieldRepo)) {
					if !seenRepo[r] {
						seenRepo[r] = true
						repos = append(repos, r)
					}
				}
			}
		}
	}
	if len(briefBases) > 0 {
		bases = briefBases
	} else {
		if ctl := s.StreamCtl(stream); ctl != nil {
			for _, b := range Split(ctl.F(FieldBase)) {
				if !seenBase[b] {
					seenBase[b] = true
					bases = append(bases, b)
				}
			}
		}
		for _, c := range s.Work.Cards() {
			if c.Placed() && c.Row == stream {
				for _, b := range Split(c.F(FieldBase)) {
					if !seenBase[b] {
						seenBase[b] = true
						bases = append(bases, b)
					}
				}
			}
		}
	}
	sort.Strings(repos)
	sort.Strings(bases)
	return repos, bases
}

// CardRepoAndBase gives a card's repo and base: its brief's REPO and BASE as
// swarm.ReadCardBase reads them, falling back to its stream's repo and base
// (StreamReposAndBases) where the brief names none.
func CardRepoAndBase(s *Snapshot, c *Card) (repo, base string) {
	if c == nil {
		return "", ""
	}
	cb := swarm.ReadCardBase([]byte(c.F("brief")))
	repo = cb.Named
	if repo == "" {
		repo = cb.Repo
	}
	base = cb.Ref
	if repo == "" || base == "" {
		sRepos, sBases := StreamReposAndBases(s, c.Row)
		if repo == "" && len(sRepos) > 0 {
			repo = sRepos[0]
		}
		if base == "" && len(sBases) > 0 {
			base = sBases[0]
		}
	}
	return repo, base
}

// StreamRepos is the repositories a stream's cards name (StreamReposAndBases).
func StreamRepos(s *Snapshot, stream string) []string {
	repos, _ := StreamReposAndBases(s, stream)
	return repos
}

// StreamBasesOf is the bases a stream's cards name (StreamReposAndBases).
func StreamBasesOf(s *Snapshot, stream string) []string {
	_, bases := StreamReposAndBases(s, stream)
	return bases
}

// StreamTitle is the first sentence of a brief's THE TASK: the text after `THE TASK.`
// or `THE TASK:` at the start of a line, to the first `. ` and without its final dot.
// "" when the brief names no task.
func StreamTitle(brief string) string {
	for _, line := range strings.Split(brief, "\n") {
		t := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(t, "THE TASK.")
		if !ok {
			rest, ok = strings.CutPrefix(t, "THE TASK:")
		}
		if !ok {
			continue
		}
		title := strings.TrimSpace(rest)
		if i := strings.Index(title, ". "); i >= 0 {
			title = title[:i]
		}
		return strings.TrimSpace(strings.TrimSuffix(title, "."))
	}
	return ""
}

// StreamsOf is the streams listing: every stream row, with the repositories and bases
// its cards record, its release, its open and landed counts and, with Cards, every card
// in work order (a state at a time). Only keeps the streams a repository in Repos
// records (any of them) and, with Release set, the streams of that release. Mixed names
// every stream whose cards name more than one repository.
func StreamsOf(s *Snapshot, r StreamsReq) StreamsView {
	var v StreamsView
	for _, stream := range s.Work.Rows() {
		repos, bases := StreamReposAndBases(s, stream)
		if len(r.Repos) > 0 && !anyRepo(r.Repos, repos) {
			continue
		}
		var release, display string
		var fields map[string]string
		if ctl := s.StreamCtl(stream); ctl != nil {
			release = ctl.F(FieldRelease)
			display = ctl.F(FieldDisplay)
			fields = ctl.Fields
		}
		if r.Release != "" && release != r.Release {
			continue
		}
		row := StreamRow{Stream: stream, Repos: repos, Bases: bases, Release: release, Display: StreamDisplay(stream, display, release)}
		for _, col := range States {
			for _, c := range s.Work.Cell(stream, col) {
				if IsOpen(col) {
					row.Open++
				} else {
					row.Landed++
				}
				if r.Cards {
					row.Cards = append(row.Cards, StreamCard{ID: c.ID, State: col, Tier: c.F(FieldTier),
						Title: StreamTitle(c.F("brief")), Needs: Split(c.F("needs"))})
				}
			}
		}
		row.State = ShownStreamState(fields, row.Open)
		if len(repos) > 1 {
			v.Mixed = append(v.Mixed, stream)
		}
		v.Streams = append(v.Streams, row)
	}
	sort.Strings(v.Mixed)
	return v
}

// anyRepo says one of the wanted repositories is one of the repositories recorded.
func anyRepo(want, have []string) bool {
	for _, w := range want {
		for _, h := range have {
			if RepoName(w) == RepoName(h) {
				return true
			}
		}
	}
	return false
}

// repoName is a repository as the owner/name a --repo flag names: a URL or a clone
// path's last two path segments, without a .git suffix; a value already owner/name
// unchanged.
func RepoName(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, ".git")
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	} else if i := strings.Index(v, "@"); i >= 0 && strings.Contains(v, ":") {
		v = v[i+1:]
		if j := strings.Index(v, ":"); j >= 0 {
			v = v[j+1:]
		}
	}
	parts := strings.Split(strings.Trim(v, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return v
}

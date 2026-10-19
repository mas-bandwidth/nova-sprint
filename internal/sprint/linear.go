package sprint

import (
	"fmt"
	"strings"
)

// PropLinearBases is the sprint-row setting described by docs/SPEC-SPRINT.md section 7.
const PropLinearBases = "linear_bases"

func linearBasesValid(v string) bool {
	if v == ReadTierDefault {
		return true
	}
	for _, pair := range strings.Split(v, ",") {
		at := strings.LastIndex(pair, ":")
		if at < 1 || at == len(pair)-1 || strings.TrimSpace(pair) != pair || !strings.Contains(pair[:at], "/") || strings.ContainsAny(pair[at+1:], " \t,") {
			return false
		}
	}
	return v != ""
}

// IsLinearBase is the single source of truth for the per-repository/base mode
// specified by docs/SPEC-SPRINT.md section 7 and modelled by tla/LandLinear.tla.
func IsLinearBase(s *Snapshot, repo, base string) bool {
	if s == nil || s.Work == nil || repo == "" || base == "" {
		return false
	}
	v, ok := s.Work.Prop(PropLinearBases)
	if !ok || v == ReadTierDefault {
		return false
	}
	for _, pair := range strings.Split(v, ",") {
		at := strings.LastIndex(pair, ":")
		if at > 0 && repoKey(pair[:at]) == repoKey(repo) && pair[at+1:] == base {
			return true
		}
	}
	return false
}

// LinearCard is the information the linear replay needs from a landed card.
type LinearCard struct {
	ID, Title, Head, Source, PullRequest string
	Forward                            bool
}

// LinearSourceCommit is one source stream first-parent commit and its card identity.
type LinearSourceCommit struct {
	SHA, Card, Subject string
	Merge              bool
}

// LinearGit is the deliberately small Git boundary used by the pure replay coordinator.
// The command adapter implements it with the lander's existing git runner.
type LinearGit interface {
	LatestTrailer(base, key string) (string, error)
	MergeBase(base, sourceTip string) (string, error)
	FirstParentRange(from, to string) ([]LinearSourceCommit, error)
	CardOnBase(base, card string) (bool, error)
	CherryPick(sha string, mainline bool) error
	Squash(head string) error
	Tree(ref string) (string, error)
	Commit(message string) error
	Abort() error
	Tip() (string, error)
	MergeCommits(base, tip string) ([]string, error)
	Push(tip, base string) error
}

func linearMessage(id, title, source, pr string) string {
	if title == "" {
		title = id
	}
	body := "Finish report: nova-sprint card " + id
	if pr != "" {
		body = "Pull request: " + pr
	}
	return id + ": " + title + "\n\n" + body + "\n\nNova-Card: " + id + "\nNova-Linear-Source: " + source
}

// LandLinear replays a batch without merge commits. It performs no push until every
// replay/squash succeeds and the final range has been proved merge-free.
func LandLinear(g LinearGit, repo, base, sourceTip string, cards []LinearCard) error {
	last, err := g.LatestTrailer(base, "Nova-Linear-Source")
	if err != nil {
		return err
	}
	if last == "" {
		last, err = g.MergeBase(base, sourceTip)
		if err != nil {
			return err
		}
	}
	for _, card := range cards {
		if !card.Forward {
			if err := g.Squash(card.Head); err != nil {
				_ = g.Abort()
				return fmt.Errorf("linear replay refused card %s at %s: %w", card.ID, card.Head, err)
			}
			if err := g.Commit(linearMessage(card.ID, card.Title, card.Source, card.PullRequest)); err != nil {
				_ = g.Abort()
				return err
			}
			continue
		}
		commits, err := g.FirstParentRange(last, card.Source)
		if err != nil {
			return err
		}
		for _, c := range commits {
			seen, err := g.CardOnBase(base, c.Card)
			if err != nil {
				return err
			}
			if seen {
				last = c.SHA
				continue
			}
			if err := g.CherryPick(c.SHA, c.Merge); err != nil {
				_ = g.Abort()
				return fmt.Errorf("linear replay refused card %s at stream commit %s: %w", c.Card, c.SHA, err)
			}
			if err := g.Commit(linearMessage(c.Card, c.Subject, c.SHA, "")); err != nil {
				_ = g.Abort()
				return err
			}
			last = c.SHA
		}
		have, err := g.Tree("HEAD")
		if err != nil {
			return err
		}
		want, err := g.Tree(card.Head)
		if err != nil {
			return err
		}
		if have != want {
			if err := g.Squash(card.Head); err != nil {
				_ = g.Abort()
				return fmt.Errorf("linear replay refused card %s at stream commit %s: %w", card.ID, card.Source, err)
			}
			if err := g.Commit(linearMessage(card.ID, card.Title, card.Source, card.PullRequest)); err != nil {
				_ = g.Abort()
				return err
			}
		}
	}
	tip, err := g.Tip()
	if err != nil {
		return err
	}
	merges, err := g.MergeCommits(base, tip)
	if err != nil {
		return err
	}
	if len(merges) > 0 {
		return fmt.Errorf("the linear base %s:%s takes no merge commit; %s is one", repoKey(repo), base, merges[0])
	}
	return g.Push(tip, base)
}

func init() {
	Defaults = append(Defaults, Default{Name: PropLinearBases, Home: HomeProp, Value: ReadTierDefault, Why: "no repository base is linear until the coordinator names it"})
	workingChecks[PropLinearBases] = workingCheck{BySet: true}
}

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The deal's BASE reader (dealfacts.go) on fakes of gh and git: no network, no git, no
// clock but the one passed in (it starts at 1000 seconds).

func fakeDealFacts(runs string, jobs map[string]string, tip, files string) (*dealFactsReader, *int) {
	d := newDealFactsReader()
	calls := 0
	d.gh = func(_ context.Context, args ...string) (string, error) {
		calls++
		path := args[1]
		if strings.Contains(path, "/jobs") {
			for id, out := range jobs {
				if strings.Contains(path, "/runs/"+id+"/") {
					return out, nil
				}
			}
			return "", errors.New("no such run")
		}
		return runs, nil
	}
	d.git = func(_ context.Context, _ string, args ...string) (string, error) {
		switch args[0] {
		case "fetch":
			return "", nil
		case "rev-parse":
			return tip + "\n", nil
		case "ls-tree":
			return files, nil
		}
		return "", errors.New("unexpected git " + args[0])
	}
	d.mirror = func(string) (string, error) { return "/mirror", nil }
	return d, &calls
}

func TestTheDealFactsReaderReadsTheNewestCompletedBaseCommit(t *testing.T) {
	t.Parallel()
	const newest, done = "a497d2b2af01a497d2b2af01a497d2b2af01a497", "7b07e97be15f7b07e97be15f7b07e97be15f7b07"
	runs := strings.Join([]string{
		"11\tCI\t" + newest + "\tin_progress\t",
		"12\tTLC records\t" + newest + "\tcompleted\tfailure",
		"21\tCI\t" + done + "\tcompleted\tfailure",
		"22\tdocs\t" + done + "\tcompleted\tsuccess",
	}, "\n")
	jobs := map[string]string{"21": "ubuntu-24.04 / Debug\tfailure\tubuntu-24.04\nwindows / Release (a)\tfailure\twindows-2022\nhappy path / macos-15\tsuccess\tmacos-15\n"}
	d, calls := fakeDealFacts(runs, jobs, newest, "functional/main.cpp\nfunctional/posix_hard_killed_client.cpp\n")
	b := sprint.RepoBase{Repo: "mas-bandwidth/rocketnet", Base: "main"}
	now := time.Unix(1000, 0)

	ci, trees := d.facts(now, []sprint.RepoBase{b})
	assert.Empty(t, ci, "a base never read has no fact: the deal holds nothing")
	assert.Empty(t, trees)

	d.refresh(context.Background(), now)
	ci, trees = d.facts(now, []sprint.RepoBase{b})
	require.Len(t, ci, 1)
	assert.Equal(t, done, ci[0].Tip, "the tip is the newest commit whose runs all completed")
	assert.True(t, ci[0].Red)
	var names []string
	for _, c := range ci[0].TipFailed {
		names = append(names, c.Name+"="+strings.Join(c.Platforms, ","))
	}
	assert.Equal(t, []string{"CI / ubuntu-24.04 / Debug=linux", "CI / windows / Release (a)=windows"}, names)
	require.Len(t, trees, 1)
	assert.Equal(t, newest, trees[0].Sha)
	assert.Equal(t, []string{"functional/main.cpp", "functional/posix_hard_killed_client.cpp"}, trees[0].Files)

	// read again only after dealFactsEvery; a completed run's jobs are read once
	before := *calls
	d.refresh(context.Background(), now.Add(time.Second))
	assert.Equal(t, before, *calls, "read again inside dealFactsEvery")
	d.refresh(context.Background(), now.Add(dealFactsEvery))
	assert.Equal(t, before+1, *calls, "the runs are read again, the completed run's jobs not")

	// a base no tick asks for is forgotten
	d.refresh(context.Background(), now.Add(dealFactsForget+2*time.Minute))
	assert.Empty(t, d.due(now.Add(dealFactsForget+3*time.Minute)))
	ci, _ = d.facts(now.Add(dealFactsForget+3*time.Minute), []sprint.RepoBase{b})
	assert.Empty(t, ci, "a forgotten base kept its fact")
}

func TestTheDealFactsReaderKeepsNoOlderTipsTree(t *testing.T) {
	t.Parallel()
	d, _ := fakeDealFacts("", nil, "abc", "a.go\n")
	b := sprint.RepoBase{Repo: "o/r", Base: "main"}
	now := time.Unix(1000, 0)
	d.facts(now, []sprint.RepoBase{b})
	d.refresh(context.Background(), now)
	_, trees := d.facts(now, []sprint.RepoBase{b})
	require.Len(t, trees, 1)
	d.git = func(context.Context, string, ...string) (string, error) { return "", errors.New("fetch failed") }
	d.refresh(context.Background(), now.Add(dealFactsEvery))
	_, trees = d.facts(now.Add(dealFactsEvery), []sprint.RepoBase{b})
	assert.Empty(t, trees, "a tree that could not be read again stood for a tip it may not be")
}

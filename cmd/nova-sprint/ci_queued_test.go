package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeCI is a ciLister with canned answers, and no forge: the queued jobs, the
// online runners, and the runners read's error.
type fakeCI struct {
	jobs       []ciQueuedJob
	runners    []ciRunner
	runnersErr error
}

func (f fakeCI) QueuedJobs(ctx context.Context, repo string) ([]ciQueuedJob, error) {
	return f.jobs, nil
}

func (f fakeCI) RunnersOnline(ctx context.Context, repo string) ([]ciRunner, error) {
	return f.runners, f.runnersErr
}

// The queued-CI reader names whether a runner for a job's labels is online: "no"
// when the runners list is empty, "unknown" when the runners read was refused (a
// 403, say), and "yes" when an online runner carries every label.
func TestTheCIQueuedReaderNamesTheRunnersState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const repo = "mas-bandwidth/rocketnet"
	labels := []string{"self-hosted", "Linux", "X64", "rocketnet"}
	since := time.Date(2030, 1, 2, 2, 47, 0, 0, time.UTC)
	job := ciQueuedJob{RunID: "42", Run: "CI", Job: "build", Labels: labels, QueuedSince: since}

	// 1. no runners at all: the job has no runner
	got := ciQueuedFactsOf(ctx, fakeCI{jobs: []ciQueuedJob{job}}, repo)
	require.Len(t, got, 1)
	require.Equal(t, "no", got[0].RunnerOnline, "an empty runners list is no runner")
	require.Equal(t, repo, got[0].Repo)
	require.Equal(t, "build", got[0].Job)
	require.Equal(t, labels, got[0].Labels)

	// 2. a 403 on the runners list: unknown, and never an error
	got = ciQueuedFactsOf(ctx, fakeCI{jobs: []ciQueuedJob{job}, runnersErr: errors.New("gh api: 403 Forbidden")}, repo)
	require.Len(t, got, 1)
	require.Equal(t, "unknown", got[0].RunnerOnline, "a refused runners read is unknown")

	// 3. an online runner carrying every label: yes
	got = ciQueuedFactsOf(ctx, fakeCI{jobs: []ciQueuedJob{job}, runners: []ciRunner{{Name: "r1", Labels: labels}}}, repo)
	require.Len(t, got, 1)
	require.Equal(t, "yes", got[0].RunnerOnline, "a runner with every label is online")

	// an online runner missing a label is no runner for this job
	got = ciQueuedFactsOf(ctx, fakeCI{jobs: []ciQueuedJob{job}, runners: []ciRunner{{Name: "r1", Labels: labels[:3]}}}, repo)
	require.Equal(t, "no", got[0].RunnerOnline, "a runner missing a label is no runner")
}

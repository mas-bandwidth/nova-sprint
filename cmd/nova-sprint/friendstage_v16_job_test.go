package main

import (
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/stretchr/testify/assert"
)

// A v1.6 stage wrote JOB.md with no BENCH line, because it only ever staged on
// the friend's own machine. A re-run of such a job directory on this machine
// (a local run) accepts it and prints the JOB.md path; a re-run with --bench
// refuses, because a bench stage never wrote that directory.
func TestFriendStageRerunsAJobV16StagedWithNoBenchLine(t *testing.T) {
	ta, _ := amyWithIdentity(t)
	dealToAmy(t, ta)

	rec := &benchRec{t: t, files: map[string]string{}}
	rec.install()

	jobs := filepath.Join(t.TempDir(), "jobs")
	jobDir := filepath.Join(jobs, "s1-1.w1")

	v16 := "CARD: s1-1.w1\n" +
		"GEN: 1\n" +
		"BRANCH: sprint/s1-1.w1.g1.e0\n" +
		"BASE: sprint/s1\n" +
		"HEAD: sprint/s1\n" +
		"FINISH: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1\n" +
		"FINISH-FAILED: nova-sprint finish --as friend.amy s1-1.w1@1 --epoch 0 --head sprint/s1 --branch sprint/s1-1.w1.g1.e0 --base sprint/s1 --failed\n"
	rec.files[filepath.Join(jobDir, friend.JobFile)] = v16

	code, out, errs := ta.do("friend stage s1-1.w1@1 --as friend.amy --dir " + jobs)
	assert.Equal(t, 0, code, "%s", errs)
	assert.Contains(t, out, filepath.Join(jobDir, "JOB.md"))

	code, _, errs = ta.do("friend stage s1-1.w1@1 --as friend.amy --bench m1 --dir " + jobs)
	assert.Equal(t, 1, code)
	assert.Contains(t, errs, "already staged as another card, branch, head or bench")
}

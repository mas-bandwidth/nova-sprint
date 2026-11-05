package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
	"github.com/mas-bandwidth/nova-tools/pkg/member"
)

// laneRig is a Launch whose children are fake procs the test ends: started receives
// each one as it starts, with the argv and environment it was given.
type laneRig struct {
	l       *Launch
	started chan spawned
	spent   int64 // the tokens the fake usage reads
}

type spawned struct {
	proc      *fakeProc
	argv, env []string
	dir       string
}

func newLaneRig(t *testing.T) *laneRig {
	r := &laneRig{started: make(chan spawned, 1)}
	root := t.TempDir()
	r.l = &Launch{Bench: "m1", Root: root, Home: t.TempDir(), Harness: "/opt/harness/opencode", Wall: "/opt/wall/nova-sandbox",
		Env:         []string{"PATH=/usr/bin:/bin", "LANG=C", "X_API_KEY=x-secret", "Y_API_KEY=y-secret", "GH_TOKEN=gh-secret", "OTHER=1"},
		SampleEvery: 15 * time.Second,
		Spawn: func(argv, env []string, dir string, _ *os.File) (Proc, error) {
			p := &fakeProc{exit: make(chan int, 1)}
			r.started <- spawned{p, argv, env, dir}
			return p, nil
		},
		Usage: func(string) (cardcost.Usage, bool) {
			u := cardcost.NoUsage()
			u.Tokens.Input = r.spent
			return u, true
		},
	}
	return r
}

func (r *laneRig) start(t *testing.T, p member.Packet) (*procLane, spawned) {
	t.Helper()
	ln, err := r.l.Start(p, t0)
	require.NoError(t, err)
	return ln.(*procLane), <-r.started
}

func readPacket() member.Packet {
	return member.Packet{Card: "s1-1.r1.m1", Kind: "read", Attempt: 2, Gen: 1, Epoch: 3, Model: "x/y", Tokens: "unmetered", Deadline: 600}
}

// The child runs under the wall around the harness, in its job, handed the route's own
// provider key and no other secret; its home, data and temp are the launch's own.
func TestTheChildRunsUnderTheWallWithOneKey(t *testing.T) {
	r := newLaneRig(t)
	p := workPacket("s1-1.w1")
	ln, sp := r.start(t, p)
	assert.Equal(t, "/opt/wall/nova-sandbox", sp.argv[0])
	assert.True(t, hasRun(sp.argv, "--write", ln.job))
	assert.True(t, hasRun(sp.argv, "--cwd", ln.job))
	assert.True(t, hasRun(sp.argv, "--", "/opt/harness/opencode", "run", "--model", "x/y", "--title", p.Card, "--"))
	prompt := sp.argv[len(sp.argv)-1]
	assert.True(t, strings.HasPrefix(prompt, "Read "+filepath.Join(ln.job, "JOB.md")+" first."), prompt)
	assert.Contains(t, prompt, "Your wall: this run is ended at 10 minutes")
	assert.Equal(t, ln.job, sp.dir)
	env := strings.Join(sp.env, "\n")
	assert.Contains(t, env, "X_API_KEY=x-secret")
	assert.NotContains(t, env, "y-secret")
	assert.NotContains(t, env, "gh-secret")
	assert.NotContains(t, env, "OTHER=")
	assert.Contains(t, env, "HOME="+ln.data)
	assert.Contains(t, env, "TMPDIR="+ln.tmp)
	assert.Contains(t, env, "PATH="+ln.shims()+":/usr/bin:/bin")
	shim, err := os.ReadFile(filepath.Join(ln.shims(), "sh"))
	require.NoError(t, err)
	assert.Contains(t, string(shim), `index(u,"KEY")`)
	_, err = os.Stat(filepath.Join(ln.job, "JOB.md"))
	require.NoError(t, err, "the frame is installed")
	sp.proc.exit <- 0
	<-ln.finished
}

// A lane past its deadline, counted from the first pass that sees its child run, is
// ended and reported with End deadline.
func TestADeadlineEndsTheLane(t *testing.T) {
	r := newLaneRig(t)
	ln, sp := r.start(t, workPacket("s1-1.w1"))
	assert.False(t, ln.Poll(t0.Add(time.Hour)), "the first pass that sees the child arms its deadline")
	assert.False(t, ln.Poll(t0.Add(time.Hour+9*time.Minute)))
	assert.False(t, sp.proc.isKilled())
	ln.Poll(t0.Add(time.Hour + 10*time.Minute))
	assert.True(t, sp.proc.isKilled())
	<-ln.finished
	require.True(t, ln.Poll(t0.Add(2*time.Hour)))
	res, _ := ln.Result()
	assert.Equal(t, member.EndDeadline, res.End)
	assert.False(t, res.Ran)
	fin, why := member.Judge(res, member.Push{})
	assert.Equal(t, member.FinishFailed, fin)
	assert.True(t, strings.HasPrefix(why, "deadline: "), why)
}

// A metered lane whose spend reaches its tokens is ended with End budget; the spend is
// read at most once in SampleEvery.
func TestATokenBudgetEndsTheLane(t *testing.T) {
	r := newLaneRig(t)
	ln, sp := r.start(t, workPacket("s1-1.w1"))
	ln.Poll(t0)
	r.spent = 2000
	ln.Poll(t0.Add(5 * time.Second))
	assert.False(t, sp.proc.isKilled(), "not sampled yet")
	ln.Poll(t0.Add(15 * time.Second))
	assert.True(t, sp.proc.isKilled())
	<-ln.finished
	res, _ := ln.Result()
	assert.Equal(t, member.EndBudget, res.End)
	assert.Equal(t, "budget: tokens 2000 of 1000", res.Budget)
	assert.Contains(t, res.Usage, "budget=2000/1000")
}

// A child that exits nonzero with no result is a failed run with its exit and output; its
// RESULT.md, when it wrote one, is read for a read's verdict.
func TestAFailedHarnessAndAReadVerdict(t *testing.T) {
	r := newLaneRig(t)
	ln, sp := r.start(t, workPacket("s1-1.w1"))
	require.NoError(t, os.WriteFile(ln.log, []byte("panic: boom\n"), 0o644))
	sp.proc.exit <- 3
	<-ln.finished
	res, push := ln.Result()
	assert.Equal(t, 3, res.Exit)
	assert.False(t, res.Ran)
	assert.Contains(t, res.HarnessOutput, "panic: boom")
	assert.Contains(t, push.None, "names no commit")

	rd, sp := r.start(t, readPacket())
	assert.Equal(t, filepath.Join(rd.job, "tmp"), rd.tmp, "a read's scratch is in its job")
	require.NoError(t, os.WriteFile(filepath.Join(rd.job, "RESULT.md"), []byte("head: -\nbranch: -\nverdict: ok\ngate: -\noutput: -\nreport: reads clean\n"), 0o644))
	sp.proc.exit <- 0
	<-rd.finished
	res, _ = rd.Result()
	assert.True(t, res.Ran)
	assert.Equal(t, "ok", res.Verdict)
	assert.Equal(t, "reads clean", res.Report)
}

// A packet with no route is refused before anything is made.
func TestAPacketWithNoRouteIsRefused(t *testing.T) {
	r := newLaneRig(t)
	p := workPacket("s1-1.w1")
	p.Model = ""
	_, err := r.l.Start(p, t0)
	assert.ErrorContains(t, err, "names no route")
}

// Failed launches are kept, the newest KeepFailed; a launch that did its work is removed.
func TestFailedLaunchesAreKeptNewestFive(t *testing.T) {
	r := newLaneRig(t)
	var lanes []*procLane
	for i := range KeepFailed + 1 {
		p := workPacket("s1-1.w" + string(rune('1'+i)))
		ln, sp := r.start(t, p)
		sp.proc.exit <- 1
		<-ln.finished
		lanes = append(lanes, ln)
	}
	for _, ln := range lanes {
		ln.Remove(true)
	}
	_, err := os.Stat(lanes[0].slot)
	assert.True(t, os.IsNotExist(err), "the oldest failure is pushed out")
	_, err = os.Stat(lanes[KeepFailed].slot)
	assert.NoError(t, err)
	ok, sp := r.start(t, workPacket("s1-1.x1"))
	sp.proc.exit <- 0
	<-ok.finished
	ok.Remove(false)
	_, err = os.Stat(ok.slot)
	assert.True(t, os.IsNotExist(err))
}

// The harness's messages fold into one usage record.
func TestSumUsage(t *testing.T) {
	u := SumUsage([][]string{
		{"deepseek-flash", "deepseek", "10", "5", "0", "100", "2", "0.01"},
		{"deepseek-flash", "deepseek", "20", "5", "1", "200", "0", "0.02"},
	})
	assert.Equal(t, "deepseek/deepseek-flash", u.Model)
	assert.Equal(t, int64(30), u.Tokens.Input)
	assert.Equal(t, int64(300), u.Tokens.CacheRead)
	assert.Equal(t, int64(2), u.Tokens.Requests)
	assert.Equal(t, int64(343), u.Tokens.Total())
	assert.Equal(t, "0.03", u.Actual)
}

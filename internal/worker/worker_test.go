package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/member"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// fakeServer answers the worker's verbs from a queue the test sets; every verb is
// recorded. code, when set for a verb word, is its exit code.
type fakeServer struct {
	q      queueOut
	take   []member.Packet
	code   map[string]int
	served [][]string
}

func (s *fakeServer) send(_ context.Context, verbs ...[]string) ([]sprintwire.Result, error) {
	var out []sprintwire.Result
	for _, v := range verbs {
		s.served = append(s.served, v)
		r := sprintwire.Result{Code: s.code[v[0]]}
		switch v[0] {
		case "queue":
			b, _ := json.Marshal(s.q)
			r.Stdout = string(b)
		case "take":
			b, _ := json.Marshal(map[string]any{"packets": s.take})
			r.Stdout = string(b)
			s.take = nil
		}
		out = append(out, r)
	}
	return out, nil
}

// sent are the verbs served whose first word is verb.
func (s *fakeServer) sent(verb string) [][]string {
	var got [][]string
	for _, v := range s.served {
		if v[0] == verb {
			got = append(got, v)
		}
	}
	return got
}

// fakeLane ends when the test says, with the result given.
type fakeLane struct {
	done          bool
	res           member.Result
	push          member.Push
	stopped       bool
	removed, kept bool
	printed       time.Time
}

func (l *fakeLane) Poll(time.Time) bool                  { return l.done }
func (l *fakeLane) Result() (member.Result, member.Push) { return l.res, l.push }
func (l *fakeLane) Printed() time.Time                   { return l.printed }
func (l *fakeLane) Stop()                                { l.stopped = true }
func (l *fakeLane) Remove(keep bool)                     { l.removed, l.kept = true, keep }

type fakeLauncher struct {
	lanes map[string]*fakeLane
	fail  error
}

func (f *fakeLauncher) Start(p member.Packet, _ time.Time) (Lane, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	l := &fakeLane{}
	f.lanes[p.Card] = l
	return l, nil
}

const sha = "0123456789abcdef0123456789abcdef01234567"

var t0 = time.Unix(1_800_000_000, 0)

func workPacket(id string) member.Packet {
	return member.Packet{Card: id, Kind: "work", As: "m1", Primary: "s1-1", Attempt: 1, Gen: 1, Epoch: 3, Branch: "sprint/" + id + ".g1.e3",
		Route: "heavy", Model: "x/y", Tokens: "1000", Deadline: 600}
}

func newRig(reader bool) (*Worker, *fakeServer, *fakeLauncher, *bytes.Buffer) {
	s := &fakeServer{code: map[string]int{}, q: queueOut{Epoch: 3, Width: 2}}
	l := &fakeLauncher{lanes: map[string]*fakeLane{}}
	var out bytes.Buffer
	as := "m1"
	if reader {
		as = "reader-m1"
	}
	return New(Config{As: as, Reader: reader, Launcher: l, Out: &out}, s.send), s, l, &out
}

func hasRun(v []string, words ...string) bool {
	for i := range v {
		if i+len(words) <= len(v) && slices.Equal(v[i:i+len(words)], words) {
			return true
		}
	}
	return false
}

// A taken card runs, and its pushed result is finished with its head, branch and epoch;
// the launch is removed, not kept.
func TestTakeRunFinish(t *testing.T) {
	w, s, l, out := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	acted, err := w.Tick(t0)
	require.NoError(t, err)
	assert.Equal(t, 1, acted)
	take := s.sent("take")
	require.Len(t, take, 1)
	assert.Equal(t, []string{"take", "--as", "m1", "--limit", "2", "--json", "--epoch", "3"}, take[0])
	require.Contains(t, l.lanes, p.Card)

	// still running: nothing is reported
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 1, Packet: &p}}
	_, err = w.Tick(t0.Add(time.Second))
	require.NoError(t, err)
	assert.Empty(t, s.sent("finish"))

	ln := l.lanes[p.Card]
	ln.done = true
	ln.res = member.Result{Ran: true, OK: true, Shaped: true, Verdict: "ok", Head: sha, Report: "did the work"}
	ln.push = member.Push{Sha: sha}
	acted, err = w.Tick(t0.Add(2 * time.Second))
	require.NoError(t, err)
	assert.Equal(t, 1, acted)
	fin := s.sent("finish")
	require.Len(t, fin, 1)
	v := fin[0]
	assert.True(t, hasRun(v, "--as", "m1", p.Card+"@1"))
	assert.True(t, hasRun(v, "--head", sha, "--branch", p.Branch))
	assert.Equal(t, []string{"--epoch", "3"}, v[len(v)-2:])
	assert.NotContains(t, v, "--failed")
	assert.Contains(t, strings.Join(v, " "), "pushed="+sha+" to "+p.Branch+": did the work")
	assert.Zero(t, w.Running())
	assert.True(t, ln.removed)
	assert.False(t, ln.kept)
	assert.Contains(t, out.String(), "push "+p.Card+" pushed="+sha)
}

// A harness that died is finished failed with its exit code and its output's tail; the
// launch is kept for reading.
func TestAFailedHarnessIsFinishedFailed(t *testing.T) {
	w, s, l, _ := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, err := w.Tick(t0)
	require.NoError(t, err)
	ln := l.lanes[p.Card]
	ln.done = true
	ln.res = member.Result{Exit: 1, Report: "the child ended without a result (exit 1)", HarnessOutput: "boom\n"}
	ln.push = member.Push{None: "the child's result names no commit"}
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 1, Packet: &p}}
	_, err = w.Tick(t0.Add(time.Second))
	require.NoError(t, err)
	fin := s.sent("finish")
	require.Len(t, fin, 1)
	assert.Contains(t, fin[0], "--failed")
	assert.True(t, hasRun(fin[0], "--exit", "1", "--harness", "boom"))
	assert.Contains(t, strings.Join(fin[0], " "), "no RESULT.md shape")
	assert.NotContains(t, fin[0], "--head")
	assert.True(t, ln.kept)
}

// A finish the server did not answer is kept and sent again; the lane is held meanwhile,
// and the beat's live set names it held.
func TestAnUnansweredFinishIsSentAgain(t *testing.T) {
	w, s, l, _ := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, _ = w.Tick(t0)
	ln := l.lanes[p.Card]
	ln.done, ln.res, ln.push = true, member.Result{Ran: true, OK: true, Shaped: true, Verdict: "ok", Head: sha}, member.Push{Sha: sha}
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 1, Packet: &p}}
	s.code["finish"] = 2
	_, _ = w.Tick(t0.Add(time.Second))
	assert.Equal(t, 1, w.Running())
	w.Beat(t0)
	beat := s.sent("fleet")
	require.Len(t, beat, 1)
	assert.True(t, hasRun(beat[0], "--live", p.Card+"@1:held"))
	s.code["finish"] = 0
	_, _ = w.Tick(t0.Add(2 * time.Second))
	assert.Len(t, s.sent("finish"), 2)
	assert.Zero(t, w.Running())
}

// A card the sprint took back is reaped: its lane is ended and no report is sent.
func TestAMovedClaimIsReapedWithNoReport(t *testing.T) {
	w, s, l, out := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, _ = w.Tick(t0)
	moved := p
	moved.Gen = 2
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 2, Packet: &moved}}
	_, _ = w.Tick(t0.Add(time.Second))
	assert.True(t, l.lanes[p.Card].stopped)
	assert.Zero(t, w.Running())
	assert.Empty(t, s.sent("finish"))
	assert.Contains(t, out.String(), "reaped s1-1.w1: the claim moved")
}

// A STOPPED machine ends every lane, hands each back with a stop-return, and takes
// nothing until it runs again.
func TestAStoppedMachineStopReturnsItsLanes(t *testing.T) {
	w, s, l, _ := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, _ = w.Tick(t0)
	s.q.Machine = "STOPPED"
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 1, Packet: &p}, {ID: "s2-1.w1", Col: "ready"}}
	_, _ = w.Tick(t0.Add(time.Second))
	assert.True(t, l.lanes[p.Card].stopped)
	sr := s.sent("stop-return")
	require.Len(t, sr, 1)
	assert.Equal(t, []string{"stop-return", "--as", "m1", p.Card + "@1", "--epoch", "3", "--reason", StopReturnReason}, sr[0])
	assert.Len(t, s.sent("take"), 1, "nothing is taken while STOPPED")
}

// A launch that cannot start is handed back failed at once.
func TestALaunchThatCannotStartIsHandedBack(t *testing.T) {
	w, s, l, _ := newRig(false)
	l.fail = assert.AnError
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, _ = w.Tick(t0)
	fin := s.sent("finish")
	require.Len(t, fin, 1)
	assert.True(t, hasRun(fin[0], "--failed", "--report"))
	assert.Contains(t, strings.Join(fin[0], " "), "staging refused:")
	assert.Zero(t, w.Running())
}

// A reader begins the asked reads its width holds, reports a verdict, and hands back a
// read with none.
func TestAReaderBeginsReportsAndReturns(t *testing.T) {
	w, s, l, _ := newRig(true)
	r1 := member.Packet{Card: "s1-1.r1.reader-m1", Kind: "read", Attempt: 1, Gen: 1, Epoch: 3, Model: "x/y", Tokens: "1000", Deadline: 600}
	r2 := r1
	r2.Card = "s2-1.r1.reader-m1"
	r3 := r1
	r3.Card = "s3-1.r1.reader-m1"
	s.q.Cards = []queueCard{{ID: r1.Card, Col: "asked", Gen: 1, Packet: &r1}, {ID: r2.Card, Col: "asked", Gen: 1, Packet: &r2}, {ID: r3.Card, Col: "asked", Gen: 1, Packet: &r3}}
	_, _ = w.Tick(t0)
	begin := s.sent("read")
	require.Len(t, begin, 1)
	assert.Equal(t, []string{"read", "--as", "reader-m1", "--begin", r1.Card + "@1", r2.Card + "@1", "--epoch", "3"}, begin[0], "two lanes: the width")
	q := s.sent("queue")
	assert.True(t, hasRun(q[0], "--packets", "0"), "the first pass knows no width yet")

	l.lanes[r1.Card].done, l.lanes[r1.Card].res = true, member.Result{Ran: true, Shaped: true, Verdict: "ok", Report: "reads clean"}
	l.lanes[r2.Card].done, l.lanes[r2.Card].res = true, member.Result{Ran: false, Report: "the child ended without a result"}
	s.q.Cards = []queueCard{{ID: r1.Card, Col: "reading", Gen: 1, Packet: &r1}, {ID: r2.Card, Col: "reading", Gen: 1, Packet: &r2}}
	_, _ = w.Tick(t0.Add(time.Second))
	reads := s.sent("read")[1:]
	require.Len(t, reads, 2)
	assert.True(t, hasRun(reads[0], "--ok", r1.Card, "--finding", "reads clean"))
	assert.True(t, hasRun(reads[1], "--return", r2.Card))
	assert.Contains(t, strings.Join(reads[1], " "), "no verdict (ran=false")
	assert.True(t, hasRun(s.sent("queue")[1], "--packets", "0", "--have", r1.Card+","+r2.Card), "both lanes full: no packets wanted")
}

// A member's room is in half slots when a read card is ready: a work card holds two, a
// read card one.
func TestRoomIsInHalfSlotsWithReadsReady(t *testing.T) {
	w, _, _, _ := newRig(false)
	w.width = 2
	p := workPacket("s1-1.w1")
	w.lanes[p.Card] = &lane{p: p, l: &fakeLane{}, state: running}
	assert.Equal(t, 1, w.room(queueOut{Cards: []queueCard{{ID: "s2-1.w1", Col: "ready"}}}), "work alone: whole slots")
	assert.Equal(t, 2, w.room(queueOut{Cards: []queueCard{{ID: "s2-1.r1.m1", Col: "ready"}}}), "a read ready: half slots")
}

// A running lane that printed is stamped once in ProgressEvery.
func TestProgressIsStampedEveryThreeMinutes(t *testing.T) {
	w, s, l, _ := newRig(false)
	p := workPacket("s1-1.w1")
	s.q.Cards = []queueCard{{ID: p.Card, Col: "ready", Gen: 1}}
	s.take = []member.Packet{p}
	_, _ = w.Tick(t0)
	s.q.Cards = []queueCard{{ID: p.Card, Col: "working", Gen: 1, Packet: &p}}
	l.lanes[p.Card].printed = t0.Add(time.Minute)
	_, _ = w.Tick(t0.Add(2 * time.Minute))
	assert.Empty(t, s.sent("progress"))
	_, _ = w.Tick(t0.Add(ProgressEvery))
	pr := s.sent("progress")
	require.Len(t, pr, 1)
	assert.Equal(t, []string{"progress", "--as", "m1", p.Card + "@1", "--epoch", "3"}, pr[0])
	_, _ = w.Tick(t0.Add(ProgressEvery + 4*time.Minute))
	assert.Len(t, s.sent("progress"), 1, "nothing printed since the stamp")
}

// A draining worker takes nothing and beats --draining; its drain waits at most the
// longest deadline held, capped.
func TestADrainTakesNothing(t *testing.T) {
	w, s, _, _ := newRig(false)
	w.Drain()
	s.q.Cards = []queueCard{{ID: "s1-1.w1", Col: "ready"}}
	_, _ = w.Tick(t0)
	assert.Empty(t, s.sent("take"))
	w.Beat(t0)
	assert.Contains(t, s.sent("fleet")[0], "--draining")
	assert.Equal(t, DrainMost, DrainBound(time.Hour))
	assert.Equal(t, time.Minute, DrainBound(time.Minute))
}

// A disk under its floor refuses every take, said once.
func TestNoRoomRefusesTheTake(t *testing.T) {
	w, s, _, out := newRig(false)
	w.cfg.Room = func() (bool, string) { return false, "3 GiB free" }
	s.q.Cards = []queueCard{{ID: "s1-1.w1", Col: "ready"}}
	_, _ = w.Tick(t0)
	_, _ = w.Tick(t0.Add(time.Second))
	assert.Empty(t, s.sent("take"))
	assert.Equal(t, 1, strings.Count(out.String(), "take REFUSED: 3 GiB free"))
}

// The queue's width is followed every pass; an answer that is not the queue's is an error.
func TestWidthFollowsTheQueue(t *testing.T) {
	w, s, _, out := newRig(false)
	_, err := w.Tick(t0)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "width 0 -> 2 (nova-config)")
	s.code["queue"] = 2
	_, err = w.Tick(t0)
	assert.ErrorContains(t, err, "queue: exit 2")
}

// a lock for the fake proc's state, shared with the lane goroutine
type fakeProc struct {
	mu     sync.Mutex
	killed bool
	exit   chan int
}

func (p *fakeProc) Wait() int { return <-p.exit }
func (p *fakeProc) isKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}
func (p *fakeProc) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.killed {
		p.killed = true
		p.exit <- -1
	}
}

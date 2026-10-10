package main

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sort"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// Down is not an error when they are actually down (the owner, 2026-10-10): the seat
// check, through its binding, says a member that does not beat on an offline host down,
// and DOWN only when the host answers or gives no clear no-answer.

// offlineApp is a sprint whose m1 beats (the test app's live member) and whose captain and
// hulk never do, with an inventory naming machines and a probe answering by answers; the
// probes made are recorded.
func offlineApp(t *testing.T, machines []string, answers map[string]hostAnswer) (*testApp, *[]string) {
	t.Helper()
	return offlineAppOf(t, "m1,captain,hulk", machines, answers)
}

// offlineAppOf is offlineApp with the members named (m1 and m2 beat, the others never).
func offlineAppOf(t *testing.T, members string, machines []string, answers map[string]hostAnswer) (*testApp, *[]string) {
	t.Helper()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a --members " + members)
	ta.ok("start")
	ta.ok("tick")
	var mu sync.Mutex
	probed := []string{}
	o := mockHealthyOutside()
	o.hostProbe = func(_ context.Context, host string) hostAnswer {
		mu.Lock()
		probed = append(probed, host)
		sort.Strings(probed)
		mu.Unlock()
		return answers[host]
	}
	ta.a.outside = o
	ta.a.inventory = func(context.Context, string) ([]config.MachineWidth, error) {
		var ws []config.MachineWidth
		for _, m := range machines {
			ws = append(ws, config.MachineWidth{Machine: m, Default: true})
		}
		return ws, nil
	}
	return ta, &probed
}

var everyMachine = []string{"m1", "captain", "hulk"}

func TestSeatCheckSaysAMemberOnAnOfflineHostDown(t *testing.T) {
	t.Parallel()
	ta, probed := offlineApp(t, everyMachine, map[string]hostAnswer{
		"m1": {Answered: true}, "captain": {Offline: true}, "hulk": {Offline: true}})
	code, out, _ := ta.do("seat check")
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "MACHINERY fleet OK up=1 held=0 offline=captain,hulk down=0")
	// the beating member is probed only as the control of the seat's own network
	assert.Equal(t, []string{"captain", "hulk", "m1"}, *probed)
}

func TestSeatCheckSaysAMemberOnAReachableHostAFault(t *testing.T) {
	t.Parallel()
	ta, _ := offlineApp(t, everyMachine, map[string]hostAnswer{
		"m1": {Answered: true}, "captain": {Answered: true}, "hulk": {Offline: true}})
	code, out, _ := ta.do("seat check")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "MACHINERY fleet DOWN up=1 held=0 offline=hulk down=captain:never")
}

func TestSeatCheckNeverExcusesANameThatDoesNotResolve(t *testing.T) {
	t.Parallel()
	ta, _ := offlineApp(t, everyMachine, map[string]hostAnswer{
		"m1": {Answered: true}, "captain": {Err: "its name does not resolve: no such host"}, "hulk": {Offline: true}})
	code, out, _ := ta.do("seat check")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, `down=captain:never host="captain: its name does not resolve: no such host"`)
}

func TestSeatCheckExcusesNoneWhenTheSeatsOwnNetworkIsUnproved(t *testing.T) {
	t.Parallel()
	ta, _ := offlineApp(t, everyMachine, map[string]hostAnswer{
		"m1": {Offline: true}, "captain": {Offline: true}, "hulk": {Offline: true}})
	code, out, _ := ta.do("seat check")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "down=captain:never,hulk:never")
	assert.Contains(t, out, "control m1 did not answer either")
	assert.NotContains(t, out, "offline=")
}

func TestSeatCheckProbesOnlyAMemberWithAMachineRow(t *testing.T) {
	t.Parallel()
	ta, probed := offlineApp(t, []string{"m1", "hulk"}, map[string]hostAnswer{
		"m1": {Answered: true}, "captain": {Offline: true}, "hulk": {Offline: true}})
	code, out, _ := ta.do("seat check")
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, `offline=hulk down=captain:never host="captain: not probed: no machine row names it"`)
	assert.Equal(t, []string{"hulk", "m1"}, *probed)
}

func TestSeatCheckRunsEveryProbeAtOnceUnderOneBound(t *testing.T) {
	t.Parallel()
	ta, _ := offlineApp(t, everyMachine, nil)
	var started sync.WaitGroup
	started.Add(3) // captain, hulk and the control m1
	o := ta.a.outside
	o.probeBound = 2 * time.Second
	o.hostProbe = func(ctx context.Context, host string) hostAnswer {
		started.Done()
		started.Wait() // each probe waits for the others: serial probes would never all start
		if host == "m1" {
			return hostAnswer{Answered: true}
		}
		<-ctx.Done() // a host that never answers: the one bound ends every probe
		return classifyDial(ctx.Err())
	}
	ta.a.outside = o
	t0 := time.Now()
	code, out, _ := ta.do("seat check")
	assert.Less(t, time.Since(t0), 4*time.Second, "one bound over all the probes, not one per probe")
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "offline=captain,hulk")
}

// The seat is itself a fleet member ("studio"): reaching itself proves nothing of its
// network, so it is never the control, whether known by its hostname or by isSelf.
func TestSeatCheckNeverUsesThisMachineAsTheControl(t *testing.T) {
	t.Parallel()
	answers := map[string]hostAnswer{"m1": {Answered: true}, "m2": {Answered: true}, "captain": {Offline: true}, "hulk": {Offline: true}}

	t.Run("by hostname, the only beating member", func(t *testing.T) {
		t.Parallel()
		ta, probed := offlineApp(t, everyMachine, answers)
		o := ta.a.outside
		o.hostname = func() string { return "m1" }
		ta.a.outside = o
		code, out, _ := ta.do("seat check")
		assert.Equal(t, 1, code, out)
		assert.Contains(t, out, "down=captain:never,hulk:never")
		assert.Contains(t, out, "no beating member other than this machine")
		assert.NotContains(t, out, "offline=")
		assert.Equal(t, []string{"captain", "hulk"}, *probed, "this machine is not probed as the control")
	})
	t.Run("by isSelf, another beating member is the control", func(t *testing.T) {
		t.Parallel()
		ta, probed := offlineAppOf(t, "m1,m2,captain,hulk", []string{"m1", "m2", "captain", "hulk"}, answers)
		o := ta.a.outside
		o.isSelf = func(_ context.Context, name string) bool { return name == "m1" }
		ta.a.outside = o
		code, out, _ := ta.do("seat check")
		assert.Equal(t, 0, code, out)
		assert.Contains(t, out, "offline=captain,hulk")
		assert.Equal(t, []string{"captain", "hulk", "m2"}, *probed, "m2, not this machine m1, is the control")
	})
}

func TestSelfByAddressKnowsThisMachine(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	self := selfByAddress(func() string { return "studio" })
	assert.True(t, self(ctx, "Studio"), "its own hostname, any case")
	assert.True(t, self(ctx, "localhost"), "a name that resolves to loopback")
	assert.False(t, self(ctx, "no-such-member.invalid"), "a name that does not resolve is not this machine")
}

// The real check probes: removing the probe or the self test from realOutside fails here.
func TestRealOutsideProbesHostsAndKnowsItself(t *testing.T) {
	t.Parallel()
	o := newTestApp(t).a.realOutside()
	require.NotNil(t, o.hostProbe, "realOutside must probe hosts")
	assert.Equal(t, reflect.ValueOf(dialHost).Pointer(), reflect.ValueOf(o.hostProbe).Pointer(), "realOutside's probe is dialHost")
	require.NotNil(t, o.isSelf, "realOutside must know this machine, so it is never the control")
}

func TestClassifyDialCountsOnlyAClearNoAnswerOffline(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		err  error
		want hostAnswer
	}{
		{"refused: the host answered", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, hostAnswer{Answered: true}},
		{"timeout", context.DeadlineExceeded, hostAnswer{Offline: true}},
		{"no route to host", &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, hostAnswer{Offline: true}},
		{"network unreachable", &net.OpError{Op: "dial", Err: syscall.ENETUNREACH}, hostAnswer{Offline: true}},
	} {
		assert.Equal(t, c.want, classifyDial(c.err), c.name)
	}
	dns := classifyDial(&net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "captain"}})
	assert.False(t, dns.Offline, "a name that does not resolve is no evidence of offline")
	assert.False(t, dns.Answered)
	assert.Contains(t, dns.Err, "does not resolve")
	other := classifyDial(errors.New("permission denied"))
	require.False(t, other.Offline)
	assert.Equal(t, "permission denied", other.Err)
}

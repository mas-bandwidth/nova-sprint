package reserve

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pipeConn struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func newPipePair() (client *pipeConn, server *pipeConn) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	return &pipeConn{r: s2cR, w: c2sW}, &pipeConn{r: c2sR, w: s2cW}
}

func (p *pipeConn) Read(b []byte) (int, error)      { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error)     { return p.w.Write(b) }
func (p *pipeConn) SetReadDeadline(time.Time) error { return nil }
func (p *pipeConn) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []chan time.Time
	onAfter chan struct{}
}

func newFakeClock(t0 time.Time) *fakeClock {
	return &fakeClock{now: t0, onAfter: make(chan struct{}, 16)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	ws := c.waiters
	c.waiters = nil
	c.mu.Unlock()
	for _, ch := range ws {
		select {
		case ch <- now:
		default:
		}
	}
}

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, ch)
	c.mu.Unlock()
	select {
	case c.onAfter <- struct{}{}:
	default:
	}
	return ch
}

func TestUnitSubscriberAuthSnapshotPingReconnectAndDealerProjection(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 16, 22, 0, 0, 0, time.UTC)
	clk := newFakeClock(t0)

	c1, s1 := newPipePair()
	c2, s2 := newPipePair()
	var dialMu sync.Mutex
	dials := 0

	updates := make(chan FleetSnapshot, 8)
	sub := NewSubscriber(Config{
		Client:   "sprint@studio",
		Password: "secret-pw",
		Clock:    clk,
		Dial: func(context.Context) (Conn, bool, error) {
			dialMu.Lock()
			defer dialMu.Unlock()
			dials++
			if dials == 1 {
				return c1, true, nil
			}
			return c2, true, nil
		},
		OnUpdate: func(s FleetSnapshot) {
			updates <- s
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- sub.Run(ctx) }()

	// Session 1: verify auth + subscribe, push initial snapshot with CI + paused sprint lease + soft claim.
	rd1 := bufio.NewReader(s1)
	line1, err := rd1.ReadString('\n')
	require.NoError(t, err)
	var auth AuthMsg
	require.NoError(t, json.Unmarshal([]byte(line1), &auth))
	assert.Equal(t, AuthMsg{Op: "auth", Version: 3, Password: "secret-pw", Client: "sprint@studio"}, auth)

	line2, err := rd1.ReadString('\n')
	require.NoError(t, err)
	var subMsg SubscribeMsg
	require.NoError(t, json.Unmarshal([]byte(line2), &subMsg))
	assert.Equal(t, SubscribeMsg{Op: "subscribe", Host: ""}, subMsg)

	_, err = s1.Write([]byte(`{"op":"snapshot","machines":[{"host":"b1","online":true,"tags":["linux","amd64"],"usable_cores":16,"resources":{"cpu":16,"memory":64,"ps5":1},"free":{"cpu":0,"memory":16,"ps5":0},"leases":[{"lease_id":"L-1","host":"b1","needs":{"cpu":8,"memory":16},"state":"active","holder":"ci","source":"gha","label":"ci","priority":"blocker"},{"lease_id":"L-2","host":"b1","needs":{"cpu":4,"memory":16,"ps5":1},"state":"paused","holder":"bench","source":"b1","consumer":"p1.w1@1","label":"work","priority":"normal"},{"lease_id":"L-sub","host":"b1","needs":{"cpu":2,"memory":4},"sublease":true,"parent_lease":"L-1","state":"active","holder":"ci","source":"gha","label":"ci"}],"claims":[{"lease_id":"C-1","host":"b1","needs":"cpu 8, memory 16","source":"glenn","why":"profiling","expires_at":"2026-11-17T02:00:00Z"}],"queue":[{"req_id":"q1","host":"b1","needs":"cpu 2, memory 4","holder":"bench","consumer":"p2.w1@1"}]}]}` + "\n"))
	require.NoError(t, err)

	snap1 := <-updates
	require.True(t, snap1.Connected)
	require.Len(t, snap1.Machines, 1)

	// Wait until ping goroutine registers clk.After(PingInterval), then advance 5s.
	<-clk.onAfter
	clk.Advance(5 * time.Second)
	pingLine, err := rd1.ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, pingLine, `"op":"ping"`)
	_, err = s1.Write([]byte(`{"op":"pong","ts":1}` + "\n"))
	require.NoError(t, err)

	brMap := ToBenchReserve(sub.Snapshot(), clk.Now())
	br := brMap["b1"]
	assert.True(t, br.AnyPaused)
	assert.False(t, br.Offline)
	assert.Equal(t, map[string]int{"cpu": 8, "memory": 16}, br.ExternalHeld, "sublease must add 0 host debit")
	assert.Equal(t, map[string]int{"cpu": 8, "memory": 16}, br.SoftClaimed)
	assert.Equal(t, map[string]int{"cpu": 16, "memory": 32}, br.NonSprintUsed)
	assert.Equal(t, 16, br.ActiveClaimedCPU)
	assert.Equal(t, int64(5000), br.PausedMillis["p1.w1@1"])
	assert.Equal(t, int64(5000), br.PausedMillis["p2.w1@1"])

	// Drop connection s1 -> subscriber enters backoff After(); wake it deterministically.
	_ = s1.Close()
	rd2 := bufio.NewReader(s2)
	stopWake := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopWake:
				return
			case <-clk.onAfter:
				clk.Advance(250 * time.Millisecond)
			}
		}
	}()
	_, err = rd2.ReadString('\n') // auth
	require.NoError(t, err)
	_, err = rd2.ReadString('\n') // subscribe
	require.NoError(t, err)
	close(stopWake)

	_, err = s2.Write([]byte(`{"op":"snapshot","machines":[{"host":"b1","online":true,"tags":["linux","amd64"],"resources":{"cpu":16,"memory":64,"ps5":1},"free":{"cpu":12,"memory":48,"ps5":0},"leases":[{"lease_id":"L-2","host":"b1","needs":{"cpu":4,"memory":16,"ps5":1},"state":"active","holder":"bench","source":"b1","consumer":"p1.w1@1","label":"work","priority":"normal"}],"claims":[],"queue":[]}]}` + "\n"))
	require.NoError(t, err)

	snap2 := <-updates
	assert.Equal(t, "b1", snap2.ByHost()["b1"].Host)
	br2 := ToBenchReserve(snap2, clk.Now())["b1"]
	assert.False(t, br2.AnyPaused)
	assert.Equal(t, map[string]int{"cpu": 4, "memory": 16, "ps5": 1}, br2.SprintHolding["p1.w1@1"])
	assert.GreaterOrEqual(t, br2.PausedMillis["p1.w1@1"], int64(5000), "cumulative PausedMillis preserved after unpause")

	cancel()
	_ = s2.Close()
	require.NoError(t, <-done)
}

func TestUnitDisplayLabelConfigAndExpiredClaim(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ci", DisplayLabel("ci", "work", "normal"))
	assert.Equal(t, "claim", DisplayLabel("person", "", "normal"))
	assert.Equal(t, "blocker", DisplayLabel("bench", "blocker", "blocker"))
	assert.Equal(t, "critical", DisplayLabel("friend", "", "critical"))
	assert.Equal(t, "read", DisplayLabel("bench", "read", "read"))
	assert.Equal(t, "fix", DisplayLabel("bench", "fix", "normal"))
	assert.Equal(t, "work", DisplayLabel("friend", "custom", "normal"))

	cfg := ConfigFromLookup(func(k string) string {
		if k == AddrEnv {
			return "studio:7744"
		}
		if k == PasswordEnv {
			return "pw"
		}
		return ""
	}, "sprint")
	assert.True(t, cfg.Configured())
	assert.Equal(t, "studio:7744", cfg.Addr)

	now := time.Date(2026, 11, 16, 23, 0, 0, 0, time.UTC)
	snap := FleetSnapshot{
		Connected: true,
		Machines: []MachineSnapshot{{
			Host:      "space",
			Online:    true,
			Resources: map[string]int{"cpu": 24, "memory": 54},
			Claims: []Claim{
				{LeaseID: "C-exp", Exclusive: true, ExpiresAt: "2026-11-16T22:59:59Z"},
				{LeaseID: "C-live", Needs: NeedsMap{"cpu": 8, "memory": 16}, ExpiresAt: "2026-11-17T01:00:00Z"},
			},
		}},
	}
	br := ToBenchReserve(snap, now)["space"]
	assert.Equal(t, map[string]int{"cpu": 8, "memory": 16}, br.NonSprintUsed, "expired claim ignored; active claim kept")
}

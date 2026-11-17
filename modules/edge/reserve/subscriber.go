package reserve

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Conn is the stream transport used by Subscriber (TCP, Unix socket, or
// hermetic in-memory pipe in unit tests).
type Conn interface {
	io.ReadWriteCloser
	SetReadDeadline(t time.Time) error
}

// Clock abstracts time so unit tests never touch real time or sleep.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the production wall clock.
type RealClock struct{}

// Now returns time.Now().
func (RealClock) Now() time.Time { return time.Now() }

// After returns time.After(d).
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config configures a live nova-reserve NDJSON subscriber/client (§8).
type Config struct {
	Addr     string // TCP host:port (NOVA_RESERVE_ADDR)
	Password string // TCP password (NOVA_RESERVE_PASSWORD)
	Socket   string // Unix socket path (NOVA_RESERVE_SOCKET or DefaultSocket)
	Client   string // client identifier in auth frame (e.g. "sprint@studio")
	Host     string // optional host filter for subscribe ("" = whole fleet)

	// Dial overrides network dialing for hermetic unit tests. It returns the
	// connection and whether TCP auth must be sent first.
	Dial func(ctx context.Context) (Conn, bool, error)
	// Clock overrides RealClock in unit tests.
	Clock Clock
	// OnUpdate is invoked whenever a fresh FleetSnapshot is committed (used by
	// nova-sprint run to push sub-second updates to the dashboard cache).
	OnUpdate func(FleetSnapshot)
	// Log receives distinct connection/protocol warnings when non-nil.
	Log func(string)
}

// ConfigFromEnv builds Config from the process environment. Configured()
// reports whether a TCP address or existing Unix socket is available.
func ConfigFromEnv(client string) Config {
	return ConfigFromLookup(os.Getenv, client)
}

// ConfigFromLookup builds Config from lookup without touching globals.
func ConfigFromLookup(lookup func(string) string, client string) Config {
	addr := strings.TrimSpace(lookup(AddrEnv))
	sock := strings.TrimSpace(lookup(SocketEnv))
	if addr == "" && sock == "" {
		if st, err := os.Stat(DefaultSocket); err == nil && !st.IsDir() {
			sock = DefaultSocket
		}
	}
	return Config{
		Addr:     addr,
		Password: lookup(PasswordEnv),
		Socket:   sock,
		Client:   cmp.Or(strings.TrimSpace(client), "nova-sprint"),
	}
}

// Configured reports whether Config names a dial target.
func (c Config) Configured() bool {
	return c.Dial != nil || strings.TrimSpace(c.Addr) != "" || strings.TrimSpace(c.Socket) != ""
}

type pauseSpan struct {
	accumMs int64
	since   time.Time
	active  bool
	seenAt  time.Time
}

// Subscriber maintains a live FleetSnapshot from nova-reserve serve via the §8
// subscribe stream, sending 5s ping keepalives and reconnecting automatically.
type Subscriber struct {
	cfg   Config
	clock Clock

	mu       sync.RWMutex
	snap     FleetSnapshot
	pauses   map[string]map[string]*pauseSpan // host -> consumer -> pause/queue span
	lastWarn string
}

// NewSubscriber constructs a Subscriber for cfg.
func NewSubscriber(cfg Config) *Subscriber {
	clk := cfg.Clock
	if clk == nil {
		clk = RealClock{}
	}
	return &Subscriber{
		cfg:    cfg,
		clock:  clk,
		pauses: map[string]map[string]*pauseSpan{},
	}
}

// Snapshot returns a deep copy of the latest FleetSnapshot, advancing any
// currently paused or queued consumer spans to now.
func (s *Subscriber) Snapshot() FleetSnapshot {
	now := s.clock.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.snap.Clone()
	if !out.Connected {
		return out
	}
	for host, byCons := range s.pauses {
		for cons, sp := range byCons {
			ms := sp.accumMs
			if sp.active && now.After(sp.since) {
				ms += now.Sub(sp.since).Milliseconds()
			}
			if ms > 0 {
				if out.PausedMillis == nil {
					out.PausedMillis = map[string]map[string]int64{}
				}
				if out.PausedMillis[host] == nil {
					out.PausedMillis[host] = map[string]int64{}
				}
				out.PausedMillis[host][cons] = max(out.PausedMillis[host][cons], ms)
			}
		}
	}
	return out
}

// FetchOnce dials nova-reserve once, performs auth + subscribe, reads the
// initial snapshot frame, and closes the connection (used by `nova-sprint check`
// and incremental `event` fallback refreshes).
func FetchOnce(ctx context.Context, cfg Config) (FleetSnapshot, error) {
	sub := NewSubscriber(cfg)
	conn, needAuth, err := sub.dial(ctx)
	if err != nil {
		return FleetSnapshot{}, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(sub.clock.Now().Add(ReadTimeout)); err != nil {
		return FleetSnapshot{}, fmt.Errorf("set read deadline: %w", err)
	}
	if err := sub.handshake(conn, needAuth); err != nil {
		return FleetSnapshot{}, err
	}
	rd := bufio.NewReaderSize(conn, 64*1024)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return FleetSnapshot{}, fmt.Errorf("nova-reserve subscribe read: %w", err)
		}
		var env wireFrame
		if err := json.Unmarshal(line, &env); err != nil {
			return FleetSnapshot{}, fmt.Errorf("nova-reserve invalid JSON frame: %w", err)
		}
		switch env.Op {
		case "refused":
			return FleetSnapshot{}, fmt.Errorf("nova-reserve refused (%s): %s", env.Code, env.Message)
		case "snapshot":
			sub.applySnapshot(env.ReconnectUntil, env.Machines)
			return sub.Snapshot(), nil
		}
	}
}

// Watch runs the subscriber loop until ctx is canceled, forwarding any final
// error to cfg.Log when present.
func (s *Subscriber) Watch(ctx context.Context) {
	if err := s.Run(ctx); err != nil && s.cfg.Log != nil {
		s.cfg.Log(err.Error())
	}
}

// Run maintains the subscription until ctx is canceled. When cfg is not
// configured, Run waits for ctx.Done() and returns nil.
func (s *Subscriber) Run(ctx context.Context) error {
	if !s.cfg.Configured() {
		<-ctx.Done()
		return nil
	}
	backoff := 200 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return nil
		}
		connectedAt := s.clock.Now()
		err := s.runSession(ctx)
		s.markDisconnected()
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			s.warn("nova-reserve subscribe: " + err.Error())
		}
		if s.clock.Now().Sub(connectedAt) >= PingInterval {
			backoff = 200 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.clock.After(backoff):
		}
		backoff = min(backoff*2, 2*time.Second)
	}
}

type wireFrame struct {
	Op             string            `json:"op"`
	Code           string            `json:"code,omitempty"`
	Message        string            `json:"message,omitempty"`
	ReconnectUntil string            `json:"reconnect_until,omitempty"`
	Machines       []MachineSnapshot `json:"machines,omitempty"`
	Type           string            `json:"type,omitempty"`
	LeaseID        string            `json:"lease_id,omitempty"`
	Host           string            `json:"host,omitempty"`
}

func (s *Subscriber) dial(ctx context.Context) (Conn, bool, error) {
	if s.cfg.Dial != nil {
		return s.cfg.Dial(ctx)
	}
	d := &net.Dialer{Timeout: 3 * time.Second}
	if addr := strings.TrimSpace(s.cfg.Addr); addr != "" {
		c, err := d.DialContext(ctx, "tcp", addr)
		return c, true, err
	}
	sock := strings.TrimSpace(s.cfg.Socket)
	if sock == "" {
		return nil, false, errors.New("neither NOVA_RESERVE_ADDR nor NOVA_RESERVE_SOCKET is configured")
	}
	c, err := d.DialContext(ctx, "unix", sock)
	return c, false, err
}

func (s *Subscriber) handshake(w io.Writer, needAuth bool) error {
	if needAuth {
		if err := writeJSONLine(w, AuthMsg{
			Op:       "auth",
			Version:  ProtocolVersion,
			Password: s.cfg.Password,
			Client:   cmp.Or(s.cfg.Client, "nova-sprint"),
		}); err != nil {
			return fmt.Errorf("send auth: %w", err)
		}
	}
	if err := writeJSONLine(w, SubscribeMsg{Op: "subscribe", Host: s.cfg.Host}); err != nil {
		return fmt.Errorf("send subscribe: %w", err)
	}
	return nil
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func (s *Subscriber) runSession(ctx context.Context) error {
	conn, needAuth, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var writeMu sync.Mutex
	sendLine := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return writeJSONLine(conn, v)
	}

	if err := conn.SetReadDeadline(s.clock.Now().Add(ReadTimeout)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}
	if needAuth {
		if err := sendLine(AuthMsg{
			Op:       "auth",
			Version:  ProtocolVersion,
			Password: s.cfg.Password,
			Client:   cmp.Or(s.cfg.Client, "nova-sprint"),
		}); err != nil {
			return fmt.Errorf("send auth: %w", err)
		}
	}
	if err := sendLine(SubscribeMsg{Op: "subscribe", Host: s.cfg.Host}); err != nil {
		return fmt.Errorf("send subscribe: %w", err)
	}

	done := make(chan struct{})
	defer close(done)

	go func() {
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			case <-done:
				return
			case <-s.clock.After(PingInterval):
				if err := sendLine(PingMsg{Op: "ping", TS: s.clock.Now().UnixMilli()}); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()

	rd := bufio.NewReaderSize(conn, 64*1024)
	refreshPending := false
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return err
		}
		if err := conn.SetReadDeadline(s.clock.Now().Add(ReadTimeout)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var frame wireFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("decode frame: %w", err)
		}
		switch frame.Op {
		case "refused":
			return fmt.Errorf("refused (%s): %s", frame.Code, frame.Message)
		case "snapshot":
			refreshPending = false
			s.clearWarn()
			snap := s.applySnapshot(frame.ReconnectUntil, frame.Machines)
			if s.cfg.OnUpdate != nil {
				s.cfg.OnUpdate(snap)
			}
		case "event":
			if frame.Machines != nil {
				snap := s.applySnapshot(frame.ReconnectUntil, frame.Machines)
				if s.cfg.OnUpdate != nil {
					s.cfg.OnUpdate(snap)
				}
			} else if !refreshPending {
				refreshPending = true
				if err := sendLine(SubscribeMsg{Op: "subscribe", Host: s.cfg.Host}); err != nil {
					return fmt.Errorf("refresh snapshot on event: %w", err)
				}
			}
		case "pong":
		}
	}
}

func (s *Subscriber) applySnapshot(reconnectUntil string, machines []MachineSnapshot) FleetSnapshot {
	now := s.clock.Now()
	s.mu.Lock()
	cloned := make([]MachineSnapshot, len(machines))
	for i, m := range machines {
		cloned[i] = m.Clone()
		s.updateHostPausesLocked(m, now)
	}
	pm := map[string]map[string]int64{}
	for host, byCons := range s.pauses {
		for cons, sp := range byCons {
			if !sp.active && now.Sub(sp.seenAt) > 10*time.Minute {
				delete(byCons, cons)
				continue
			}
			ms := sp.accumMs
			if sp.active && now.After(sp.since) {
				ms += now.Sub(sp.since).Milliseconds()
			}
			if ms > 0 {
				if pm[host] == nil {
					pm[host] = map[string]int64{}
				}
				pm[host][cons] = ms
			}
		}
	}
	s.snap = FleetSnapshot{
		Connected:      true,
		ReconnectUntil: reconnectUntil,
		Machines:       cloned,
		PausedMillis:   pm,
		UpdatedAt:      now,
	}
	out := s.snap.Clone()
	s.mu.Unlock()
	return out
}

func (s *Subscriber) updateHostPausesLocked(m MachineSnapshot, now time.Time) {
	if s.pauses[m.Host] == nil {
		s.pauses[m.Host] = map[string]*pauseSpan{}
	}
	byCons := s.pauses[m.Host]
	pausedNow := map[string]int64{}
	for _, l := range m.Leases {
		if l.Consumer == "" {
			continue
		}
		if l.IsPaused() {
			pausedNow[l.Consumer] = max(pausedNow[l.Consumer], l.PausedMs)
		} else if sp, ok := byCons[l.Consumer]; ok {
			sp.seenAt = now
			if l.PausedMs > sp.accumMs {
				sp.accumMs = l.PausedMs
			}
		}
	}
	for _, w := range m.Queue {
		if w.Consumer != "" {
			if _, ok := pausedNow[w.Consumer]; !ok {
				pausedNow[w.Consumer] = 0
			}
		}
	}
	for cons, serverMs := range pausedNow {
		sp := byCons[cons]
		if sp == nil {
			sp = &pauseSpan{accumMs: serverMs, since: now, active: true, seenAt: now}
			byCons[cons] = sp
			continue
		}
		if sp.active && now.After(sp.since) {
			sp.accumMs += now.Sub(sp.since).Milliseconds()
		}
		sp.accumMs = max(sp.accumMs, serverMs)
		sp.since = now
		sp.active = true
		sp.seenAt = now
	}
	for cons, sp := range byCons {
		if _, still := pausedNow[cons]; !still && sp.active {
			if now.After(sp.since) {
				sp.accumMs += now.Sub(sp.since).Milliseconds()
			}
			sp.active = false
			sp.seenAt = now
		}
	}
}

func (s *Subscriber) markDisconnected() {
	s.mu.Lock()
	if s.snap.Connected {
		s.snap.Connected = false
	}
	s.mu.Unlock()
}

func (s *Subscriber) warn(msg string) {
	s.mu.Lock()
	same := s.lastWarn == msg
	s.lastWarn = msg
	s.mu.Unlock()
	if !same && s.cfg.Log != nil {
		s.cfg.Log(msg)
	}
}

func (s *Subscriber) clearWarn() {
	s.mu.Lock()
	s.lastWarn = ""
	s.mu.Unlock()
}

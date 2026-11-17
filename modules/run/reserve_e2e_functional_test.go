package run

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/dash"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/reserve"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

type arbHostState struct {
	host      string
	online    bool
	tags      []string
	resources map[string]int
	leases    []*arbLeaseEntry
	claims    []*arbClaimEntry
}

type arbLeaseEntry struct {
	lease     reserve.Lease
	pausedAt  time.Time
	ownerConn net.Conn
}

type arbClaimEntry struct {
	claim reserve.Claim
}

// mockReserveArbitrator speaks the RESOURCES.md §8 NDJSON wire protocol over TCP
// for end-to-end functional verification of nova-sprint's subscriber, dealer,
// soft-claim blocker, CI pause/unpause, sub-lease/deadlock, and dashboard views.
type mockReserveArbitrator struct {
	password string
	ln       net.Listener
	addr     string

	mu          sync.Mutex
	seq         int
	hosts       map[string]*arbHostState
	hostOrder   []string
	subscribers map[net.Conn]struct{}
	conns       map[net.Conn]struct{}
}

func startMockArbitrator(t *testing.T, password string, machines []reserve.MachineSnapshot) *mockReserveArbitrator {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := &mockReserveArbitrator{
		password:    password,
		ln:          ln,
		addr:        ln.Addr().String(),
		hosts:       make(map[string]*arbHostState, len(machines)),
		subscribers: make(map[net.Conn]struct{}),
		conns:       make(map[net.Conn]struct{}),
	}
	for _, m := range machines {
		a.hosts[m.Host] = &arbHostState{
			host:      m.Host,
			online:    m.Online,
			tags:      slices.Clone(m.Tags),
			resources: maps.Clone(m.Resources),
		}
		a.hostOrder = append(a.hostOrder, m.Host)
	}
	go a.acceptLoop()
	t.Cleanup(a.shutdownArbitrator)
	return a
}

func (a *mockReserveArbitrator) shutdownArbitrator() {
	_ = a.ln.Close()
	a.mu.Lock()
	conns := make([]net.Conn, 0, len(a.conns))
	for c := range a.conns {
		conns = append(conns, c)
	}
	a.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (a *mockReserveArbitrator) acceptLoop() {
	for {
		conn, err := a.ln.Accept()
		if err != nil {
			return
		}
		a.mu.Lock()
		a.conns[conn] = struct{}{}
		a.mu.Unlock()
		go a.serveClientConn(conn)
	}
}

func writeNDJSONLine(conn net.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = conn.Write(b)
	return err
}

func (a *mockReserveArbitrator) serveClientConn(conn net.Conn) {
	defer func() {
		a.cleanupConn(conn)
		_ = conn.Close()
	}()
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)
	if !sc.Scan() {
		return
	}
	var auth struct {
		Op       string `json:"op"`
		Version  int    `json:"version"`
		Password string `json:"password"`
		Client   string `json:"client"`
	}
	if err := json.Unmarshal(sc.Bytes(), &auth); err != nil || auth.Op != "auth" || auth.Version != reserve.ProtocolVersion || auth.Password != a.password {
		if err := writeNDJSONLine(conn, map[string]any{
			"op":      "refused",
			"code":    "unauthorized",
			"message": "invalid password or protocol version",
		}); err != nil {
			return
		}
		return
	}

	for sc.Scan() {
		var raw map[string]any
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			return
		}
		op, _ := raw["op"].(string)
		switch op {
		case "ping":
			ts, _ := raw["ts"].(float64)
			if err := writeNDJSONLine(conn, map[string]any{"op": "pong", "ts": int64(ts)}); err != nil {
				return
			}
		case "subscribe":
			a.mu.Lock()
			a.subscribers[conn] = struct{}{}
			snap := a.snapshotLocked()
			a.mu.Unlock()
			if err := writeNDJSONLine(conn, snap); err != nil {
				return
			}
		case "request":
			var req struct {
				ReqID       string `json:"req_id"`
				Mode        string `json:"mode"`
				Host        string `json:"host"`
				Needs       string `json:"needs"`
				Exclusive   bool   `json:"exclusive"`
				NoWait      bool   `json:"nowait"`
				ParentLease string `json:"parent_lease"`
				Holder      string `json:"holder"`
				Source      string `json:"source"`
				Consumer    string `json:"consumer"`
				Label       string `json:"label"`
				Priority    string `json:"priority"`
				Why         string `json:"why"`
			}
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				return
			}
			resp, stateNotices, snap, ev := a.handleRequest(conn, req.ReqID, req.Host, req.Needs, req.Exclusive, req.ParentLease, req.Holder, req.Source, req.Consumer, req.Label, req.Priority, req.Why)
			if err := writeNDJSONLine(conn, resp); err != nil {
				return
			}
			a.dispatchUpdates(stateNotices, ev, snap)
		case "claim":
			var clReq struct {
				ReqID     string `json:"req_id"`
				Host      string `json:"host"`
				Needs     string `json:"needs"`
				Exclusive bool   `json:"exclusive"`
				TTLMs     int64  `json:"ttl_ms"`
				Source    string `json:"source"`
				Why       string `json:"why"`
			}
			if err := json.Unmarshal(sc.Bytes(), &clReq); err != nil {
				return
			}
			resp, snap, ev := a.handleClaim(clReq.ReqID, clReq.Host, clReq.Needs, clReq.Exclusive, clReq.TTLMs, clReq.Source, clReq.Why)
			if err := writeNDJSONLine(conn, resp); err != nil {
				return
			}
			a.dispatchUpdates(nil, ev, snap)
		case "release":
			leaseID, _ := raw["lease_id"].(string)
			stateNotices, snap, ev := a.handleRelease(leaseID)
			a.dispatchUpdates(stateNotices, ev, snap)
		}
	}
}

type connFrame struct {
	conn  net.Conn
	frame map[string]any
}

func (a *mockReserveArbitrator) cleanupConn(conn net.Conn) {
	a.mu.Lock()
	delete(a.conns, conn)
	delete(a.subscribers, conn)
	var ids []string
	for _, h := range a.hosts {
		for _, le := range h.leases {
			if le.ownerConn == conn {
				ids = append(ids, le.lease.LeaseID)
			}
		}
	}
	a.mu.Unlock()
	for _, id := range ids {
		notices, snap, ev := a.handleRelease(id)
		a.dispatchUpdates(notices, ev, snap)
	}
}

func (a *mockReserveArbitrator) dispatchUpdates(notices []connFrame, ev, snap map[string]any) {
	for _, n := range notices {
		if n.conn != nil {
			if err := writeNDJSONLine(n.conn, n.frame); err != nil {
				_ = n.conn.Close()
			}
		}
	}
	if snap == nil {
		return
	}
	a.mu.Lock()
	subs := make([]net.Conn, 0, len(a.subscribers))
	for c := range a.subscribers {
		subs = append(subs, c)
	}
	a.mu.Unlock()
	for _, s := range subs {
		if ev != nil {
			if err := writeNDJSONLine(s, ev); err != nil {
				_ = s.Close()
				continue
			}
		}
		if err := writeNDJSONLine(s, snap); err != nil {
			_ = s.Close()
		}
	}
}

func (a *mockReserveArbitrator) handleRequest(conn net.Conn, reqID, host, needsSpec string, exclusive bool, parentID, holder, source, consumer, label, priority, why string) (map[string]any, []connFrame, map[string]any, map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()

	h, ok := a.hosts[host]
	if !ok || !h.online {
		return map[string]any{"op": "refused", "req_id": reqID, "code": "impossible", "message": "unknown or offline host"}, nil, nil, nil
	}
	parsed, err := card.ParseNeeds(needsSpec, nil, nil)
	if err != nil && !exclusive {
		return map[string]any{"op": "refused", "req_id": reqID, "code": "bad_request", "message": err.Error()}, nil, nil, nil
	}
	needs := maps.Clone(parsed.Amounts)
	if exclusive && len(needs) == 0 {
		needs = maps.Clone(h.resources)
	}
	for _, tag := range parsed.Tags {
		if !slices.Contains(h.tags, tag) {
			return map[string]any{"op": "refused", "req_id": reqID, "code": "impossible", "message": "missing tag " + tag}, nil, nil, nil
		}
	}

	// §6.2 / §6.3: Nested sub-lease check and self-deadlock cycle detection.
	if parentID != "" {
		for _, le := range h.leases {
			if le.lease.LeaseID == parentID {
				ancFree := maps.Clone(map[string]int(le.lease.Needs))
				for _, sub := range h.leases {
					if sub.lease.Sublease && sub.lease.ParentLease == parentID {
						for k, v := range sub.lease.Needs {
							ancFree[k] -= v
						}
					}
				}
				fitsAncestor := true
				for k, v := range needs {
					if v > ancFree[k] {
						fitsAncestor = false
						break
					}
				}
				if fitsAncestor {
					a.seq++
					id := fmt.Sprintf("L-%d", a.seq)
					entry := &arbLeaseEntry{
						ownerConn: conn,
						lease: reserve.Lease{
							LeaseID:     id,
							Host:        host,
							Needs:       reserve.NeedsMap(needs),
							Sublease:    true,
							ParentLease: parentID,
							State:       "active",
							Holder:      holder,
							Source:      source,
							Consumer:    consumer,
							Label:       label,
							Priority:    priority,
							Why:         why,
							Cores:       needs["cpu"],
						},
					}
					h.leases = append(h.leases, entry)
					snap := a.snapshotLocked()
					ev := map[string]any{"op": "event", "type": "granted", "lease_id": id, "host": host}
					return grantedFrame(reqID, entry.lease), nil, snap, ev
				}
				if !a.fitsHostLocked(h, needs, false) {
					return map[string]any{
						"op":      "refused",
						"req_id":  reqID,
						"code":    "deadlock",
						"message": fmt.Sprintf("nova-reserve: RESERVE DEADLOCK: request by %s (%s) waits on lease %s", source, needsSpec, parentID),
						"cycle":   []string{parentID, reqID},
					}, nil, nil, nil
				}
			}
		}
	}

	var notices []connFrame
	if holder == "ci" {
		// §7.1: CI wins immediately and transitions overlapping active non-CI leases to paused.
		for !a.fitsCILocked(h, needs) {
			var victim *arbLeaseEntry
			for i := len(h.leases) - 1; i >= 0; i-- {
				cand := h.leases[i]
				if !cand.lease.Sublease && cand.lease.Holder != "ci" && cand.lease.State == "active" {
					victim = cand
					break
				}
			}
			if victim == nil {
				break
			}
			victim.lease.State = "paused"
			victim.pausedAt = time.Now()
			notices = append(notices, connFrame{
				conn: victim.ownerConn,
				frame: map[string]any{
					"op":       "state",
					"lease_id": victim.lease.LeaseID,
					"state":    "paused",
					"reason":   "displaced by ci on " + host,
				},
			})
		}
	} else if !a.fitsHostLocked(h, needs, true) {
		return map[string]any{"op": "refused", "req_id": reqID, "code": "no_room", "message": "nova-reserve: NO ROOM"}, nil, nil, nil
	}

	a.seq++
	id := fmt.Sprintf("L-%d", a.seq)
	if label == "" {
		if holder == "ci" {
			label = "ci"
		} else {
			label = "work"
		}
	}
	entry := &arbLeaseEntry{
		ownerConn: conn,
		lease: reserve.Lease{
			LeaseID:   id,
			Host:      host,
			Needs:     reserve.NeedsMap(needs),
			Exclusive: exclusive,
			State:     "active",
			Holder:    holder,
			Source:    source,
			Consumer:  consumer,
			Label:     label,
			Priority:  priority,
			Why:       why,
			Cores:     needs["cpu"],
		},
	}
	h.leases = append(h.leases, entry)
	snap := a.snapshotLocked()
	ev := map[string]any{"op": "event", "type": "granted", "lease_id": id, "host": host}
	return grantedFrame(reqID, entry.lease), notices, snap, ev
}

func (a *mockReserveArbitrator) handleClaim(reqID, host, needsSpec string, exclusive bool, ttlMs int64, source, why string) (map[string]any, map[string]any, map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.hosts[host]
	if !ok {
		return map[string]any{"op": "refused", "req_id": reqID, "code": "impossible", "message": "unknown host"}, nil, nil
	}
	parsed, err := card.ParseNeeds(needsSpec, nil, nil)
	if err != nil && !exclusive {
		return map[string]any{"op": "refused", "req_id": reqID, "code": "bad_request", "message": err.Error()}, nil, nil
	}
	if ttlMs <= 0 {
		ttlMs = int64((4 * time.Hour) / time.Millisecond)
	}
	a.seq++
	id := fmt.Sprintf("C-%d", a.seq)
	exp := time.Now().UTC().Add(time.Duration(ttlMs) * time.Millisecond).Format(time.RFC3339Nano)
	cl := reserve.Claim{
		LeaseID:   id,
		Host:      host,
		Needs:     reserve.NeedsMap(parsed.Amounts),
		Exclusive: exclusive,
		Source:    source,
		Label:     "claim",
		Why:       why,
		ExpiresAt: exp,
	}
	h.claims = append(h.claims, &arbClaimEntry{claim: cl})
	snap := a.snapshotLocked()
	ev := map[string]any{"op": "event", "type": "claimed", "lease_id": id, "host": host}
	return map[string]any{
		"op":         "granted",
		"req_id":     reqID,
		"lease_id":   id,
		"host":       host,
		"needs":      parsed.Amounts,
		"expires_at": exp,
	}, snap, ev
}

func (a *mockReserveArbitrator) handleRelease(leaseID string) ([]connFrame, map[string]any, map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var notices []connFrame
	var changedHost *arbHostState
	for _, h := range a.hosts {
		for i, le := range h.leases {
			if le.lease.LeaseID == leaseID {
				h.leases = slices.Delete(h.leases, i, i+1)
				changedHost = h
				break
			}
		}
		for i, ce := range h.claims {
			if ce.claim.LeaseID == leaseID {
				h.claims = slices.Delete(h.claims, i, i+1)
				changedHost = h
				break
			}
		}
	}
	if changedHost == nil {
		return nil, nil, nil
	}
	// §7.1: Paused leases resume ahead of any new work as soon as capacity fits.
	for _, le := range changedHost.leases {
		if le.lease.State == "paused" && a.fitsCILocked(changedHost, le.lease.ResolvedNeeds(changedHost.resources)) {
			durMs := max(int64(1), time.Since(le.pausedAt).Milliseconds())
			le.lease.PausedMs += durMs
			le.lease.State = "active"
			le.pausedAt = time.Time{}
			notices = append(notices, connFrame{
				conn: le.ownerConn,
				frame: map[string]any{
					"op":       "state",
					"lease_id": le.lease.LeaseID,
					"state":    "active",
					"reason":   "ci completed on " + changedHost.host,
				},
			})
		}
	}
	snap := a.snapshotLocked()
	ev := map[string]any{"op": "event", "type": "released", "lease_id": leaseID, "host": changedHost.host}
	return notices, snap, ev
}

func (a *mockReserveArbitrator) fitsCILocked(h *arbHostState, needs map[string]int) bool {
	used := map[string]int{}
	for _, le := range h.leases {
		if le.lease.Sublease || le.lease.State == "paused" {
			continue
		}
		for k, v := range le.lease.ResolvedNeeds(h.resources) {
			used[k] += v
		}
	}
	for k, v := range needs {
		if used[k]+v > h.resources[k] {
			return false
		}
	}
	return true
}

func (a *mockReserveArbitrator) fitsHostLocked(h *arbHostState, needs map[string]int, includeClaims bool) bool {
	if len(a.pausedOnLocked(h)) > 0 {
		return false
	}
	used := map[string]int{}
	for _, le := range h.leases {
		if le.lease.Sublease || le.lease.State == "paused" {
			continue
		}
		for k, v := range le.lease.ResolvedNeeds(h.resources) {
			used[k] += v
		}
	}
	if includeClaims {
		for _, ce := range h.claims {
			for k, v := range ce.claim.ResolvedNeeds(h.resources) {
				used[k] += v
			}
		}
	}
	for k, v := range needs {
		if used[k]+v > h.resources[k] {
			return false
		}
	}
	return true
}

func (a *mockReserveArbitrator) pausedOnLocked(h *arbHostState) []*arbLeaseEntry {
	var out []*arbLeaseEntry
	for _, le := range h.leases {
		if le.lease.State == "paused" {
			out = append(out, le)
		}
	}
	return out
}

func (a *mockReserveArbitrator) snapshotLocked() map[string]any {
	machines := make([]reserve.MachineSnapshot, 0, len(a.hostOrder))
	for _, name := range a.hostOrder {
		h := a.hosts[name]
		used := map[string]int{}
		leases := make([]reserve.Lease, 0, len(h.leases))
		for _, le := range h.leases {
			cp := le.lease.Clone()
			if cp.State == "paused" && !le.pausedAt.IsZero() {
				cp.PausedMs += max(int64(1), time.Since(le.pausedAt).Milliseconds())
			}
			leases = append(leases, cp)
			if !cp.Sublease && cp.State != "paused" {
				for k, v := range cp.ResolvedNeeds(h.resources) {
					used[k] += v
				}
			}
		}
		claims := make([]reserve.Claim, 0, len(h.claims))
		for _, ce := range h.claims {
			claims = append(claims, ce.claim.Clone())
			for k, v := range ce.claim.ResolvedNeeds(h.resources) {
				used[k] += v
			}
		}
		free := make(map[string]int, len(h.resources))
		for k, capAmt := range h.resources {
			free[k] = max(0, capAmt-used[k])
		}
		machines = append(machines, reserve.MachineSnapshot{
			Host:        h.host,
			Online:      h.online,
			Tags:        slices.Clone(h.tags),
			UsableCores: h.resources["cpu"],
			Resources:   maps.Clone(h.resources),
			Free:        free,
			Leases:      leases,
			Claims:      claims,
		})
	}
	return map[string]any{
		"op":              "snapshot",
		"reconnect_until": "",
		"machines":        machines,
	}
}

func grantedFrame(reqID string, l reserve.Lease) map[string]any {
	cpus := fmt.Sprintf("%d", max(1, l.Cores))
	return map[string]any{
		"op":       "granted",
		"req_id":   reqID,
		"lease_id": l.LeaseID,
		"sublease": l.Sublease,
		"host":     l.Host,
		"needs":    l.Needs,
		"cores":    l.Cores,
		"env": map[string]string{
			"NOVA_RESERVE_LEASE":         l.LeaseID,
			"NOVA_RESERVE_HOST":          l.Host,
			"NOVA_RESERVE_CPUS":          cpus,
			"GOMAXPROCS":                 cpus,
			"CARGO_BUILD_JOBS":           cpus,
			"RAYON_NUM_THREADS":          cpus,
			"CMAKE_BUILD_PARALLEL_LEVEL": cpus,
			"MAKEFLAGS":                  "-j" + cpus,
		},
	}
}

type peerReserveConn struct {
	conn net.Conn
	sc   *bufio.Scanner
}

func dialPeerReserve(t *testing.T, addr, password, client string) *peerReserveConn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := writeNDJSONLine(conn, map[string]any{
		"op":       "auth",
		"version":  reserve.ProtocolVersion,
		"password": password,
		"client":   client,
	}); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	return &peerReserveConn{conn: conn, sc: sc}
}

func (p *peerReserveConn) sendAndRead(t *testing.T, msg map[string]any) map[string]any {
	t.Helper()
	if err := writeNDJSONLine(p.conn, msg); err != nil {
		t.Fatal(err)
	}
	return p.readNext(t)
}

func (p *peerReserveConn) readNext(t *testing.T) map[string]any {
	t.Helper()
	if err := p.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if !p.sc.Scan() {
		t.Fatalf("peer connection closed unexpectedly: %v", p.sc.Err())
	}
	var out map[string]any
	if err := json.Unmarshal(p.sc.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

type e2eConfigReader struct {
	snap config.Snapshot
}

func (r *e2eConfigReader) Snapshot(context.Context) (config.Snapshot, error) {
	return r.snap, nil
}

func (r *e2eConfigReader) Close() {}

type e2eStepClock struct {
	mu     sync.Mutex
	now    time.Time
	tick   chan time.Time
	parked chan struct{}
}

type e2eTickBeat struct {
	tick   chan time.Time
	parked chan struct{}
}

func (b e2eTickBeat) C() <-chan time.Time {
	select {
	case b.parked <- struct{}{}:
	default:
	}
	return b.tick
}

func (e2eTickBeat) Stop() {}

func (c *e2eStepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *e2eStepClock) Every(d time.Duration) Beat {
	if d == TickEvery {
		return e2eTickBeat{tick: c.tick, parked: c.parked}
	}
	return beat{make(chan time.Time)}
}

func (c *e2eStepClock) advanceOneTick(t *testing.T, dt time.Duration) {
	t.Helper()
	c.mu.Lock()
	c.now = c.now.Add(dt)
	now := c.now
	c.mu.Unlock()
	c.tick <- now
	select {
	case <-c.parked:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for serve tick to complete")
	}
}

type boardResourceEntry struct {
	Status string               `json:"status"`
	Tags   []string             `json:"tags"`
	CPU    *dash.ResourceMetric `json:"cpu"`
	Memory *dash.ResourceMetric `json:"memory"`
}

type boardSprintPayload struct {
	Data struct {
		Tables struct {
			Resources map[string]boardResourceEntry `json:"resources"`
			Friends   json.RawMessage               `json:"friends"`
		} `json:"tables"`
	} `json:"data"`
}

func fetchBoardPayload(t *testing.T, boardAddr string) boardSprintPayload {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + boardAddr + "/api/sprint")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/sprint status=%d body=%s", resp.StatusCode, string(body))
	}
	var out boardSprintPayload
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode /api/sprint: %v\nbody=%s", err, string(body))
	}
	return out
}

func waitForBoardPayload(t *testing.T, boardAddr string, pred func(boardSprintPayload) bool) boardSprintPayload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last boardSprintPayload
	for time.Now().Before(deadline) {
		last = fetchBoardPayload(t, boardAddr)
		if pred(last) {
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	b, err := json.Marshal(last.Data.Tables)
	if err != nil {
		t.Fatalf("timed out waiting for /api/sprint condition (marshal err: %v)", err)
	}
	t.Fatalf("timed out waiting for /api/sprint condition; last tables=%s", string(b))
	return last
}

// TestFunctionalReserveSubscribeDealClaimAndCIPauseE2E exercises the complete
// v3 client/server resource architecture over real TCP NDJSON sockets:
//  1. TCP auth rejection on bad password + live subscribe snapshot ingestion.
//  2. Hardware-tagged dealing (p1 needing ps5 -> b1, never b2 or alice) and
//     friend dealing (pf -> alice), plus nested sub-lease at 0 extra host debit
//     and RESERVE DEADLOCK cycle refusal.
//  3. Timed soft person claim (nova-reserve claim) updating /api/sprint in
//     sub-second time without beat writes, leaving p1.w1 running while blocking p2.
//  4. Immediate --holder ci grant pausing p1.w1 behind CI on Resources while
//     keeping Friends 100% unchanged (R64) and blocking deals/preemption (R52, R53).
//  5. Releasing CI unpausing p1.w1 with PausedMillis > 0 (R39b) and allowing
//     blocker work to deal onto remaining bench room.
func TestFunctionalReserveSubscribeDealClaimAndCIPauseE2E(t *testing.T) {
	t.Parallel()

	arb := startMockArbitrator(t, "secret-v3", []reserve.MachineSnapshot{
		{
			Host:      "b1",
			Online:    true,
			Tags:      []string{"linux", "amd64"},
			Resources: map[string]int{"cpu": 8, "memory": 16, "ps5": 1},
		},
		{
			Host:      "b0",
			Online:    true,
			Tags:      []string{"linux", "amd64"},
			Resources: map[string]int{"cpu": 8, "memory": 16},
		},
	})

	// 1. Verify TCP auth rejects wrong password with "unauthorized".
	bad := dialPeerReserve(t, arb.addr, "wrong-pw", "intruder@host")
	refused := bad.readNext(t)
	if refused["op"] != "refused" || refused["code"] != "unauthorized" {
		t.Fatalf("bad password response = %+v, want unauthorized refusal", refused)
	}

	// Seed sprint store with friend alice, benches b1 & b2, friend card pf,
	// and hardware-tagged card p1 (ps5 1).
	st := started(t)
	must(t, st, "seed-rows",
		store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github", Pos: 1},
		store.PutRow{Name: "emma", Session: "sess-emma", Role: "worker"},
		store.SetRowState{Row: "emma", State: "up"},
		store.PutRow{Name: "b1", Session: "sess-b1", Role: "worker"},
		store.SetRowState{Row: "b1", State: "up"},
		store.SetRowLoad{Row: "b1", CPUPeakDeci: 120, Tiers: "pro", Version: "v3.0.0"},
		store.PutRow{Name: "b0", Session: "sess-b0", Role: "worker"},
		store.SetRowState{Row: "b0", State: "up"},
		store.SetRowLoad{Row: "b0", CPUPeakDeci: 80, Tiers: "pro", Version: "v3.0.0"},
		store.PutCard{
			ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Priority: "normal",
			Brief: "Task: PS5 suite\nRESOURCES: cpu 4, memory 8, ps5 1\n",
			Paths: []string{"ps5"},
		},
		store.PutCard{
			ID: "pf", Kind: "producer", Stream: "s", Tier: "frontier", Priority: "normal",
			Brief: "Task: Friend review\n",
			Paths: []string{"doc"},
		},
	)
	must(t, st, "ready-initial",
		store.SetCardState{Card: "p1", State: "ready"},
		store.SetCardState{Card: "pf", State: "ready"},
	)

	eight := 8
	cfgReader := &e2eConfigReader{
		snap: config.Snapshot{
			Friends: []config.Friend{
				{Name: "emma", Harness: "codex", Model: "carbon-4", Width: 1, Tiers: "frontier", Enabled: true},
			},
			Machines: []config.Machine{
				{Name: "b1", Roles: "bench", OS: "linux", Arch: "amd64", Cores: &eight},
				{Name: "b0", Roles: "bench", OS: "linux", Arch: "amd64", Cores: &eight},
			},
		},
	}
	ro, err := NewConfigRoster(cfgReader, 1)
	if err != nil {
		t.Fatal(err)
	}
	fakeBus := &fakeBus{who: map[string]bus.Presence{
		"emma": {Name: "emma"},
		"b1":   {Name: "b1"},
		"b0":   {Name: "b0"},
	}}
	clock := &e2eStepClock{
		now:    time.Now().UTC().Truncate(time.Second),
		tick:   make(chan time.Time),
		parked: make(chan struct{}, 1),
	}
	boardReady := make(chan string, 1)
	var runLog strings.Builder

	runCtx, cancelRun := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() {
		runDone <- serve(runCtx, Config{
			Store:    st,
			Roster:   ro,
			Bus:      fakeBus,
			Policy:   &answering{},
			Git:      stillGit{},
			ReposDir: t.TempDir(),
			Listen:   "127.0.0.1:0",
			Ready:    boardReady,
			Clock:    clock,
			Log:      &runLog,
			Version:  "v3.0.0",
			Reserve: reserve.Config{
				Addr:     arb.addr,
				Password: "secret-v3",
				Client:   "sprint@studio",
			},
		}, &healthy{})
	}()
	defer func() {
		cancelRun()
		if err := <-runDone; err != nil {
			t.Errorf("serve returned error: %v", err)
		}
	}()

	boardAddr := <-boardReady
	select {
	case <-clock.parked:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for serve initial heads pass")
	}

	// Wait until live subscribe snapshot (memory=16 on b1) is committed before tick 1.
	waitForBoardPayload(t, boardAddr, func(p boardSprintPayload) bool {
		b1, ok := p.Data.Tables.Resources["b1"]
		return ok && b1.Memory != nil && b1.Memory.Total == 16
	})

	// 2. Tick 1: p1 (needing ps5 1) deals to b1 (never alice or b2), and pf deals to alice.
	clock.advanceOneTick(t, time.Second)
	ctx := context.Background()
	snap1, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap1.Cards["p1.w1"].Holder; got != "b1" {
		t.Fatalf("p1.w1 holder = %q, want b1 (only bench with ps5); cards=%+v\nlog=%s", got, snap1.Cards, runLog.String())
	}
	if got := snap1.Cards["pf.w1"].Holder; got != "emma" {
		t.Fatalf("pf.w1 holder = %q, want alice; cards=%+v", got, snap1.Cards)
	}

	// Transition p1.w1 and pf.w1 to working, and hold b2 so remaining admission tests isolate b1.
	must(t, st, "take-initial",
		store.SetRowHeld{Row: "b0", Held: true, Reason: "isolate b1 admission"},
	)
	clock.advanceOneTick(t, time.Second)

	// Bench b1 worker acquires its step lease as a peer client over TCP.
	b1Worker := dialPeerReserve(t, arb.addr, "secret-v3", "worker@b1")
	grant1 := b1Worker.sendAndRead(t, map[string]any{
		"op":       "request",
		"req_id":   "r-p1",
		"mode":     "pinned",
		"host":     "b1",
		"needs":    "cpu 4, memory 8, ps5 1",
		"holder":   "bench",
		"source":   "b1",
		"consumer": "p1.w1@1",
		"label":    "work",
		"priority": "normal",
	})
	if grant1["op"] != "granted" {
		t.Fatalf("b1 worker lease grant = %+v, want granted", grant1)
	}
	parentLeaseID, _ := grant1["lease_id"].(string)

	// Nested sub-command inside NOVA_RESERVE_LEASE sub-leases cpu 2, memory 4 at 0 extra host debit (§6.2).
	subConn := dialPeerReserve(t, arb.addr, "secret-v3", "gate@b1")
	subGrant := subConn.sendAndRead(t, map[string]any{
		"op":           "request",
		"req_id":       "r-sub-ok",
		"mode":         "pinned",
		"host":         "b1",
		"needs":        "cpu 2, memory 4",
		"parent_lease": parentLeaseID,
		"holder":       "bench",
		"source":       "b1",
		"consumer":     "p1.w1@1",
		"label":        "work",
	})
	if subGrant["op"] != "granted" || subGrant["sublease"] != true {
		t.Fatalf("nested sublease = %+v, want granted with sublease=true", subGrant)
	}

	// Nested child asking for another ps5 1 (already sub-consumed if we subleased ps5, or ask for ps5 2 / cpu 6 > ancestor_free)
	// First sub-lease ps5 1 inside parent, then a sibling asking for ps5 1 exceeds ancestor_free and hits RESERVE DEADLOCK (§6.3)!
	subPS5 := subConn.sendAndRead(t, map[string]any{
		"op":           "request",
		"req_id":       "r-sub-ps5",
		"mode":         "pinned",
		"host":         "b1",
		"needs":        "ps5 1",
		"parent_lease": parentLeaseID,
		"holder":       "bench",
		"source":       "b1",
	})
	if subPS5["op"] != "granted" || subPS5["sublease"] != true {
		t.Fatalf("ps5 sublease = %+v, want granted sublease=true", subPS5)
	}
	deadConn := dialPeerReserve(t, arb.addr, "secret-v3", "deadlock-child@b1")
	deadResp := deadConn.sendAndRead(t, map[string]any{
		"op":           "request",
		"req_id":       "r-sub-dead",
		"mode":         "pinned",
		"host":         "b1",
		"needs":        "ps5 1",
		"parent_lease": parentLeaseID,
		"holder":       "bench",
		"source":       "b1",
	})
	if deadResp["op"] != "refused" || deadResp["code"] != "deadlock" {
		t.Fatalf("nested cycle response = %+v, want refused code=deadlock", deadResp)
	}

	// Verify /api/sprint shows b1 cpu.held == 4 (subleases added 0 extra host debit!).
	waitForBoardPayload(t, boardAddr, func(p boardSprintPayload) bool {
		b1 := p.Data.Tables.Resources["b1"]
		return b1.CPU != nil && b1.CPU.Held == 4 && b1.CPU.ByLabel["work"] == 4
	})

	// 3. Timed soft user claim (nova-reserve claim) reserves remaining cpu 4 on b1
	// immediately on /api/sprint without disturbing p1.w1, and blocks ready p2!
	glennPeer := dialPeerReserve(t, arb.addr, "secret-v3", "glenn@studio")
	claimResp := glennPeer.sendAndRead(t, map[string]any{
		"op":     "claim",
		"req_id": "c-glenn",
		"host":   "b1",
		"needs":  "cpu 4, memory 8",
		"ttl_ms": 3600000,
		"source": "glenn",
		"why":    "kernel profiling",
	})
	if claimResp["op"] != "granted" {
		t.Fatalf("soft claim response = %+v, want granted", claimResp)
	}
	claimID, _ := claimResp["lease_id"].(string)

	waitForBoardPayload(t, boardAddr, func(p boardSprintPayload) bool {
		b1 := p.Data.Tables.Resources["b1"]
		return b1.CPU != nil && b1.CPU.Held == 8 && b1.CPU.ByLabel["work"] == 4 && b1.CPU.ByLabel["claim"] == 4
	})

	must(t, st, "add-p2",
		store.PutCard{
			ID: "p2", Kind: "producer", Stream: "s", Tier: "pro", Priority: "normal",
			Brief: "Task: Second job\nRESOURCES: cpu 4, memory 8\n",
			Paths: []string{"job2"},
		},
		store.SetCardState{Card: "p2", State: "ready"},
	)
	clock.advanceOneTick(t, time.Second)
	snap2, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, dealt := snap2.Cards["p2.w1"]; dealt {
		t.Fatalf("p2 dealt onto soft-claimed b1: %+v", snap2.Cards["p2.w1"])
	}
	if snap2.Cards["p1.w1"].State != "working" {
		t.Fatalf("soft claim disturbed running p1.w1: %+v", snap2.Cards["p1.w1"])
	}

	// Capture Friends table before CI pause so we can verify R64 (Friends 100% unchanged).
	beforeCI := fetchBoardPayload(t, boardAddr)

	// 4. Release soft claim and acquire --holder ci lease (cpu 6, memory 12) on b1:
	// CI wins immediately, displaces p1.w1 to state="paused", updates Resources
	// (by_label["ci"]=6, paused_by_label["work"]=4), leaves Friends unchanged,
	// and blocks both p2 and blocker pblock from dealing/preempting onto paused b1!
	if err := writeNDJSONLine(glennPeer.conn, map[string]any{"op": "release", "lease_id": claimID}); err != nil {
		t.Fatal(err)
	}
	ciPeer := dialPeerReserve(t, arb.addr, "secret-v3", "github-runner@b1")
	ciGrant := ciPeer.sendAndRead(t, map[string]any{
		"op":     "request",
		"req_id": "r-ci",
		"mode":   "pinned",
		"host":   "b1",
		"needs":  "cpu 6, memory 12",
		"holder": "ci",
		"source": "github-runner-b1",
		"label":  "ci",
	})
	if ciGrant["op"] != "granted" {
		t.Fatalf("ci lease grant = %+v, want immediate granted", ciGrant)
	}
	ciLeaseID, _ := ciGrant["lease_id"].(string)

	statePaused := b1Worker.readNext(t)
	if statePaused["op"] != "state" || statePaused["state"] != "paused" {
		t.Fatalf("b1Worker state notice = %+v, want state=paused", statePaused)
	}

	duringCI := waitForBoardPayload(t, boardAddr, func(p boardSprintPayload) bool {
		b1 := p.Data.Tables.Resources["b1"]
		return b1.CPU != nil && b1.CPU.ByLabel["ci"] == 6 && b1.CPU.PausedByLabel["work"] == 4 && b1.CPU.ByLabel["claim"] == 0
	})
	if string(duringCI.Data.Tables.Friends) != string(beforeCI.Data.Tables.Friends) {
		t.Fatalf("R64 violated: Friends table changed during bench CI pause:\nbefore=%s\nduring=%s", beforeCI.Data.Tables.Friends, duringCI.Data.Tables.Friends)
	}

	// Add a blocker card pblock (cpu 2, memory 4); AnyPaused on b1 forbids new deals and preemption (R52, R53)!
	must(t, st, "add-pblock",
		store.PutCard{
			ID: "pblock", Kind: "producer", Stream: "s", Tier: "pro", Priority: "blocker",
			Brief: "Task: Urgent hotfix\nRESOURCES: cpu 2, memory 4\n",
			Paths: []string{"hotfix"},
		},
		store.SetCardState{Card: "pblock", State: "ready"},
	)
	clock.advanceOneTick(t, time.Second)
	snap3, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, dealt := snap3.Cards["pblock.w1"]; dealt {
		t.Fatalf("pblock dealt onto CI-paused bench b1: %+v", snap3.Cards["pblock.w1"])
	}
	if snap3.Cards["p1.w1"].State != "working" {
		t.Fatalf("p1.w1 was killed or preempted during CI pause: %+v", snap3.Cards["p1.w1"])
	}

	// 5. Release CI lease -> p1.w1 immediately resumes (state="active", PausedMs > 0),
	// board clears CI/paused overlays, and next tick deals pblock onto b1's remaining room!
	if err := writeNDJSONLine(ciPeer.conn, map[string]any{"op": "release", "lease_id": ciLeaseID}); err != nil {
		t.Fatal(err)
	}
	stateResumed := b1Worker.readNext(t)
	if stateResumed["op"] != "state" || stateResumed["state"] != "active" {
		t.Fatalf("b1Worker resume notice = %+v, want state=active", stateResumed)
	}

	waitForBoardPayload(t, boardAddr, func(p boardSprintPayload) bool {
		b1 := p.Data.Tables.Resources["b1"]
		return b1.CPU != nil && b1.CPU.ByLabel["ci"] == 0 && b1.CPU.ByLabel["work"] == 4 && len(b1.CPU.PausedByLabel) == 0
	})

	clock.advanceOneTick(t, time.Second)
	snap4, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap4.Cards["pblock.w1"].Holder; got != "b1" {
		t.Fatalf("after CI released, pblock.w1 holder = %q, want b1; cards=%+v\nlog=%s", got, snap4.Cards, runLog.String())
	}
}

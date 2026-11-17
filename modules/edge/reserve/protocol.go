// Package reserve implements the client/server NDJSON subscriber and status
// client for nova-reserve v3 (RESOURCES.md §8, SPEC-V3.md R48-R54, R59-R62).
package reserve

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
)

const (
	// ProtocolVersion is the NDJSON wire protocol version of nova-reserve v3.
	ProtocolVersion = 3
	// DefaultSocket is the local Unix socket path when NOVA_RESERVE_ADDR is unset.
	DefaultSocket = "/var/tmp/nova-reserve/sock"
	// AddrEnv is the Tailscale/TCP address of central nova-reserve serve.
	AddrEnv = "NOVA_RESERVE_ADDR"
	// PasswordEnv is the TCP auth secret for NOVA_RESERVE_ADDR.
	PasswordEnv = "NOVA_RESERVE_PASSWORD"
	// SocketEnv overrides DefaultSocket when connecting over a Unix socket.
	SocketEnv = "NOVA_RESERVE_SOCKET"
	// PingInterval is the §8.1 keepalive cadence.
	PingInterval = 5 * time.Second
	// ReadTimeout bounds silence on a subscribed stream before reconnecting.
	ReadTimeout = 15 * time.Second
)

// NeedsMap decodes either a JSON object {"cpu":8,"memory":16} or a "--needs"
// resource specification string "cpu 8, memory 16".
type NeedsMap map[string]int

// UnmarshalJSON decodes either an object map or a "--needs" string.
func (m *NeedsMap) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*m = nil
		return nil
	}
	if trimmed[0] == '"' {
		var spec string
		if err := json.Unmarshal(trimmed, &spec); err != nil {
			return err
		}
		parsed, err := card.ParseNeeds(spec, nil, nil)
		if err != nil {
			*m = NeedsMap{}
			return nil
		}
		*m = NeedsMap(parsed.Amounts)
		return nil
	}
	var obj map[string]int
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	*m = NeedsMap(obj)
	return nil
}

// AuthMsg is the first frame sent on TCP connections (§8.1).
type AuthMsg struct {
	Op       string `json:"op"`
	Version  int    `json:"version"`
	Password string `json:"password"`
	Client   string `json:"client"`
}

// SubscribeMsg requests an initial snapshot followed by live transitions (§8.1).
type SubscribeMsg struct {
	Op   string `json:"op"`
	Host string `json:"host"`
}

// PingMsg is the 5s client keepalive (§8.1).
type PingMsg struct {
	Op string `json:"op"`
	TS int64  `json:"ts"`
}

// Lease is one active or paused resource lease on a machine (§8.2).
type Lease struct {
	LeaseID     string   `json:"lease_id"`
	Host        string   `json:"host,omitempty"`
	Needs       NeedsMap `json:"needs,omitempty"`
	Exclusive   bool     `json:"exclusive,omitempty"`
	Sublease    bool     `json:"sublease,omitempty"`
	ParentLease string   `json:"parent_lease,omitempty"`
	Paused      bool     `json:"paused,omitempty"` // true when displaced by CI in snapshot LeaseView
	State       string   `json:"state,omitempty"`  // "active" or "paused" on state frames
	Holder      string   `json:"holder,omitempty"`
	Source      string   `json:"source,omitempty"`
	Consumer    string   `json:"consumer,omitempty"`
	Label       string   `json:"label,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Why         string   `json:"why,omitempty"`
	Cores       int      `json:"cores,omitempty"`
	CPUs        []int    `json:"cpus,omitempty"`
	PID         int      `json:"pid,omitempty"`
	Since       string   `json:"since,omitempty"`
	StartedAt   string   `json:"started_at,omitempty"`
	PausedMs    int64    `json:"paused_ms,omitempty"`
}

// IsPaused reports whether l is currently paused behind CI.
func (l Lease) IsPaused() bool {
	return l.Paused || strings.EqualFold(l.State, "paused")
}

// Clone returns a deep copy of l.
func (l Lease) Clone() Lease {
	l.Needs = NeedsMap(maps.Clone(map[string]int(l.Needs)))
	l.CPUs = slices.Clone(l.CPUs)
	return l
}

// ResolvedNeeds returns l's concrete resource vector on a machine with total
// capacity, expanding Exclusive holds to total when Needs is empty.
func (l Lease) ResolvedNeeds(total map[string]int) map[string]int {
	if l.Exclusive && len(l.Needs) == 0 {
		return maps.Clone(total)
	}
	return map[string]int(l.Needs)
}

// Claim is one timed soft operator claim on a machine (§7.3, §8.2).
type Claim struct {
	LeaseID   string   `json:"lease_id"`
	Host      string   `json:"host,omitempty"`
	Needs     NeedsMap `json:"needs,omitempty"`
	Exclusive bool     `json:"exclusive,omitempty"`
	Source    string   `json:"source,omitempty"`
	Label     string   `json:"label,omitempty"`
	Why       string   `json:"why,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

// Clone returns a deep copy of c.
func (c Claim) Clone() Claim {
	c.Needs = NeedsMap(maps.Clone(map[string]int(c.Needs)))
	return c
}

// Expired reports whether c's TTL has elapsed at now.
func (c Claim) Expired(now time.Time) bool {
	if strings.TrimSpace(c.ExpiresAt) == "" || now.IsZero() {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, c.ExpiresAt)
	if err != nil {
		return false
	}
	return !now.Before(t)
}

// ResolvedNeeds returns c's concrete resource vector on a machine with total
// capacity, expanding Exclusive claims to total when Needs is empty.
func (c Claim) ResolvedNeeds(total map[string]int) map[string]int {
	if c.Exclusive && len(c.Needs) == 0 {
		return maps.Clone(total)
	}
	return map[string]int(c.Needs)
}

// Waiter is one queued request waiting for capacity (§8.2).
type Waiter struct {
	ReqID     string   `json:"req_id"`
	Host      string   `json:"host,omitempty"`
	Needs     NeedsMap `json:"needs,omitempty"`
	Exclusive bool     `json:"exclusive,omitempty"`
	Holder    string   `json:"holder,omitempty"`
	Source    string   `json:"source,omitempty"`
	Consumer  string   `json:"consumer,omitempty"`
	Label     string   `json:"label,omitempty"`
	Priority  string   `json:"priority,omitempty"`
	Why       string   `json:"why,omitempty"`
}

// Clone returns a deep copy of w.
func (w Waiter) Clone() Waiter {
	w.Needs = NeedsMap(maps.Clone(map[string]int(w.Needs)))
	return w
}

// MachineSnapshot is one fleet machine's live state inside a snapshot frame (§8.2).
type MachineSnapshot struct {
	Host        string         `json:"host"`
	Online      bool           `json:"online"`
	Tags        []string       `json:"tags,omitempty"`
	UsableCores int            `json:"usable_cores,omitempty"`
	Resources   map[string]int `json:"resources,omitempty"`
	Free        map[string]int `json:"free,omitempty"`
	Leases      []Lease        `json:"leases,omitempty"`
	Claims      []Claim        `json:"claims,omitempty"`
	Queue       []Waiter       `json:"queue,omitempty"`
}

// Clone returns a deep copy of m.
func (m MachineSnapshot) Clone() MachineSnapshot {
	out := m
	out.Tags = slices.Clone(m.Tags)
	out.Resources = maps.Clone(m.Resources)
	out.Free = maps.Clone(m.Free)
	if m.Leases != nil {
		out.Leases = make([]Lease, len(m.Leases))
		for i, l := range m.Leases {
			out.Leases[i] = l.Clone()
		}
	}
	if m.Claims != nil {
		out.Claims = make([]Claim, len(m.Claims))
		for i, c := range m.Claims {
			out.Claims[i] = c.Clone()
		}
	}
	if m.Queue != nil {
		out.Queue = make([]Waiter, len(m.Queue))
		for i, q := range m.Queue {
			out.Queue[i] = q.Clone()
		}
	}
	return out
}

// FleetSnapshot is the live fleet resource view maintained from subscribe (§8.2).
type FleetSnapshot struct {
	Connected      bool                        `json:"connected,omitempty"`
	ReconnectUntil string                      `json:"reconnect_until,omitempty"`
	Machines       []MachineSnapshot           `json:"machines,omitempty"`
	PausedMillis   map[string]map[string]int64 `json:"paused_millis,omitempty"` // host -> consumer -> cumulative paused/queued ms
	UpdatedAt      time.Time                   `json:"-"`
}

// Clone returns a deep copy of s.
func (s FleetSnapshot) Clone() FleetSnapshot {
	out := s
	if s.Machines != nil {
		out.Machines = make([]MachineSnapshot, len(s.Machines))
		for i, m := range s.Machines {
			out.Machines[i] = m.Clone()
		}
	}
	if s.PausedMillis != nil {
		out.PausedMillis = make(map[string]map[string]int64, len(s.PausedMillis))
		for h, pm := range s.PausedMillis {
			out.PausedMillis[h] = maps.Clone(pm)
		}
	}
	return out
}

// ByHost indexes s.Machines by host name.
func (s FleetSnapshot) ByHost() map[string]MachineSnapshot {
	if len(s.Machines) == 0 {
		return nil
	}
	out := make(map[string]MachineSnapshot, len(s.Machines))
	for _, m := range s.Machines {
		out[m.Host] = m
	}
	return out
}

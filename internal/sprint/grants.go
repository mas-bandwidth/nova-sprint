package sprint

import (
	"fmt"
	"time"
)

// Grant is one server-owned bench lease and its first expiry obligation.
// Late edits retain Owed and Delivery (tla/BenchGrant.tla Extend and End).
type Grant struct {
	ID       string               `json:"id"`
	Bench    string               `json:"bench"`
	Grantee  string               `json:"grantee"`
	By       string               `json:"by"`
	Granted  time.Time            `json:"granted"`
	Deadline time.Time            `json:"deadline"`
	Ended    time.Time            `json:"ended"`
	Owed     time.Time            `json:"owed"`
	Delivery *CoordinatorDelivery `json:"delivery"`
	Judged   bool                 `json:"judged"`
	Changed  time.Time            `json:"changed_at"`
}

// Grants keeps its allocator across clear and pruning, so a token is never reused.
type Grants struct {
	Next  uint64  `json:"next"`
	Items []Grant `json:"items"`
}

type GrantRequest struct {
	Op, ID, Bench, Grantee string
	For                    time.Duration
}

// Validate holds the coordinator's operands before any write.
func (r GrantRequest) Validate(member bool) error {
	switch r.Op {
	case "add":
		if !ValidID(r.Bench) {
			return fmt.Errorf("bench %q is not a valid name", r.Bench)
		}
		name := r.Grantee
		if friend, ok := FriendOfRow(name); ok {
			name = friend
		}
		if !ValidID(name) {
			return fmt.Errorf("grantee %q is not a valid name", r.Grantee)
		}
		if !member {
			return fmt.Errorf("grantee %s is not a fleet member or friend row", r.Grantee)
		}
	case "extend", "end":
		if !ValidID(r.ID) {
			return fmt.Errorf("grant %q is not a valid id", r.ID)
		}
	default:
		return fmt.Errorf("unknown grant operation %q", r.Op)
	}
	if r.Op != "end" && (r.For < time.Minute || r.For > 30*24*time.Hour) {
		return fmt.Errorf("grant duration must be at least one minute and at most thirty days")
	}
	return nil
}

// Due is a wall-clock obligation, independent of RUNNING/STOPPED.
func (g Grant) Due(now time.Time) bool {
	return !g.Owed.IsZero() || (g.Ended.IsZero() && !now.Before(g.Deadline))
}

// Expiry is immutable once owed, even if the coordinator extends the grant late.
func (g Grant) Expiry() time.Time {
	if !g.Owed.IsZero() {
		return g.Owed
	}
	return g.Deadline
}

func (g *Grant) KeepOwed(now time.Time) {
	if g.Owed.IsZero() && g.Due(now) {
		g.Owed = g.Deadline
	}
}

// Prunable never drops an unresolved obligation after an offline interval.
func (g Grant) Prunable(now time.Time, keep time.Duration) bool {
	terminal := g.Judged || (g.Delivery != nil && g.Delivery.Sent) ||
		(!g.Ended.IsZero() && g.Owed.IsZero() && g.Delivery == nil)
	return terminal && !now.Before(g.Changed.Add(keep))
}

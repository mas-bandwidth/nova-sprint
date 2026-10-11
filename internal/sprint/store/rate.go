package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/ratebudget"
)

// The global rate limiter's store side (sprint/rate_budget.go, pkg/ratebudget): the
// budgets the coordinator set are the fleet table's properties at the sprint's epoch, and
// the windows and leases live beside the tables in the same Redis, outside every table, so
// a take writes no table and needs no line on the server (servelanes.go, the beat lane).

// memBudgets is the in-process budget backend of each in-memory store, so every view of
// one Mem (its pinned copies) shares one window.
var memBudgets sync.Map // *memState -> *ratebudget.Mem

// Budget is the store's budget backend: the Redis the tables are in, or the in-memory
// store's own.
func (st *Store) Budget() ratebudget.Backend {
	switch b := st.B.(type) {
	case *Redis:
		return ratebudget.Redis{C: b.C}
	case *Mem:
		v, _ := memBudgets.LoadOrStore(b.memState, &ratebudget.Mem{})
		return v.(*ratebudget.Mem)
	}
	return &ratebudget.Mem{}
}

// BudgetProps is the fleet table's properties at the sprint's epoch: the budgets set
// (PropModelRPM, PropProviderConcurrency). One read of the table's shape, never its cards.
func (st *Store) BudgetProps(ctx context.Context) (map[string]string, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return nil, err
	}
	shapes, err := pinned.B.Shapes(ctx, []string{st.Names.Table(sprint.Fleet)})
	if err != nil {
		return nil, err
	}
	if len(shapes) != 1 {
		return nil, fmt.Errorf("the fleet table's shape read %d tables", len(shapes))
	}
	return shapes[0].Props, nil
}

// BudgetTake is one take of the global budget for a model request (budget take): the
// model's requests-per-minute window, then its provider's concurrency lease for holder.
// Answers: OK, or the wait before asking again; RPM and Concurrent the budgets read (0
// unlimited). An error is a budget not read: the caller proceeds (ratebudget.Waiter).
type BudgetAnswer struct {
	Grant           ratebudget.Grant
	RPM, Concurrent int
}

// BudgetTake reads the budgets and takes: an rpm grant first (a request over the window
// waits without holding a slot), then the provider's slot. With neither set it grants at
// once and writes nothing.
func (st *Store) BudgetTake(ctx context.Context, model, holder string, now time.Time) (BudgetAnswer, error) {
	props, err := st.BudgetProps(ctx)
	if err != nil {
		return BudgetAnswer{}, err
	}
	provider, _, _ := strings.Cut(model, "/")
	a := BudgetAnswer{RPM: atoiOr0(props[sprint.PropModelRPM(model)]), Concurrent: atoiOr0(props[sprint.PropProviderConcurrency(provider)])}
	b := st.Budget()
	pre := st.Names.Prefix
	if a.Concurrent > 0 {
		// a slot first, renewed if held: a holder that already holds its slot renews it
		// before the window decides, so a long stall never loses a held slot
		g, err := b.Acquire(ctx, ratebudget.LeaseKey(pre, provider), holder, a.Concurrent, ratebudget.LeaseTTL, now)
		if err != nil || !g.OK {
			a.Grant = g
			return a, err
		}
	}
	if a.RPM > 0 {
		g, err := b.TakeRate(ctx, ratebudget.RateKey(pre, model), a.RPM, ratebudget.Window, now, ratebudget.ID(holder))
		if err != nil || !g.OK {
			a.Grant = g
			if a.Concurrent > 0 {
				// the window is spent: the slot is not held while the request waits for it
				// ignored: a release that fails leaves a lease that expires by itself (LeaseTTL)
				_ = b.Release(ctx, ratebudget.LeaseKey(pre, provider), holder)
			}
			return a, err
		}
	}
	a.Grant = ratebudget.Grant{OK: true}
	return a, nil
}

// BudgetRenew renews holder's concurrency slot of the model's provider while its request
// runs (budget renew; the lease's LeaseTTL from now): a live request never loses its slot,
// and a dead one's runs out. It takes no rpm grant. Not held and no room: Grant.OK false.
func (st *Store) BudgetRenew(ctx context.Context, model, holder string, now time.Time) (ratebudget.Grant, error) {
	props, err := st.BudgetProps(ctx)
	if err != nil {
		return ratebudget.Grant{}, err
	}
	provider, _, _ := strings.Cut(model, "/")
	n := atoiOr0(props[sprint.PropProviderConcurrency(provider)])
	if n <= 0 {
		return ratebudget.Grant{OK: true}, nil
	}
	return st.Budget().Acquire(ctx, ratebudget.LeaseKey(st.Names.Prefix, provider), holder, n, ratebudget.LeaseTTL, now)
}

// BudgetRelease frees holder's concurrency slot of the model's provider (budget release):
// a request ended. A slot not held is no error.
func (st *Store) BudgetRelease(ctx context.Context, model, holder string) error {
	provider, _, _ := strings.Cut(model, "/")
	return st.Budget().Release(ctx, ratebudget.LeaseKey(st.Names.Prefix, provider), holder)
}

// BudgetUse is the grants in the model's window now (the routes table's rpm use).
func (st *Store) BudgetUse(ctx context.Context, model string, now time.Time) (int, error) {
	return st.Budget().Use(ctx, ratebudget.RateKey(st.Names.Prefix, model), ratebudget.Window, now)
}

func atoiOr0(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

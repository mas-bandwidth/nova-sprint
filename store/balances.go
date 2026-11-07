package store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

// Balance is one read of a provider's balance (sprint.balances):
// when it was read, the store's seq at the read (the window's edge in the
// log), the balance in dollars as an exact decimal, and the provider's
// answer as read. Rows are only added: spend over a window is the fall
// between two reads.
type Balance struct {
	Provider string    `json:"provider"`
	At       time.Time `json:"at"`
	Seq      int64     `json:"seq"`
	USD      string    `json:"balance_usd"`
	Raw      string    `json:"raw"`
}

// Balances is the store's record of provider balances, kept beside the
// sprint's state (global: a clear keeps it). *PG and *Fake have it.
type Balances interface {
	AddBalance(ctx context.Context, b Balance) error
	// LastBalance is the provider's latest read, false when there is none.
	LastBalance(ctx context.Context, provider string) (Balance, bool, error)
}

// checkBalance refuses a balance the table would: no provider, a seq below
// 0, a balance that is not a decimal at or above 0, text that is not UTF-8.
func checkBalance(b Balance) error {
	r, ok := new(big.Rat).SetString(b.USD)
	switch {
	case b.Provider == "" || badText(b.Provider) || badText(b.Raw):
		return fmt.Errorf("%w: balance needs a provider and UTF-8 text", ErrInvalid)
	case b.Seq < 0:
		return fmt.Errorf("%w: balance seq %d is below 0", ErrInvalid, b.Seq)
	case !ok || r.Sign() < 0:
		return fmt.Errorf("%w: balance %q is not dollars at or above 0", ErrInvalid, b.USD)
	}
	return nil
}

// AddBalance records a read.
func (p *PG) AddBalance(ctx context.Context, b Balance) error {
	if err := checkBalance(b); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO sprint.balances (provider, at, seq, balance_usd, raw) VALUES ($1, $2, $3, $4::numeric, $5)`,
		b.Provider, b.At, b.Seq, b.USD, b.Raw)
	return err
}

// LastBalance is the provider's latest read.
func (p *PG) LastBalance(ctx context.Context, provider string) (Balance, bool, error) {
	b := Balance{Provider: provider}
	err := p.pool.QueryRow(ctx, `SELECT at, seq, balance_usd::text, raw FROM sprint.balances WHERE provider = $1 ORDER BY at DESC, id DESC LIMIT 1`,
		provider).Scan(&b.At, &b.Seq, &b.USD, &b.Raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Balance{}, false, nil
	}
	return b, err == nil, err
}

// AddBalance records a read.
func (f *Fake) AddBalance(_ context.Context, b Balance) error {
	if err := checkBalance(b); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.balances = append(f.balances, b)
	return nil
}

// LastBalance is the provider's latest read: the last added at the latest time.
func (f *Fake) LastBalance(_ context.Context, provider string) (Balance, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out Balance
	found := false
	for _, b := range f.balances {
		if b.Provider == provider && (!found || !b.At.Before(out.At)) {
			out, found = b, true
		}
	}
	return out, found, nil
}

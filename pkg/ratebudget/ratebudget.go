// Package ratebudget is the sprint's one global rate limiter: a requests-per-window budget
// per model and a concurrency budget per provider, each one shared bucket in the sprint's
// store that every caller on every machine takes from (the owner, 2026-10-09: "The
// per-model pacing budget should be global btw. across all fleet machines"; 2026-10-11:
// "the rate limited should just stall out delay, not fail"; "i don't want this rate
// limiter causing errors"). The model is tla/RateBudget.tla.
//
// THE RATE BUDGET IS A WINDOW, NOT A BUCKET. A token bucket of n refilled at n a minute
// grants up to 2n-1 in some sixty seconds (a full bucket spent, then a minute's refill).
// The provider's word is "under 10 requests per minute", so the store keeps the log of
// grants of the last Window (a sorted set, score the grant's time in milliseconds) and
// grants one only while fewer than n lie in it: at most n grants in ANY span of Window
// (tla/RateBudget.tla, NeverOverRate). A caller over it is told how long until the oldest
// grant leaves the window; it waits that long and asks again.
//
// THE CONCURRENCY BUDGET IS LEASES. A provider's in-flight slots are a sorted set of
// holders, score the lease's expiry: a slot is held until its holder releases it or its
// lease expires, so a holder that crashed frees its slot by the clock and never strands
// a lane (NoSlotLost). A holder renews by taking again.
//
// ATOMIC WITHOUT SCRIPTS. A worker's store user holds no scripting (pkg/redisacl), so a
// take is WATCH on its key, a read, then one MULTI/EXEC that writes only if the key did
// not move: two callers on two machines never both take the last grant (the broken
// variant, a read then an unguarded write, is MCRateBudgetBrokenRace and fails
// NeverOverRate). A take that loses its race reads again, up to Races times, then
// answers "wait a moment" (RaceWait): never an error.
//
// IT NEVER FAILS A CALLER. Wait is the caller's loop: it stalls while the budget is
// spent, bounded only by the caller's context (the card's deadline), and a store that
// errors or does not answer is a budget it cannot read: Wait logs one line and the
// caller proceeds as if unlimited (Open). The limiter is never a source of errors.
//
// THE CLOCK IS THE CALLER'S. A worker's store user may not run TIME, so every take
// carries its caller's now; the window is held over the times the callers wrote. Clock
// skew between machines of d can shift a grant's place in the window by d, so Window is
// a minute plus Skew: at most n grants in any 60 s of real time while skew stays under
// Skew.
package ratebudget

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Minute is the provider's window; Skew the clock skew between machines it allows
	// for; Window the span the store holds grants over.
	Minute = 60 * time.Second
	Skew   = 2 * time.Second
	Window = Minute + Skew
	// LeaseTTL is how long a concurrency slot is held without a renewal: a holder that
	// crashed frees its slot after it. A live holder renews every LeaseTTL/3.
	LeaseTTL = 2 * time.Minute
	// Races is how many times a take that lost its WATCH race reads again before it
	// answers RaceWait.
	Races    = 8
	RaceWait = 50 * time.Millisecond
	// MaxPoll is the longest a waiter sleeps before it asks again: a slot released early
	// is found within it.
	MaxPoll = 5 * time.Second
	// StoreTimeout bounds one take against the store: past it the budget is unread and
	// the caller proceeds (Open).
	StoreTimeout = 3 * time.Second
)

// Grant is the answer to one take: OK, or the time to wait before asking again.
type Grant struct {
	OK   bool
	Wait time.Duration
}

// Backend is a store the budgets live in. Every call is one atomic take or release.
type Backend interface {
	// TakeRate records one grant for key at now when fewer than n grants lie in
	// (now-window, now]; else it answers how long until the oldest leaves.
	TakeRate(ctx context.Context, key string, n int, window time.Duration, now time.Time, id string) (Grant, error)
	// Acquire holds (or renews) one of n slots of key for holder until now+ttl; with
	// every slot held by another holder whose lease is live, it answers a wait.
	Acquire(ctx context.Context, key, holder string, n int, ttl time.Duration, now time.Time) (Grant, error)
	// Release frees holder's slot of key; a slot not held is no error.
	Release(ctx context.Context, key, holder string) error
	// Use is the grants in key's window at now and the live slots of a lease key, for
	// the routes table and the dashboard.
	Use(ctx context.Context, key string, window time.Duration, now time.Time) (int, error)
}

// ID is a fresh grant id: the holder's words and 8 random bytes, so two grants in one
// millisecond are two members of the window.
func ID(holder string) string {
	var b [8]byte
	// ignored: crypto/rand.Read never returns an error on the platforms the sprint runs on
	_, _ = rand.Read(b[:])
	return holder + "#" + hex.EncodeToString(b[:])
}

// RateKey is the store key of a model's window under the sprint's prefix: keyed by the
// provider key's model, never by the sprint's epoch, because the window counts requests
// the provider has seen, which a clear does not unsee; it expires by itself two windows
// after its last grant.
func RateKey(prefix, model string) string { return prefix + "sprint:budget:rpm:" + model }

// LeaseKey is the store key of a provider's concurrency slots (see RateKey for why it
// carries no epoch); it expires by itself two lease times after its last take.
func LeaseKey(prefix, provider string) string {
	return prefix + "sprint:budget:concurrent:" + provider
}

func ms(t time.Time) float64 { return float64(t.UnixMilli()) }

// Redis is the Backend in the sprint's Redis store.
type Redis struct{ C redis.UniversalClient }

// TakeRate is WATCH key; ZCOUNT of the window; MULTI ZREMRANGEBYSCORE (the grants that
// left) ZADD PEXPIRE EXEC only while the count is under n.
func (r Redis) TakeRate(ctx context.Context, key string, n int, window time.Duration, now time.Time, id string) (Grant, error) {
	if n <= 0 {
		return Grant{OK: true}, nil
	}
	from := now.Add(-window)
	for range Races {
		var g Grant
		err := r.C.Watch(ctx, func(tx *redis.Tx) error {
			in, err := tx.ZCount(ctx, key, "("+ftoa(ms(from)), "+inf").Result()
			if err != nil {
				return err
			}
			if int(in) >= n {
				// the oldest grant still in the window leaves at its time + window
				old, err := tx.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{Min: "(" + ftoa(ms(from)), Max: "+inf", Count: 1}).Result()
				if err != nil {
					return err
				}
				g.Wait = RaceWait
				if len(old) == 1 {
					leaves := time.UnixMilli(int64(old[0].Score)).Add(window)
					if w := leaves.Sub(now) + time.Millisecond; w > 0 {
						g.Wait = w
					}
				}
				return nil
			}
			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.ZRemRangeByScore(ctx, key, "-inf", ftoa(ms(from)))
				p.ZAdd(ctx, key, redis.Z{Score: ms(now), Member: id})
				p.PExpire(ctx, key, 2*window)
				return nil
			})
			if err == nil {
				g.OK = true
			}
			return err
		}, key)
		if errors.Is(err, redis.TxFailedErr) {
			continue // another caller took between our read and our write: read again
		}
		return g, err
	}
	return Grant{Wait: RaceWait}, nil
}

// Acquire is WATCH key; the live slots; MULTI ZREMRANGEBYSCORE (the expired leases) ZADD
// PEXPIRE EXEC only while the holder holds one already or fewer than n are live.
func (r Redis) Acquire(ctx context.Context, key, holder string, n int, ttl time.Duration, now time.Time) (Grant, error) {
	if n <= 0 {
		return Grant{OK: true}, nil
	}
	for range Races {
		var g Grant
		err := r.C.Watch(ctx, func(tx *redis.Tx) error {
			live, err := tx.ZRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{Min: "(" + ftoa(ms(now)), Max: "+inf"}).Result()
			if err != nil {
				return err
			}
			held := false
			for _, z := range live {
				held = held || z.Member == holder
			}
			if !held && len(live) >= n {
				// the first lease to expire frees a slot by then at the latest; a release
				// frees one sooner, which the waiter's poll finds
				g.Wait = min(time.UnixMilli(int64(live[0].Score)).Sub(now)+time.Millisecond, MaxPoll)
				return nil
			}
			_, err = tx.TxPipelined(ctx, func(p redis.Pipeliner) error {
				p.ZRemRangeByScore(ctx, key, "-inf", ftoa(ms(now)))
				p.ZAdd(ctx, key, redis.Z{Score: ms(now.Add(ttl)), Member: holder})
				p.PExpire(ctx, key, 2*ttl)
				return nil
			})
			if err == nil {
				g.OK = true
			}
			return err
		}, key)
		if errors.Is(err, redis.TxFailedErr) {
			continue
		}
		return g, err
	}
	return Grant{Wait: RaceWait}, nil
}

// Release is ZREM key holder.
func (r Redis) Release(ctx context.Context, key, holder string) error {
	return r.C.ZRem(ctx, key, holder).Err()
}

// Use is ZCOUNT of the window (a rate key) or of the live leases (window 0, a lease key).
func (r Redis) Use(ctx context.Context, key string, window time.Duration, now time.Time) (int, error) {
	from := now.Add(-window)
	n, err := r.C.ZCount(ctx, key, "("+ftoa(ms(from)), "+inf").Result()
	return int(n), err
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 0, 64) }

// Mem is the Backend in process: one mutex over the same windows and leases, for a store
// with no Redis (the in-memory store of the tests and twins).
type Mem struct {
	mu     sync.Mutex
	grants map[string][]time.Time
	leases map[string]map[string]time.Time
	// Fail, when set, is returned by every call: a store that does not answer.
	Fail error
}

func (m *Mem) init() {
	if m.grants == nil {
		m.grants, m.leases = map[string][]time.Time{}, map[string]map[string]time.Time{}
	}
}

// TakeRate is Redis.TakeRate under one mutex.
func (m *Mem) TakeRate(_ context.Context, key string, n int, window time.Duration, now time.Time, _ string) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return Grant{}, m.Fail
	}
	m.init()
	if n <= 0 {
		return Grant{OK: true}, nil
	}
	from := now.Add(-window)
	var in []time.Time
	for _, t := range m.grants[key] {
		if t.After(from) {
			in = append(in, t)
		}
	}
	m.grants[key] = in
	if len(in) >= n {
		oldest := in[0]
		for _, t := range in {
			if t.Before(oldest) {
				oldest = t
			}
		}
		return Grant{Wait: max(oldest.Add(window).Sub(now)+time.Millisecond, time.Millisecond)}, nil
	}
	m.grants[key] = append(in, now)
	return Grant{OK: true}, nil
}

// Acquire is Redis.Acquire under one mutex.
func (m *Mem) Acquire(_ context.Context, key, holder string, n int, ttl time.Duration, now time.Time) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return Grant{}, m.Fail
	}
	m.init()
	if n <= 0 {
		return Grant{OK: true}, nil
	}
	ls := m.leases[key]
	if ls == nil {
		ls = map[string]time.Time{}
		m.leases[key] = ls
	}
	first := time.Time{}
	for h, exp := range ls {
		if !exp.After(now) {
			delete(ls, h)
			continue
		}
		if first.IsZero() || exp.Before(first) {
			first = exp
		}
	}
	if _, held := ls[holder]; !held && len(ls) >= n {
		return Grant{Wait: min(first.Sub(now)+time.Millisecond, MaxPoll)}, nil
	}
	ls[holder] = now.Add(ttl)
	return Grant{OK: true}, nil
}

// Release is Redis.Release under one mutex.
func (m *Mem) Release(_ context.Context, key, holder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	m.init()
	delete(m.leases[key], holder)
	return nil
}

// Use is Redis.Use under one mutex.
func (m *Mem) Use(_ context.Context, key string, window time.Duration, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return 0, m.Fail
	}
	m.init()
	from := now.Add(-window)
	n := 0
	for _, t := range m.grants[key] {
		if t.After(from) {
			n++
		}
	}
	for _, exp := range m.leases[key] {
		if exp.After(now) {
			n++
		}
	}
	return n, nil
}

// Taker is one take of a budget, as a waiter asks it: the server's verb (budget take)
// through the sprint's wire, or a Backend in process (Take).
type Taker func(ctx context.Context) (Grant, error)

// Waiter is a caller's loop over a Taker: it stalls while the budget is spent and never
// fails its caller.
type Waiter struct {
	// Now and Sleep are the clock; nil is the wall clock. Sleep returns ctx's error when
	// ctx ends first.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Log receives one line for the first wait, one every LogEvery after, one when the
	// store cannot be read; nil logs nothing.
	Log      func(line string)
	LogEvery time.Duration
	// What names the budget in the lines (rpm 10 for mercury-3).
	What string
}

// Outcome is how a Wait ended: Granted after Waited; Open when the budget could not be
// read and the caller proceeds unlimited (one line logged); Ended when ctx ended first
// (the card's deadline: the caller is ending anyway).
type Outcome struct {
	Granted, Open, Ended bool
	Waited               time.Duration
	Waits                int
}

func (w Waiter) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w Waiter) sleep(ctx context.Context, d time.Duration) error {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	// up to a fifth more, at random: callers told the same wait do not all ask again in the
	// same instant, and none is always first (the store keeps no queue)
	if d > 0 {
		d += mrand.N(d/5 + 1)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (w Waiter) log(line string) {
	if w.Log != nil {
		w.Log(line)
	}
}

// Wait takes until granted: a grant ends it; a wait sleeps min(the wait, MaxPoll) and
// asks again; an error (the store down, a timeout) ends it Open after one line; ctx's end
// ends it Ended. It never returns an error: the limiter is never the caller's failure.
func (w Waiter) Wait(ctx context.Context, take Taker) Outcome {
	start := w.now()
	every := w.LogEvery
	if every <= 0 {
		every = 30 * time.Second
	}
	var out Outcome
	var logged time.Time
	for {
		if ctx.Err() != nil {
			out.Ended, out.Waited = true, w.now().Sub(start)
			return out
		}
		tctx, cancel := context.WithTimeout(ctx, StoreTimeout)
		g, err := take(tctx)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				out.Ended, out.Waited = true, w.now().Sub(start)
				return out
			}
			w.log(fmt.Sprintf("RATE BUDGET OPEN %s: the budget could not be read (%s); proceeding unlimited", w.What, oneLine(err.Error())))
			out.Open, out.Waited = true, w.now().Sub(start)
			return out
		}
		if g.OK {
			out.Granted, out.Waited = true, w.now().Sub(start)
			if out.Waits > 0 {
				w.log(fmt.Sprintf("RATE BUDGET GRANTED %s after %s (%d waits)", w.What, out.Waited.Round(time.Second), out.Waits))
			}
			return out
		}
		out.Waits++
		d := min(max(g.Wait, time.Millisecond), MaxPoll)
		if now := w.now(); logged.IsZero() || now.Sub(logged) >= every {
			logged = now
			w.log(fmt.Sprintf("RATE BUDGET WAIT %s: spent; waiting %s (waited %s so far); the card stalls, it does not fail", w.What, g.Wait.Round(time.Millisecond), now.Sub(start).Round(time.Second)))
		}
		if err := w.sleep(ctx, d); err != nil {
			out.Ended, out.Waited = true, w.now().Sub(start)
			return out
		}
	}
}

func oneLine(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c == '\n' || c == '\r' {
			b[i] = ' '
		}
	}
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

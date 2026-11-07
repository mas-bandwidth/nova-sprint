// Package cost is the cost reconciliation (v1's cost_reconcile.go in v2's
// shape; the owner, 2026-10-04: "we MUST track the complete cost of what we
// do on the fleet and friends on API plans"): each provider's own count of
// what its key spent, set beside the store's priced API records of that
// provider over the same window. A provider's count is its balance's fall
// between two reads (sprint.balances); the store's is the cost of every API
// run of that provider whose price was recorded in the log between the two
// reads' seqs. A gap over 5% of the provider's figure and over $1 opens ONE
// judgement on the coordinator row (kind cost-gap, choices ack | retry),
// never a second while one is open.
package cost

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/usage"
)

// Every is how often the run loop reconciles.
const Every = time.Hour

// The gap that opens the judgement: over GapShare of the provider's figure
// and over GapFloor micro-dollars ($1; a cent's difference is no gap).
const (
	GapShare = 0.05
	GapFloor = 1_000_000
)

// Kind is the judgement's kind; Choices its answers.
const (
	Kind    = "cost-gap"
	Choices = "ack | retry"
)

// Reading is one provider's balance as read now: Known false with Why when
// there is none to read.
type Reading struct {
	Provider string
	Known    bool
	Why      string
	USD      string // the balance, an exact decimal of dollars
	Raw      string // the answer as read
}

// Reader reads one provider's balance.
type Reader interface {
	Read(ctx context.Context) Reading
}

// DeepSeek reads GET https://api.deepseek.com/user/balance with the key
// DEEPSEEK_API_KEY from the environment, as the worker and the run loop get
// it (nova-secrets exec puts it there; nothing here reads a secrets file).
// Transport and Getenv are seams: a unit test answers with a fake.
type DeepSeek struct {
	Transport http.RoundTripper
	Getenv    func(string) string
}

// DeepSeekBalanceURL is the endpoint.
const DeepSeekBalanceURL = "https://api.deepseek.com/user/balance"

// Read is the balance in USD (balance_infos' USD total_balance).
func (d DeepSeek) Read(ctx context.Context) Reading {
	r := Reading{Provider: "deepseek"}
	get := d.Getenv
	if get == nil {
		get = os.Getenv
	}
	key := get("DEEPSEEK_API_KEY")
	if key == "" || strings.ContainsAny(key, "\r\n") {
		r.Why = "no DEEPSEEK_API_KEY in the environment: run it under nova-secrets exec --only DEEPSEEK_API_KEY"
		return r
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DeepSeekBalanceURL, nil)
	if err != nil {
		r.Why = err.Error()
		return r
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	c := http.Client{Transport: d.Transport, Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		r.Why = "GET " + DeepSeekBalanceURL + " failed: " + strings.ReplaceAll(err.Error(), key, "<key>")
		return r
	}
	defer func() { _ = resp.Body.Close() }() // ignored: read-only
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	r.Raw = string(body)
	if err != nil || resp.StatusCode != http.StatusOK {
		r.Why = fmt.Sprintf("GET %s answered HTTP %d", DeepSeekBalanceURL, resp.StatusCode)
		return r
	}
	var b struct {
		Infos []struct {
			Currency string `json:"currency"`
			Total    string `json:"total_balance"`
		} `json:"balance_infos"`
	}
	if json.Unmarshal(body, &b) != nil {
		r.Why = "the balance answer is not the JSON DeepSeek documents"
		return r
	}
	for _, in := range b.Infos {
		if in.Currency == "USD" {
			if usd, err := usage.Decimal(in.Total); err == nil {
				r.Known, r.USD = true, usd
				return r
			}
		}
	}
	r.Why = "the balance answer has no USD total_balance"
	return r
}

// Inception is mercury's provider: it publishes no balance or usage
// endpoint (docs.inceptionlabs.ai/api-reference, read 2026-11-07: chat, FIM,
// edit, decisions and models; usage only per response), so its reading is
// unknown and its spend is the store's records alone.
type Inception struct{}

// Read is unknown, with why.
func (Inception) Read(context.Context) Reading {
	return Reading{Provider: "inception", Why: "inception publishes no balance or usage endpoint (docs.inceptionlabs.ai/api-reference, 2026-11-07): its spend is the store's records alone"}
}

// Readers are the providers reconciled, with the DeepSeek transport t (nil
// is the default).
func Readers(t http.RoundTripper) []Reader { return []Reader{DeepSeek{Transport: t}, Inception{}} }

// Store is what a reconciliation reads and writes.
type Store interface {
	store.Store
	store.Balances
}

// Line is one provider's reconciliation.
type Line struct {
	Provider  string
	Known     bool
	Why       string // why it was not compared: unknown, a first read, a top-up
	Balance   string
	Prev      string    // the read before, "" for none
	From, To  time.Time // the window
	Spent     int64     // micro-dollars: the balance's fall
	Recorded  int64     // micro-dollars: the store's priced API records of the provider in the window
	Gap       int64     // Spent less Recorded
	Judgement string    // the cost-gap judgement opened, "" when none
}

// Text is the line as the verb prints it.
func (l Line) Text() string {
	switch {
	case !l.Known:
		return fmt.Sprintf("COST %s unknown: %s", l.Provider, l.Why)
	case l.Why != "":
		return fmt.Sprintf("COST %s balance=$%s: %s", l.Provider, l.Balance, l.Why)
	}
	s := fmt.Sprintf("COST %s %s..%s provider=$%s recorded=$%s gap=$%s", l.Provider, l.From.UTC().Format(time.RFC3339), l.To.UTC().Format(time.RFC3339),
		usage.USD(l.Spent), usage.USD(l.Recorded), signedUSD(l.Gap))
	if l.Judgement != "" {
		s += " judgement=" + l.Judgement
	}
	return s
}

func signedUSD(n int64) string {
	if n < 0 {
		return "-" + usage.USD(-n)
	}
	return usage.USD(n)
}

// Reconcile reads every reader, records each known balance at now and the
// store's seq, and sets the fall since the provider's last read beside the
// store's records of the same window. coordinator is the row a judgement is
// opened on.
func Reconcile(ctx context.Context, st Store, now time.Time, coordinator string, readers []Reader) ([]Line, error) {
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var out []Line
	for _, rd := range readers {
		r := rd.Read(ctx)
		l := Line{Provider: r.Provider, Known: r.Known, Why: r.Why, Balance: r.USD, To: now}
		if !r.Known {
			out = append(out, l)
			continue
		}
		prev, had, err := st.LastBalance(ctx, r.Provider)
		if err != nil {
			return out, err
		}
		if err := st.AddBalance(ctx, store.Balance{Provider: r.Provider, At: now, Seq: snap.Seq, USD: r.USD, Raw: r.Raw}); err != nil {
			return out, err
		}
		if !had {
			l.Why = "the first read: the next one measures the spend between them"
			out = append(out, l)
			continue
		}
		l.Prev, l.From = prev.USD, prev.At
		l.Spent = micro(prev.USD) - micro(r.USD)
		if l.Spent < 0 {
			l.Why = "the balance rose since " + prev.At.UTC().Format(time.RFC3339) + " (a top-up): no spend is measured this window"
			out = append(out, l)
			continue
		}
		if l.Recorded, err = Recorded(ctx, st, r.Provider, prev.Seq, snap.Seq); err != nil {
			return out, err
		}
		l.Gap = l.Spent - l.Recorded
		if over(l.Gap, l.Spent) {
			if l.Judgement, err = openGap(ctx, st, snap, now, coordinator, l); err != nil {
				return out, err
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// over says a gap passes both bounds.
func over(gap, spent int64) bool {
	g := max(gap, -gap)
	return g > GapFloor && float64(g) > GapShare*float64(spent)
}

// micro is a decimal of dollars in micro-dollars, half up.
func micro(usd string) int64 {
	r, ok := new(big.Rat).SetString(usd)
	if !ok {
		return 0
	}
	r.Mul(r, big.NewRat(1_000_000, 1))
	n := new(big.Int).Add(r.Num(), new(big.Int).Quo(r.Denom(), big.NewInt(2)))
	return n.Quo(n, r.Denom()).Int64()
}

// Recorded is the store's priced API spend of provider in the log's events
// after seq from up to seq to: every consumer whose priced API record was
// written in that window, at the cost written (a record priced twice counts
// its change).
func Recorded(ctx context.Context, st store.Store, provider string, from, to int64) (int64, error) {
	var sum int64
	for after := from; after < to; {
		evs, err := st.Events(ctx, after, store.MaxEvents)
		if err != nil || len(evs) == 0 {
			return sum, err
		}
		for _, e := range evs {
			if e.Seq > to {
				return sum, nil
			}
			after = e.Seq
			if !e.Accepted || e.After == nil {
				continue
			}
			for id, c := range e.After.Cards {
				if (c.Kind != "work" && c.Kind != "read") || !c.Priced {
					continue
				}
				r := usage.MustParse(c.Usage)
				if r.Billing != usage.BillingAPI || providerOf(r.Model) != provider {
					continue
				}
				var was int64
				if e.Before != nil {
					if b, ok := e.Before.Cards[id]; ok && b.Priced {
						was = b.Cost
					}
				}
				sum += c.Cost - was
			}
		}
	}
	return sum, nil
}

func providerOf(model string) string {
	p, _, _ := strings.Cut(model, "/")
	return p
}

// openGap opens the cost-gap judgement on the coordinator row unless one is
// open; it returns the open one's id either way.
func openGap(ctx context.Context, st store.Store, snap store.Snapshot, now time.Time, coordinator string, l Line) (string, error) {
	n := 0
	for id, j := range snap.Judgements {
		if j.Kind != Kind {
			continue
		}
		if j.State == "open" {
			return id, nil
		}
		n++
	}
	about := cmp.Or(coordinator, "coordinator")
	id := fmt.Sprintf("%s.%s.%d", Kind, about, n+1)
	q := fmt.Sprintf("%s's balance fell $%s from %s to %s, and the store recorded $%s of priced API runs on %s in that window: a gap of $%s. A cost not recorded (a model with no price, a run that reported no tokens, spend outside the sprint) or a price that is wrong. ack once explained; retry to reconcile again.",
		l.Provider, usage.USD(l.Spent), l.From.UTC().Format(time.RFC3339), l.To.UTC().Format(time.RFC3339), usage.USD(l.Recorded), l.Provider, signedUSD(l.Gap))
	op := store.Op{ID: fmt.Sprintf("cost-reconcile-%s-%d-%d", l.Provider, snap.Seq, now.Unix()), Epoch: snap.Epoch, Actor: about, Kind: "judgement", At: now,
		Writes: []store.Write{store.PutJudgement{ID: id, Kind: Kind, About: about, Question: q, Group: Kind,
			Command: fmt.Sprintf("nova-sprint judge %s <choice> --reason <why>   choices: %s", id, Choices)}}}
	r, err := st.Apply(ctx, op)
	if err != nil {
		return "", err
	}
	if !r.Accepted {
		return "", fmt.Errorf("the cost-gap judgement was refused: %s", r.Reason)
	}
	return id, nil
}

// Hourly says when the run loop reconciles: the first beat, then every
// Every after the last (a fake clock in tests).
type Hourly struct{ last time.Time }

// Due says now is a reconcile's time, and marks it.
func (h *Hourly) Due(now time.Time) bool {
	if !h.last.IsZero() && now.Sub(h.last) < Every {
		return false
	}
	h.last = now
	return true
}

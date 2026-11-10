// Package usage is a consumer run's usage record: the tokens it spent by
// class, the model, how it is billed, the harness's own dollar figure, and,
// when it has no price, why. It is v1's record (nova-tools pkg/cardcost,
// internal/sprint/cost.go) in v2's shape: one line of key=value words, kept
// in the card's usage column beside cost and priced (store round 25).
//
//	input=19541 cache_read=36336 cache_write=0 output=692 reasoning=70
//	model=deepseek/deepseek-flash wall=612 harness=nova-worker
//	at=2026-11-09T22:50:06Z billing=api charged=0.042196
//
// A token class the harness did not report is left out, never written as 0;
// wall is the run's seconds as the row measured them (left out when it did
// not); harness and at are the engine's stamps at the report (the holder
// row's harness from the roster, and the report's time, UTC), so the record
// says who ran it, with what, and when it ended, for every harness and every
// card kind; charged is "-" when the harness reported no dollars;
// unpriced=<why> closes a record that has no price. The words are the
// record: Parse reads them back, String writes them, and a word this package
// does not know is kept, in order, after the ones it does.
package usage

import (
	"cmp"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Unreported is a token count the harness did not report: an absence, never
// a zero.
const Unreported int64 = -1

// How a run is billed: the holder row's harness decides (Billing), never the
// words a run reports.
const (
	BillingAPI          = "api"          // a bench on an API key: tokens priced exactly by the policy's sheet
	BillingSubscription = "subscription" // a kin on a subscription seat: free at the margin, tokens kept
)

// Why a run has no price (unpriced=<why>).
const (
	WhyNoTokens = "no-tokens" // an API run that reported no token
	WhyNoPrice  = "no-price"  // an API run whose model has no entry in prices()
)

// Tokens is what one run spent, by class. Each is Unreported when the
// harness did not say.
type Tokens struct {
	Input, CacheRead, CacheWrite, Output, Reasoning int64
}

// None is a run of which no token was reported.
func None() Tokens {
	return Tokens{Unreported, Unreported, Unreported, Unreported, Unreported}
}

// Reported says the harness reported a token class of the run.
func (t Tokens) Reported() bool {
	return t.Input >= 0 || t.CacheRead >= 0 || t.CacheWrite >= 0 || t.Output >= 0 || t.Reasoning >= 0
}

// Total is the run's tokens over the classes reported.
func (t Tokens) Total() int64 {
	var sum int64
	for _, n := range []int64{t.Input, t.CacheRead, t.CacheWrite, t.Output, t.Reasoning} {
		if n > 0 {
			sum += n
		}
	}
	return sum
}

// Add is t and u summed class by class; a class reported by neither stays
// unreported.
func (t Tokens) Add(u Tokens) Tokens {
	add := func(a, b int64) int64 {
		if a < 0 && b < 0 {
			return Unreported
		}
		return max(a, 0) + max(b, 0)
	}
	return Tokens{add(t.Input, u.Input), add(t.CacheRead, u.CacheRead), add(t.CacheWrite, u.CacheWrite), add(t.Output, u.Output), add(t.Reasoning, u.Reasoning)}
}

// Record is one run's usage record.
type Record struct {
	Tokens   Tokens
	Model    string // provider/model, as the harness reported the run
	Models   int    // how many models the run used, when more than one (the model is the one with the most tokens)
	Wall     int64  // the run's seconds, as the row measured them; Unreported when it did not
	Harness  string // the holder row's harness (the roster's), stamped by the engine; "" before it priced the run
	At       string // when the run was reported, RFC 3339 UTC, stamped by the engine; "" before
	Billing  string // api or subscription; "" before the engine priced it
	Charged  string // the harness's own dollars, an exact decimal; "" when it reported none
	Unpriced string // why the record has no price; "" when it has one
	Extra    []string
}

// Empty is a record with nothing in it.
func Empty() Record { return Record{Tokens: None(), Wall: Unreported} }

// Parse reads a usage line. An empty line is Empty. A token count that is
// not a whole number at or above 0, a charged figure that is not a decimal,
// or a word with no "=" is an error naming it: the words are a machine's,
// and a malformed one is refused at the verb, never stored.
func Parse(line string) (Record, error) {
	r := Empty()
	counts := map[string]*int64{"input": &r.Tokens.Input, "cache_read": &r.Tokens.CacheRead, "cache_write": &r.Tokens.CacheWrite,
		"output": &r.Tokens.Output, "reasoning": &r.Tokens.Reasoning, "wall": &r.Wall}
	for _, w := range strings.Fields(line) {
		k, v, ok := strings.Cut(w, "=")
		switch {
		case !ok || k == "":
			return Record{}, fmt.Errorf("usage word %q is not key=value", w)
		case counts[k] != nil:
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return Record{}, fmt.Errorf("usage %s=%s is not a whole number at or above 0", k, v)
			}
			*counts[k] = n
		case k == "model":
			r.Model = v
		case k == "models":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return Record{}, fmt.Errorf("usage models=%s is not a count at or above 1", v)
			}
			r.Models = n
		case k == "harness":
			r.Harness = v
		case k == "at":
			r.At = v
		case k == "billing":
			if v != BillingAPI && v != BillingSubscription {
				return Record{}, fmt.Errorf("usage billing=%s is not api or subscription", v)
			}
			r.Billing = v
		case k == "charged":
			if v == "-" {
				r.Charged = ""
				continue
			}
			d, err := Decimal(v)
			if err != nil {
				return Record{}, fmt.Errorf("usage charged=%s is not dollars (a decimal such as 0.042196)", v)
			}
			r.Charged = d
		case k == "unpriced":
			r.Unpriced = v
		default:
			r.Extra = append(r.Extra, w)
		}
	}
	return r, nil
}

// MustParse is Parse for a line this package wrote: a line that does not
// read is Empty with its words kept as Extra, never a panic.
func MustParse(line string) Record {
	r, err := Parse(line)
	if err != nil {
		r = Empty()
		r.Extra = strings.Fields(line)
	}
	return r
}

// String is the record's one line: the token classes reported, model,
// models, wall, harness, at, billing, charged (once billed: "-" for none)
// and unpriced, then the words it does not know. Empty is "".
func (r Record) String() string {
	var ws []string
	count := func(k string, n int64) {
		if n >= 0 {
			ws = append(ws, k+"="+strconv.FormatInt(n, 10))
		}
	}
	count("input", r.Tokens.Input)
	count("cache_read", r.Tokens.CacheRead)
	count("cache_write", r.Tokens.CacheWrite)
	count("output", r.Tokens.Output)
	count("reasoning", r.Tokens.Reasoning)
	if r.Model != "" {
		ws = append(ws, "model="+word(r.Model))
	}
	if r.Models > 1 {
		ws = append(ws, "models="+strconv.Itoa(r.Models))
	}
	count("wall", r.Wall)
	if r.Harness != "" {
		ws = append(ws, "harness="+word(r.Harness))
	}
	if r.At != "" {
		ws = append(ws, "at="+word(r.At))
	}
	if r.Billing != "" {
		ws = append(ws, "billing="+r.Billing, "charged="+cmp.Or(r.Charged, "-"))
	} else if r.Charged != "" {
		ws = append(ws, "charged="+r.Charged)
	}
	if r.Unpriced != "" {
		ws = append(ws, "unpriced="+word(r.Unpriced))
	}
	return strings.Join(append(ws, r.Extra...), " ")
}

// word keeps a value one word.
func word(v string) string { return strings.Join(strings.Fields(v), "_") }

// Decimal is s as a canonical exact decimal of dollars (no exponent, no
// sign, no trailing zeros past the point): "0.0421960" is "0.042196".
func Decimal(s string) (string, error) {
	whole, frac, dot := strings.Cut(s, ".")
	if whole == "" || strings.Trim(whole, "0123456789") != "" || strings.Trim(frac, "0123456789") != "" || (dot && frac == "") || len(s) > 40 {
		return "", fmt.Errorf("%q is not a decimal", s)
	}
	whole = strings.TrimLeft(whole, "0")
	frac = strings.TrimRight(frac, "0")
	out := cmp.Or(whole, "0")
	if frac != "" {
		out += "." + frac
	}
	return out, nil
}

// USD is micro-dollars as an exact decimal of dollars: 42196 is "0.042196".
func USD(micro int64) string {
	r := new(big.Rat).SetFrac(big.NewInt(max(micro, 0)), big.NewInt(1_000_000))
	d, _ := Decimal(r.FloatString(6)) // ignored: FloatString is always a decimal
	return d
}

// Compact is a token count as a table shows it (v1's CompactTokens): under
// a thousand as itself, else one decimal place, half up, with a 1000-based
// suffix (1.2M, 2M, 48.1M); a count that rounds up to the next suffix takes
// it (999950 is 1M, not 1000K).
func Compact(n int64) string {
	n = max(n, 0)
	units := []struct {
		div int64
		suf string
	}{{1_000, "K"}, {1_000_000, "M"}, {1_000_000_000, "B"}, {1_000_000_000_000, "T"}}
	for i := len(units) - 1; i >= 0; i-- {
		u := units[i]
		if n < u.div {
			continue
		}
		scaled := new(big.Int).Mul(big.NewInt(n), big.NewInt(10))
		scaled.Add(scaled, big.NewInt(u.div/2))
		scaled.Quo(scaled, big.NewInt(u.div))
		whole, frac := scaled.Int64()/10, scaled.Int64()%10
		if whole >= 1000 && i+1 < len(units) {
			return Compact(units[i+1].div) // rounded up into the next suffix
		}
		if frac == 0 {
			return strconv.FormatInt(whole, 10) + u.suf
		}
		return strconv.FormatInt(whole, 10) + "." + strconv.FormatInt(frac, 10) + u.suf
	}
	return strconv.FormatInt(n, 10)
}

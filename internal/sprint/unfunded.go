package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// A PROVIDER OUT OF MONEY FLIPS TO UNFUNDED BY ITSELF, AND ONLY THE OWNER FUNDS IT AGAIN
// (the owner, 2026-10-19, after openrouter and opencode were retried for the tenth time on
// provider rows that still said funded=true: "when a route is not funded, it must FLIP into
// unfunded *automatically* in the config"). The provider row's funded field in nova-config
// is the one true source (docs/SPEC-SPRINT.md section 5, a provider unfunded;
// tla/ProviderFunding.tla):
//
//   - the deal, every draw of a read and the tier's judgment leave out a route whose
//     provider's applied row says funded=false or is malformed, as they leave out a resting
//     route; the route stays enabled across the flip (one write per provider, never per
//     route), so funded alone keeps it out (providerFunded; NoDealToUnfunded);
//   - a take the provider refused for want of credit (class out-of-credit, a 402) on a row
//     that still says funded=true owes ONE flip of the provider (UnfundedOwed, the fleet
//     property PropUnfundedOwed): the run loop writes funded=false through nova-config as
//     the machine, once, and applies it (cmd/nova-sprint/unfunded_flip.go;
//     OneWritePerProvider). A rate limit, a 5xx and a timeout never owe one
//     (RateLimitKeepsFunding): only the provider rest a credit refusal starts does;
//   - only the owner funds a provider again (nova-config provider set <p> --funded true);
//     the funded verb is the owner's and ends only the sprint store's rest (OwnerFunded),
//     the balance poll ends no funds rest of a row that says funded=false, and routes wake
//     refuses an unfunded provider (WakeRoutesFunded; OnlyOwnerFunds,
//     StaysUnfundedUntilOwner, NoEnableOnUnfunded).

// providerFunded says the deal may draw a route of the provider: its applied nova-config
// row says funded=true with no problem. A route that names no provider, a store that holds
// no provider row at all, and a provider with no row of its own keep the deal as it was
// before the rows were read: dealt (the selftest's and the check fixtures' placeholder rows,
// funded=false, name a provider no route of theirs names). Only a row applied as
// funded=false, or a malformed row, keeps its routes out. It is the one home of the
// question for the deal, the reads and the judgments.
//
// tla/ProviderFunding.tla Deal, NoDealToUnfunded; docs/SPEC-SPRINT.md section 5
func (s *Snapshot) providerFunded(name string) bool {
	if forced, _ := forceProviderFunded.Load().(string); forced != "" && forced == name || name == "" || len(s.Providers) == 0 {
		return true
	}
	p, applied := s.Providers[name]
	return !applied || p.Problem == "" && p.Funded
}

// forceProviderFunded is the test-only seam of the reversed witness
// (TestUnfundedProviderIsNeverDealt): the provider it names is funded whatever its row says,
// as the deal was before it read the rows. Empty (always, outside that test) forces none;
// keyed by the name, so no other test's provider is touched.
var forceProviderFunded atomic.Value

// rateLimitWitness is the test-only seam of the reversed witness of RateLimitKeepsFunding
// (TestRateLimitOwesNoFlip): a rate limit whose provider words carry it is let through as a
// refusal for credit (refusal, provider_funds.go), so it rests the provider and owes a flip.
// Empty (always, outside that test) lets none through. Keyed by the words, never a switch
// for the whole package, so no other test's rate limit is touched.
var rateLimitWitness atomic.Value

// rateLimitFlips says the rate limit's words carry the witness's mark.
func rateLimitFlips(msg string) bool {
	w, _ := rateLimitWitness.Load().(string)
	return w != "" && strings.Contains(msg, w)
}

// rowFunded says the provider's applied row says funded=true with no problem: the row a
// credit refusal flips. A provider with no row has nothing to flip.
func (s *Snapshot) rowFunded(name string) bool {
	p, applied := s.Providers[name]
	return applied && p.Problem == "" && p.Funded
}

// rowUnfunded says the provider's applied row says funded=false (no problem): the state
// only the owner ends.
func (s *Snapshot) rowUnfunded(name string) bool {
	p, applied := s.Providers[name]
	return applied && p.Problem == "" && !p.Funded
}

// UnfundedWhy is the words the deal and the judgments say of a route left out because its
// provider is unfunded.
func UnfundedWhy(provider string) string {
	return "provider " + provider + " unfunded (the owner's to fund)"
}

// OwnerFundsRemedy is the owner's verbs that fund a provider again: the row first, then its
// apply; the funded verb ends the sprint store's rest after.
func OwnerFundsRemedy(provider, owner string) string {
	return "the owner funds it: nova-config provider set " + provider + " --funded true --actor " + cmp.Or(owner, "<owner>") + ", then nova-config apply --kind provider"
}

// PropUnfundedOwed is the fleet table's property that holds a provider's flip: owed (a credit
// refusal on a row that says funded=true, the run loop's to write) or done (written, or the
// row said funded=false already), ONE per provider, keyed by the rest it was owed for.
func PropUnfundedOwed(provider string) string { return "provider_unfunded_owed_" + provider }

// The states of PropUnfundedOwed.
const (
	FlipOwed = "owed"
	FlipDone = "done"
)

// maxFlipMsg bounds the provider's words a flip carries into the config note.
const maxFlipMsg = 200

// UnfundedFlip is one provider's flip as PropUnfundedOwed holds it: its state, the start of
// the rest it is owed for (the key: one flip per rest), when it was owed or done, and for an
// owed one the refusal's class, status, card and bounded words; for a done one the note.
type UnfundedFlip struct {
	Provider string
	State    string
	Rest     time.Time
	At       time.Time
	Class    string
	Status   string
	Card     string
	Msg      string
	Note     string
}

// value is the flip as the property holds it:
// `owed <rest> <at> <class> <status> <card> <msg>` or `done <rest> <at> <note>`.
func (f UnfundedFlip) value() string {
	if f.State == FlipDone {
		return oneLine(FlipDone + " " + stamp(f.Rest) + " " + stamp(f.At) + " " + f.Note)
	}
	return oneLine(FlipOwed + " " + stamp(f.Rest) + " " + stamp(f.At) + " " + orDash(f.Class) + " " + orDash(f.Status) + " " + orDash(f.Card) + " " + f.Msg)
}

// ConfigNote is the note the machine writes on the provider row when it flips it:
// `unfunded by the machine <time>: <class> <status> <msg> on <card>`.
func (f UnfundedFlip) ConfigNote(now time.Time) string {
	return oneLine(fmt.Sprintf("unfunded by the machine %s: %s %s %s on %s", stamp(now), f.Class, f.Status, f.Msg, f.Card))
}

// parseFlip is a flip as its property holds it; ok is false for a value that is not one.
func parseFlip(provider, v string) (UnfundedFlip, bool) {
	f := strings.Fields(v)
	if len(f) < 3 || f[0] != FlipOwed && f[0] != FlipDone {
		return UnfundedFlip{}, false
	}
	rest, e1 := time.Parse(time.RFC3339, f[1])
	at, e2 := time.Parse(time.RFC3339, f[2])
	if e1 != nil || e2 != nil {
		return UnfundedFlip{}, false
	}
	out := UnfundedFlip{Provider: provider, State: f[0], Rest: rest, At: at}
	if out.State == FlipDone {
		out.Note = strings.Join(f[3:], " ")
		return out, true
	}
	if len(f) < 6 {
		return UnfundedFlip{}, false
	}
	out.Class, out.Status, out.Card, out.Msg = f[3], f[4], f[5], strings.Join(f[6:], " ")
	return out, true
}

// UnfundedFlips is every flip the fleet table's properties hold, by provider.
func UnfundedFlips(props map[string]string) map[string]UnfundedFlip {
	out := map[string]UnfundedFlip{}
	for name, v := range props {
		p, ok := strings.CutPrefix(name, PropUnfundedOwed(""))
		if !ok || p == "" {
			continue
		}
		if f, ok := parseFlip(p, v); ok {
			out[p] = f
		}
	}
	return out
}

// FlipsOwed is the flips owed now, in provider order: each recorded in the properties as
// owed. The run loop writes each (cmd/nova-sprint/unfunded_flip.go).
func FlipsOwed(props map[string]string) []UnfundedFlip {
	flips := UnfundedFlips(props)
	var out []UnfundedFlip
	for _, p := range slices.Sorted(maps.Keys(flips)) {
		if flips[p].State == FlipOwed {
			out = append(out, flips[p])
		}
	}
	return out
}

// creditRest says the provider's rest is one a take refused for credit began
// (RouteRest.Refused), holding at now, with the refusal's words parsed: the one rest that
// owes a flip. A rate limit, a 5xx or a timeout never begins one (providerRestsDue reads
// only refusal), nor does the coordinator's rest; a refused key (auth) is not credit.
func creditRest(rest RouteRest, now time.Time) (class, status, msg string, ok bool) {
	if !rest.Resting(now) || !rest.Refused() {
		return "", "", "", false
	}
	m := causeRE.FindStringSubmatch(rest.Why)
	if m == nil || m[1] != RestCredit {
		return RestCredit, "-", "", true
	}
	return m[1], m[2], cutText(strings.TrimSpace(m[3]), maxFlipMsg), true
}

// FlipsDue says whether a pass of the run loop has a flip to plan or to write, from the
// fleet table's properties and the applied provider rows alone (no card read): a flip owed,
// or a credit rest holding whose flip is not yet recorded, on a provider whose row the
// store holds.
func FlipsDue(props map[string]string, providers map[string]Provider, now time.Time) bool {
	flips := UnfundedFlips(props)
	for _, f := range flips {
		if f.State == FlipOwed {
			return true
		}
	}
	t := NewTable(Fleet)
	t.SetProps(props)
	for p, rest := range ProviderRests(t) {
		if _, _, _, ok := creditRest(rest, now); !ok {
			continue
		}
		if _, applied := providers[p]; !applied {
			continue
		}
		if f, had := flips[p]; had && f.Rest.Equal(rest.At) {
			continue
		}
		return true
	}
	return false
}

// UnfundedOwed is the plan step that records the flips owed: for each provider a take
// refused for credit rests (its rest, providerRestsDue, the tick wrote) whose applied row
// still says funded=true, ONE flip owed (PropUnfundedOwed, never one per route or per
// take), keyed by that rest; a provider whose row says funded=false already is recorded
// done with nothing to write (the owner's, or the machine's earlier flip). A flip recorded
// for the same rest is never recorded again, so an owner who funds the row while the sprint
// rest still holds is never flipped back: only a new refusal, after the rest ended, owes
// another (StaysUnfundedUntilOwner).
//
// tla/ProviderFunding.tla BillingFailure, OneWritePerProvider, RateLimitKeepsFunding;
// docs/SPEC-SPRINT.md section 5
func UnfundedOwed(s *Snapshot) Plan {
	var p Plan
	flips := UnfundedFlips(s.Fleet.Props())
	rests := ProviderRests(s.Fleet)
	for _, name := range slices.Sorted(maps.Keys(rests)) {
		rest := rests[name]
		class, status, msg, ok := creditRest(rest, s.Now)
		if !ok {
			continue
		}
		if f, had := flips[name]; had && f.Rest.Equal(rest.At) {
			continue // this rest's flip is recorded: one per rest, never again
		}
		var f UnfundedFlip
		switch {
		case s.rowFunded(name):
			card := ""
			if len(rest.Cards) > 0 {
				card = rest.Cards[0]
			}
			f = UnfundedFlip{Provider: name, State: FlipOwed, Rest: rest.At, At: s.Now, Class: class, Status: status, Card: card, Msg: msg}
		case s.rowUnfunded(name):
			f = UnfundedFlip{Provider: name, State: FlipDone, Rest: rest.At, At: s.Now, Note: "the row said funded=false already: nothing written"}
		default:
			continue // no row, or a malformed one: nothing to flip
		}
		was, had := s.Fleet.Prop(PropUnfundedOwed(name))
		p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropUnfundedOwed(name), Value: f.value(), Was: was, WasAbsent: !had})
		if f.State == FlipOwed {
			p.Units = append(p.Units, Unit{Key: name, Moved: "provider " + name + " owes its flip to unfunded: " + class + " " + status + " on " + orDash(f.Card)})
		}
	}
	return p
}

// UnfundedFlipped is the plan step that records a flip written (or found written): the
// provider's owed flip becomes done with the note, guarded on the owed value read, so a
// flip is recorded done once.
//
// tla/ProviderFunding.tla BillingFailure (the write), OneWritePerProvider
func UnfundedFlipped(s *Snapshot, provider string, rest time.Time, note string) Plan {
	var p Plan
	was, had := s.Fleet.Prop(PropUnfundedOwed(provider))
	f, ok := parseFlip(provider, was)
	if !had || !ok || f.State != FlipOwed || !f.Rest.Equal(rest) {
		p.refuse(provider, "provider "+provider+" owes no flip for the rest of "+stamp(rest)+": nothing to record")
		return p
	}
	done := UnfundedFlip{Provider: provider, State: FlipDone, Rest: rest, At: s.Now, Note: note}
	p.Props = append(p.Props, PropWrite{Table: Fleet, Name: PropUnfundedOwed(provider), Value: done.value(), Was: was})
	p.Units = append(p.Units, Unit{Key: provider, Moved: "provider " + provider + " flipped unfunded: " + note})
	return p
}

// OwnerFunded is the funded verb, the owner's alone: refused for any other actor (the
// coordinator and the machine included) with the owner's remedy, else Funded. It ends only
// the sprint store's rest and never writes nova-config: the row is funded by the owner's own
// nova-config provider set.
//
// tla/ProviderFunding.tla OwnerFund, MachineFundAttempt, OnlyOwnerFunds
func OwnerFunded(s *Snapshot, r FundedReq, owner string) Plan {
	if why := NotOwnerFunds(owner, r.Who, r.Provider); why != "" {
		var p Plan
		p.refuse(r.Provider, why)
		return p
	}
	return Funded(s, r)
}

// NotOwnerFunds is why who may not run funded ("" is may): the sprint names no owner, or
// who is not the owner (the seat's own comparison, NotSeat: the owner is init --owner's).
func NotOwnerFunds(owner, who, provider string) string {
	switch {
	case who == MachineActor && owner != MachineActor:
		return "funded is the owner's, never the machine's: " + OwnerFundsRemedy(provider, owner) + "; nothing was changed"
	case owner == "":
		return "funded is the owner's and the sprint names no owner (init --owner <name>, or NOVA_SPRINT_OWNER): " + OwnerFundsRemedy(provider, owner) + "; nothing was changed"
	case who != owner:
		return "funded is the owner's alone: " + owner + ", not " + orDash(who) + "; " + OwnerFundsRemedy(provider, owner) + "; nothing was changed"
	}
	return ""
}

// WakeRoutesFunded is routes wake with the unfunded provider refused: a provider, or a
// route of a provider, whose applied row says funded=false is the owner's to fund, and no
// wake serves it (the owner's verb is named); any other target is WakeRoutes.
//
// tla/ProviderFunding.tla RouteEnable, NoEnableOnUnfunded
func WakeRoutesFunded(s *Snapshot, r RouteWakeReq, owner string) Plan {
	provider, _, _, ok := routeTarget(s, r.Target)
	if ok && s.rowUnfunded(provider) {
		var p Plan
		p.refuse(r.Target, "routes wake: "+UnfundedWhy(provider)+": "+OwnerFundsRemedy(provider, owner)+"; nothing was changed")
		return p
	}
	return WakeRoutes(s, r)
}

// routeUnfunded is the provider of the route named, and whether its row keeps it out of the
// deal (providerFunded).
func (s *Snapshot) routeUnfunded(name string) (string, bool) {
	for _, r := range s.Routes {
		if r.Name == name {
			return r.Provider, !s.providerFunded(r.Provider)
		}
	}
	return "", false
}

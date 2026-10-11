package sprint

import (
	"fmt"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/harness"
)

// A PINNED CARD GOES ONLY TO A WORKER THAT CAN SERVE ITS PIN (v1.2.14; 2026-10-11: a card
// whose brief pinned `model: anthropic/claude-fable-5-1` was dealt to batman, hetzner2,
// superman and b3, its whole redeal bound in 22 seconds, each take refused `class=auth`
// for want of an Anthropic key no fleet member holds, and a judgment then blamed the card).
//
// A `model: <provider>/<model>` line pins a card to that model, run under the default
// harness (opencode), which reads the provider's key from the member's environment. The
// sprint knows a provider's key is on the fleet only by the routes nova-config applies: a
// member SERVES a pin when
//
//   - an enabled route of the pin's provider runs under opencode (the fleet play installs
//     the keys of the providers the routes name, and no other),
//   - the provider does not rest (PropProviderRest: its funds or its key), and
//   - the member's pins of that provider do not rest (pinRestsOf): a refusal of one for its
//     key or credit rests them on that member for RouteRestFor, and after MaxPinRefusals
//     such refusals the rest STANDS, with no clock, until the member is brought up again
//     (its control card's since moves past the last refusal: the key play and fleet up).
//
// A store with no route at all knows nothing of keys: a pin is served there by every member
// whose pins do not rest (a rest written while routes were held stays, and binds the deal, the
// withdrawal and the take alike), and a refusal of one ends its take as any provider failure
// does (takeEnded: a redeal spent, the redeal bound and its judgment end it), so a pin no member
// can serve never loops there and is never stranded when the routes change.
//
// The deal, the redeal, the rebalance and the level never place a pinned card on a member
// that does not serve its pin (routeOf, routeServed, notLaunching); a take of one on a member
// whose pins of its provider rest is refused (pinRestWhy) and the tick withdraws it
// (restWithdrawals). A pinned card no member of its pool serves waits ready, burns no
// redeal, and the tick raises ONE judgment of the pin (NNoRoute, subject PinSubject), open
// while any card of the pin waits for it, closed when one is served; once every member's
// rest stands it stays open, never reopened, until something changes. A take the provider
// refused for its key or credit on a pin, in a routed store, is a coordination failure, never
// the worker's and never the card's (pinRefusedReturned, as needs_stop.go's): no
// FieldTakeEnded, so no redeal is spent; no ok field, so no fail mark and no ok% hit on the
// member's row; its consumer end (EndPinUnserved) counts in neither ok nor failed. Its record
// is ONE per member on the work card (FieldPinTake, latest wins, with FieldPinRefusals its
// count), so the card's fields are bounded by the members, never by the refusals. A late
// refusal (the deadline already ended the take) keeps the deadline's path, as every late
// report does. The model is tla/RouteIndex.tla (THE PIN).

// FieldPinTake + <member> is the latest refused pin take's record on a work card, and
// FieldPinRefusals + <member> the count of them: one of each per member, latest wins.
const (
	FieldPinTake     = "pin_take_"
	FieldPinRefusals = "pin_refusals_"
)

// MaxPinRefusals is how many refusals of a provider's pins on one member rest them for a
// time; at it the rest stands until the member is brought up again.
const MaxPinRefusals = 3

// EndPinUnserved is the consumer end a pin's refused take records on the producer: counted
// in neither ok nor failed by the route and model stats (stats.go), as EndNeedsUnlanded.
const EndPinUnserved = "pin not served"

// PinSubject is the stream word a pin's judgment is filed under: no stream id has a colon,
// so it is never a stream's.
func PinSubject(model string) string { return "pin:" + model }

// pinOf is the model the card's brief pins, "" for none or for model lines that do not
// read (judged under the tier its line 1 names, routeTier).
func pinOf(c *Card) string {
	m, bad := modelOf(c)
	if bad != "" {
		return ""
	}
	return m.Pin
}

// pinProvider is the provider a pinned model names: the word before its first slash.
func pinProvider(model string) string {
	p, _, _ := strings.Cut(model, "/")
	return p
}

// PinTakes is the refused pin take records a work card holds, one per member (FieldPinTake),
// with each member's count of them (FieldPinRefusals).
func PinTakes(wc *Card) (takes map[string]ProviderTake, counts map[string]int) {
	for k, v := range wc.Fields {
		if m, ok := strings.CutPrefix(k, FieldPinTake); ok && v != "" {
			if takes == nil {
				takes, counts = map[string]ProviderTake{}, map[string]int{}
			}
			takes[m], counts[m] = parseTake(v), wc.Int(FieldPinRefusals+m)
		}
	}
	return takes, counts
}

// pinRestKey is a member's rest of a provider's pins in pinRests.
func pinRestKey(provider, member string) string { return provider + " " + member }

// pinRestsOf is each member's rest of a provider's pins at s.Now, by pinRestKey, its why: a
// pure function of the fleet table's pin take records (PinTakes) and the members' control
// cards. The latest refusal rests that provider's pins on the member until RouteRestFor after
// it; with MaxPinRefusals or more of them (over every work card) the rest stands with no end
// until the member is brought up again. A member brought up (its control card's since later
// than a refusal) starts afresh, as tla/RouteIndex.tla's MemberUp resets prc: a record that
// ended before its since counts nothing and rests nothing, and pinRefusedReturned restarts
// the count of a record older than it.
func (s *Snapshot) pinRestsOf() map[string]string {
	out := map[string]string{}
	if s.Fleet == nil {
		return out
	}
	var last map[string]time.Time
	var count map[string]int
	for _, c := range s.Fleet.Column(Ready, Working, Finished, DoneOK, DoneFailed, DoneDefect, Withdrawn) {
		if c.F(FieldRoute) != RoutePin {
			continue // a pin's records live on its own work card: the scan costs no other card anything
		}
		takes, counts := PinTakes(c)
		for m, t := range takes {
			at, err := time.Parse(time.RFC3339, t.Finished)
			if err != nil || refusal(t.Error) == "" || s.memberSince(m).After(at) {
				continue // not a refusal, or the member was brought up after it: it counts nothing
			}
			if last == nil {
				last, count = map[string]time.Time{}, map[string]int{}
			}
			k := pinRestKey(pinProvider(t.Model), m)
			count[k] += max(counts[m], 1)
			if at.After(last[k]) {
				last[k] = at
			}
		}
	}
	for k, at := range last {
		prov, m, _ := strings.Cut(k, " ")
		if until := at.Add(RouteRestFor); s.Now.Before(until) {
			out[k] = "provider " + prov + " refused " + m + "'s key or credit on a pin; its pins rest on " + m + " until " + stamp(until)
			continue
		}
		if count[k] < MaxPinRefusals {
			continue
		}
		out[k] = fmt.Sprintf("provider %s refused %s's key or credit on a pin %d times; its pins rest on %s until it is brought up again (fix the key, then fleet down and fleet up %s)", prov, m, count[k], m, m)
	}
	return out
}

// memberSince is when the member's control card says it last came up (its since), the zero
// time when it says none.
func (s *Snapshot) memberSince(member string) time.Time {
	t, _ := time.Parse(time.RFC3339, s.MemberCtl(member).F("since"))
	return t
}

// pinRests is the members' pin rests at s.Now: the ones a dealing step settled once
// (withRests), else read from the fleet table now.
func (s *Snapshot) pinRests() map[string]string {
	if s.pinRested != nil {
		return s.pinRested
	}
	return s.pinRestsOf()
}

// pinRestWhy is why the member's pins of the provider rest, "" when they do not: the
// provider's own rest (PropProviderRest) or the member's (pinRests). It reads no route, so a
// take, which loads none, asks it too.
func (s *Snapshot) pinRestWhy(prov, member string) string {
	if v, ok := s.Fleet.Prop(PropProviderRest(prov)); ok {
		if rest, ok := parseRest(v); ok && rest.Resting(s.Now) {
			return "provider " + prov + " rests until " + rest.UntilSaid() + " (" + rest.Said() + ")"
		}
	}
	return s.pinRests()[pinRestKey(prov, member)]
}

// pinServedWhy is why member does not serve the pinned model, "" when it does (or member is
// "", a check of the card alone). A store with no route knows nothing of keys, so it skips the
// route check, but a rest the fleet table still holds (the provider's, or the member's pins,
// from before the routes went) applies there as everywhere: the deal, the withdrawal and the
// take ask the same rests, so no pinned card is dealt where a take refuses it.
func (s *Snapshot) pinServedWhy(model, member string) string {
	if member == "" {
		return ""
	}
	prov := pinProvider(model)
	if len(s.Routes) == 0 {
		return s.pinRestWhy(prov, member)
	}
	keyed := false
	for _, r := range s.Routes {
		if r.Enabled && r.Provider == prov && !harness.IsHeadless(r.Harness) {
			keyed = true
			break
		}
	}
	if !keyed {
		return "no enabled opencode route names provider " + prov + ", so no member holds its key"
	}
	return s.pinRestWhy(prov, member)
}

// pinUnservedWhy is the judgment's sentence for the pinned primary c (ec as its next deal
// draws it) when no member of its deal pool (dealPool over up) serves its pin; "" when it
// is not pinned, its pool is empty (its own hold or judgment), or a member serves it.
func (s *Snapshot) pinUnservedWhy(c, ec *Card, up []string) string {
	pin := pinOf(ec)
	if pin == "" {
		return ""
	}
	pool, _, _ := s.dealPool(c, up)
	if len(pool) == 0 {
		return ""
	}
	var whys []string
	for _, m := range pool {
		w := s.pinServedWhy(pin, m)
		if w == "" {
			return ""
		}
		whys = append(whys, m+": "+w)
	}
	return "no worker can serve pin " + pin + " (" + strings.Join(dedupWhys(whys), "; ") + "): it waits and spends no redeal; give the fleet the provider's key and an enabled route of " + pinProvider(pin) + ", or brief it again on a route's tier"
}

// dedupWhys folds members that share a why into one entry, in the order first seen.
func dedupWhys(whys []string) []string {
	by, order := map[string][]string{}, []string(nil)
	for _, w := range whys {
		m, why, _ := strings.Cut(w, ": ")
		if _, ok := by[why]; !ok {
			order = append(order, why)
		}
		by[why] = append(by[why], m)
	}
	out := make([]string, 0, len(order))
	for _, why := range order {
		out = append(out, strings.Join(by[why], ", ")+": "+why)
	}
	return out
}

// pinRefused says a failed finish of the work card c is the provider's refusal of a pin for
// its key or credit in a store that holds routes: a coordination failure (pinRefusedReturned).
// In a store with no route nothing is known of keys, so the refusal ends the take as any
// provider failure does (takeEnded), and the redeal bound ends it: never a loop.
func pinRefused(s *Snapshot, c *Card, kind string, r FinishReq) bool {
	return len(s.Routes) > 0 && kind == cardhdr.EndProvider && c.F(FieldRoute) == RoutePin && refusal(r.Report) != ""
}

// pinRefusedReturned is the unit of a take of a pin the provider refused for its key or
// credit: the work card is withdrawn WITHOUT FieldTakeEnded (no redeal spent) and with no ok
// field (no fail mark, no ok% hit on the member), its record (FieldPinTake + member, latest
// wins, FieldPinRefusals + member counted) rests the pin's provider for that member
// (pinRestsOf), and the primary goes back to ready, where the deal gives it to a member that
// serves the pin or the pin's one judgment names it.
func pinRefusedReturned(s *Snapshot, c, pr *Card, r FinishReq) Unit {
	set := nextGen(c, "", s.Now)
	set["withdrawn"] = stamp(s.Now)
	line := cutText(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.Report, cardhdr.EndProvider), ":")), MaxProviderErrorBytes)
	set[FieldProviderError] = line
	set[FieldTakenBack] = "provider " + pinProvider(c.F(FieldModel)) + " refused " + c.Row + " on pin " + c.F(FieldModel) + ": a coordination failure, not the worker's"
	dealt, taken := takeStamps(c)
	set[FieldPinTake+c.Row] = ProviderTake{Route: RoutePin, Model: c.F(FieldModel), Member: c.Row,
		Finished: stamp(s.Now), Error: line, Taken: taken}.String()
	// the count restarts when the member was brought up after the record's last refusal
	// (tla/RouteIndex.tla, MemberUp resets prc): only refusals since it came up count
	n := c.Int(FieldPinRefusals + c.Row)
	if prev, err := time.Parse(time.RFC3339, parseTake(c.F(FieldPinTake+c.Row)).Finished); err != nil || s.memberSince(c.Row).After(prev) {
		n = 0
	}
	set[FieldPinRefusals+c.Row] = itoa(n + 1)
	prSet := map[string]string{}
	addConsumer(pr, prSet, workConsumer(s, c, 0, EndPinUnserved, costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)))
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, moveEntry(c, c.Row, Withdrawn, set, "taken", "dealt")),
		change(Work, moveEntry(pr, pr.Row, Ready, prSet, "work")),
	}, Moved: fmt.Sprintf("%s working -> withdrawn gen=%d, provider %s refused %s on pin %s (a coordination failure: no redeal spent, no fail mark); %s working -> ready",
		c.ID, c.Int("gen")+1, pinProvider(c.F(FieldModel)), c.Row, c.F(FieldModel), pr.ID)}
}

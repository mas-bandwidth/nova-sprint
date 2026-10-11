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
//   - the member's own takes of a pin of that provider were not refused for its key or
//     credit within RouteRestFor (FieldPinTake: the pin's rest for that member alone).
//
// A store with no route at all knows nothing of keys: every pin is served there, as before.
//
// The deal, the redeal, the rebalance and the level never place a pinned card on a member
// that does not serve its pin (routeOf, routeServed, notLaunching). A pinned card no member
// of its pool serves waits ready, burns no redeal, and the tick raises ONE judgment of the
// pin (NNoRoute, subject PinSubject), open while any card of the pin waits for it, closed
// when one is served or none waits: never one a tick. A take the provider refused for its
// key or credit on a pin is a coordination failure, never the worker's and never the card's
// (pinRefusedReturned, as needs_stop.go's): no FieldTakeEnded, so no redeal is spent; no ok
// field, so no fail mark and no ok% hit on the member's row; its consumer end
// (EndPinUnserved) counts in neither ok nor failed. Its record (FieldPinTake) rests that
// provider's pins for that member. The model is tla/PinServe.tla.

// FieldPinTake is a pin's refused take's record on a returned work card, numbered 1, 2,
// 3, ... with no gap (PinTakes reads every one): the member's rest of the pin's provider
// (pinRests) is read from these records alone.
const FieldPinTake = "pin_take_"

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

// PinTakes is every refused pin take record a work card holds (FieldPinTake), in order.
func PinTakes(wc *Card) []ProviderTake {
	var out []ProviderTake
	for n := 1; ; n++ {
		v := wc.F(FieldPinTake + itoa(n))
		if v == "" {
			return out
		}
		out = append(out, parseTake(v))
	}
}

// pinRestKey is a member's rest of a provider's pins in pinRests.
func pinRestKey(provider, member string) string { return provider + " " + member }

// pinRestsOf is each member's rest of a provider's pins at now, a pure function of the
// fleet table's pin take records (PinTakes): a take of a pin the provider refused for its
// key or credit (refusal) rests that provider's pins for that member until RouteRestFor
// after it ended. A fixed end from the record, so it ends by itself, and a member given the
// key is dealt the pin again then; a refusal after it rests it again, spending nothing.
func pinRestsOf(fleet *Table, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	if fleet == nil {
		return out
	}
	for _, c := range fleet.Column(Ready, Working, Finished, DoneOK, DoneFailed, DoneDefect, Withdrawn) {
		for _, t := range PinTakes(c) {
			at, err := time.Parse(time.RFC3339, t.Finished)
			if err != nil || refusal(t.Error) == "" {
				continue
			}
			until := at.Add(RouteRestFor)
			k := pinRestKey(pinProvider(t.Model), t.Member)
			if now.Before(until) && until.After(out[k]) {
				out[k] = until
			}
		}
	}
	return out
}

// pinRests is the members' pin rests at s.Now: the ones a dealing step settled once
// (withRests), else read from the fleet table now.
func (s *Snapshot) pinRests() map[string]time.Time {
	if s.pinRested != nil {
		return s.pinRested
	}
	return pinRestsOf(s.Fleet, s.Now)
}

// pinServedWhy is why member does not serve the pinned model, "" when it does (or member is
// "", a check of the card alone, or the store holds no route: nothing is known of keys).
func (s *Snapshot) pinServedWhy(model, member string) string {
	if member == "" || len(s.Routes) == 0 {
		return ""
	}
	prov := pinProvider(model)
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
	if v, ok := s.Fleet.Prop(PropProviderRest(prov)); ok {
		if rest, ok := parseRest(v); ok && rest.Resting(s.Now) {
			return "provider " + prov + " rests until " + rest.UntilSaid() + " (" + rest.Said() + ")"
		}
	}
	if until, ok := s.pinRests()[pinRestKey(prov, member)]; ok {
		return "provider " + prov + " refused " + member + "'s key or credit on a pin; its pins rest on " + member + " until " + stamp(until)
	}
	return ""
}

// pinUnservedWhy is the judgment's sentence for the pinned primary c (ec as its next deal
// draws it) when no member of its deal pool (dealPool over up) serves its pin; "" when it
// is not pinned, its pool is empty (its own hold or judgment), or a member serves it.
func (s *Snapshot) pinUnservedWhy(c, ec *Card, up []string) string {
	pin := pinOf(ec)
	if pin == "" || len(s.Routes) == 0 {
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
	return "no worker can serve pin " + pin + " (" + strings.Join(dedupWhys(whys), "; ") + "): it waits ready and spends no redeal; give the fleet the provider's key and an enabled route of " + pinProvider(pin) + ", or brief it again on a route's tier"
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
// its key or credit: a coordination failure (pinRefusedReturned).
func pinRefused(c *Card, kind string, r FinishReq) bool {
	return kind == cardhdr.EndProvider && c.F(FieldRoute) == RoutePin && refusal(r.Report) != ""
}

// pinRefusedReturned is the unit of a take of a pin the provider refused for its key or
// credit: the work card is withdrawn WITHOUT FieldTakeEnded (no redeal spent) and with no ok
// field (no fail mark, no ok% hit on the member), its record (FieldPinTake) rests the pin's
// provider for that member (pinRestsOf), and the primary goes back to ready, where the deal
// gives it to a member that serves the pin or the pin's one judgment names it.
func pinRefusedReturned(s *Snapshot, c, pr *Card, r FinishReq) Unit {
	set := nextGen(c, "", s.Now)
	set["withdrawn"] = stamp(s.Now)
	line := cutText(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(r.Report, cardhdr.EndProvider), ":")), MaxProviderErrorBytes)
	set[FieldProviderError] = line
	set[FieldTakenBack] = "provider " + pinProvider(c.F(FieldModel)) + " refused " + c.Row + " on pin " + c.F(FieldModel) + ": a coordination failure, not the worker's"
	dealt, taken := takeStamps(c)
	set[FieldPinTake+itoa(len(PinTakes(c))+1)] = ProviderTake{Route: RoutePin, Model: c.F(FieldModel), Member: c.Row,
		Finished: stamp(s.Now), Error: line, Taken: taken}.String()
	prSet := map[string]string{}
	addConsumer(pr, prSet, workConsumer(s, c, 0, EndPinUnserved, costRecord(s, r.Usage, c.F(FieldRoute), c.F(FieldModel), false, dealt, taken)))
	return Unit{Key: c.ID, Stream: pr.Row, Changes: []Change{
		change(Fleet, moveEntry(c, c.Row, Withdrawn, set, "taken", "dealt")),
		change(Work, moveEntry(pr, pr.Row, Ready, prSet, "work")),
	}, Moved: fmt.Sprintf("%s working -> withdrawn gen=%d, provider %s refused %s on pin %s (a coordination failure: no redeal spent, no fail mark); %s working -> ready",
		c.ID, c.Int("gen")+1, pinProvider(c.F(FieldModel)), c.Row, c.F(FieldModel), pr.ID)}
}

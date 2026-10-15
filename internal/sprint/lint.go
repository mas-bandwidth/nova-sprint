package sprint

import (
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// GATE-NEEDS (docs/SPEC-SPRINT.md, the gate-host rule; docs/SPEC-CARD-CONTRACT.md,
// the gate-host rule): a brief names its gate's needs as a header line, e.g.
// `GATE-NEEDS: linux, tla`. The deal matches a card to a member only when the member's
// facts (FieldFacts) satisfy every need, and a card no member satisfies is a judgment,
// never a silent wait. The brief lint refuses a brief that tells the worker to ssh to a
// named host or run its gate "on <host>" (internal/card.Checks, the gate-host check),
// naming GATE-NEEDS as the remedy, because the gate runs on the member it is dealt to,
// inside the wall, never over ssh.

// gateNeedsCache memoizes GateNeeds by brief (a brief never names a different
// GATE-NEEDS line once written, and the deal walks every ready brief each tick):
// one parse a brief, as modelCache memoizes ReadModel (route.go).
var gateNeedsCache sync.Map // brief string -> []string

// GateNeeds reads a brief's GATE-NEEDS header: the needs, comma-separated and
// trimmed, with "none" and "-" dropped; nil when the brief names none.
func GateNeeds(brief string) []string {
	if v, ok := gateNeedsCache.Load(brief); ok {
		return v.([]string)
	}
	var out []string
	for line := range strings.SplitSeq(brief, "\n") {
		if k, v, ok := cardhdr.KeyValue(line); ok && k == "GATE-NEEDS" {
			for _, n := range Split(v) {
				if n != "none" && n != "-" {
					out = append(out, n)
				}
			}
			break
		}
	}
	gateNeedsCache.Store(brief, out)
	return out
}

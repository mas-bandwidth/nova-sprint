package sprint

import (
	"fmt"
	"strings"
)

// The fleet table against nova-config (docs/SPEC-SPRINT.md section 8, "Drift
// alarms", "The fleet table against nova-config"). On 2026-10-24 widths changed in
// nova-config at 19:15Z were never applied, and five benches ran at width 8
// against a configured 4 for an hour: only a hand-run fleet sync applies the
// inventory, and nothing said it was owed. So the binding reads what a sync would
// sync to (the same read and the same plan as fleet sync --check, cmd/nova-sprint
// fleetsync.go), at most every few minutes and only when the config store
// answers, the tick plans the same FleetDrift on its first read, and this part
// raises one judgment per drift set naming each member and field and the one
// command that applies it. It applies nothing: the sync is the coordinator's. The
// episode machine is the pass's, tla/CoordinatorPass.tla, as the drift alarms' and
// the base CI judgment's are: no module of its own.
const NConfigDrift = "the fleet table differs from nova-config"

// ConfigDriftFacts is the tick's comparison of the fleet table with nova-config:
// the drift fleet sync --check reports (FleetDrift), planned on the tick's first
// read. Empty Drift is a read that found none, and closes the judgment.
type ConfigDriftFacts struct {
	Drift []Drift
}

// ConfigDriftLine is the judgment's text for a drift set: each member and field,
// and the command that applies them.
func ConfigDriftLine(drift []Drift) string {
	lines := make([]string, 0, len(drift))
	for _, d := range drift {
		lines = append(lines, d.Kind+": "+d.Line())
	}
	return fmt.Sprintf("the fleet table differs from nova-config (drift=%d), and nothing applies it until a sync runs: %s; run: nova-sprint fleet sync",
		len(drift), strings.Join(lines, "; "))
}

// TickConfigDrift is the tick's config-drift judgment, planned in the deadlines
// part on the facts the binding read (TickReq.ConfigDrift): one judgment per drift
// set, about the whole sprint as the drift alarms are (stream level, no stream: it
// names no card, so the check never retires it as an orphan), keyed by its type
// and its line, so a changed set closes the judgment of the old set and raises one
// of the new; closed when a read finds no drift. A tick that read nothing (nil: not due, or the config store did not
// answer) keeps an open judgment, and a wait on it, as they are: no read is never
// a clear.
func TickConfigDrift(s *Snapshot, r TickReq) (Plan, int) {
	var p Plan
	var conds []cond
	if f := r.ConfigDrift; f != nil {
		if len(f.Drift) > 0 {
			conds = append(conds, cond{typ: NConfigDrift, streamLevel: true, decisions: TickDecisions[NConfigDrift], what: ConfigDriftLine(f.Drift)})
		}
	} else {
		carry := func(o Open) {
			// held as it stands: the same key, so notify neither writes nor closes it
			conds = append(conds, cond{typ: NConfigDrift, streamLevel: true, what: o.Note.What})
		}
		for _, o := range s.Open {
			if o.Note.Kind == Judgment && o.Note.Type == NConfigDrift {
				carry(o)
			}
		}
		for _, o := range s.Acked {
			if !o.Note.Review.IsZero() && o.Note.Type == NConfigDrift {
				carry(o)
			}
		}
	}
	due := notify(&p, s, conds, []string{NConfigDrift}, r)
	return p, due
}

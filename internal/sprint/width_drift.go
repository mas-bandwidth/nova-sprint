package sprint

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// NWidthDrift is the tick's judgment that a fleet row's width disagrees with
// nova-config's: one judgment per drifting row, written once while the row
// drifts, closed when it agrees again (the read of nova-config is the fix,
// never a widening of it). tla/WidthHard.tla OneSource.
const NWidthDrift = "a row's width is not nova-config's"

// FieldWidthFrom marks a member's drain as nova-config's (WidthFromConfig): a
// clear carries it, so a drained row stays marked as following the config.
// tla/WidthHard.tla OneSource.
const (
	FieldWidthFrom  = "width_from"
	WidthFromConfig = "config"
)

// WidthDrift is one fleet row whose width disagrees with nova-config: a
// member's control-card width against its machine row's width (cfg), or a
// friend's seat width against her friend row's width (friendCfg). Sprint is
// the row's current width, Config nova-config's.
type WidthDrift struct {
	Row    string
	Sprint int
	Config int
}

// WidthDriftRows is every drifting fleet row: a member whose control-card
// width differs from its machine row's width, a friend whose seat width
// differs from her friend row's. The config maps may be nil (no read: never
// act on a missing read); a row the config does not name is not judged.
// tla/WidthHard.tla OneSource.
func WidthDriftRows(s *Snapshot, cfg, friendCfg map[string]int) []WidthDrift {
	if s == nil || s.Fleet == nil {
		return nil
	}
	var out []WidthDrift
	if len(cfg) > 0 {
		for _, m := range s.Members() {
			ctl := s.MemberCtl(m)
			if ctl == nil {
				continue
			}
			if n, ok := cfg[m]; ok {
				have := MemberWidth(ctl)
				if have == 0 && ctl.F(FieldWidthFrom) != WidthFromConfig {
					continue // the coordinator's own drain (fleet up --width 0)
				}
				if have != n {
					out = append(out, WidthDrift{Row: m, Sprint: have, Config: n})
				}
			}
		}
	}
	if len(friendCfg) > 0 {
		for _, f := range s.Friends {
			if n, ok := friendCfg[f.Name]; ok && n != f.Width {
				out = append(out, WidthDrift{Row: f.Name, Sprint: f.Width, Config: n})
			}
		}
	}
	slices.SortFunc(out, func(a, b WidthDrift) int { return cmpStr(a.Row, b.Row) })
	return out
}

// tla/WidthHard.tla OneSource.
func cmpStr(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Line is the one line naming one drifting row, both values.
// tla/WidthHard.tla OneSource.
func (d WidthDrift) Line() string {
	return fmt.Sprintf("width: %s: sprint width %d, nova-config width %d (the one width is nova-config's)", d.Row, d.Sprint, d.Config)
}

// widthCfgRead is the binding's read of nova-config's row widths for the tick:
// the members' widths and the friends' widths, and whether the read held. It
// is set by the store before a read (SetWidthConfigRead), so the deadlines
// part's width judgment reads the same nova-config the rest of the tick does.
var (
	widthCfgMu   sync.RWMutex
	widthCfgRead func(context.Context) (map[string]int, map[string]int, bool)
)

// SetWidthConfigRead sets the tick's read of nova-config's row widths
// (tla/WidthHard.tla OneSource). nil is no read: no width drift is judged.
func SetWidthConfigRead(fn func(context.Context) (map[string]int, map[string]int, bool)) {
	widthCfgMu.Lock()
	widthCfgRead = fn
	widthCfgMu.Unlock()
}

// tla/WidthHard.tla OneSource.
func widthConfig() (cfg, friendCfg map[string]int, ok bool) {
	widthCfgMu.RLock()
	fn := widthCfgRead
	widthCfgMu.RUnlock()
	if fn == nil {
		return nil, nil, false
	}
	return fn(context.Background())
}

// friendNamed says a friend's seat names this row.
func friendNamed(s *Snapshot, name string) bool {
	for _, f := range s.Friends {
		if f.Name == name {
			return true
		}
	}
	return false
}

// tickWidthDrift is the tick's width-drift judgment, run as a part of the
// tick's end (TickEnd): one judgment per drifting row, written once while it
// drifts (notify keys it by type and row, so the same row is one episode),
// closed when the row agrees again. A tick that read no config raises or
// closes nothing, and a side whose read is missing keeps its judgments open:
// never act on a missing read. tla/WidthHard.tla OneSource.
func tickWidthDrift(s *Snapshot, r TickReq) (Plan, int) {
	cfg, friendCfg, ok := widthConfig()
	if !ok {
		return Plan{}, 0
	}
	if cfg == nil && friendCfg == nil {
		return Plan{}, 0
	}
	var p Plan
	drifts := WidthDriftRows(s, cfg, friendCfg)
	conds := make([]cond, 0, len(drifts))
	for _, d := range drifts {
		conds = append(conds, cond{typ: NWidthDrift, stream: d.Row, streamLevel: true, what: d.Line()})
	}
	// a side the tick did not read keeps its open judgments: the row is not
	// said to agree again on a missing read
	for _, o := range s.Open {
		if o.Note.Type != NWidthDrift || o.Note.Kind != Judgment {
			continue
		}
		sub := o.Subject()
		if (cfg == nil && s.MemberCtl(sub) != nil) || (friendCfg == nil && friendNamed(s, sub)) {
			conds = append(conds, cond{typ: NWidthDrift, stream: sub, streamLevel: true, what: o.Note.What})
		}
	}
	due := notify(&p, s, conds, []string{NWidthDrift}, r)
	return p, due
}

// init wires the width-drift judgment into the tick's overdue part, in the
// same operation: a drift is written once per drift beside the overdue lines,
// and an idle tick costs no operation of its own (tla/WidthHard.tla OneSource).
func init() {
	wrap := func(orig TickPartFn) TickPartFn {
		return func(s *Snapshot, r TickReq) (Plan, int) {
			p, due := orig(s, r)
			w, wd := tickWidthDrift(s, r)
			p.Notes = append(p.Notes, w.Notes...)
			p.Closes = append(p.Closes, w.Closes...)
			p.Updates = append(p.Updates, w.Updates...)
			p.Units = append(p.Units, w.Units...)
			p.Rows = append(p.Rows, w.Rows...)
			p.Props = append(p.Props, w.Props...)
			if w.Stop != "" {
				p.Stop = w.Stop
			}
			return p, due + wd
		}
	}
	for i := range TickEnd {
		if TickEnd[i].Name == "overdue" && TickEnd[i].Fn != nil {
			TickEnd[i].Fn = wrap(TickEnd[i].Fn)
		}
	}
	for i := range TickParts {
		if TickParts[i].Name == "overdue" && TickParts[i].Fn != nil {
			TickParts[i].Fn = wrap(TickParts[i].Fn)
		}
	}
}

package sprint

import (
	"fmt"
	"slices"
)

// NWidthDrift is the tick's judgment that a fleet row's width disagrees with nova-config's: one
// judgment per drifting row, written once while the row drifts, closed when it agrees again (the
// read of nova-config is the fix, never a widening of it). tla/WidthHard.tla OneSource.
const NWidthDrift = "a row's width is not nova-config's"

// The one width is nova-config's (the owner, 2026-10-18: "every fleet machine and every
// friend follows the one width definition in nova-config, with no exceptions"; tla/WidthHard.tla
// OneSource). A row whose width differs is a drift: held at the take (HoldWidths) but loud here,
// in check, the doctor and the tick. A drift is never fixed by widening nova-config: the read of
// nova-config is the fix (fleet sync / friend sync, or the server's steps, which write nova-config's
// width back as they run).

// WidthDrift is one fleet row whose width disagrees with nova-config: a member's control-card copy
// against its machine row's width (cfg), or a friend's seat width against her friend row's width
// (friendCfg). Sprint is the row's current width, Config nova-config's.
type WidthDrift struct {
	Row    string
	Sprint int
	Config int
}

// WidthDriftRows is every drifting fleet row: a member whose control-card copy (MemberWidth)
// differs from its machine row's width, a friend whose seat width differs from her friend row's.
// The config maps may be nil (no read: never act on a missing read); a row the config does not
// name is not judged.
// tla/WidthHard.tla (OneSource)
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
					continue // the coordinator's own drain (fleet up --width 0; tla/WidthHard.tla (OneSource))
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

// tla/WidthHard.tla (OneSource)
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
// tla/WidthHard.tla (OneSource)
func (d WidthDrift) Line() string {
	return fmt.Sprintf("width: %s: sprint width %d, nova-config width %d (the one width is nova-config's)", d.Row, d.Sprint, d.Config)
}

// tickWidthDrift is the tick's width-drift judgment, planned in the deadlines part the way the
// drift alarms are (TickDeadlines): one judgment per drifting row, written once while it drifts
// (notify keys it by type and row, so the same row is one episode), closed when the row agrees
// again. A tick that read no config (Widths and FriendWidths both nil) raises or closes nothing:
// never act on a missing read.
// tla/WidthHard.tla (OneSource)
func tickWidthDrift(s *Snapshot, r TickReq) (Plan, int) {
	if r.Widths == nil && r.FriendWidths == nil {
		return Plan{}, 0
	}
	var p Plan
	drifts := WidthDriftRows(s, r.Widths, r.FriendWidths)
	conds := make([]cond, 0, len(drifts))
	for _, d := range drifts {
		conds = append(conds, cond{typ: NWidthDrift, stream: d.Row, streamLevel: true, what: d.Line()})
	}
	due := notify(&p, s, conds, []string{NWidthDrift}, r)
	return p, due
}

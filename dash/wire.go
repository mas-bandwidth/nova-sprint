package dash

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

func integer(n int) string { return strconv.Itoa(n) }
func moneyCell(cents *big.Int) string {
	if cents == nil {
		return "-"
	}
	dollars, fraction := new(big.Int), new(big.Int)
	dollars.QuoRem(cents, big.NewInt(100), fraction)
	digits := dollars.String()
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return fmt.Sprintf("$%s.%02d", digits, fraction.Int64())
}
func (w workRow) MarshalJSON() ([]byte, error) {
	tiers := map[string]string{}
	for k, v := range w.ByTier {
		tiers[k] = moneyCell(v)
	}
	m := map[string]any{"blocked": integer(w.Blocked), "waiting": integer(w.Waiting), "ready": integer(w.Ready), "working": integer(w.Working), "review": integer(w.Review), "fix": integer(w.Fix), "merging": integer(w.Merging), "landed": integer(w.Landed), "fail": integer(w.Fail), "cost": moneyCell(w.Cost), "cost_by_tier": tiers}
	if len(w.BlockedCards) > 0 {
		m["blocked_cards"] = w.BlockedCards
	}
	return json.Marshal(m)
}
func (f fleetRow) MarshalJSON() ([]byte, error) {
	pct := fmt.Sprintf("%.1f%%", f.OKPct)
	m := map[string]string{"status": f.Status, "width": integer(f.Width), "ready": integer(f.Ready), "working": integer(f.Working), "done": integer(f.Done), "blocker_working": integer(f.Blocker), "critical_working": integer(f.Critical), "fix_working": integer(f.Fix), "reads_working": integer(f.Reads), "ok": integer(f.OK), "failed": integer(f.Failed), "okpct": pct}
	if f.Reason != "" {
		m["reason"] = f.Reason
	}
	return json.Marshal(m)
}

type numberCells map[string]int

func (m numberCells) MarshalJSON() ([]byte, error) {
	v := map[string]string{}
	for k, n := range m {
		v[k] = integer(n)
	}
	return json.Marshal(v)
}

// streamCost accumulates exact micro-dollars; v1 sends currency strings and a numeric count.
type streamCost struct {
	amounts  map[string]*big.Int
	unpriced int
}

func (s *streamCost) add(key string, micros int64) {
	if s.amounts[key] == nil {
		s.amounts[key] = new(big.Int)
	}
	s.amounts[key].Add(s.amounts[key], big.NewInt(micros))
}
func (s *streamCost) MarshalJSON() ([]byte, error) {
	out := map[string]any{}
	if s.unpriced > 0 {
		out["unpriced_runs"] = s.unpriced
	}
	parts := map[string]*big.Int{}
	for _, key := range []string{"work_cost", "read_cost"} {
		if micros, ok := s.amounts[key]; ok {
			parts[key] = micros
		}
	}
	_, split := partitionCents(parts)
	for key, micros := range s.amounts {
		cents := split[key]
		if cents == nil {
			cents = new(big.Int).Add(micros, big.NewInt(9999))
			cents.Div(cents, big.NewInt(10000))
		}
		out[key] = moneyCell(cents)
	}
	return json.Marshal(out)
}

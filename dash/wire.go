package dash

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func integer(n int) string { return strconv.Itoa(n) }
func moneyCell(n *float64) string {
	if n == nil {
		return "-"
	}
	raw := fmt.Sprintf("%.2f", *n)
	parts := strings.SplitN(raw, ".", 2)
	digits := parts[0]
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return "$" + digits + "." + parts[1]
}
func (w workRow) MarshalJSON() ([]byte, error) {
	tiers := map[string]string{}
	for k, v := range w.ByTier {
		x := v
		tiers[k] = moneyCell(&x)
	}
	return json.Marshal(map[string]any{"blocked": integer(w.Blocked), "waiting": integer(w.Waiting), "ready": integer(w.Ready), "working": integer(w.Working), "review": integer(w.Review), "fix": integer(w.Fix), "merging": integer(w.Merging), "landed": integer(w.Landed), "fail": integer(w.Fail), "cost": moneyCell(w.Cost), "cost_by_tier": tiers})
}
func (f fleetRow) MarshalJSON() ([]byte, error) {
	pct := "-"
	if f.OKPct != nil {
		pct = fmt.Sprintf("%.1f%%", *f.OKPct)
	}
	return json.Marshal(map[string]string{"status": f.Status, "width": integer(f.Width), "ready": integer(f.Ready), "working": integer(f.Working), "done": integer(f.Done), "blocker_working": integer(f.Blocker), "critical_working": integer(f.Critical), "fix_working": integer(f.Fix), "reads_working": integer(f.Reads), "ok": integer(f.OK), "failed": integer(f.Failed), "okpct": pct})
}

type numberCells map[string]int

func (m numberCells) MarshalJSON() ([]byte, error) {
	v := map[string]string{}
	for k, n := range m {
		v[k] = integer(n)
	}
	return json.Marshal(v)
}

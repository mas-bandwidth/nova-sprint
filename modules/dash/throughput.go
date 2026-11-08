package dash

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

func round1(n float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(n, 'f', 1, 64), 64)
	return v
}

// rateWindow is the throughput tile's rate, the minutes it was measured over
// and the landings inside them, from the store's landings; 0 for none.
func (c *Cache) rateWindow() (float64, float64, int) {
	if c.landingsOK {
		if r, w, n, _ := landingRate(c.landings, c.clock()); r > 0 {
			return r, w, n
		}
	}
	return 0, 0, 0
}

// MinEtaLandings is the fewest landings in the rate window an ETA is given
// from (v1 asked five landed in the epoch; v2 measures a one-hour window, so
// three in it is the floor).
const MinEtaLandings = 3

// etaMinutes is v1's estimate: the cards left at rate cards an hour, in whole
// minutes rounded up; 0 for none (no rate, too few landings, nothing left).
func etaMinutes(left int, rate float64, landings int) int64 {
	if rate <= 0 || landings < MinEtaLandings || left <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(left) * 60 / rate))
}

// etaString is v1's form: "47m", "1h12m", and from a day on "52d2h" (hours
// rounded up); "-" for no estimate.
func etaString(m int64) string {
	switch {
	case m >= 24*60:
		h := (m + 59) / 60
		return fmt.Sprintf("%dd%dh", h/24, h%24)
	case m >= 60:
		return fmt.Sprintf("%dh%dm", m/60, m%60)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	}
	return "-"
}

// landingRate is the rate the ETA (and the throughput tile) read, from the
// epoch's landing times: the landings in the last hour when there are at least
// MinEtaLandings of them; else the sprint's average (landings over the time
// since the first, at least ten minutes) when there are that many in all.
// window is the minutes the rate was measured over, n the landings counted,
// average whether it is the sprint's average; rate 0 is no rate.
func landingRate(landings []time.Time, now time.Time) (rate, window float64, n int, average bool) {
	first := now
	for _, at := range landings {
		if at.After(now) {
			continue
		}
		if now.Sub(at) < time.Hour {
			n++
		}
		if at.Before(first) {
			first = at
		}
	}
	if n >= MinEtaLandings {
		return float64(n), 60, n, false
	}
	total := 0
	for _, at := range landings {
		if !at.After(now) {
			total++
		}
	}
	if total >= MinEtaLandings {
		span := max(now.Sub(first), 10*time.Minute)
		return round1(float64(total) / span.Hours()), round1(span.Minutes()), total, true
	}
	return 0, 60, n, false
}

// summaryLine is v1's headline, which the page reads the ETA from.
func summaryLine(d envelope, eta string) string {
	pct := "0.0%"
	if d.All > 0 {
		pct = strconv.FormatFloat(100*float64(d.Landed)/float64(d.All), 'f', 1, 64) + "%"
	}
	line := fmt.Sprintf("%d/%d %s", d.Landed, d.All, pct)
	if d.Done {
		return line + " done"
	}
	return line + " -> ETA " + eta
}

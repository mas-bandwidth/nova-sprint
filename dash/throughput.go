package dash

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

type sample struct {
	at     time.Time
	landed int
}

func (c *Cache) addSample(at time.Time, landed int) {
	if len(c.samples) > 0 && landed < c.samples[len(c.samples)-1].landed {
		c.samples = nil
	}
	c.samples = append(c.samples, sample{at, landed})
	i := 0
	for i < len(c.samples) && at.Sub(c.samples[i].at) > time.Hour {
		i++
	}
	c.samples = append([]sample(nil), c.samples[i:]...)
}
func round1(n float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(n, 'f', 1, 64), 64)
	return v
}

// rate matches v1 sampling, with zero during the warmup.
func (c *Cache) rate() (float64, float64) {
	r, m, _ := c.rateWindow()
	return r, m
}

// rateWindow is rate plus the landings inside the window (0 in the warmup).
func (c *Cache) rateWindow() (float64, float64, int) {
	if len(c.samples) < 2 {
		return 0, 0, 0
	}
	a, b := c.samples[0], c.samples[len(c.samples)-1]
	span := b.at.Sub(a.at)
	minutes := round1(span.Minutes())
	if span < 10*time.Minute {
		return 0, minutes, 0
	}
	return round1(float64(b.landed-a.landed) * 3600 / span.Seconds()), minutes, b.landed - a.landed
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

package dash

import (
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
	if len(c.samples) < 2 {
		return 0, 0
	}
	a, b := c.samples[0], c.samples[len(c.samples)-1]
	span := b.at.Sub(a.at)
	minutes := round1(span.Minutes())
	if span < 10*time.Minute {
		return 0, minutes
	}
	return round1(float64(b.landed-a.landed) * 3600 / span.Seconds()), minutes
}

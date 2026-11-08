package dash

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestThroughputWindow(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &Cache{}
	c.addSample(base, 0)
	c.addSample(base.Add(9*time.Minute), 5)
	if rate, _ := c.rate(); rate != 0 {
		t.Fatalf("warmup rate=%v", rate)
	}
	c.addSample(base.Add(10*time.Minute), 5)
	if rate, minutes := c.rate(); rate != 30 || minutes != 10 {
		t.Fatalf("rate=%v minutes=%v", rate, minutes)
	}
	c.addSample(base.Add(71*time.Minute), 5)
	if rate, minutes := c.rate(); rate != 0 || minutes != 0 {
		t.Fatalf("trim rate=%v minutes=%v", rate, minutes)
	}
	c.addSample(base.Add(72*time.Minute), 1)
	if len(c.samples) != 1 {
		t.Fatal("count drop did not reset")
	}
	if rate, _ := c.rate(); rate != 0 {
		t.Fatalf("reset rate=%v", rate)
	}
}
func TestThroughputZeroAndRounding(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &Cache{}
	c.addSample(base, 4)
	c.addSample(base.Add(11*time.Minute), 4)
	if r, m := c.rate(); r != 0 || m != 11 {
		t.Fatalf("zero rate=%v minutes=%v", r, m)
	}
	c.addSample(base.Add(12*time.Minute+17*time.Second), 5)
	if r, m := c.rate(); r != 4.9 || m != 12.3 {
		t.Fatalf("rounded rate=%v minutes=%v", r, m)
	}
}
func TestHealthGraceClock(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	c := &Cache{at: base, now: func() time.Time { return now }}
	check := func() int {
		r := httptest.NewRecorder()
		Handler(c).ServeHTTP(r, httptest.NewRequest("GET", "/healthz", nil))
		return r.Code
	}
	now = base.Add(31 * time.Second)
	if check() != 200 {
		t.Fatal("health failed within stale grace")
	}
	c.checkAlarm(now)
	if c.alarm {
		t.Fatal("early alarm")
	}
	now = base.Add(32 * time.Second)
	if check() != 503 {
		t.Fatal("health did not fail after stale grace")
	}
	c.checkAlarm(now)
	if !c.alarm {
		t.Fatal("stale alarm missing")
	}
}

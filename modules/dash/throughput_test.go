package dash

import (
	"net/http/httptest"
	"testing"
	"time"
)

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

package worker

import (
	"errors"
	"testing"
)

func TestOpenCodeUsageFromItsOwnRecord(t *testing.T) {
	old := opencodeSpend
	defer func() { opencodeSpend = old }()
	h := &OpenCodeHarness{}
	opencodeSpend = func(db string) (float64, error) { return 0.123457, nil }
	if c, ok := h.Usage(Job{JobDir: t.TempDir()}); !ok || c.MicroUSD != 123457 || !c.Priced {
		t.Fatalf("priced run: %+v %v", c, ok)
	}
	opencodeSpend = func(db string) (float64, error) { return 0, nil }
	if c, ok := h.Usage(Job{JobDir: t.TempDir()}); ok {
		t.Fatalf("a route with no price is unpriced, not a priced zero: %+v", c)
	}
	opencodeSpend = func(db string) (float64, error) { return 0, errors.New("no sqlite3") }
	if _, ok := h.Usage(Job{JobDir: t.TempDir()}); ok {
		t.Fatal("an unreadable record is unpriced")
	}
}

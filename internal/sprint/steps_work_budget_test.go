package sprint

import (
	"testing"
	"time"
)

func snap1() *Snapshot {
	return &Snapshot{Now: time.Now(), Routes: []Route{
		{Name: "r1", Tier: "flash", Provider: "testp", Model: "testp/m1", Enabled: true},
		{Name: "r2", Tier: "flash", Provider: "testp", Model: "testp/m2", Enabled: true},
		{Name: "r3", Tier: "flash", Provider: "testp", Model: "testp/m3", Enabled: true},
	}}
}

// TestProviderConcurrencyBudget checks that the providerInFlightCount correctly counts
// takes in flight for a provider.
func TestProviderConcurrencyBudget(t *testing.T) {
	t.Parallel()
	// Setup: a provider "testp" with concurrency budget 2
	fleet := NewTable(Fleet)
	fleet.SetRows([]string{"m1", "m2", "m3"})
	fleet.SetProps(map[string]string{
		PropProviderConcurrency("testp"): "2",
	})
	// Set up fleet member control cards with status "up" in their Fields
	for _, m := range []string{"m1", "m2", "m3"} {
		ctrl := &Card{ID: CtlID(m), Row: m, Col: Ready, Score: 0, Fields: map[string]string{"kind": "member", "status": "up"}}
		fleet.Put(ctrl)
	}
	// Three ready cards on different members, all on testp provider
	book1 := &Card{ID: "c1", Row: "m1", Col: Ready, Score: 1.0, Fields: map[string]string{
		"kind": "work", "stream": "stream1", "member": "m1", "route": "r1", "model": "testp/m1", "gen": "1",
	}}
	book2 := &Card{ID: "c2", Row: "m2", Col: Ready, Score: 2.0, Fields: map[string]string{
		"kind": "work", "stream": "stream2", "member": "m2", "route": "r2", "model": "testp/m2", "gen": "1",
	}}
	book3 := &Card{ID: "c3", Row: "m3", Col: Ready, Score: 3.0, Fields: map[string]string{
		"kind": "work", "stream": "stream3", "member": "m3", "route": "r3", "model": "testp/m3", "gen": "1",
	}}
	fleet.Put(book1)
	fleet.Put(book2)
	fleet.Put(book3)
	s := snap1()
	s.Fleet = fleet
	// providerInFlightCount should return 0 initially
	if got := providerInFlightCount(s, "testp"); got != 0 {
		t.Fatalf("providerInFlightCount = %d, want 0", got)
	}
	// Set c1 and c2 to Working (simulating takes)
	c1 := fleet.Card("c1")
	c1.Col = Working
	fleet.Put(c1)
	c2 := fleet.Card("c2")
	c2.Col = Working
	fleet.Put(c2)
	// providerInFlightCount should return 2
	if got := providerInFlightCount(s, "testp"); got != 2 {
		t.Fatalf("providerInFlightCount = %d, want 2", got)
	}
	// providerConcurrencyBudget should return 2
	budget, ok := providerConcurrencyBudget(s, "testp")
	if !ok || budget != 2 {
		t.Fatalf("providerConcurrencyBudget = %d, %v, want 2, true", budget, ok)
	}
}

// TestProviderConcurrencyProperty tests that PropProviderConcurrency returns the expected key.
func TestProviderConcurrencyProperty(t *testing.T) {
	t.Parallel()
	if got := PropProviderConcurrency("testp"); got != "provider_concurrency_testp" {
		t.Fatalf("PropProviderConcurrency(\"testp\") = %s, want provider_concurrency_testp", got)
	}
}

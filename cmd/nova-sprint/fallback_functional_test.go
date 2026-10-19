//go:build functional

package main

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fallbackCardView struct {
	Primary *sprint.Card   `json:"primary"`
	Work    []*sprint.Card `json:"work_cards"`
}

// TestProviderFallbackFunctional: three providers, A first, and two fake members.
// Every take on A comes back class=rate-limited: A cools and each card falls back
// at once to B, at the same attempt and generation, spending no attempt or redeal;
// the reads that follow are asked on B, never on cooled A, and nothing stalls
// (tla/ProviderFallback.tla, Fallback and NoDealToCooling).
func TestProviderFallbackFunctional(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "pa", Model: "a", Enabled: true, First: true},
		{Name: "flash-b", Tier: "flash", Provider: "pb", Model: "b", Enabled: true},
		{Name: "flash-c", Tier: "flash", Provider: "pc", Model: "c", Enabled: true},
	})
	ta.m.SetProviders(map[string]sprint.Provider{
		"pa": {Name: "pa", Billing: "plan", Funded: true},
		"pb": {Name: "pb", Billing: "plan", Funded: true},
		"pc": {Name: "pc", Billing: "plan", Funded: true},
	})
	ta.ok("init --readers reader-a,reader-b --members m1,m2")
	ta.ok("add --stream s1 --count 2")
	ta.ok("start")
	ta.ok("tick") // the members are up and the two cards dealt

	type q struct {
		Epoch uint64 `json:"epoch"`
		Cards []struct {
			ID      string `json:"id"`
			Primary string `json:"primary"`
			Attempt int    `json:"attempt"`
			Gen     int    `json:"gen"`
		} `json:"cards"`
	}
	var cards []struct {
		ID, Primary  string
		Attempt, Gen int
	}
	var epoch string
	for _, m := range []string{"m1", "m2"} {
		var got q
		require.NoError(t, json.Unmarshal([]byte(ta.ok("queue --as "+m+" --json")), &got), m)
		epoch = strconv.FormatUint(got.Epoch, 10)
		for _, c := range got.Cards {
			cards = append(cards, struct {
				ID, Primary  string
				Attempt, Gen int
			}{c.ID, c.Primary, c.Attempt, c.Gen})
		}
	}
	require.Len(t, cards, 2, "two cards dealt")
	for _, m := range []string{"m1", "m2"} {
		ta.ok("take --as " + m + " --limit 100")
	}
	for _, c := range cards {
		var v fallbackCardView
		require.NoError(t, json.Unmarshal([]byte(ta.ok("card "+c.Primary+" --json")), &v), c.Primary)
		require.NotEmpty(t, v.Work)
		assert.Equal(t, "flash-a", v.Work[0].F(sprint.FieldRoute), "%s is on A first", c.ID)
	}
	const report = "provider failure: provider: class=rate-limited status=429 msg=Too many requests"
	for _, c := range cards {
		member := ""
		if c.Primary == "s1-1" {
			member = "m1"
		} else {
			member = "m2"
		}
		ta.ok("finish --as " + member + " " + c.ID + "@" + strconv.Itoa(c.Gen) + " --epoch " + epoch + " --failed --report '" + report + "'")
	}
	ta.ok("tick")
	for _, c := range cards {
		var v fallbackCardView
		require.NoError(t, json.Unmarshal([]byte(ta.ok("card "+c.Primary+" --json")), &v), c.Primary)
		require.NotEmpty(t, v.Work)
		assert.Equal(t, "flash-b", v.Work[0].F(sprint.FieldRoute), "%s fell back to B", c.ID)
		assert.Equal(t, strconv.Itoa(c.Attempt), v.Work[0].F("attempt"), "the same attempt")
		assert.Contains(t, []string{"", "0"}, v.Work[0].F("redeals"), "no redeal spent")
		assert.Equal(t, sprint.Working, v.Primary.Col, "the card is working, never stalled")
	}
	// the work on B is taken and ends ok; the reads that follow are asked on B,
	// never on cooled A
	for _, m := range []string{"m1", "m2"} {
		ta.ok("take --as " + m + " --limit 100")
	}
	for _, c := range cards {
		var v fallbackCardView
		require.NoError(t, json.Unmarshal([]byte(ta.ok("card "+c.Primary+" --json")), &v), c.Primary)
		ta.ok("finish --as " + v.Work[0].Row + " " + c.ID + "@" + v.Work[0].F("gen") + " --epoch " + epoch + " --head " + c.ID)
	}
	ta.ok("tick")
	ta.ok("tick")
	for _, c := range cards {
		var v fallbackCardView
		require.NoError(t, json.Unmarshal([]byte(ta.ok("card "+c.Primary+" --json")), &v), c.Primary)
		for _, rc := range v.Work {
			if rc.F("kind") == "read" {
				assert.NotEqual(t, "flash-a", rc.F(sprint.FieldRoute), "a read is never asked on cooled A")
			}
		}
		assert.Equal(t, 1, v.Primary.Int("attempt"), "no attempt spent")
		assert.NotEqual(t, sprint.Waiting, v.Primary.Col, "no card stalls")
	}
}

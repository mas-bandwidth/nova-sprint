package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestProviderFallbackReadCardFinishesOnTheNextRoute is the read-cards variant of the
// provider fallback (docs/SPEC-SPRINT.md section 5, provider fallback;
// tla/ProviderFallback.tla Fallback, FaultSpendsNothing): a member's read card whose
// provider answers 429 is asked again at once on the next route in line, the same read
// card and the same reader, with no re-ask counted and no reader spent, and it finishes
// there. It runs the store's own read step on the in-memory store (ReadStep loads the
// routes on a return), so it covers the real-store path behind the Ultramarine stall
// without redis.
func TestProviderFallbackReadCardFinishesOnTheNextRoute(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-m1,reader-m2 --members m1:8,m2:8")
	ta.ok("set --read-cards on")
	ta.ok("reader set reader-m2 --tiers all")
	ta.m.SetRoutes([]sprint.Route{
		{Name: "flash-a", Tier: "flash", Provider: "pa", Model: "a", Enabled: true, Deadline: 600},
		{Name: "flash-b", Tier: "flash", Provider: "pb", Model: "b", Enabled: true, Deadline: 600},
		{Name: "flash-c", Tier: "flash", Provider: "pc", Model: "c", Enabled: true, Deadline: 600},
	})
	ta.m.SetTiers(map[string][]string{"flash": {"flash-a", "flash-b", "flash-c"}})
	ta.m.SetProviders(map[string]sprint.Provider{
		"pa": {Name: "pa", Funded: true}, "pb": {Name: "pb", Funded: true}, "pc": {Name: "pc", Funded: true},
	})
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: the flash card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n\nthe flag is reachable from the verb\n")), 0o644))
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	ta.ok("take --as m1 --limit 1")
	var card cardView
	ta.json("card s1-1", &card)
	require.Len(t, card.Work, 1)
	wc := card.Work[0]
	ta.ok("finish --as m1 " + wc.ID + "@" + wc.F("gen") + " --report done --head " + landHead)
	for i := 0; i < 4 && len(card.Reads) == 0; i++ {
		ta.ok("tick")
		ta.json("card s1-1", &card)
	}
	require.Len(t, card.Reads, 1, "one read card dealt")
	rc := card.Reads[0]
	first := rc.F(sprint.FieldRoute)
	require.NotEmpty(t, first, "a read card on a route")

	ta.ok("take --as " + rc.Row + " --limit 1")
	ta.ok("read --as " + rc.Row + " --return " + rc.ID + " --reason 'no verdict: provider: class=rate-limited status=429 msg=Too many requests' --epoch 0")

	ta.json("card s1-1", &card)
	require.Len(t, card.Reads, 1)
	rc = card.Reads[0]
	assert.Equal(t, sprint.Working, rc.Col, "the same read card, ready to finish")
	assert.NotEqual(t, first, rc.F(sprint.FieldRoute), "re-asked on the next route in line")
	assert.Empty(t, rc.F(sprint.FieldReasked), "no re-ask counted: the reader is not spent")
	assert.Empty(t, rc.F(sprint.FieldReturned), "not a returned read")

	ta.ok("read --as " + rc.Row + " --ok " + rc.ID + " --epoch 0 --usage 'input=1000 cache_read=0 cache_write=0 output=100 model=" + rc.F(sprint.FieldModel) + "'")
	ta.json("card s1-1", &card)
	require.Len(t, card.Reads, 1)
	assert.Equal(t, "ok", card.Reads[0].F("verdict"), "the read finishes on the next provider")
	assert.NotEqual(t, first, card.Reads[0].F(sprint.FieldRoute))
	assert.Empty(t, card.Reads[0].F(sprint.FieldReasked), "the reader was never spent")
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// TestReadCardsDefaultOnFirstInstallAndAfterClear pins the owner's cases on the twin
// store (docs/SPEC-SPRINT.md section 11, set --read-cards): a first install asks every
// read a card in review needs at once, as read cards on the fleet table, never one per
// tick through the readers table; the same after a clear; and a store whose read-cards
// property is absent behaves as on.
func TestReadCardsDefaultOnFirstInstallAndAfterClear(t *testing.T) {
	t.Parallel()
	run := func() {
		ta := newTestApp(t)
		ta.defaultReadCardsOff = false
		ta.live = []string{"m1", "m2", "m3"}
		ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "s1-1.md"), []byte(passingBrief("s1-1: work (s1) tier: pro\n")), 0o644))
		ta.ok("add --stream s1 --brief-dir " + dir)
		ta.ok("start")
		ta.ok("tick")
		ta.ok("take --as m1 --limit 1")
		ta.ok("finish --as m1 s1-1.w1@1 --report done --head " + landHead)
		ta.ok("tick")

		var card cardView
		ta.json("card s1-1", &card)
		require.Len(t, card.Reads, 2, "a pro card's two reads are asked at once, never one per tick")
		for _, rc := range card.Reads {
			require.Equal(t, "1", rc.F(sprint.FieldReadCard), "the read is a read card, not a readers-table read")
			require.NotEqual(t, "m1", rc.Row, "never the worker")
		}
	}
	t.Run("first install", func(t *testing.T) { t.Parallel(); run() })
	t.Run("after clear", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.defaultReadCardsOff = false
		ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
		// change every setting, then clear: a clear starts the next epoch with none of them
		ta.ok("set --read-cards off --read-tier pro --dealt-max 9h --go-lanes 4 --fleet-tiers pro --friends-tiers pro")
		ta.ok("clear --confirm sprint")
		st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
		require.NoError(t, err)
		s, err := st.Load(context.Background(), store.All, nil)
		require.NoError(t, err)
		require.True(t, s.ReadCardsOn(), "after a clear the read-cards property is absent, and read cards default on")
	})
}

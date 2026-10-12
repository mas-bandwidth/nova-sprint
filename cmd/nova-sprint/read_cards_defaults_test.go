package main

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/stretchr/testify/require"
)

// A fresh or existing propertyless store asks every required read as a card.
// Init preserves an explicit override; default and clear restore the working
// default, while off retains the supported readers-table flow.
func TestReadCardsDefaultUsesTheActualCommandAndStoreLifecycle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		settings []string
		prop     string
		hadProp  bool
		existing bool
		off      bool
	}{
		{name: "fresh propertyless"},
		{name: "existing propertyless after init", existing: true},
		{name: "explicit on after init", settings: []string{"set --read-cards on", "init"}, prop: "on", hadProp: true},
		{name: "explicit off after init", settings: []string{"set --read-cards off", "init"}, prop: "off", hadProp: true, off: true},
		{name: "off reset to default", settings: []string{"set --read-cards off", "set --read-cards default"}, prop: "default", hadProp: true},
		{name: "off then clear", settings: []string{"set --read-cards off", "clear --confirm sprint"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ta := newTestApp(t) // no legacy opt-in: keep the actual store default
			ta.live = []string{"m1", "m2", "m3"}
			ta.ok("init --readers reader-m1,reader-m2,reader-m3 --members m1:2,m2:2,m3:2")
			if tc.existing {
				ta.ok("add --stream warm --count 1 --one")
				ta.ok("drop warm-1 --reason prior-store-history")
				ta.ok("init")
			}
			for _, line := range tc.settings {
				ta.ok(line)
			}
			st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
			require.NoError(t, err)
			before, err := st.Load(context.Background(), store.All, nil)
			require.NoError(t, err)
			prop, had := before.Work.Prop(sprint.PropReadCards)
			require.Equal(t, tc.hadProp, had, "inspect the stored property, not only a computed default")
			require.Equal(t, tc.prop, prop)
			if tc.existing {
				require.True(t, before.Work.HasRow("warm"), "repeated init retains the pre-existing work stream")
			}

			ta.ok("add --stream s1 --count 1 --one --brief-file " + proBriefFile(t))
			ta.ok("start")
			ta.ok("tick")
			ta.ok("tick")
			ta.ok("take --as m1 --limit 1")
			ta.ok("finish --as m1 s1-1.w1@1 --report done")
			ta.ok("tick")
			var card cardView
			ta.json("card s1-1", &card)
			for _, rd := range card.Reads {
				t.Logf("read=%s row=%s col=%s read_card=%q", rd.ID, rd.Row, rd.Col, rd.F(sprint.FieldReadCard))
			}
			after, err := st.Load(context.Background(), store.All, nil)
			require.NoError(t, err)
			if tc.off {
				require.NotEmpty(t, after.Readers.Column(sprint.Asked, sprint.Reading), "explicit off keeps the legacy producer")
				require.NotEmpty(t, card.Reads)
				for _, rd := range card.Reads {
					require.Empty(t, rd.F(sprint.FieldReadCard), "an explicit off never creates read-card-mode reads")
				}
			} else {
				require.Len(t, card.Reads, 2, "the pro card's two reads are dealt in the same tick")
				for _, rd := range card.Reads {
					require.Equal(t, "1", rd.F(sprint.FieldReadCard), "each read uses the default card producer")
				}
				require.Empty(t, after.Readers.Column(sprint.Asked, sprint.Reading), "the legacy producer asks no new reads")
			}
			ta.clean()
		})
	}
}

// Help and the reset result state the same default the next tick uses.
func TestReadCardsHelpAndDefaultResetSayOn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	help := ta.ok("set --help")
	require.Contains(t, help, "on or default")
	require.NotContains(t, help, "off or default")
	ta.ok("init")
	ta.ok("set --read-cards off")
	require.Contains(t, ta.ok("set --read-cards default"), "read-cards default (on:")
	code, _, errs := ta.do("set --read-cards invalid")
	require.Equal(t, 1, code)
	require.Contains(t, errs, "on, off or default (on)")
}

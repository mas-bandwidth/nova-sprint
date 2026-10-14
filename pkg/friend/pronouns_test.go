package friend

import (
	"context"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// Exercise the present status message on the actual daemon-to-seat bus path.
func TestPresentReportsTheConfiguredFriendPronounsOnTheBus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		set  Pronouns
		want string
	}{
		{SheHer, "her"}, {HeHim, "his"}, {TheyThem, "their"}, {"", "their"},
	} {
		t.Run(string(tc.set), func(t *testing.T) {
			now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
			store := bustest.NewFake(now, "pat", "seat")
			b := &bus.Bus{Store: store}
			d := &Daemon{Friend: "pat", Coordinator: "seat", Pro: NewPronounsRef(tc.set), m: &Machine{}, Record: func(string) {}}
			l := &loop{ctx: context.Background(), d: d, b: b}
			l.tellSuperseded(PresentPlan{Skipped: Skipped{Notes: 1}}, nil, "startup", now, now)
			entries, err := b.Log(context.Background(), "")
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Contains(t, entries[0].Message().Body, "Every older message on "+tc.want+" stream was marked superseded unread")
			assert.NotContains(t, entries[0].Message().Body, "{their}")
		})
	}
}

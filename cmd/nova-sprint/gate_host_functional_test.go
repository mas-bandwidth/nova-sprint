//go:build functional

package main

import (
	"context"
	"strconv"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/config"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// setMemberFacts writes each member's gate facts onto its control card in one step, the
// way the `fleet up --facts` verb (which lands beside this change) will record them. The
// deal rule reads FieldFacts, whatever writes it.
func setMemberFacts(t *testing.T, st *store.Store, facts map[string]string) {
	t.Helper()
	_, err := st.Run(context.Background(), store.Step{
		Verb: "fleet up --facts",
		Load: []string{sprint.Fleet},
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			var units []sprint.Unit
			for member, fs := range facts {
				c := s.MemberCtl(member)
				if c == nil {
					continue
				}
				units = append(units, sprint.Unit{
					Key: c.ID,
					Changes: []sprint.Change{{Table: sprint.Fleet, Entry: ntable.BatchMemberEntry{
						ID:     c.ID,
						Expect: &ntable.MemberExpect{Revision: strconv.FormatUint(c.Rev, 10), Place: &ntable.PlaceExpect{Row: c.Row, Col: c.Col}},
						Set:    map[string]string{sprint.FieldFacts: fs},
					}}},
				})
			}
			return sprint.Plan{Units: units}
		},
	})
	require.NoError(t, err)
}

// A GATE-NEEDS card is admitted on a real server and store and the tick's gate-host deal
// rule reads it: admit a GATE-NEEDS linux card, and once the two fake members carry their
// facts the tick deals it only to the linux member; an unsatisfiable card raises one
// judgment naming the need.
func TestAGateNeedsCardIsDealtOnlyToAMemberWhoseFactsMeetIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	addr := testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(ctx, admin))
	for _, x := range [][2]string{{"flash-a", "flash"}} {
		require.NoError(t, admin.SAdd(ctx, config.RoutesKey, x[0]).Err())
		require.NoError(t, admin.HSet(ctx, config.RouteKey(x[0]), "name", x[0], "tier", x[1], "provider", "p-"+x[0], "model", "m",
			"tokens", "100000", "deadline", "900", "enabled", "true").Err())
	}
	ta := newFunctionalTestApp(t, addr)
	ta.runVerb("init", "--members", "m-linux:2,m-darwin:2")
	ta.runVerb("fleet", "beat", "m-linux", "--load", "0")
	ta.runVerb("fleet", "beat", "m-darwin", "--load", "0")
	ta.runVerb("fleet", "up", "m-linux", "--width", "2")
	ta.runVerb("fleet", "up", "m-darwin", "--width", "2")

	st, err := ta.a.store(common{redis: addr, actor: "coordinator"})
	require.NoError(t, err)
	setMemberFacts(t, st, map[string]string{"m-linux": "linux,go", "m-darwin": "darwin,go"})

	linuxBrief := passingBrief("RESULT: c1 sha=000000000000 tier: flash\nKIND: fix\nPATHS: internal/c1.go\nGATE-NEEDS: linux\n\nFix c1.")
	ta.runVerb("add", "--stream", "s1", "--one", "c1", "--brief", linuxBrief)
	ta.runVerb("start")
	ta.runVerb("fleet", "beat", "m-linux", "--load", "0")
	ta.runVerb("fleet", "beat", "m-darwin", "--load", "0")
	ta.runVerb("tick")

	s, err := st.Load(ctx, []string{sprint.Work, sprint.Fleet}, nil)
	require.NoError(t, err)
	pr := s.Work.Card("c1")
	require.NotNil(t, pr, "the GATE-NEEDS linux card is admitted")
	wc := s.Fleet.Card(sprint.WorkCardID("c1", 1))
	require.NotNil(t, wc, "the GATE-NEEDS linux card is dealt")
	assert.Equal(t, "m-linux", wc.Row, "the GATE-NEEDS linux card is dealt only to the linux member")

	// an unsatisfiable card: no member up carries tla, so one judgment names the need.
	unsat := passingBrief("RESULT: c2 sha=000000000000 tier: flash\nKIND: fix\nPATHS: internal/c2.go\nGATE-NEEDS: tla\n\nFix c2.")
	ta.runVerb("add", "--stream", "s1", "--one", "c2", "--brief", unsat)
	ta.runVerb("tick")
	s, err = st.Load(ctx, []string{sprint.Work, sprint.Fleet}, nil)
	require.NoError(t, err)
	assert.Nil(t, s.Fleet.Card(sprint.WorkCardID("c2", 1)), "the unsatisfiable card is never dealt")
	inbox, err := st.Inbox(ctx, 0, 0, 1000)
	require.NoError(t, err)
	var judged string
	for _, o := range inbox.Open {
		if o.Note.Type == sprint.NGateNeeds {
			judged = o.Note.What
		}
	}
	assert.NotEmpty(t, judged, "one judgment names the need")
	assert.Contains(t, judged, "GATE-NEEDS tla")
}

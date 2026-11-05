package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
	"github.com/stretchr/testify/require"
)

// Removal keeps the record that Absent tests. Try the delayed real create at
// the current revision, inside the real removing operation's fence.
func TestRevivedRefusalPlanExists(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"drop-ready", "drop-working", "defer-waiting"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			h.setup(1)
			add := AddStep(sprint.AddReq{Stream: "s1", IDs: []string{"candidate"}, Held: mode == "defer-waiting"})
			snap := h.snap()
			plan := add.Plan(snap)
			require.Empty(t, plan.Refused)
			delayed, err := h.st.operation(add.Verb, h.st.Actor, "delayed", plan, snap)
			require.NoError(t, err)
			var create ntable.BatchManifest
			for _, man := range delayed.Manifests {
				if man.Table == "t-work" {
					for _, e := range man.Members {
						if e.ID == "candidate" && e.Expect != nil && e.Expect.Absent {
							create = man
						}
					}
				}
			}
			require.NotEmpty(t, create.OperationID, "real add must expect candidate absent")
			h.must(add)
			_, err = h.m.Apply(h.ctx, freshRefusalManifest(h.m, create, "before-removal"))
			require.Equal(t, "MEMBEREXISTS", refusalCode(err))
			if mode == "drop-working" {
				h.must(DealStep(sprint.DealReq{Sel: sprint.Sel{IDs: []string{"candidate"}}}))
			}
			checked := 0
			wrapped := &revivedRefusalBackend{Backend: h.m}
			wrapped.after = func(ctx context.Context, man ntable.BatchManifest) {
				for _, e := range man.Members {
					if !e.Remove {
						continue
					}
					checked++
					pending := h.m.Pending()
					require.NotNil(t, pending, "real removal must hold its operation fence")
					mm := h.m.tables[man.Table].members[e.ID]
					require.NotNil(t, mm, "removal erased member %s on %s", e.ID, man.Table)
					require.False(t, mm.placed, "removal must actually unplace %s", e.ID)
					if man.Table == create.Table && e.ID == "candidate" {
						_, err := h.m.Apply(ctx, freshRefusalManifest(h.m, create, "after-removal"))
						require.Equal(t, "MEMBEREXISTS", refusalCode(err), "removal revived the delayed real add: %v", err)
						require.Equal(t, pending.ID, h.m.Pending().ID, "late send moved the removing fence")
					}
				}
			}
			h.st.B = wrapped
			sel := sprint.Sel{IDs: []string{"candidate"}}
			if mode == "defer-waiting" {
				h.must(DeferStep(sprint.DeferReq{Sel: sel, Reason: "deferred"}))
			} else {
				h.must(DropStep(sprint.DropReq{Sel: sel, Reason: "dropped"}))
			}
			require.GreaterOrEqual(t, checked, 1)
			if mode == "drop-working" {
				require.GreaterOrEqual(t, checked, 2, "drop must also retire the dealt fleet card")
			}
			require.Nil(t, h.m.Pending(), "removing operation must finish")
		})
	}
}

func freshRefusalManifest(m *Mem, man ntable.BatchManifest, id string) ntable.BatchManifest {
	man.ExpectedTableRevision = fmt.Sprint(m.tables[man.Table].rev)
	man.OperationID = id
	return man
}

type revivedRefusalBackend struct {
	Backend
	after func(context.Context, ntable.BatchManifest)
}

func (b *revivedRefusalBackend) AtEpoch(epoch uint64, old bool) Backend {
	return &revivedRefusalBackend{Backend: b.Backend.AtEpoch(epoch, old), after: b.after}
}
func (b *revivedRefusalBackend) Apply(ctx context.Context, man ntable.BatchManifest) (ntable.Receipt, error) {
	r, err := b.Backend.Apply(ctx, man)
	if err == nil {
		b.after(ctx, man)
	}
	return r, err
}

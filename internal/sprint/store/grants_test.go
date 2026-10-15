package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

func TestGrantRecordSurvivesAClear(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"m1"}))
	req := sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: time.Hour}
	g, _, err := h.st.ChangeGrant(h.ctx, req, "first")
	require.NoError(t, err)
	_, err = h.st.Clear(h.ctx)
	require.NoError(t, err)
	gs, err := h.st.Grants(h.ctx)
	require.NoError(t, err)
	require.Equal(t, []sprint.Grant{g}, gs.Items)
	_, replay, err := h.st.ChangeGrant(h.ctx, req, "first")
	require.NoError(t, err)
	require.NotEmpty(t, replay.Refused, "a prior epoch op is never a new add")
	_, err = h.st.Teardown(h.ctx)
	require.NoError(t, err)
	gs, err = h.st.Grants(h.ctx)
	require.NoError(t, err)
	require.Empty(t, gs.Items)
}

type grantCut struct{ before, after bool }
type grantFault struct {
	*Mem
	cut *grantCut
}

func (f *grantFault) AtEpoch(e uint64, old bool) Backend {
	return &grantFault{Mem: f.Mem.AtEpoch(e, old).(*Mem), cut: f.cut}
}
func (f *grantFault) Release(ctx context.Context, op OpRecord, commit bool) error {
	if commit && op.Grants != nil && f.cut.before {
		f.cut.before = false
		return errors.New("cut before release")
	}
	err := f.Mem.Release(ctx, op, commit)
	if err == nil && commit && op.Grants != nil && f.cut.after {
		f.cut.after = false
		return errors.New("lost release reply")
	}
	return err
}

func TestGrantCommitRecoveryAndReplay(t *testing.T) {
	t.Parallel()
	for _, after := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[after], func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"m1"}))
			h.st.B = &grantFault{Mem: h.m, cut: &grantCut{before: !after, after: after}}
			req := sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: time.Hour}
			_, _, err := h.st.ChangeGrant(h.ctx, req, "same")
			require.ErrorIs(t, err, ErrUnknown)
			// Reopening loses no frozen payload. Fenced repairs the journal before the
			// second callerOp check; no new grant or second log is made.
			h.st.B = h.m
			g, res, err := h.st.ChangeGrant(h.ctx, req, "same")
			require.NoError(t, err)
			require.True(t, res.Replay)
			require.Equal(t, "g1", g.ID)
			gs, err := h.st.Grants(h.ctx)
			require.NoError(t, err)
			require.Len(t, gs.Items, 1)
			require.Equal(t, uint64(1), gs.Next)
			req.For = 2 * time.Hour
			_, _, err = h.st.ChangeGrant(h.ctx, req, "same")
			require.Error(t, err)
			pinned, err := h.st.Pinned(h.ctx)
			require.NoError(t, err)
			lines, _, err := pinned.B.LogSince(h.ctx, "", 1000)
			require.NoError(t, err)
			count := 0
			for _, line := range lines {
				if line.Verb == "grant add" {
					count++
				}
			}
			require.Equal(t, 1, count)
		})
	}
}

func TestGrantPendingEndCannotPruneUntilJudged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"m1"}))
	g, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: time.Minute}, "add")
	require.NoError(t, err)
	h.now = h.now.Add(time.Minute)
	pending, err := h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	_, _, err = h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "end", ID: g.ID}, "end")
	require.NoError(t, err)
	h.now = h.now.Add(8 * 24 * time.Hour)
	pending, err = h.st.PrepareGrantDeliveries(h.ctx, "new-sender", false, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Empty(t, pending)
	gs, err := h.st.Grants(h.ctx)
	require.NoError(t, err)
	require.Len(t, gs.Items, 1, "judgment is committed before terminal pruning")
	require.True(t, gs.Items[0].Judged)
	require.Equal(t, "sender", gs.Items[0].Delivery.From)
	_, err = h.st.PrepareGrantDeliveries(h.ctx, "new-sender", false, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, h.openOf(sprint.NTimer), 1)
	h.now = h.now.Add(7 * 24 * time.Hour)
	_, err = h.st.PrepareGrantDeliveries(h.ctx, "new-sender", false, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	gs, err = h.st.Grants(h.ctx)
	require.NoError(t, err)
	require.Empty(t, gs.Items)
	require.Equal(t, uint64(1), gs.Next)
}

func TestGrantFirstExpiryIsNotRearmedAndRetentionUsesLastChange(t *testing.T) {
	t.Parallel()
	for _, judge := range []bool{false, true} {
		t.Run(map[bool]string{false: "sent", true: "judged"}[judge], func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"m1"}))
			g, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: time.Minute}, "first")
			require.NoError(t, err)
			h.now = h.now.Add(time.Minute)
			p, err := h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
			require.NoError(t, err)
			require.Len(t, p, 1)
			original := p[0]
			if judge {
				h.now = h.now.Add(24 * time.Hour)
				_, err = h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
			} else {
				err = h.st.MarkGrantSent(h.ctx, g.ID, p[0].Delivery.Token)
			}
			require.NoError(t, err)
			h.now = h.now.Add(time.Hour)
			edited, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "extend", ID: g.ID, For: time.Minute}, "late")
			require.NoError(t, err)
			require.Equal(t, h.now.Add(time.Minute), edited.Deadline, "--for starts from server now")
			require.Equal(t, original.Owed, edited.Owed)
			require.Equal(t, original.Delivery.Token, edited.Delivery.Token)
			h.now = h.now.Add(7*24*time.Hour - time.Nanosecond)
			p, err = h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
			require.NoError(t, err)
			require.Empty(t, p)
			gs, err := h.st.Grants(h.ctx)
			require.NoError(t, err)
			require.Len(t, gs.Items, 1, "retention begins at late edit")
			h.now = h.now.Add(time.Nanosecond)
			p, err = h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
			require.NoError(t, err)
			require.Empty(t, p)
			gs, err = h.st.Grants(h.ctx)
			require.NoError(t, err)
			require.Empty(t, gs.Items)
		})
	}
}

func TestGrantFriendRowFreezesCanonicalRecipientOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"friend.stella"}))
	require.NoError(t, h.m.SetCoordinator(h.ctx, "stella"))
	h.st.Actor = "stella"
	g, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "friend.stella", For: time.Minute}, "friend")
	require.NoError(t, err)
	require.Equal(t, "friend.stella", g.Grantee)
	h.now = h.now.Add(time.Minute)
	pending, err := h.st.PrepareGrantDeliveries(h.ctx, "server-login", true, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "stella", pending[0].Delivery.To, "grantee and seat canonicalize to one recipient")
	require.Equal(t, "server-login", pending[0].Delivery.From)
	require.Contains(t, pending[0].Delivery.Subject, "grantee=friend.stella")
}

func TestGrantExtensionMayShortenAnUnexpiredDeadline(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	require.NoError(t, h.m.RowsAdd(h.ctx, h.st.Names.Table(sprint.Fleet), []string{"m1"}))
	g, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: time.Hour}, "add")
	require.NoError(t, err)
	h.now = h.now.Add(10 * time.Minute)
	short, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "extend", ID: g.ID, For: time.Minute}, "shorten")
	require.NoError(t, err)
	require.Equal(t, h.now.Add(time.Minute), short.Deadline)
	require.True(t, short.Deadline.Before(g.Deadline))
	require.True(t, short.Owed.IsZero())
	pending, err := h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Empty(t, pending)
	h.now = short.Deadline
	pending, err = h.st.PrepareGrantDeliveries(h.ctx, "sender", true, 24*time.Hour, 7*24*time.Hour)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, short.Deadline, pending[0].Owed)
	long, _, err := h.st.ChangeGrant(h.ctx, sprint.GrantRequest{Op: "extend", ID: g.ID, For: time.Hour}, "later")
	require.NoError(t, err)
	require.Equal(t, short.Deadline, long.Owed)
	require.Equal(t, pending[0].Delivery.Token, long.Delivery.Token)
}

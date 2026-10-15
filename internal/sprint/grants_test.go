package sprint

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGrantRules(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []time.Duration{time.Minute, 30 * 24 * time.Hour} {
		require.NoError(t, (GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: d}).Validate(true))
	}
	for _, d := range []time.Duration{0, time.Minute - time.Nanosecond, 30*24*time.Hour + 1} {
		require.Error(t, (GrantRequest{Op: "add", Bench: "b1", Grantee: "m1", For: d}).Validate(true))
	}
	require.ErrorContains(t, (GrantRequest{Op: "add", Bench: "b1", Grantee: "absent", For: time.Hour}).Validate(false), "absent")
	require.Error(t, (GrantRequest{Op: "add", Bench: "../bad", Grantee: "m1", For: time.Hour}).Validate(true))
	g := Grant{Deadline: now.Add(time.Hour), Changed: now}
	require.False(t, g.Due(now))
	g.KeepOwed(now)
	require.True(t, g.Owed.IsZero())
	require.True(t, g.Due(g.Deadline))
	g.KeepOwed(g.Deadline)
	original := g.Owed
	g.Deadline = now.Add(2 * time.Hour)
	g.Ended = now.Add(time.Hour)
	g.KeepOwed(now.Add(3 * time.Hour))
	require.Equal(t, original, g.Expiry())
	require.False(t, g.Prunable(now.Add(30*24*time.Hour), 7*24*time.Hour), "owed expiry survives a long outage")
	g.Judged = true
	require.False(t, g.Prunable(now.Add(7*24*time.Hour-time.Nanosecond), 7*24*time.Hour))
	require.True(t, g.Prunable(now.Add(7*24*time.Hour), 7*24*time.Hour))
}

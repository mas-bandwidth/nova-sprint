package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// The server never inherits the caller's environment, so a verb it runs that takes
// --pg is sent the caller's NOVA_PG_DSN when no --pg was given: friend stage through
// nsc.sh found no config store before (v1.6.14). An explicit --pg wins, a DSN that
// carries a password is never put on the wire, and a verb with no --pg is sent none.
func TestAVerbSentToTheServerTakesNovaPGDSNForItsPG(t *testing.T) {
	t.Parallel()
	const dsn = "postgres://nova_config@127.0.0.1:5432/nova"
	sentWith := func(t *testing.T, env map[string]string, args ...string) []string {
		t.Helper()
		env[ServerEnv] = "127.0.0.1:6390"
		c := newApp(func(k string) string { return env[k] })
		t.Cleanup(c.close)
		var sent [][]string
		c.forward = func(_ context.Context, _ string, verbs ...[]string) ([]sprintwire.Result, error) {
			sent = append(sent, verbs...)
			return []sprintwire.Result{{Code: 0}}, nil
		}
		var out, errb bytes.Buffer
		require.Equal(t, 0, runInto(c, args, &out, &errb), errb.String())
		require.Len(t, sent, 1)
		return sent[0]
	}

	got := sentWith(t, map[string]string{"NOVA_PG_DSN": dsn, "NOVA_PG_PASSWORD_ENV": "NOVA_PG_CONFIG_PASSWORD"}, "friend", "stage", "c1@2", "--as", "friend.stella")
	assert.Equal(t, []string{"friend", "stage", "--pg", dsn, "c1@2", "--as", "friend.stella"}, got)

	got = sentWith(t, map[string]string{"NOVA_PG_DSN": dsn}, "friend", "stage", "c1@2", "--as", "friend.stella", "--pg", "postgres://other@10.0.0.1:5432/nova")
	assert.Equal(t, []string{"friend", "stage", "c1@2", "--as", "friend.stella", "--pg", "postgres://other@10.0.0.1:5432/nova"}, got, "an explicit --pg wins")

	got = sentWith(t, map[string]string{"NOVA_PG_DSN": "postgres://nova_config:hunter2@127.0.0.1:5432/nova"}, "friend", "stage", "c1@2", "--as", "friend.stella")
	assert.NotContains(t, got, "--pg", "a DSN with a password stays off the wire")

	got = sentWith(t, map[string]string{}, "friend", "stage", "c1@2", "--as", "friend.stella")
	assert.NotContains(t, got, "--pg", "no NOVA_PG_DSN, no --pg")

	got = sentWith(t, map[string]string{"NOVA_PG_DSN": dsn}, "start")
	assert.Equal(t, []string{"start"}, got, "a verb with no --pg is sent none")
}

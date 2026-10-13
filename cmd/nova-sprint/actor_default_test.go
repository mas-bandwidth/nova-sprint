package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
)

func actorDefaultRig(t *testing.T, name string, rec sprint.HookRecord) (*testApp, map[string]string) {
	t.Helper()
	ta, env := hookSprint(t, name)
	env["NOVA_SPRINT_ACTOR"] = ""
	st := &store.Store{B: ta.m, Now: ta.a.now}
	require.NoError(t, writeHook(context.Background(), st, rec))
	ta.busProven(name)
	return ta, env
}

func liveActorHook(name string, now time.Time) sprint.HookRecord {
	return sprint.HookRecord{
		Name: name, Session: "fake-session", State: sprint.HookHooked,
		Proven: now, Live: now.Add(time.Hour),
	}
}

func TestCoordinatorVerbDefaultsToTheProvenHookOnce(t *testing.T) {
	t.Parallel()
	const name = "actor-default-live"
	ta, _ := actorDefaultRig(t, name, liveActorHook(name, t0))
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one")
	require.Equal(t, 0, code, "%s%s", out, errs)
	assert.Equal(t, 1, strings.Count(errs, "ACTOR "+name+" (coordinator hook proven)"), errs)
	assert.Contains(t, out, "ADD OK stream=s1 cards=1", out)
	assert.Greater(t, ta.applies(), before, "the proven coordinator's one-card add was persisted")
}

func TestForwardedCoordinatorVerbDefaultsOnTheServerHook(t *testing.T) {
	t.Parallel()
	const name = "actor-default-forwarded"
	ta, env := actorDefaultRig(t, name, liveActorHook(name, t0))
	env["NOVA_SPRINT_ACTOR"] = "server-must-not-act"
	clientEnv := map[string]string{ServerEnv: "127.0.0.1:6390"}
	client := newApp(func(k string) string { return clientEnv[k] })
	t.Cleanup(client.close)
	var sent [][]string
	client.forward = func(_ context.Context, _ string, verbs ...[]string) ([]sprintwire.Result, error) {
		sent = append(sent, verbs...)
		return ta.a.serveFrom(sprintwire.Request{Verbs: verbs}, true).Results, nil
	}
	var out, errs bytes.Buffer
	code := runInto(client, []string{"add", "--stream", "s1", "--count", "1", "--one"}, &out, &errs)
	require.Equal(t, 0, code, "%s%s", out.String(), errs.String())
	assert.Equal(t, [][]string{{"add", "--stream", "s1", "--count", "1", "--one"}}, sent,
		"an absent actor stays absent in the request so the server can use only its proven hook")
	assert.Equal(t, 1, strings.Count(errs.String(), "ACTOR "+name+" (coordinator hook proven)"), errs.String())
	assert.NotContains(t, errs.String(), "server-must-not-act", errs.String())
}

func TestForwardedCoordinatorVerbDefaultsAfterServerRestart(t *testing.T) {
	t.Parallel()
	const name = "actor-default-restart"
	ta, env := actorDefaultRig(t, name, liveActorHook(name, t0))
	env["NOVA_SPRINT_ACTOR"] = "server-must-not-act"
	ta.restartTestServer()
	st := &store.Store{B: ta.m, Now: ta.a.now}
	rec, ok, err := readHook(context.Background(), st)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, sprint.HookGone, rec.State, "restart invalidates the old sprint-hook proof")
	assert.True(t, rec.Live.IsZero(), "restart clears the previous live interval")

	second := ta.hookOn(name)
	second.next("hello")
	challenge := second.next("challenge").Challenge
	require.Equal(t, "ok", second.say(hookMsg{T: "prove", Challenge: challenge}).T)
	ta.busProven(name)
	clientEnv := map[string]string{ServerEnv: "127.0.0.1:6390"}
	client := newApp(func(k string) string { return clientEnv[k] })
	t.Cleanup(client.close)
	var sent [][]string
	client.forward = func(_ context.Context, _ string, verbs ...[]string) ([]sprintwire.Result, error) {
		sent = append(sent, verbs...)
		return ta.a.serveFrom(sprintwire.Request{Verbs: verbs}, true).Results, nil
	}
	before := ta.applies()
	var out, errs bytes.Buffer
	code := runInto(client, []string{"add", "--stream", "s1", "--count", "1", "--one"}, &out, &errs)
	require.Equal(t, 0, code, "%s%s", out.String(), errs.String())
	assert.Equal(t, [][]string{{"add", "--stream", "s1", "--count", "1", "--one"}}, sent,
		"the client leaves the actor absent for the restarted server to resolve")
	assert.Equal(t, 1, strings.Count(errs.String(), "ACTOR "+name+" (coordinator hook proven)"), errs.String())
	assert.NotContains(t, errs.String(), "server-must-not-act", errs.String())
	assert.Contains(t, out.String(), "ADD OK stream=s1 cards=1", out.String())
	assert.Greater(t, ta.applies(), before, "the restarted server used the newly proven actor")
}

func TestCoordinatorActorDefaultKeepsExplicitActor(t *testing.T) {
	t.Parallel()
	const name = "actor-default-explicit"
	ta, _ := actorDefaultRig(t, name, liveActorHook(name, t0))
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one --actor forged")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, errs, "the coordinator's alone", errs)
	assert.NotContains(t, errs, "ACTOR "+name, errs)
	assert.Equal(t, before, ta.applies(), "an explicit other identity must not write")
}

func TestCoordinatorActorDefaultNeedsBothLivePushes(t *testing.T) {
	t.Parallel()
	const name = "actor-default-no-sprint-proof"
	ta, env := hookSprint(t, name)
	env["NOVA_SPRINT_ACTOR"] = ""
	ta.busProven(name) // the bus push alone is insufficient
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one")
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, errs, "--actor <name> is required", errs)
	assert.Contains(t, errs, "ACTOR default withheld: the coordinator's sprint hook is missing, stale, or unproven", errs)
	assert.NotContains(t, errs, "there is no default", errs)
	assert.NotContains(t, errs, "ACTOR "+name, errs)
	assert.Equal(t, before, ta.applies(), "an unproven identity must not write")
}

func TestCoordinatorActorDefaultKeepsTheOverdueAckRemedy(t *testing.T) {
	t.Parallel()
	const name = "actor-default-ack"
	oldest := &sprint.HookItem{ID: 7, What: "the queued push", At: t0.Add(-sprint.HookAckBound - time.Second)}
	rec := liveActorHook(name, t0)
	rec.Oldest, rec.Unacked = oldest, 1
	ta, _ := actorDefaultRig(t, name, rec)
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, errs, sprint.HookUnacked(*oldest, 1), errs)
	assert.NotContains(t, errs, "--actor <name> is required", errs)
	assert.Equal(t, 1, strings.Count(errs, "ACTOR "+name+" (coordinator hook proven)"), errs)
	assert.Equal(t, before, ta.applies(), "the overdue push still blocks the write")
}

func TestCoordinatorActorDefaultRejectsIncompleteOrForgedProof(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		want   string
		change func(*testing.T, *testApp, string)
		record func(string) sprint.HookRecord
	}{
		{
			name: "empty-session",
			want: "ACTOR default withheld: the coordinator's sprint hook is missing, stale, or unproven",
			record: func(name string) sprint.HookRecord {
				rec := liveActorHook(name, t0)
				rec.Session = ""
				return rec
			},
		},
		{
			name: "stale-sprint-proof",
			want: "ACTOR default withheld: the coordinator's sprint hook is missing, stale, or unproven",
			record: func(name string) sprint.HookRecord {
				rec := liveActorHook(name, t0)
				rec.Live = t0.Add(-time.Second)
				return rec
			},
		},
		{
			name:   "wrong-sprint-name",
			want:   "ACTOR default withheld: the coordinator's sprint hook is missing, stale, or unproven",
			record: func(name string) sprint.HookRecord { return liveActorHook(name+"-forged", t0) },
		},
		{
			name:   "missing-bus-proof",
			want:   "ACTOR default withheld: the coordinator's bus push proof is missing, stale, or invalid",
			record: func(name string) sprint.HookRecord { return liveActorHook(name, t0) },
		},
		{
			name:   "stale-bus-proof",
			want:   "ACTOR default withheld: the coordinator's bus push proof is missing, stale, or invalid",
			record: func(name string) sprint.HookRecord { return liveActorHook(name, t0) },
			change: func(t *testing.T, ta *testApp, name string) {
				ta.now = t0.Add(sprint.HookSharedBound + time.Second)
			},
		},
		{
			name:   "wrong-bus-role",
			want:   "ACTOR default withheld: the coordinator's bus push proof is missing, stale, or invalid",
			record: func(name string) sprint.HookRecord { return liveActorHook(name, t0) },
			change: func(t *testing.T, ta *testApp, name string) {
				h, err := ta.a.hooker(context.Background())
				require.NoError(t, err)
				bh := h.(busHooker)
				rec, _, err := bh.Shared(context.Background(), name)
				require.NoError(t, err)
				fields := rec.Fields()
				fields["role"] = "worker"
				require.NoError(t, bh.set(context.Background(), name, fields))
				ta.a.hookerForget(name)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			holder := "actor-default-" + strings.ReplaceAll(tc.name, "_", "-")
			ta, env := hookSprint(t, holder)
			env["NOVA_SPRINT_ACTOR"] = ""
			st := &store.Store{B: ta.m, Now: ta.a.now}
			require.NoError(t, writeHook(context.Background(), st, tc.record(holder)))
			if tc.name != "missing-bus-proof" {
				ta.busProven(holder)
			}
			if tc.change != nil {
				tc.change(t, ta, holder)
			}
			before := ta.applies()
			code, out, errs := ta.do("add --stream s1 --count 1 --one")
			assert.Equal(t, 2, code, "%s%s", out, errs)
			assert.Contains(t, errs, "--actor <name> is required", errs)
			assert.Contains(t, errs, tc.want, errs)
			assert.NotContains(t, errs, "ACTOR "+holder, errs)
			assert.Equal(t, before, ta.applies(), "unproven/forged identity wrote")
		})
	}
}

func TestCoordinatorActorDefaultNamesBackendFailureWithoutLeakingDetails(t *testing.T) {
	t.Parallel()
	const name = "actor-default-backend-error"
	ta, env := hookSprint(t, name)
	env["NOVA_SPRINT_ACTOR"] = ""
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return nil, errors.New("private connection detail")
	}
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one")
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, errs, "--actor <name> is required", errs)
	assert.Contains(t, errs, "ACTOR default withheld: the sprint backend could not be opened", errs)
	assert.NotContains(t, errs, "private connection detail", errs)
	assert.NotContains(t, errs, "there is no default", errs)
	assert.Equal(t, before, ta.applies(), "a backend failure does not infer an actor or write")
}

func TestCoordinatorActorDefaultPreservesEnvironmentAndExplicitEmptyPrecedence(t *testing.T) {
	t.Parallel()
	t.Run("environment actor wins", func(t *testing.T) {
		t.Parallel()
		const name = "actor-default-env"
		ta, env := actorDefaultRig(t, name, liveActorHook(name, t0))
		env["NOVA_SPRINT_ACTOR"] = "different"
		before := ta.applies()
		code, out, errs := ta.do("add --stream s1 --count 1 --one")
		assert.NotEqual(t, 0, code, "%s%s", out, errs)
		assert.Contains(t, errs, "the coordinator's alone", errs)
		assert.NotContains(t, errs, "ACTOR "+name, errs)
		assert.Equal(t, before, ta.applies(), "the environment actor was silently overridden")
	})
	t.Run("explicit empty wins", func(t *testing.T) {
		t.Parallel()
		const name = "actor-default-empty"
		ta, _ := actorDefaultRig(t, name, liveActorHook(name, t0))
		before := ta.applies()
		code, out, errs := ta.do("add --stream s1 --count 1 --one --actor ''")
		assert.Equal(t, 2, code, "%s%s", out, errs)
		assert.Contains(t, errs, "--actor <name> is required", errs)
		assert.NotContains(t, errs, "ACTOR "+name, errs)
		assert.Equal(t, before, ta.applies(), "an explicit empty actor was replaced")
	})
	t.Run("explicit identity wins environment", func(t *testing.T) {
		t.Parallel()
		const name = "actor-default-cli"
		ta, env := actorDefaultRig(t, name, liveActorHook(name, t0))
		env["NOVA_SPRINT_ACTOR"] = "different"
		code, out, errs := ta.do("add --stream s1 --count 1 --one --actor " + name)
		assert.Equal(t, 0, code, "%s%s", out, errs)
		assert.NotContains(t, errs, "ACTOR "+name+" (coordinator hook proven)", errs)
	})
}

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
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/hook/hooktest"
)

// actorDefaultRig is a sprint whose coordinator name is hooked in and proven as the
// coordinator in its nova-hook record, the process's actor unset.
func actorDefaultRig(t *testing.T, name string) (*testApp, *hooktest.Rig, map[string]string) {
	t.Helper()
	ta, rig, env := coordSprint(t, name)
	env["NOVA_SPRINT_ACTOR"] = ""
	rig.Hook(t, name, hook.Coordinator)
	return ta, rig, env
}

func TestCoordinatorVerbDefaultsToTheProvenHookOnce(t *testing.T) {
	t.Parallel()
	const name = "actor-default-live"
	ta, _, _ := actorDefaultRig(t, name)
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
	ta, _, env := actorDefaultRig(t, name)
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

// A restarted server reads the same nova-hook record: the proof is nova-hook's, never the
// server's memory, so the first verb after a restart defaults to the proven coordinator.
func TestForwardedCoordinatorVerbDefaultsAfterServerRestart(t *testing.T) {
	t.Parallel()
	const name = "actor-default-restart"
	ta, _, env := actorDefaultRig(t, name)
	env["NOVA_SPRINT_ACTOR"] = "server-must-not-act"
	old := ta.a
	a := newApp(old.getenv)
	a.now, a.sleep, a.backend, a.serveAddr, a.bus, a.busOpen, a.hookStoreFn = old.now, old.sleep, old.backend, old.serveAddr, old.bus, old.busOpen, old.hookStoreFn
	a.meter, a.mergeQueue, a.checkTwin = old.meter, old.mergeQueue, old.checkTwin
	ta.a = a
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
	assert.Greater(t, ta.applies(), before, "the restarted server used the proven actor")
}

func TestCoordinatorActorDefaultKeepsExplicitActor(t *testing.T) {
	t.Parallel()
	const name = "actor-default-explicit"
	ta, _, _ := actorDefaultRig(t, name)
	before := ta.applies()
	code, out, errs := ta.do("add --stream s1 --count 1 --one --actor forged")
	assert.NotEqual(t, 0, code, "%s%s", out, errs)
	assert.Contains(t, errs, "the coordinator's alone", errs)
	assert.NotContains(t, errs, "ACTOR "+name, errs)
	assert.Equal(t, before, ta.applies(), "an explicit other identity must not write")
}

// The default wants the record proven as the coordinator: the bus push alone (no server
// holding the sprint push), an unhooked record, a record of another role, a record of
// another name, and no answer to the heartbeat within its window each withhold it.
func TestCoordinatorActorDefaultNeedsTheRecordProven(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		record func(*testing.T, *hooktest.Rig, string)
	}{
		{"never-hooked", func(*testing.T, *hooktest.Rig, string) {}},
		{"bus-push-only", func(t *testing.T, rig *hooktest.Rig, name string) { hookBusOnly(t, rig, name) }},
		{"unhooked", func(t *testing.T, rig *hooktest.Rig, name string) {
			rig.Hook(t, name, hook.Coordinator)
			rig.Unhook(t, name)
		}},
		{"comms", func(t *testing.T, rig *hooktest.Rig, name string) { rig.Hook(t, name, hook.Comms) }},
		{"another-name", func(t *testing.T, rig *hooktest.Rig, name string) { rig.Hook(t, name+"-forged", hook.Coordinator) }},
		{"no-answer", func(t *testing.T, rig *hooktest.Rig, name string) {
			rig.Hook(t, name, hook.Coordinator)
			require.NoError(t, rig.Client.HSet(context.Background(), hook.RecordKey(name), "answered_at", time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC).UnixMilli()).Err())
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			holder := "actor-default-" + tc.name
			ta, rig, env := coordSprint(t, holder)
			env["NOVA_SPRINT_ACTOR"] = ""
			tc.record(t, rig, holder)
			before := ta.applies()
			code, out, errs := ta.do("add --stream s1 --count 1 --one")
			assert.Equal(t, 2, code, "%s%s", out, errs)
			assert.Contains(t, errs, "--actor <name> is required", errs)
			assert.Contains(t, errs, "ACTOR default withheld: the coordinator's nova-hook record is not proven as the coordinator", errs)
			assert.NotContains(t, errs, "ACTOR "+holder+" ", errs)
			assert.Equal(t, before, ta.applies(), "an unproven identity must not write")
		})
	}
	// a record that cannot be read withholds it too, naming no backend detail
	ta, _, env := coordSprint(t, "actor-default-unread")
	env["NOVA_SPRINT_ACTOR"] = ""
	ta.a.hookStoreFn = func(context.Context) (hook.Store, error) { return hook.Store{}, errors.New("private dial detail") }
	code, out, errs := ta.do("add --stream s1 --count 1 --one")
	assert.Equal(t, 2, code, "%s%s", out, errs)
	assert.Contains(t, errs, "ACTOR default withheld: the coordinator's nova-hook record could not be read", errs)
	assert.NotContains(t, errs, "private dial detail", errs)
}

func TestCoordinatorActorDefaultNamesBackendFailureWithoutLeakingDetails(t *testing.T) {
	t.Parallel()
	const name = "actor-default-backend-error"
	ta, _, env := coordSprint(t, name)
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
		ta, _, env := actorDefaultRig(t, name)
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
		ta, _, _ := actorDefaultRig(t, name)
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
		ta, _, env := actorDefaultRig(t, name)
		env["NOVA_SPRINT_ACTOR"] = "different"
		code, out, errs := ta.do("add --stream s1 --count 1 --one --actor " + name)
		assert.Equal(t, 0, code, "%s%s", out, errs)
		assert.NotContains(t, errs, "ACTOR "+name+" (coordinator hook proven)", errs)
	})
}

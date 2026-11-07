package main

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestSelfBusReplyStopsWithoutSending(t *testing.T) {
	t.Setenv("NOVA_BUS_FROM", "sprint")
	t.Setenv("NOVA_BUS_ID", "1-1")
	t.Setenv("PATH", t.TempDir())
	if code := command([]string{"bus-reply"}); code != 0 {
		t.Fatalf("self callback exit %d", code)
	}
}
func TestDashRejectsConflictingModes(t *testing.T) {
	if code := command([]string{"dash", "--pull", "http://example.test", "--pg", "fake"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
}

func TestHiddenSetupModesFailClosedWithoutCredentials(t *testing.T) {
	t.Setenv("NOVA_PG_DSN", "")
	t.Setenv("NOVA_SPRINT_PG_PASSWORD", "")
	t.Setenv("NOVA_PG_ADMIN_PASSWORD", "")
	for _, args := range [][]string{{"--internal-model-probe", "unsupported"}, {"--internal-setup-role"}, {"--internal-setup-role-fresh"}, {"--internal-write-continuation"}, {"--internal-role-exists"}, {"--internal-store-probe"}, {"--internal-store-setup"}} {
		code, handled := internalCommand(context.Background(), args)
		if !handled || code == 0 {
			t.Fatalf("%v: handled=%v exit=%d", args, handled, code)
		}
	}
}

type oldTools struct{}

func (oldTools) Check(context.Context, bool) ([]verbs.OperationError, error) { return nil, nil }
func (oldTools) NovaToolsVersion(context.Context) (string, error)            { return "nova-version v1.6.29", nil }
func TestCheckRefusesToolsBelowV2(t *testing.T) {
	e := verbs.Engine{Health: oldTools{}, ToolPin: toolPin}
	result := e.Execute(context.Background(), []string{"check"})
	if result.Exit != 1 || !strings.Contains(result.Output, "below pin v2.0.0") {
		t.Fatalf("result=%+v", result)
	}
}

func TestDirtyBuildDoesNotClaimCommitIdentity(t *testing.T) {
	clean := []debug.BuildSetting{{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "false"}}
	if got := sourceVersion(clean); got != "dev+0123456789ab" {
		t.Fatalf("clean %q", got)
	}
	clean[1].Value = "true"
	if got := sourceVersion(clean); got != "dev" {
		t.Fatalf("dirty %q", got)
	}
	if got := sourceVersion(nil); got != "dev" {
		t.Fatalf("unknown %q", got)
	}
}

func TestSimulationCoordinatorIsPersistedChoice(t *testing.T) {
	ctx := context.Background()
	s := store.NewFake()
	choice := verbs.Choices{Store: s, Actor: "rowan", Now: time.Now}
	if err := choice.Save(ctx, "coordinator", "rowan"); err != nil {
		t.Fatal(err)
	}
	e, err := verbs.Simulation(s, "other", "dev+0123456789ab", toolPin, time.Now, func(context.Context, string) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.Env.Coordinator(ctx)
	if err != nil || got != "rowan" {
		t.Fatalf("coordinator=%q err=%v", got, err)
	}
}

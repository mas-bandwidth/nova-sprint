package main

import (
	"context"
	"github.com/mas-bandwidth/nova-sprint/policy"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/verbs"
	"runtime/debug"
	"strings"
	"testing"
)

func TestSelfBusReplyStopsWithoutSending(t *testing.T) {
	t.Setenv("NOVA_BUS_FROM", "sprint")
	t.Setenv("NOVA_BUS_ID", "1-1")
	t.Setenv("PATH", t.TempDir())
	if code := command([]string{"bus-reply"}); code != 0 {
		t.Fatalf("self callback exit %d", code)
	}
}
func TestDashRefusesAFlagItDoesNotTake(t *testing.T) {
	if code := command([]string{"dash", "--pull", "http://example.test", "--pg", "fake"}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestPolicyEvalIsTheChild(t *testing.T) {
	if policy.EvalArg != "policy-eval" {
		t.Fatal(policy.EvalArg)
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

func (oldTools) Fleet(context.Context) error                    { return nil }
func (oldTools) ToolsVersion(context.Context) (string, error)   { return "v1.6.29", nil }
func (oldTools) Schema(context.Context) (bool, error)           { return true, nil }
func (oldTools) History(context.Context) error                  { return nil }
func (oldTools) Run(context.Context) (run.ChooserStatus, error) { return run.ChooserStatus{}, nil }

func TestCheckRefusesToolsBelowThePin(t *testing.T) {
	e := verbs.Engine{Doctor: oldTools{}, Bus: present{}, ToolPin: toolPin}
	result := e.Execute(context.Background(), []string{"check"})
	if result.Exit != 1 || !strings.Contains(result.Output, "below the pin "+toolPin) {
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

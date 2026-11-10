package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

func TestSetupPrerequisitesRecheckToolsVersion(t *testing.T) {
	t.Parallel()
	fleet := true
	version := "v1.6.29"
	s := SetupPrerequisites{Pin: "v1.6.20", Installed: func(context.Context) (string, error) { return version, nil }, Env: ExecEnvironment{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "nova-fleet" {
			t.Fatalf("ran %s: the tools' version is read from build info, never a retired tool", name)
		}
		if !fleet {
			return nil, errors.New("offline")
		}
		return nil, nil
	}}}
	check, err := s.Tools(context.Background())
	if err != nil || !check.Verified {
		t.Fatal(check, err)
	}
	fleet = false
	check, err = s.Tools(context.Background())
	if err != nil || check.Verified || check.Next != "nova-fleet check" {
		t.Fatal(check, err)
	}
	fleet = true
	version = "bad"
	_, err = s.Tools(context.Background())
	var op *OperationError
	if !errors.As(err, &op) || !strings.HasPrefix(op.Problem, "nova-tools version unknown (") || op.Fix != "nova-fleet install --tag v1.6.20 <machine>" {
		t.Fatal(err)
	}
	version = "v1.6.19"
	_, err = s.Tools(context.Background())
	if !errors.As(err, &op) || op.Problem != "nova-tools v1.6.19 is below pin v1.6.20" || op.Fix != "nova-fleet install --tag v1.6.20 <machine>" {
		t.Fatal(err)
	}
}
func TestSetupCoordinatorSavedAndRechecked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	present := true
	env := ExecEnvironment{Getenv: func(string) string { return "" }, Choices: Choices{Store: store.NewFake(), Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}, Command: func(context.Context, string, ...string) ([]byte, error) {
		if present {
			return []byte("WHO OK present=1\nalice\t1m\n"), nil
		}
		return []byte("WHO OK present=0\n"), nil
	}}
	s := SetupPrerequisites{Env: env}
	c, err := s.Coordinator(ctx, "alice")
	if err != nil || !c.Action {
		t.Fatal(c, err)
	}
	if err = s.SaveCoordinator(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	// the recheck with no caller (Completion's) is of the saved answer; a
	// caller is the hooked coordinator, and TestUpTakeOverMovesTheSavedCoordinator
	// holds that the answer follows it
	c, err = s.Coordinator(ctx, "")
	if err != nil || !c.Verified {
		t.Fatal(c, err)
	}
	present = false
	c, err = s.Coordinator(ctx, "")
	if err != nil || c.Verified || (c.Refusal == nil || c.Refusal.Problem != "alice is not hooked in" || c.Refusal.Fix != "run nova-bus hook --as alice under your session's Monitor tool") {
		t.Fatal(c, err)
	}
}

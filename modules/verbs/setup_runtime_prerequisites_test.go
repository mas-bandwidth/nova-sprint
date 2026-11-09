package verbs

import (
	"context"
	"errors"
	"strings"
	"testing"
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

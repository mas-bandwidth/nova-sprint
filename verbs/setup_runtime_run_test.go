package verbs

import (
	"context"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/run"
)

func TestSetupRunRequiresRealOwnerAndPresence(t *testing.T) {
	t.Parallel()
	status := run.ChooserStatus{Held: true, Operation: "run", Host: "bench-a"}
	present := true
	r := PGSetupRun{Inspect: func(context.Context, string, string) (run.ChooserStatus, error) { return status, nil }, Env: ExecEnvironment{Command: func(context.Context, string, ...string) ([]byte, error) {
		if present {
			return []byte("WHO OK present=1\nsprint\t1m\n"), nil
		}
		return []byte("WHO OK present=0\n"), nil
	}}}
	ok, err := r.Ready(context.Background(), "bench-a")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	status.Host = "other"
	ok, err = r.Ready(context.Background(), "bench-a")
	if err != nil || ok {
		t.Fatal(ok, err)
	}
	status.Host = "bench-a"
	present = false
	ok, err = r.Ready(context.Background(), "bench-a")
	if err != nil || ok {
		t.Fatal(ok, err)
	}
}

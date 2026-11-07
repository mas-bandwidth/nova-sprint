package main

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/run"
)

func TestRunInfoProjectsObservedOwner(t *testing.T) {
	at := time.Unix(1000, 0)
	calls := 0
	info := pgRunInfo{dsn: "postgres://nova_config@localhost/db", password: "password", inspect: func(_ context.Context, dsn, password string) (run.ChooserStatus, error) {
		calls++
		if dsn != "postgres://nova_sprint@localhost/db" || password != "password" {
			t.Fatal("incorrect connection arguments")
		}
		return run.ChooserStatus{Held: true, Operation: "run", Host: "bench", PID: 12, BackendPID: 34, ApplicationName: "app", Since: at}, nil
	}}
	got, err := info.Inspect(context.Background())
	if err != nil || calls != 1 || !got.Held || got.Host != "bench" || got.Operation != "run" || got.PID != 12 || got.BackendPID != 34 || !got.Since.Equal(at) {
		t.Fatalf("status=%+v err=%v calls=%d", got, err, calls)
	}
}

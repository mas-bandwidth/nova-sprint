package verbs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestSetupCompletionReportsVerifiedState(t *testing.T) {
	ctx := context.Background()
	db := store.NewFake()
	choices := Choices{Store: db, Actor: "alice", Now: func() time.Time { return time.Unix(1000, 0) }}
	for _, entry := range [][2]string{{"coordinator", "alice"}, {"run-on", "studio"}, {"dashboard", "tailnet"}} {
		if err := choices.Save(ctx, entry[0], entry[1]); err != nil {
			t.Fatal(err)
		}
	}
	cmd := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "nova-bus" {
			return []byte("WHO OK present=1\nalice\t1m\n"), nil
		}
		return []byte(`{"result":{"verb":"machine list","status":"ok","exit":0},"facts":{"kind":"machine","rows":2},"items":[{"kind":"machine","fields":{"name":"studio","roles":"services"}},{"kind":"machine","fields":{"name":"amd-1","roles":"bench"}}]}`), nil
	}
	transport := dashboardRoundTrip(func(req *http.Request) (*http.Response, error) {
		body := "ok"
		if req.URL.Path == "/api/sprint" {
			body = `{"ok":true,"data":{"epoch":1}}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	work := &runtimeWork{workers: true}
	r := RuntimeSetup{Prerequisites: SetupPrerequisites{Env: ExecEnvironment{Choices: choices, Command: cmd, Getenv: func(string) string { return "" }}}, Run: &runtimeRun{ready: true}, Workload: work, Services: FleetServices{Command: cmd}, Snapshot: db, RunService: FleetService{Machine: "studio", Seat: "studio"}, Dashboards: map[string]DashboardTarget{"tailnet": {URL: "http://studio:8080"}}, Dashboard: DashboardProbe{Transport: transport}}
	line, err := r.Completion(ctx)
	if err != nil || line != "UP DONE run on studio; dashboard http://studio:8080; 1 workers up; coordinator alice" {
		t.Fatal(line, err)
	}
	work.workers = false
	if _, err = r.Completion(ctx); err == nil {
		t.Fatal("reported stale worker state")
	}
}

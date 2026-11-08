package verbs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/dash"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

type dashboardRoundTrip func(*http.Request) (*http.Response, error)

func (f dashboardRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDashboardProbeChecksHealthEpochAndPublicCache(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name              string
		public            bool
		health            int
		body, cache, etag string
		want              bool
	}{
		{"private", false, 200, `{"ok":true,"data":{"epoch":3}}`, "", "", true},
		{"public", true, 200, `{"ok":true,"data":{"epoch":3}}`, "public, max-age=1", `"tag"`, true},
		{"stale", false, 200, `{"ok":true,"data":{"epoch":2}}`, "", "", false},
		{"unhealthy", false, 503, `{"ok":true,"data":{"epoch":3}}`, "", "", false},
		{"uncached", true, 200, `{"ok":true,"data":{"epoch":3}}`, "no-store, max-age=1", `"tag"`, false},
		{"etag", true, 200, `{"ok":true,"data":{"epoch":3}}`, "max-age=1", "", false},
		{"invalid json", false, 200, `{"ok":true,"data":{"epoch":3}}garbage`, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DashboardProbe{Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
				status := 200
				body := tc.body
				if r.URL.Path == "/healthz" {
					status = tc.health
					body = "ok"
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Cache-Control": []string{tc.cache}, "Etag": []string{tc.etag}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			got, err := p.Verify(context.Background(), "http://host:8080", 3, tc.public)
			var notReady *DashboardNotReady
			if got != tc.want || (tc.want && err != nil) || (!tc.want && !errors.As(err, &notReady)) {
				t.Fatal(got, err)
			}
			if tc.name == "invalid json" && !strings.Contains(err.Error(), "parse failed at byte 31") {
				t.Fatal(err)
			}
		})
	}
}

// The probe reads what the dash's own handler serves: the real envelope,
// round-tripped through dash.Handler on a recorder (no socket).
func TestDashboardProbeReadsTheDashHandler(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := store.NewFake()
	snap, err := st.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1000, 0)
	cache := dash.NewFromFeed(dash.Feed{Snap: snap, At: at}, func() time.Time { return at })
	handler := dash.Handler(cache)
	p := DashboardProbe{Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		resp := rec.Result()
		resp.Request = r
		return resp, nil
	})}
	if ok, err := p.Verify(ctx, "http://board:8080", snap.Epoch, false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err := p.Verify(ctx, "http://board:8080", snap.Epoch+1, false)
	var notReady *DashboardNotReady
	if ok || !errors.As(err, &notReady) || !strings.Contains(notReady.Why, fmt.Sprintf("data.epoch %d, want %d", snap.Epoch, snap.Epoch+1)) {
		t.Fatal(ok, err)
	}
	// The private board sends no-store: a public probe names the header.
	ok, err = p.Verify(ctx, "http://board:8080", snap.Epoch, true)
	if ok || !errors.As(err, &notReady) || !strings.Contains(notReady.Why, "ETag") {
		t.Fatal(ok, err)
	}
}

// A transport failure carries the request and its cause.
func TestDashboardProbeUnreachableNamesRequestAndCause(t *testing.T) {
	t.Parallel()
	p := DashboardProbe{Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp 203.0.113.7:8080: i/o timeout")
	})}
	_, err := p.Verify(context.Background(), "http://203.0.113.7:8080", 1, true)
	var u *DashboardUnreachable
	if !errors.As(err, &u) || !errors.Is(err, errDashboardReach) || err.Error() != "GET http://203.0.113.7:8080/healthz: dial tcp 203.0.113.7:8080: i/o timeout" {
		t.Fatal(err)
	}
}

// Step 5 still failing after its act prints the probe's error and its fix,
// never "step 5 is not verified".
func TestRuntimeDashboardRefusalIsTheProbeError(t *testing.T) {
	t.Parallel()
	public := &FleetService{Name: "sprint-dashboard", Machine: "edge-1"}
	r := RuntimeSetup{RunService: FleetService{Name: "sprint-run", Machine: "studio"}}
	unreachable := &DashboardUnreachable{Request: "GET http://203.0.113.7:8080/healthz", Cause: "dial tcp 203.0.113.7:8080: i/o timeout"}
	op := r.dashboardRefusal(DashboardTarget{URL: "http://203.0.113.7:8080", Public: true, Service: public}, unreachable)
	if op == nil || op.Problem != "dashboard unreachable: GET http://203.0.113.7:8080/healthz: dial tcp 203.0.113.7:8080: i/o timeout" || op.Fix != "open 8080/tcp on edge-1 for the dashboard, or front it on 80 (--dash public=edge-1:80), then nova-sprint up again" {
		t.Fatal(op)
	}
	op = r.dashboardRefusal(DashboardTarget{URL: "http://studio:8080"}, &DashboardNotReady{Why: "GET /api/sprint: data.epoch 0, want 3"})
	if op == nil || !strings.Contains(op.Problem, "data.epoch 0, want 3") || !strings.Contains(op.Fix, "sprint-run") || !strings.Contains(op.Fix, "on studio") {
		t.Fatal(op)
	}
	if r.dashboardRefusal(DashboardTarget{}, errors.New("other")) != nil {
		t.Fatal("an unrelated error became a dashboard refusal")
	}
}

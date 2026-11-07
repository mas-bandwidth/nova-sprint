package verbs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type dashboardRoundTrip func(*http.Request) (*http.Response, error)

func (f dashboardRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDashboardProbeChecksHealthEpochAndPublicCache(t *testing.T) {
	for _, tc := range []struct {
		name              string
		public            bool
		health            int
		body, cache, etag string
		want              bool
	}{
		{"private", false, 200, `{"ok":true,"epoch":3}`, "", "", true},
		{"public", true, 200, `{"ok":true,"epoch":3}`, "public, max-age=1", `"tag"`, true},
		{"stale", false, 200, `{"ok":true,"epoch":2}`, "", "", false},
		{"unhealthy", false, 503, `{"ok":true,"epoch":3}`, "", "", false},
		{"uncached", true, 200, `{"ok":true,"epoch":3}`, "no-store, max-age=1", `"tag"`, false},
		{"etag", true, 200, `{"ok":true,"epoch":3}`, "max-age=1", "", false},
		{"invalid json", false, 200, `{"ok":true,"epoch":3}garbage`, "", "", false},
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
			if tc.name == "invalid json" && !strings.Contains(err.Error(), "parse failed at byte 22") {
				t.Fatal(err)
			}
		})
	}
}

package verbs

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/policy"
)

// acmeProvider is a made-up provider whose probe answers 200 to a valid key.
var acmeProvider = policy.Provider{URL: "https://api.acme.test", Key: "ACME_API_KEY", Probe: "https://api.acme.test/v1/key"}

// probeSpec is p as the probe child is handed it.
func probeSpec(t *testing.T, p policy.Provider) string {
	t.Helper()
	var got []string
	m := ModelProbeCommand{Binary: "/opt/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Execute: func(_ context.Context, _ string, args ...string) error {
		got = args
		return nil
	}}
	if err := m.Run(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return got[len(got)-1]
}

// The probe is a GET of the configured endpoint with the configured key;
// only a 2xx passes, and the body is never read.
func TestModelProbeUsesTheConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	calls, status := 0, 200
	p := ModelListProbe{Getenv: func(name string) string {
		if name != "ACME_API_KEY" {
			t.Fatal(name)
		}
		return "example-secret"
	}, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://api.acme.test/v1/key" || r.Header.Get("Authorization") != "Bearer example-secret" {
			t.Fatal("wrong probe request", r.Method, r.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("ignored")), Header: http.Header{}, Request: r}, nil
	})}
	spec := probeSpec(t, acmeProvider)
	if got := p.Run(context.Background(), spec); got != 0 || calls != 1 {
		t.Fatal(got, calls)
	}
	status = 401
	if got := p.Run(context.Background(), spec); got != 1 || calls != 2 {
		t.Fatal(got, calls)
	}
	if got := p.Run(context.Background(), "https://attacker.example"); got != 2 || calls != 2 {
		t.Fatal(got, calls)
	}
	p.Getenv = func(string) string { return "bad\r\nheader" }
	if got := p.Run(context.Background(), spec); got != 1 || calls != 2 {
		t.Fatal(got, calls)
	}
}

func TestModelProbeNoRedirect(t *testing.T) {
	t.Parallel()
	calls := 0
	p := ModelListProbe{Getenv: func(string) string { return "secret" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if got := p.Run(context.Background(), probeSpec(t, acmeProvider)); got != 1 || calls != 1 {
		t.Fatal(got, calls)
	}
}

// The probe child opens only the provider key, and is handed the
// provider configuration, never the key value.
func TestModelProbeCommandUsesSecretsExec(t *testing.T) {
	t.Parallel()
	var got []string
	m := ModelProbeCommand{Binary: "/opt/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Execute: func(_ context.Context, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}}
	if err := m.Run(context.Background(), acmeProvider); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-secrets", "exec", "--only", "ACME_API_KEY", "--store", "/store", "--key", "/key", "--as", "studio", "--", "/opt/bin/nova-sprint", "--internal-model-probe",
		`{"URL":"https://api.acme.test","Key":"ACME_API_KEY","Probe":"https://api.acme.test/v1/key","Balance":""}`}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

// ServiceRoutes is one route and one key per chosen provider, in name
// order, from its configuration; none is none.
func TestServiceRoutesFromConfiguration(t *testing.T) {
	t.Parallel()
	routes, keys := ServiceRoutes(map[string]policy.Provider{"zeta": {URL: "https://zeta.test/api", Key: "ZETA_TOKEN"}, "acme": acmeProvider})
	if routes != "acme=https://api.acme.test@ACME_API_KEY,zeta=https://zeta.test/api@ZETA_TOKEN" || !reflect.DeepEqual(keys, []string{"ACME_API_KEY", "ZETA_TOKEN"}) {
		t.Fatal(routes, keys)
	}
	if routes, keys = ServiceRoutes(nil); routes != "" || keys != nil {
		t.Fatal(routes, keys)
	}
}

package verbs

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestModelProbeUsesOnlyKnownEndpointAndDiscardsBody(t *testing.T) {
	calls := 0
	p := ModelListProbe{Getenv: func(name string) string {
		if name != "DEEPSEEK_API_KEY" {
			t.Fatal(name)
		}
		return "example-secret"
	}, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if (r.URL.String() != "https://api.deepseek.com/models" && r.URL.String() != "https://api.deepseek.com/user/balance") || r.Header.Get("Authorization") != "Bearer example-secret" {
			t.Fatal("wrong probe request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ignored")), Header: http.Header{}, Request: r}, nil
	})}
	if got := p.Run(context.Background(), "deepseek"); got != 0 || calls != 2 {
		t.Fatal(got, calls)
	}
	if got := p.Run(context.Background(), "https://attacker.example"); got != 2 || calls != 2 {
		t.Fatal(got, calls)
	}
	p.Getenv = func(string) string { return "bad\r\nheader" }
	if got := p.Run(context.Background(), "deepseek"); got != 1 || calls != 2 {
		t.Fatal(got, calls)
	}
}
func TestModelProbeNoRedirect(t *testing.T) {
	calls := 0
	p := ModelListProbe{Getenv: func(string) string { return "secret" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if got := p.Run(context.Background(), "openrouter"); got != 1 || calls != 1 {
		t.Fatal(got, calls)
	}
}
func TestModelProbeCommandUsesSecretsExec(t *testing.T) {
	var got []string
	m := ModelProbeCommand{Binary: "/opt/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Execute: func(_ context.Context, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}}
	if err := m.Run(context.Background(), "deepseek"); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-secrets", "exec", "--only", "DEEPSEEK_API_KEY", "--store", "/store", "--key", "/key", "--as", "studio", "--", "/opt/bin/nova-sprint", "--internal-model-probe", "deepseek"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
func TestModelProbeRejectsInvalidKeyEvenIfCatalogIsPublic(t *testing.T) {
	calls := 0
	p := ModelListProbe{Getenv: func(string) string { return "fixture" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		code := http.StatusOK
		if r.URL.Path == "/api/v1/key" {
			code = http.StatusUnauthorized
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if got := p.Run(context.Background(), "openrouter"); got != 1 || calls != 1 {
		t.Fatal(got, calls)
	}
}

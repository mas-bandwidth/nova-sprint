package verbs

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The probe reads only the provider record's endpoints, sends the key its
// record names as its api wants it, and discards each body.
func TestModelProbeUsesOnlyTheRecordsEndpointsAndDiscardsBody(t *testing.T) {
	t.Parallel()
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
	if got := p.Run(context.Background(), deepseekRecord); got != 0 || calls != 2 {
		t.Fatal(got, calls)
	}
	for _, bad := range []FleetProvider{{Name: "none", Key: "NONE_API_KEY"}, {Name: "plain", Key: "PLAIN_API_KEY", ModelsURL: "http://attacker.example/models"}, {Name: "nokey", ModelsURL: "https://x.example/models"}} {
		if got := p.Run(context.Background(), bad); got != 2 || calls != 2 {
			t.Fatal(bad, got, calls)
		}
	}
	p.Getenv = func(string) string { return "bad\r\nheader" }
	if got := p.Run(context.Background(), deepseekRecord); got != 1 || calls != 2 {
		t.Fatal(got, calls)
	}
}

// An anthropic provider's key goes in x-api-key, never Authorization.
func TestModelProbeSendsAnAnthropicKeyInXAPIKey(t *testing.T) {
	t.Parallel()
	p := ModelListProbe{Getenv: func(string) string { return "sk-a" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Api-Key") != "sk-a" || r.Header.Get("Authorization") != "" || r.Header.Get("Anthropic-Version") == "" {
			t.Fatal("wrong anthropic probe headers")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	})}
	if got := p.Run(context.Background(), FleetProvider{Name: "anthropic", API: "anthropic", Key: "ANTHROPIC_API_KEY", ModelsURL: "https://api.anthropic.com/v1/models"}); got != 0 {
		t.Fatal(got)
	}
}

var openrouterRecord = FleetProvider{Name: "openrouter", API: "openai", Key: "OPENROUTER_API_KEY", ModelsURL: "https://openrouter.ai/api/v1/models", BalanceURL: "https://openrouter.ai/api/v1/key"}

func TestModelProbeNoRedirect(t *testing.T) {
	t.Parallel()
	calls := 0
	p := ModelListProbe{Getenv: func(string) string { return "secret" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if got := p.Run(context.Background(), openrouterRecord); got != 1 || calls != 1 {
		t.Fatal(got, calls)
	}
}
func TestModelProbeCommandUsesSecretsExec(t *testing.T) {
	t.Parallel()
	var got []string
	m := ModelProbeCommand{Binary: "/opt/bin/nova-sprint", Secrets: NovaSecrets{Store: "/store", KeyFile: "/key", Seat: "studio"}, Execute: func(_ context.Context, name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}}
	if err := m.Run(context.Background(), deepseekRecord); err != nil {
		t.Fatal(err)
	}
	want := []string{"nova-secrets", "exec", "--only", "DEEPSEEK_API_KEY", "--store", "/store", "--key", "/key", "--as", "studio", "--", "/opt/bin/nova-sprint", "--internal-model-probe",
		"deepseek", "openai", "DEEPSEEK_API_KEY", "https://api.deepseek.com/user/balance", "https://api.deepseek.com/models"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if back := ModelProbeProvider(want[13:]); back.Name != "deepseek" || back.Key != "DEEPSEEK_API_KEY" || back.ModelsURL != deepseekRecord.ModelsURL || back.BalanceURL != deepseekRecord.BalanceURL {
		t.Fatal(back)
	}
}
func TestModelProbeRejectsInvalidKeyEvenIfCatalogIsPublic(t *testing.T) {
	t.Parallel()
	calls := 0
	p := ModelListProbe{Getenv: func(string) string { return "fixture" }, Transport: dashboardRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		code := http.StatusOK
		if r.URL.Path == "/api/v1/key" {
			code = http.StatusUnauthorized
		}
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if got := p.Run(context.Background(), openrouterRecord); got != 1 || calls != 1 {
		t.Fatal(got, calls)
	}
}

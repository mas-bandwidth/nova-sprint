package verbs

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Known endpoints are the model-list and key pairs in nova-tools'
// internal/doctor/check_providers.go. Unknown providers fail closed.
var modelProbeProviders = map[string]struct{ endpoint, key, keyEndpoint string }{
	"deepseek":   {"https://api.deepseek.com/models", "DEEPSEEK_API_KEY", "https://api.deepseek.com/user/balance"},
	"openrouter": {"https://openrouter.ai/api/v1/models", "OPENROUTER_API_KEY", "https://openrouter.ai/api/v1/key"},
}

// ModelListProbe is run in a dedicated child under nova-secrets exec. It emits
// nothing and returns only status: 0 verified, 1 failed, 2 unsupported. The
// secret stays in memory and is never used as an argument or logged.
type ModelListProbe struct {
	Transport http.RoundTripper
	Getenv    func(string) string
}

func (p ModelListProbe) Run(ctx context.Context, service string) int {
	provider, ok := modelProbeProviders[service]
	if !ok {
		return 2
	}
	get := p.Getenv
	if get == nil {
		get = os.Getenv
	}
	key := get(provider.key)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return 1
	}
	client := http.Client{Transport: p.Transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, endpoint := range []string{provider.keyEndpoint, provider.endpoint} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 1
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return 1
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return 1
		}
	}
	return 0
}

// ModelProbeCommand runs the same binary under nova-secrets exec without
// capturing or logging either process's output.
type ModelProbeCommand struct {
	Binary  string
	Secrets NovaSecrets
	Execute func(context.Context, string, ...string) error
}

func (m ModelProbeCommand) Run(ctx context.Context, service string) error {
	provider, ok := modelProbeProviders[service]
	if !ok {
		return fmt.Errorf("model-list probe is unsupported")
	}
	if !filepath.IsAbs(m.Binary) || m.Secrets.Store == "" || m.Secrets.KeyFile == "" || m.Secrets.Seat == "" {
		return fmt.Errorf("model-list probe configuration invalid")
	}
	args := []string{"exec", "--only", provider.key, "--store", m.Secrets.Store, "--key", m.Secrets.KeyFile, "--as", m.Secrets.Seat, "--", m.Binary, "--internal-model-probe", service}
	run := m.Execute
	if run == nil {
		run = quietCommand
	}
	if err := run(ctx, "nova-secrets", args...); err != nil {
		return fmt.Errorf("model-list probe failed%s", ToolDetail(err))
	}
	return nil
}
func quietCommand(ctx context.Context, name string, args ...string) error {
	return RunQuiet(ctx, name, args...)
}

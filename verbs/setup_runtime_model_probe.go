package verbs

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// modelProbeProviders is every API-rate service up step 6 offers, the one
// table: its model-list and key endpoints (as nova-tools'
// internal/doctor/check_providers.go), its key's name, and the upstream the
// worker's key proxy routes the service's models to (edge/worker
// ParseProviders: without /v1). Unknown services fail closed; a service is
// added here and nowhere else.
var modelProbeProviders = map[string]struct{ endpoint, key, keyEndpoint, upstream string }{
	"deepseek":   {"https://api.deepseek.com/models", "DEEPSEEK_API_KEY", "https://api.deepseek.com/user/balance", "https://api.deepseek.com"},
	"openrouter": {"https://openrouter.ai/api/v1/models", "OPENROUTER_API_KEY", "https://openrouter.ai/api/v1/key", "https://openrouter.ai/api"},
}

// SetupServiceNames is every service up --services accepts, sorted.
func SetupServiceNames() []string { return slices.Sorted(maps.Keys(modelProbeProviders)) }

// ServiceRoutes is what a worker needs for the services up step 6 saved:
// its --providers routes (one per service, named as the service, the
// provider prefix of its model ids) and the key names its seat is granted.
// No service is no route and no key.
func ServiceRoutes(services []string) (routes string, keys []string) {
	parts := make([]string, 0, len(services))
	for _, name := range services {
		if p, ok := modelProbeProviders[name]; ok {
			parts = append(parts, name+"="+p.upstream+"@"+p.key)
			keys = append(keys, p.key)
		}
	}
	return strings.Join(parts, ","), keys
}

// ModelListProbe is run in a dedicated child under nova-secrets exec. It
// returns status: 0 verified, 1 failed, 2 unsupported; a failure says why on
// stderr, in one redacted line (the endpoint and its status or error). The
// secret stays in memory and is never used as an argument or logged.
type ModelListProbe struct {
	Transport http.RoundTripper
	Getenv    func(string) string
}

func (p ModelListProbe) Run(ctx context.Context, service string) int {
	provider, ok := modelProbeProviders[service]
	if !ok {
		return ChildFailed(fmt.Errorf("model-list probe is unsupported for service %q", service), 2)
	}
	get := p.Getenv
	if get == nil {
		get = os.Getenv
	}
	key := get(provider.key)
	if key == "" {
		return ChildFailed(fmt.Errorf("model-list probe: %s", missingNames(map[string]string{provider.key: key})), 1)
	}
	if strings.ContainsAny(key, "\r\n") {
		return ChildFailed(fmt.Errorf("model-list probe: %s holds a line break", provider.key), 1)
	}
	client := http.Client{Transport: p.Transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, endpoint := range []string{provider.keyEndpoint, provider.endpoint} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return ChildFailed(cause("model-list probe: GET "+endpoint, err), 1)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			return ChildFailed(cause("model-list probe: GET "+endpoint, err), 1)
		}
		_ = resp.Body.Close() // nothing is read from the body; its close cannot change the answer
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return ChildFailed(fmt.Errorf("model-list probe: GET %s answered HTTP %d", endpoint, resp.StatusCode), 1)
		}
	}
	return 0
}

// ModelProbeCommand runs the same binary under nova-secrets exec; its output
// is kept only for a failure, as the first lines, redacted (the probe child
// prints only its one-line reason; the key is never printed).
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
		if r := AsRefusal(err); r != nil {
			return r
		}
		return fmt.Errorf("model-list probe failed: %w", err)
	}
	return nil
}
func quietCommand(ctx context.Context, name string, args ...string) error {
	return RunQuiet(ctx, name, args...)
}

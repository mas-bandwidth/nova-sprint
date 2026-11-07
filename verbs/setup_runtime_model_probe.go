package verbs

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/policy"
)

// ServiceRoutes is what a worker needs for the providers up step 6 saved:
// its --providers routes (edge/worker ParseProviders, one per provider,
// named as the provider prefix of its model ids, routed to its API base)
// and the key names its seat is granted, in name order. None is no route
// and no key.
func ServiceRoutes(chosen map[string]policy.Provider) (routes string, keys []string) {
	parts := make([]string, 0, len(chosen))
	for _, name := range slices.Sorted(maps.Keys(chosen)) {
		p := chosen[name]
		parts = append(parts, name+"="+p.URL+"@"+p.Key)
		keys = append(keys, p.Key)
	}
	return strings.Join(parts, ","), keys
}

// ModelListProbe is run in a dedicated child under nova-secrets exec, handed
// the provider as JSON (policy.Provider, never the key's value): a GET of
// its probe endpoint with the key as a Bearer token, proven by a 2xx. It
// returns status: 0 verified, 1 failed, 2 not a provider; a failure says why on
// stderr, in one redacted line (the endpoint and its status or error). The
// secret stays in memory and is never used as an argument or logged.
type ModelListProbe struct {
	Transport http.RoundTripper
	Getenv    func(string) string
}

func (p ModelListProbe) Run(ctx context.Context, spec string) int {
	var provider policy.Provider
	if err := json.Unmarshal([]byte(spec), &provider); err != nil {
		return ChildFailed(fmt.Errorf("model-list probe: the provider configuration is not a policy provider: %w", err), 2)
	}
	if provider.Key == "" || provider.Probe == "" {
		return ChildFailed(fmt.Errorf("model-list probe: the provider configuration names no key or no probe"), 2)
	}
	get := p.Getenv
	if get == nil {
		get = os.Getenv
	}
	key := get(provider.Key)
	if key == "" {
		return ChildFailed(fmt.Errorf("model-list probe: %s", missingNames(map[string]string{provider.Key: key})), 1)
	}
	if strings.ContainsAny(key, "\r\n") {
		return ChildFailed(fmt.Errorf("model-list probe: %s holds a line break", provider.Key), 1)
	}
	client := http.Client{Transport: p.Transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.Probe, nil)
	if err != nil {
		return ChildFailed(cause("model-list probe: GET "+provider.Probe, err), 1)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return ChildFailed(cause("model-list probe: GET "+provider.Probe, err), 1)
	}
	_ = resp.Body.Close() // nothing is read from the body; its close cannot change the answer
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChildFailed(fmt.Errorf("model-list probe: GET %s answered HTTP %d", provider.Probe, resp.StatusCode), 1)
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

func (m ModelProbeCommand) Run(ctx context.Context, provider policy.Provider) error {
	if !filepath.IsAbs(m.Binary) || m.Secrets.Store == "" || m.Secrets.KeyFile == "" || m.Secrets.Seat == "" {
		return fmt.Errorf("model-list probe configuration invalid")
	}
	spec, err := json.Marshal(provider)
	if err != nil {
		return err
	}
	args := []string{"exec", "--only", provider.Key, "--store", m.Secrets.Store, "--key", m.Secrets.KeyFile, "--as", m.Secrets.Seat, "--", m.Binary, "--internal-model-probe", string(spec)}
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

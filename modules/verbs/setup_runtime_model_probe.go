package verbs

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// probeEndpoints are the provider record's balance_url and models_url, in
// that order, those set; each must be https (or http on loopback): the
// probe sends the key there.
func probeEndpoints(p FleetProvider) ([]string, error) {
	var out []string
	for _, e := range []string{p.BalanceURL, p.ModelsURL} {
		if e == "" {
			continue
		}
		u, err := url.Parse(e)
		if err != nil {
			return nil, fmt.Errorf("provider %s endpoint %q does not parse: %w", p.Name, Redact(e), err)
		}
		if u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && loopbackHost(u.Hostname()))) {
			return nil, fmt.Errorf("provider %s endpoint %q is not an https URL with a host and no credentials", p.Name, Redact(e))
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("provider %s has no models_url or balance_url, so its key cannot be probed", p.Name)
	}
	return out, nil
}

func loopbackHost(h string) bool {
	return strings.EqualFold(h, "localhost") || net.ParseIP(h).IsLoopback()
}

// ModelListProbe is run in a dedicated child under nova-secrets exec, for
// one provider record (its name, api, key's name, balance_url and
// models_url). It returns status: 0 verified, 1 failed, 2 unsupported; a
// failure says why on stderr, in one redacted line (the endpoint and its
// status or error). The secret stays in memory and is never used as an
// argument or logged.
type ModelListProbe struct {
	Transport http.RoundTripper
	Getenv    func(string) string
}

func (p ModelListProbe) Run(ctx context.Context, provider FleetProvider) int {
	endpoints, err := probeEndpoints(provider)
	if err != nil || !fleetKey.MatchString(provider.Key) {
		return ChildFailed(fmt.Errorf("model-list probe is unsupported for provider %q: %s", Redact(provider.Name), errText(cmpErr(err, "its key is not a secret's name"))), 2)
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
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return ChildFailed(cause("model-list probe: GET "+endpoint, err), 1)
		}
		if provider.API == "anthropic" {
			req.Header.Set("X-Api-Key", key)
			req.Header.Set("Anthropic-Version", "2023-06-01")
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
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

// cmpErr is err, or one made of why when err is nil.
func cmpErr(err error, why string) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", why)
}

// ModelProbeArgs are the internal child's arguments after its mode: the
// provider record's name, api, key's name, balance_url and models_url
// (none secret).
func ModelProbeArgs(p FleetProvider) []string {
	return []string{p.Name, p.API, p.Key, p.BalanceURL, p.ModelsURL}
}

// ModelProbeProvider is the provider record ModelProbeArgs carried.
func ModelProbeProvider(args []string) FleetProvider {
	if len(args) != 5 {
		return FleetProvider{}
	}
	return FleetProvider{Name: args[0], API: args[1], Key: args[2], BalanceURL: args[3], ModelsURL: args[4]}
}

// ModelProbeCommand runs the same binary under nova-secrets exec; its output
// is kept only for a failure, as the first lines, redacted (the probe child
// prints only its one-line reason; the key is never printed).
type ModelProbeCommand struct {
	Binary  string
	Secrets NovaSecrets
	Execute func(context.Context, string, ...string) error
}

func (m ModelProbeCommand) Run(ctx context.Context, provider FleetProvider) error {
	if _, err := probeEndpoints(provider); err != nil {
		return err
	}
	if !filepath.IsAbs(m.Binary) || m.Secrets.Store == "" || m.Secrets.KeyFile == "" || m.Secrets.Seat == "" || !fleetKey.MatchString(provider.Key) {
		return fmt.Errorf("model-list probe configuration invalid")
	}
	args := append([]string{"exec", "--only", provider.Key, "--store", m.Secrets.Store, "--key", m.Secrets.KeyFile, "--as", m.Secrets.Seat, "--", m.Binary, "--internal-model-probe"}, ModelProbeArgs(provider)...)
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

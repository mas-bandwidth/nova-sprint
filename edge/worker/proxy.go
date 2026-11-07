package worker

import (
	"cmp"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
)

const ProxyPlaceholderKey = "nova-proxy-placeholder"

type (
	ProxyRoute struct {
		Name, UpstreamURL string
		AllowedHosts      []string
		Key               func() (string, error)
	}
	KeyProxy struct {
		Routes       map[string]ProxyRoute
		DefaultRoute string
		Transport    http.RoundTripper
		lastStatus   atomic.Int32
	}
)

func (p *KeyProxy) RecordStatus(code int) {
	if p != nil {
		p.lastStatus.Store(int32(code))
	}
}
func (p *KeyProxy) ConsumeStatus() int {
	if p != nil {
		return int(p.lastStatus.Swap(0))
	}
	return 0
}
func (p *KeyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := p.resolveRoute_internal(r)
	if !ok {
		http.Error(w, "proxy refused: no route configured: set DefaultRoute or X-Nova-Route", http.StatusBadGateway)
		return
	}
	u, targetHost, err := resolveTargetURL_internal(route, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !hostAllowed_internal(targetHost, route.AllowedHosts) {
		http.Error(w, fmt.Sprintf("proxy refused: host %s is not on policy's egress allowlist for route %s: add %s to the route allowlist in policy", targetHost, route.Name, targetHost), http.StatusForbidden)
		return
	}
	var key string
	var keyErr error
	if route.Key != nil {
		key, keyErr = route.Key()
		key = strings.TrimSpace(key)
	}
	if key == "" {
		cause := "no key source configured"
		if keyErr != nil {
			cause = keyErr.Error()
		} else if route.Key != nil {
			cause = "the key source returned an empty key"
		}
		http.Error(w, fmt.Sprintf("proxy refused: missing API key for route %s: %s", route.Name, edge.RedactN(cause, edge.FailureMax)), http.StatusServiceUnavailable)
		return
	}
	p.forward_internal(w, r, u, key)
}

// resolveRoute_internal is the request's route: X-Nova-Route, else the
// first path segment when it names a route (ProxyBaseURL: the harness's base
// URL is /<provider>/v1, and the segment is cut from the path forwarded),
// else DefaultRoute, else the one route there is.
func (p *KeyProxy) resolveRoute_internal(r *http.Request) (ProxyRoute, bool) {
	if p == nil || len(p.Routes) == 0 {
		return ProxyRoute{}, false
	}
	if name := strings.TrimSpace(r.Header.Get("X-Nova-Route")); name != "" {
		route, ok := p.Routes[name]
		return route, ok
	}
	first, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if route, ok := p.Routes[first]; ok && first != "" {
		r.URL.Path = "/" + rest
		return route, true
	}
	route, ok := p.Routes[p.DefaultRoute]
	if !ok && len(p.Routes) == 1 {
		for _, v := range p.Routes {
			return v, true
		}
	}
	return route, ok
}

// ProxyBaseURL is the OpenAI-shaped base URL a harness inside the wall is
// given for model: the proxy at addr, the route of the model's provider
// prefix, /v1 (a model with no prefix takes the default route).
func ProxyBaseURL(addr, model string) string {
	base := strings.TrimSuffix(addr, "/")
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	if provider, _, ok := strings.Cut(model, "/"); ok && provider != "" {
		base += "/" + provider
	}
	return base + "/v1"
}

// Provider is one route of the key proxy as a flag gives it: the model
// prefix it serves, the upstream it forwards to (and that URL's host, the
// one host it may reach), and the name of the environment variable holding
// its key.
type Provider struct{ Name, Upstream, Host, Key string }

var (
	providerName_internal = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	keyName_internal      = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// ParseProviders reads --providers: <name>=<upstream url>@<KEY NAME>, comma
// separated, e.g. deepseek=https://api.example.com@DEEPSEEK_API_KEY. The
// key's value is never a flag: the worker reads it from its environment,
// where nova-secrets exec put it.
func ParseProviders(s string) ([]Provider, error) {
	var out []Provider
	for _, spec := range SplitList(s) {
		name, rest, _ := strings.Cut(spec, "=")
		at := strings.LastIndex(rest, "@")
		if at < 0 {
			return nil, fmt.Errorf("worker refused: --providers entry %q is not <name>=<url>@<KEY>: write it as deepseek=<upstream url>@DEEPSEEK_API_KEY", edge.Redact(spec))
		}
		p := Provider{Name: name, Upstream: rest[:at], Key: rest[at+1:]}
		u, err := url.Parse(p.Upstream)
		switch {
		case !providerName_internal.MatchString(p.Name):
			return nil, fmt.Errorf("worker refused: --providers name %q is not a provider prefix: use the provider of the models' provider/model ids", edge.Redact(p.Name))
		case err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil:
			return nil, fmt.Errorf("worker refused: --providers %s upstream %q is not an http(s) URL with a host and no credentials: pass the provider's API base URL", p.Name, edge.Redact(p.Upstream))
		case !keyName_internal.MatchString(p.Key):
			return nil, fmt.Errorf("worker refused: --providers %s key %q is not an environment variable name: pass the name nova-secrets exec sets, e.g. DEEPSEEK_API_KEY", p.Name, edge.Redact(p.Key))
		case slices.ContainsFunc(out, func(q Provider) bool { return q.Name == p.Name }):
			return nil, fmt.Errorf("worker refused: --providers names %s twice: name each provider once", p.Name)
		}
		p.Host = u.Hostname()
		out = append(out, p)
	}
	return out, nil
}

// NewKeyProxy is one route per provider, each forwarding only to its
// upstream's host with the key read from the environment at each request
// (lookup is os.LookupEnv in the binary).
func NewKeyProxy(providers []Provider, lookup func(string) (string, bool)) *KeyProxy {
	p := &KeyProxy{Routes: map[string]ProxyRoute{}}
	for _, pr := range providers {
		p.Routes[pr.Name] = ProxyRoute{Name: pr.Name, UpstreamURL: pr.Upstream, AllowedHosts: []string{pr.Host}, Key: EnvKey(pr.Key, lookup)}
	}
	return p
}

// EnvKey is a route's key source: the variable name from the environment,
// or a refusal naming it and how it gets there.
func EnvKey(name string, lookup func(string) (string, bool)) func() (string, error) {
	return func() (string, error) {
		if v, ok := lookup(name); ok && strings.TrimSpace(v) != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s is not in the worker's environment: grant it to this bench's seat (nova-secrets grant) and run the worker under nova-secrets exec --only %s,...", name, name)
	}
}

// HasRoute: the proxy forwards for provider name.
func (p *KeyProxy) HasRoute(name string) bool {
	if p == nil {
		return false
	}
	_, ok := p.Routes[name]
	return ok
}

// MissingKeys are the routes whose key is absent, one line each naming the
// key and its fix; none is nil.
func (p *KeyProxy) MissingKeys() []string {
	var out []string
	if p == nil {
		return nil
	}
	for _, name := range slices.Sorted(maps.Keys(p.Routes)) {
		if route := p.Routes[name]; route.Key != nil {
			if _, err := route.Key(); err != nil {
				out = append(out, "provider "+name+": "+err.Error())
			}
		}
	}
	return out
}
func resolveTargetURL_internal(route ProxyRoute, r *http.Request) (*url.URL, string, error) {
	base, err := url.Parse(route.UpstreamURL)
	if err != nil {
		return nil, "", fmt.Errorf("proxy refused: invalid upstream URL %q for route %s: %s", edge.Redact(route.UpstreamURL), route.Name, edge.Redact(err.Error()))
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, "", fmt.Errorf("proxy refused: invalid upstream URL %q for route %s: no scheme or host", edge.Redact(route.UpstreamURL), route.Name)
	}
	targetHost := base.Hostname()
	if override := strings.TrimSpace(r.Header.Get("X-Target-Host")); override != "" {
		targetHost = strings.ToLower(override)
		if h, _, err := net.SplitHostPort(targetHost); err == nil {
			targetHost = h
		}
		base.Host = override
	}
	joined := *base
	reqPath := r.URL.Path
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}
	joined.Path, joined.RawQuery = strings.TrimSuffix(base.Path, "/")+reqPath, r.URL.RawQuery
	return &joined, strings.ToLower(targetHost), nil
}
func hostAllowed_internal(host string, allowed []string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	for _, a := range allowed {
		cand := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a), "."))
		if hCand, _, err := net.SplitHostPort(cand); err == nil {
			cand = hCand
		}
		if h != "" && h == cand {
			return true
		}
	}
	return false
}
func (p *KeyProxy) forward_internal(w http.ResponseWriter, r *http.Request, target *url.URL, apiKey string) {
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		http.Error(w, "proxy refused: invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	for k, vals := range r.Header {
		if !slices.Contains([]string{"AUTHORIZATION", "X-NOVA-ROUTE", "X-TARGET-HOST", "HOST"}, strings.ToUpper(k)) {
			outReq.Header[k] = slices.Clone(vals)
		}
	}
	outReq.Header.Set("Authorization", "Bearer "+apiKey)
	outReq.Host = target.Host
	resp, err := cmp.Or[http.RoundTripper](p.Transport, http.DefaultTransport).RoundTrip(outReq)
	if err != nil {
		p.RecordStatus(http.StatusBadGateway)
		http.Error(w, "proxy upstream fault: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		p.RecordStatus(resp.StatusCode)
	}
	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, 16<<20)); err != nil {
		// the status is sent: the harness sees a cut body; the worker's log says why
		reportTo(nil, "key proxy: response body cut short", err)
	}
}
func CheckLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("proxy refused: invalid listen address %q (%s): use 127.0.0.1:0", addr, edge.Redact(err.Error()))
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("proxy refused: listen address %s is not loopback: use 127.0.0.1:0", addr)
	}
	return nil
}
func (p *KeyProxy) ListenLoopback(addr string) (*http.Server, string, error) {
	addr = cmp.Or(addr, "127.0.0.1:0")
	if err := CheckLoopbackAddr(addr); err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	srv := &http.Server{Handler: p}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			reportTo(nil, "key proxy stopped serving", err)
		}
	}()
	return srv, ln.Addr().String(), nil
}

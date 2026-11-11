package worker

import (
	"cmp"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

const ProxyPlaceholderKey = "nova-proxy-placeholder"

// ProxyKey is the placeholder key a job's harness is given: the proxy
// swaps it for the route's real key, and reads the job from it, so a
// provider status is the job's that made the request.
func ProxyKey(consumer string) string { return ProxyPlaceholderKey + "-" + consumer }

type (
	ProxyRoute struct {
		Name, UpstreamURL string
		AllowedHosts      []string
		Key               func() (string, error)
	}
	KeyProxy struct {
		Routes     map[string]ProxyRoute
		Transport  http.RoundTripper
		mu         sync.Mutex
		tick       int                        // numbers each request sent and each status recorded, in the order they happen
		lastStatus map[string]status_internal // per job (ProxyKey's consumer)
	}
	// status_internal is a job's provider status and the tick it was recorded at.
	status_internal struct{ code, at int }
)

// maxBody_internal is the most of one response body the proxy passes on.
const maxBody_internal = 16 << 20

// RecordStatus keeps code as job's last provider status: a failure (402,
// 429, 5xx, a response cut short as 502). An answered request ends it only
// when that request was sent after it (answered_internal).
func (p *KeyProxy) RecordStatus(job string, code int) {
	if p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.lastStatus == nil {
			p.lastStatus = map[string]status_internal{}
		}
		p.tick++
		p.lastStatus[job] = status_internal{code, p.tick}
	}
}

// ConsumeStatus is job's last provider status, forgotten once read.
func (p *KeyProxy) ConsumeStatus(job string) int {
	if p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		code := p.lastStatus[job].code
		delete(p.lastStatus, job)
		return code
	}
	return 0
}

// sent_internal numbers a request as it is sent upstream.
func (p *KeyProxy) sent_internal() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tick++
	return p.tick
}

// answered_internal ends job's failure when the request now answered was
// sent after the failure was recorded: the harness got past it. A request
// sent before the failure and answered after it says nothing of the kind
// (two requests in flight: one is cut and the harness dies, the other's
// answer arrives), and the failure stands.
func (p *KeyProxy) answered_internal(job string, sent int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.lastStatus[job]; ok && s.at < sent {
		delete(p.lastStatus, job)
	}
}

// jobOf_internal is the job a request's placeholder key names ("" for the
// bare placeholder).
func jobOf_internal(r *http.Request) string {
	job, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+ProxyKey(""))
	return job
}
func (p *KeyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route, ok := p.resolveRoute_internal(r)
	if !ok {
		http.Error(w, "proxy refused: no route for this request: the harness's base URL is /<provider>/v1 for a --providers route", http.StatusBadGateway)
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

// resolveRoute_internal is the request's route: the first path segment when
// it names a route (ProxyBaseURL: the harness's base URL is /<provider>/v1,
// and the segment is cut from the path forwarded), else the one route there
// is.
func (p *KeyProxy) resolveRoute_internal(r *http.Request) (ProxyRoute, bool) {
	if p == nil || len(p.Routes) == 0 {
		return ProxyRoute{}, false
	}
	first, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if route, ok := p.Routes[first]; ok && first != "" {
		r.URL.Path = "/" + rest
		return route, true
	}
	if len(p.Routes) == 1 {
		for _, v := range p.Routes {
			return v, true
		}
	}
	return ProxyRoute{}, false
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
	// proxyKey_internal is a placeholder key (ProxyKey): never a real one.
	proxyKey_internal = regexp.MustCompile(`^` + regexp.QuoteMeta(ProxyPlaceholderKey) + `-[A-Za-z0-9._@:-]*$`)
	keyName_internal  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
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
		//lint:ignore ST1005 the trailing ",..." is the rest of the seat's --only list
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

// RouteNames are the providers the proxy forwards for, sorted; nil for none.
func (p *KeyProxy) RouteNames() []string {
	if p == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(p.Routes))
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
	// Accept-Encoding stays the transport's own: it asks for what it decodes
	// itself, so the harness is sent plain bytes; the harness's forwarded, a
	// cut body is a decompression error inside the harness.
	for k, vals := range r.Header {
		if !slices.Contains([]string{"AUTHORIZATION", "X-NOVA-ROUTE", "X-TARGET-HOST", "HOST", "ACCEPT-ENCODING"}, strings.ToUpper(k)) {
			outReq.Header[k] = slices.Clone(vals)
		}
	}
	outReq.Header.Set("Authorization", "Bearer "+apiKey)
	outReq.Host = target.Host
	sent := p.sent_internal()
	resp, err := cmp.Or(p.Transport, upstream_internal).RoundTrip(outReq)
	if err != nil {
		// a harness that hung up before the answer is no provider failure
		if r.Context().Err() == nil {
			p.RecordStatus(jobOf_internal(r), http.StatusBadGateway)
		}
		http.Error(w, "proxy upstream fault: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		p.RecordStatus(jobOf_internal(r), resp.StatusCode)
	} else {
		p.answered_internal(jobOf_internal(r), sent)
	}
	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	buf, left := make([]byte, 32<<10), maxBody_internal
	for {
		n, readErr := resp.Body.Read(buf)
		over := n > left
		if over {
			n = left
		}
		left -= n
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				reportTo(nil, "key proxy: the harness stopped reading its response", err)
				return
			}
			// sent as it arrives: unflushed, a stream's event waits in the
			// server's buffer for 4 KiB or the end
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if over {
			// the cap is the proxy's cut, and said as one: the harness gets
			// a broken connection, never the cap's bytes ending cleanly
			reportTo(nil, "key proxy: response body cut at the cap", fmt.Errorf("the upstream sent more than %d bytes", maxBody_internal))
			panic(http.ErrAbortHandler)
		}
		if readErr == io.EOF {
			return
		}
		if readErr != nil {
			// The upstream cut the body with the status already sent. The
			// worker's log says why; the job's provider status says it to the
			// classifier (ClassifyJob: a fault, "provider HTTP 502", never the
			// card's fail), unless the harness itself hung up; and the harness
			// gets a broken connection, never a short body that ends cleanly.
			reportTo(nil, "key proxy: response body cut short", readErr)
			if r.Context().Err() == nil {
				p.RecordStatus(jobOf_internal(r), http.StatusBadGateway)
			}
			panic(http.ErrAbortHandler)
		}
	}
}

// upstream_internal is the transport of a proxy given none: HTTP/1.1 only,
// so each request has a connection of its own and an upstream's reset cuts
// one job's request. HTTP/2 puts every job of the bench on one connection
// per provider, and one reset ends them all (2026-11-11, four jobs).
var upstream_internal http.RoundTripper = newUpstream_internal()

func newUpstream_internal() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Protocols = new(http.Protocols)
	t.Protocols.SetHTTP1(true)
	// the clone's config still offers h2 in ALPN
	t.TLSClientConfig = &tls.Config{NextProtos: []string{"http/1.1"}}
	// net/http keeps 2 idle connections per host, and every request past
	// them pays a TLS handshake. 32 is two for each job of a 16-wide row:
	// a number from the row's width, not a measured one.
	t.MaxIdleConnsPerHost = 32
	return t
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

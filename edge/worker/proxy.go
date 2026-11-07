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
func (p *KeyProxy) resolveRoute_internal(r *http.Request) (ProxyRoute, bool) {
	if p == nil || len(p.Routes) == 0 {
		return ProxyRoute{}, false
	}
	route, ok := p.Routes[cmp.Or(strings.TrimSpace(r.Header.Get("X-Nova-Route")), p.DefaultRoute)]
	if !ok && len(p.Routes) == 1 {
		for _, v := range p.Routes {
			return v, true
		}
	}
	return route, ok
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

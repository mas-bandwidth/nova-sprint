package worker

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const ProxyPlaceholderKey = "nova-proxy-placeholder"

type ProxyRoute struct {
	Name, UpstreamURL string
	AllowedHosts      []string
	Key               func() string
}
type KeyProxy struct {
	Routes       map[string]ProxyRoute
	DefaultRoute string
	Transport    http.RoundTripper
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
		msg := fmt.Sprintf("proxy refused: host %s is not on policy's egress allowlist for route %s: add %s to the route allowlist in policy",
			targetHost, route.Name, targetHost)
		http.Error(w, msg, http.StatusForbidden)
		return
	}
	var key string
	if route.Key != nil {
		key = strings.TrimSpace(route.Key())
	}
	if key == "" {
		msg := fmt.Sprintf("proxy refused: missing API key for route %s: run nova-secrets exec", route.Name)
		http.Error(w, msg, http.StatusServiceUnavailable)
		return
	}
	p.forward_internal(w, r, u, key)
}
func (p *KeyProxy) resolveRoute_internal(r *http.Request) (ProxyRoute, bool) {
	if p == nil || len(p.Routes) == 0 {
		return ProxyRoute{}, false
	}
	name := strings.TrimSpace(r.Header.Get("X-Nova-Route"))
	if name == "" {
		name = p.DefaultRoute
	}
	route, ok := p.Routes[name]
	if !ok && len(p.Routes) == 1 {
		for _, v := range p.Routes {
			return v, true
		}
	}
	return route, ok
}
func resolveTargetURL_internal(route ProxyRoute, r *http.Request) (*url.URL, string, error) {
	base, err := url.Parse(route.UpstreamURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, "", fmt.Errorf("proxy refused: invalid upstream URL %q for route %s", route.UpstreamURL, route.Name)
	}
	targetHost := base.Hostname()
	if override := strings.TrimSpace(r.Header.Get("X-Target-Host")); override != "" {
		targetHost = strings.ToLower(strings.TrimSpace(override))
		if h, _, err := net.SplitHostPort(targetHost); err == nil {
			targetHost = h
		}
		base.Host = override
	}
	joined := *base
	basePath := strings.TrimSuffix(base.Path, "/")
	reqPath := r.URL.Path
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}
	joined.Path = basePath + reqPath
	joined.RawQuery = r.URL.RawQuery
	return &joined, strings.ToLower(targetHost), nil
}
func hostAllowed_internal(host string, allowed []string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return false
	}
	for _, a := range allowed {
		cand := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(a), "."))
		if hCand, _, err := net.SplitHostPort(cand); err == nil {
			cand = hCand
		}
		if cand != "" && h == cand {
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
		ku := strings.ToUpper(k)
		if ku == "AUTHORIZATION" || ku == "X-NOVA-ROUTE" || ku == "X-TARGET-HOST" || ku == "HOST" {
			continue
		}
		for _, v := range vals {
			outReq.Header.Add(k, v)
		}
	}
	outReq.Header.Set("Authorization", "Bearer "+apiKey)
	outReq.Host = target.Host
	rt := p.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	resp, err := rt.RoundTrip(outReq)
	if err != nil {
		http.Error(w, "proxy upstream fault: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 16<<20))
}
func CheckLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	switch {
	case err != nil:
		return fmt.Errorf("proxy refused: invalid listen address %q: use 127.0.0.1:0", addr)
	case host == "localhost":
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("proxy refused: listen address %s is not loopback: use 127.0.0.1:0", addr)
	}
	return nil
}
func (p *KeyProxy) ListenLoopback(addr string) (*http.Server, string, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	if err := CheckLoopbackAddr(addr); err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	srv := &http.Server{Handler: p}
	go func() { _ = srv.Serve(ln) }()
	return srv, ln.Addr().String(), nil
}

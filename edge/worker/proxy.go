package worker

import (
	"cmp"
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
		http.Error(w, fmt.Sprintf("proxy refused: host %s is not on policy's egress allowlist for route %s: add %s to the route allowlist in policy", targetHost, route.Name, targetHost), http.StatusForbidden)
		return
	}
	var key string
	if route.Key != nil {
		key = strings.TrimSpace(route.Key())
	}
	if key == "" {
		http.Error(w, fmt.Sprintf("proxy refused: missing API key for route %s: run nova-secrets exec", route.Name), http.StatusServiceUnavailable)
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
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, "", fmt.Errorf("proxy refused: invalid upstream URL %q for route %s", route.UpstreamURL, route.Name)
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
		if h != "" && cand != "" && h == cand {
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
		switch strings.ToUpper(k) {
		case "AUTHORIZATION", "X-NOVA-ROUTE", "X-TARGET-HOST", "HOST":
			continue
		}
		for _, v := range vals {
			outReq.Header.Add(k, v)
		}
	}
	outReq.Header.Set("Authorization", "Bearer "+apiKey)
	outReq.Host = target.Host
	resp, err := cmp.Or[http.RoundTripper](p.Transport, http.DefaultTransport).RoundTrip(outReq)
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
	if err != nil {
		return fmt.Errorf("proxy refused: invalid listen address %q: use 127.0.0.1:0", addr)
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
	go func() { _ = srv.Serve(ln) }()
	return srv, ln.Addr().String(), nil
}

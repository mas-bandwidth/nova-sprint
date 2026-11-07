package dash

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

const addressLimit = 60
const addressBuckets = 4096

type bucket struct {
	since time.Time
	count int
}
type addressLimiter struct {
	mu      sync.Mutex
	clients map[string]bucket
}

func newAddressLimiter() *addressLimiter { return &addressLimiter{clients: map[string]bucket{}} }
func (l *addressLimiter) allow(address string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.clients) >= addressBuckets {
		for k, v := range l.clients {
			if now.Sub(v.since) >= time.Second || now.Before(v.since) {
				delete(l.clients, k)
			}
		}
	}
	b, exists := l.clients[address]
	if !exists && len(l.clients) >= addressBuckets {
		return false
	}
	if !exists || now.Sub(b.since) >= time.Second || now.Before(b.since) {
		b = bucket{since: now}
	}
	if b.count >= addressLimit {
		return false
	}
	b.count++
	l.clients[address] = b
	return true
}

// Handler serves only read-only routes, with a bounded per-address limiter.
func (p *PullCache) Handler() http.Handler {
	static := Handler(NewFromFeed(Feed{Snap: store.Snapshot{Epoch: 1}, At: p.now()}, p.now))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !p.limiter.allow(host, p.now()) {
			http.Error(w, "rate limit", http.StatusTooManyRequests)
			return
		}
		switch r.URL.Path {
		case "/api/sprint":
			p.serveJSON(w, r)
		case "/healthz":
			p.serveHealth(w)
		default:
			static.ServeHTTP(w, r)
		}
	})
}
func (p *PullCache) serveJSON(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	body, compressed, etag := p.body, p.compressed, p.etag
	p.mu.RUnlock()
	if body == nil {
		http.Error(w, "dashboard not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=1")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if acceptsGzip(r.Header.Values("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		body = compressed
	}
	if r.Method != http.MethodHead {
		respond(w, body)
	}
}
func (p *PullCache) serveHealth(w http.ResponseWriter) {
	p.mu.RLock()
	ready := p.base != nil
	age := p.baseAge + p.now().Sub(p.fetched).Seconds()
	p.mu.RUnlock()
	if !ready || age >= 32 {
		http.Error(w, "stale", http.StatusServiceUnavailable)
		return
	}
	respond(w, []byte("ok\n"))
}

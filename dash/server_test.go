package dash

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPageRoutes(t *testing.T) {
	c := &Cache{at: time.Now()}
	h := Handler(c)
	for _, path := range []string{"/", "/app.js", "/logo-tile-192.png", "/logo-tile-384.png", "/nunito-800.woff2"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Fatalf("%s status %d", path, r.Code)
		}
		if r.Header().Get("Cache-Control") != "no-store, max-age=0" {
			t.Fatalf("%s missing cache header", path)
		}
		if path == "/" {
			text := r.Body.String()
			if strings.Contains(text, "<!--LOGO-->") || strings.Contains(text, "<!--FAVICON-->") || !strings.Contains(text, "app.js?v="+buildID) {
				t.Fatal("index substitutions missing")
			}
		}
	}
}

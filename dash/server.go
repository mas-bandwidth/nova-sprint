package dash

import (
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed page
var page embed.FS

func assetBuild() string {
	h := sha256.New()
	_ = fs.WalkDir(page, "page", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			b, e := page.ReadFile(path)
			if e != nil {
				return e
			}
			_, _ = h.Write([]byte(path))
			_, _ = h.Write(b)
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

var buildID = assetBuild()

func versionedIndex() []byte {
	b, _ := page.ReadFile("page/index.html")
	build := buildID
	html := strings.ReplaceAll(string(b), `src="app.js"`, `src="app.js?v=`+build+`"`)
	html = strings.ReplaceAll(html, `src="charts.js"`, `src="charts.js?v=`+build+`"`)
	slot := `<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=` + build + `" srcset="/logo-tile-192.png?v=` + build + ` 1x, /logo-tile-384.png?v=` + build + ` 2x" alt="">`
	icon := `<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=` + build + `"><link rel="apple-touch-icon" href="/logo-tile-192.png?v=` + build + `">`
	return []byte(strings.ReplaceAll(strings.ReplaceAll(html, "<!--LOGO-->", slot), "<!--FAVICON-->", icon))
}

type pageEnvelope struct {
	OK                bool      `json:"ok"`
	Data              any       `json:"data"`
	FetchedAt         time.Time `json:"fetchedAt"`
	DataAgeSeconds    float64   `json:"dataAgeSeconds"`
	AttemptAt         time.Time `json:"attemptAt"`
	Error             string    `json:"error"`
	ReadSeconds       *float64  `json:"readSeconds"`
	MinInterval       int       `json:"minInterval"`
	Throughput        *float64  `json:"throughput"`
	ThroughputMinutes *float64  `json:"throughputMinutes"`
	Build             string    `json:"build"`
}

func dataHandler(c *Cache, w http.ResponseWriter, r *http.Request) {
	d := c.Data().(envelope)
	result := pageEnvelope{OK: d.OK, Data: d, FetchedAt: d.At, DataAgeSeconds: time.Since(d.At).Seconds(), AttemptAt: d.At, Error: d.Error, MinInterval: 1, Build: buildID}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		z := gzip.NewWriter(w)
		defer z.Close()
		_ = json.NewEncoder(z).Encode(result)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// Handler serves the v1 page and its in-memory data.
func Handler(c *Cache) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sprint", func(w http.ResponseWriter, r *http.Request) { dataHandler(c, w, r) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		c.mu.RLock()
		healthy := c.err == nil && time.Since(c.at) < 5*time.Second
		c.mu.RUnlock()
		if !healthy {
			http.Error(w, "stale", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "/public.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(versionedIndex())
	})
	for _, size := range []string{"192", "384"} {
		path := "/logo-tile-" + size + ".png"
		file := "page/logo-robot-" + size + ".png"
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			b, _ := page.ReadFile(file)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(b)
		})
	}
	files, _ := fs.Sub(page, "page")
	for _, name := range []string{"app.js", "charts.js", "stream.html", "stream.js", "pele.js", "nunito-800.woff2", "OFL.txt", "PELE.txt"} {
		mux.Handle("/"+name, http.FileServer(http.FS(files)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		mux.ServeHTTP(w, r)
	})
}

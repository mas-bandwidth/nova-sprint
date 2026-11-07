package dash

import (
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
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

// PageEnvelope is what GET /api/sprint serves: the board (Data) and how
// fresh it is. Exported so a reader (up's dashboard probe) decodes the
// served shape itself, never a guess at it.
type PageEnvelope struct {
	OK                bool      `json:"ok"`
	Data              Board     `json:"data"`
	FetchedAt         time.Time `json:"fetchedAt"`
	DataAgeSeconds    float64   `json:"dataAgeSeconds"`
	AttemptAt         time.Time `json:"attemptAt"`
	Error             *string   `json:"error"`
	ReadSeconds       float64   `json:"readSeconds"`
	MinInterval       int       `json:"minInterval"`
	Throughput        float64   `json:"throughput"`
	ThroughputMinutes float64   `json:"throughputMinutes"`
	Build             string    `json:"build"`
}

// Board is the page's view of the sprint, as PageEnvelope.Data carries it.
type Board = envelope

func (c *Cache) pageSnapshot() PageEnvelope {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d := c.view
	if c.err != nil {
		d.OK = false
		d.Error = "store read failed"
	}
	rate, minutes := c.rate()
	now := c.clock()
	var msg *string
	if d.Error != "" {
		msg = &d.Error
	}
	return PageEnvelope{OK: d.OK, Data: d, FetchedAt: d.At, DataAgeSeconds: now.Sub(d.At).Seconds(), AttemptAt: d.At, Error: msg, ReadSeconds: c.readSeconds, Throughput: rate, ThroughputMinutes: minutes, MinInterval: 1, Build: buildID}
}
func dataHandler(c *Cache, w http.ResponseWriter, r *http.Request) {
	result := c.pageSnapshot()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Vary", "Accept-Encoding")
	if acceptsGzip(r.Header.Values("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		z := gzip.NewWriter(w)
		defer z.Close()
		_ = json.NewEncoder(z).Encode(result)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// Explicit gzip preferences override wildcard preferences.
func acceptsGzip(headers []string) bool {
	var explicit, wildcard *float64
	for _, header := range headers {
		for _, item := range strings.Split(header, ",") {
			parts := strings.Split(item, ";")
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			if name != "gzip" && name != "*" {
				continue
			}
			q := 1.0
			for _, param := range parts[1:] {
				kv := strings.SplitN(strings.TrimSpace(param), "=", 2)
				if len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), "q") {
					v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64)
					if err != nil || v < 0 || v > 1 {
						q = 0
					} else {
						q = v
					}
				}
			}
			if name == "gzip" {
				explicit = &q
			} else {
				wildcard = &q
			}
		}
	}
	if explicit != nil {
		return *explicit > 0
	}
	return wildcard != nil && *wildcard > 0
}

// Handler serves the v1 page and its in-memory data.
func Handler(c *Cache) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sprint", func(w http.ResponseWriter, r *http.Request) { dataHandler(c, w, r) })
	for _, path := range []string{"/stream/", "/api/stream/", "/api/history", "/events"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusFound) })
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		now := c.clock()
		c.checkAlarm(now)
		healthy := now.Sub(c.at) < 32*time.Second
		c.mu.Unlock()
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
		body := versionedIndex()
		if r.URL.Path == "/public.html" {
			body = publicIndex(body)
		}
		_, _ = w.Write(body)
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

func publicIndex(body []byte) []byte {
	return []byte(strings.ReplaceAll(string(body), `<script src="charts.js?v=`+buildID+`"></script>`, ""))
}

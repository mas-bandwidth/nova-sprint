package sprintdash

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The logo, drawn as server.py drew it (docs/SPEC-SPRINT-DASHBOARD.md, "Serving"), from the
// one file --logo names (Server.Logo) and the files server.py left beside it:
//
//   - an .svg is drawn in the title (32 px, currentColor) and is the favicon, /favicon.svg;
//   - logo.webp or logo.png is a photo on white: the title shows /logo-icon.png and the
//     favicon is /favicon.png, the keyed copies server.py made with ffmpeg beside it when
//     they are there and not older than it, else the photo itself; /logo.webp or /logo.png
//     is the photo;
//   - any other image is a tile (logo-robot.webp, logo-stella.png): the title shows
//     /logo-tile-192.png at 1x and /logo-tile-384.png at 2x, and the 192 is the favicon,
//     each <stem>-<size>.png beside it (server.py's sips copies) when there and not older
//     than it, else the tile itself.
//
// A logo that cannot be read draws nothing, and its paths are not found. /logo is the file
// itself, whatever its kind.

// logoKind is how the logo is drawn.
type logoKind int

const (
	logoNone logoKind = iota
	logoSVG
	logoKeyed
	logoTile
)

// keyedLogos are the names of a photo on white.
var keyedLogos = map[string]bool{"logo.webp": true, "logo.png": true}

// tileSizes are the tile's copies, by the path that serves each.
var tileSizes = map[string]int{"/logo-tile-192.png": 192, "/logo-tile-384.png": 384}

// faviconStyle is the drawn favicon's colour, dark or light as the browser is.
const faviconStyle = `<style>svg{color:#121417}@media (prefers-color-scheme:dark){svg{color:#eef0f3}}</style>`

var (
	svgDrawing = regexp.MustCompile(`(?is)<svg\b([^>]*)>(.*)</svg>`)
	svgViewBox = regexp.MustCompile(`viewBox\s*=\s*"([^"]+)"`)
	// pyEscape is Python's html.escape, which server.py wrote the viewBox with.
	pyEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;")
)

// kind is how the logo is drawn now.
func (s *Server) kind() logoKind {
	if s.Logo == "" {
		return logoNone
	}
	if fi, err := os.Stat(s.Logo); err != nil || fi.IsDir() {
		return logoNone
	}
	switch base := filepath.Base(s.Logo); {
	case strings.EqualFold(filepath.Ext(base), ".svg"):
		if _, _, ok := s.svg(); ok {
			return logoSVG
		}
		return logoNone
	case keyedLogos[base]:
		return logoKeyed
	}
	return logoTile
}

// svg is the drawing's viewBox ("0 0 32 32" when it names none) and its inner markup.
func (s *Server) svg() (viewBox, inner string, ok bool) {
	b, err := os.ReadFile(s.Logo)
	if err != nil {
		return "", "", false
	}
	m := svgDrawing.FindSubmatch(b)
	if m == nil {
		return "", "", false
	}
	viewBox = "0 0 32 32"
	if vb := svgViewBox.FindSubmatch(m[1]); vb != nil {
		viewBox = string(vb[1])
	}
	return viewBox, string(m[2]), true
}

// logoSlot is the title's logo and the favicon's links at build v, as server.py wrote them.
func (s *Server) logoSlot(v string) (slot, icon string) {
	switch s.kind() {
	case logoSVG:
		vb, inner, _ := s.svg()
		return `<svg id="logo" viewBox="` + pyEscape.Replace(vb) + `" width="32" height="32" fill="currentColor" aria-hidden="true">` + inner + `</svg>`,
			`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`
	case logoTile:
		return fmt.Sprintf(`<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=%[1]s" srcset="/logo-tile-192.png?v=%[1]s 1x, /logo-tile-384.png?v=%[1]s 2x" alt="">`, v),
			fmt.Sprintf(`<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=%[1]s">`+
				`<link rel="apple-touch-icon" href="/logo-tile-192.png?v=%[1]s">`+
				`<link rel="preload" as="image" href="/logo-tile-192.png?v=%[1]s" imagesrcset="/logo-tile-192.png?v=%[1]s 1x, /logo-tile-384.png?v=%[1]s 2x">`, v)
	case logoKeyed:
		return `<img id="logo" class="raster" src="/logo-icon.png?v=` + v + `" alt="" height="48">`,
			`<link rel="icon" type="image/png" href="/favicon.png?v=` + v + `">`
	}
	return "", ""
}

// serveLogo answers path when it is one of server.py's logo paths, and is whether it was.
func (s *Server) serveLogo(w http.ResponseWriter, path string) bool {
	switch path {
	case "/favicon.svg":
		vb, inner, ok := s.svg()
		if s.kind() != logoSVG || !ok {
			http.Error(w, "no logo.svg", http.StatusNotFound)
			return true
		}
		s.send(w, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="`+pyEscape.Replace(vb)+`" fill="currentColor">`+faviconStyle+inner+`</svg>`))
	case "/logo-tile-192.png", "/logo-tile-384.png":
		stem := strings.TrimSuffix(s.Logo, filepath.Ext(s.Logo))
		s.sendCopy(w, s.kind() == logoTile, fmt.Sprintf("%s-%d.png", stem, tileSizes[path]), "no tile logo")
	case "/logo-icon.png", "/favicon.png":
		s.sendCopy(w, s.kind() == logoKeyed, filepath.Join(filepath.Dir(s.Logo), path[1:]), "no raster logo")
	case "/logo.webp", "/logo.png":
		body, err := os.ReadFile(s.Logo)
		if s.kind() != logoKeyed || filepath.Base(s.Logo) != path[1:] || err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return true
		}
		s.send(w, "image/"+strings.TrimPrefix(filepath.Ext(path), "."), body)
	default:
		return false
	}
	return true
}

// sendCopy sends the copy of the logo server.py made at name when it is there and not older
// than the logo, else the logo itself, typed by its bytes; not found (why) when the logo is
// not of the kind (ok false) or cannot be read.
func (s *Server) sendCopy(w http.ResponseWriter, ok bool, name, why string) {
	var body []byte
	if ok {
		src, err := os.Stat(s.Logo)
		if fi, cerr := os.Stat(name); err == nil && cerr == nil && !fi.ModTime().Before(src.ModTime()) {
			body, _ = os.ReadFile(name) // ignored: an unreadable copy falls back to the logo
		}
		if body == nil {
			body, _ = os.ReadFile(s.Logo) // ignored: an unreadable logo is not found below
		}
	}
	if body == nil {
		http.Error(w, why, http.StatusNotFound)
		return
	}
	s.send(w, imageType(body), body)
}

// imageType is the image's type from its bytes, as server.py's image_type: a tile named
// .png may be WebP inside.
func imageType(b []byte) string {
	switch {
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	case strings.HasPrefix(string(b), "\x89PNG\r\n\x1a\n"):
		return "image/png"
	case strings.HasPrefix(string(b), "\xff\xd8\xff"):
		return "image/jpeg"
	}
	return "application/octet-stream"
}

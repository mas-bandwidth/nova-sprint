package sprintdash

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The logo directory (Server.LogoDir): the logo files the live page's Python server.py
// read beside the page, read the same way, so the page's logo slot and its favicon are the
// same markup and the same images. In order:
//   - logo.svg: its drawing is put in the title (32 px, currentColor) and served as
//     /favicon.svg;
//   - else a tile (logo-robot.webp, else logo-stella.png), an icon already: its 192 and
//     384 px copies (<stem>-192.png, <stem>-384.png, made with sips when missing or older
//     than the tile) are the title's image and the favicon, /logo-tile-192.png and
//     /logo-tile-384.png;
//   - else a photo on white (logo.webp, else logo.png): keyed with ffmpeg into
//     logo-icon.png (the title, 144 px tall) and favicon.png (64 px square),
//     /logo-icon.png and /favicon.png;
//   - else nothing: the slot and the favicon render nothing.
//
// A copy is remade when its time is not the source's (each copy takes its source's time).
// /logo.webp and /logo.png are served as they are when they exist. Every file is read on
// each request, so a new logo shows on the next page load with no restart.

// The logo files, as server.py named them.
var (
	logoTiles   = []string{"logo-robot.webp", "logo-stella.png"}
	logoKeyed   = []string{"logo.webp", "logo.png"}
	logoRasters = append(append([]string{}, logoTiles...), logoKeyed...)
	tileSizes   = []int{192, 384}
)

// logoCrop trims the photo's white margins before it is keyed (ffmpeg's crop=w:h:x:y),
// server.py's LOGO_CROP default: it fits the photo logo the dashboard has.
const logoCrop = "1420:660:90:120"

// faviconStyle colours the SVG favicon for the browser's theme.
const faviconStyle = "<style>svg{color:#121417}@media (prefers-color-scheme:dark){svg{color:#eef0f3}}</style>"

var (
	svgRe     = regexp.MustCompile(`(?is)<svg\b([^>]*)>(.*)</svg>`)
	viewBoxRe = regexp.MustCompile(`viewBox\s*=\s*"([^"]+)"`)
)

func (s *Server) logoPath(name string) string { return filepath.Join(s.LogoDir, name) }

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// logoSVG is logo.svg's viewBox and drawing, false when there is none.
func (s *Server) logoSVG() (viewBox, inner string, ok bool) {
	if s.LogoDir == "" {
		return "", "", false
	}
	b, err := os.ReadFile(s.logoPath("logo.svg"))
	if err != nil {
		return "", "", false
	}
	m := svgRe.FindStringSubmatch(string(b))
	if m == nil {
		return "", "", false
	}
	viewBox = "0 0 32 32"
	if vb := viewBoxRe.FindStringSubmatch(m[1]); vb != nil {
		viewBox = vb[1]
	}
	return viewBox, m[2], true
}

// raster is the raster logo's name, "" when logo.svg exists or there is none.
func (s *Server) raster() string {
	if s.LogoDir == "" || exists(s.logoPath("logo.svg")) {
		return ""
	}
	for _, n := range logoRasters {
		if exists(s.logoPath(n)) {
			return n
		}
	}
	return ""
}

// tileCopy is the path of the tile's size px copy, made when stale; "" when the raster
// logo is no tile, the size is none of tileSizes, or the copy cannot be made.
func (s *Server) tileCopy(size int) string {
	src := s.raster()
	if !slices.Contains(logoTiles, src) || (size != tileSizes[0] && size != tileSizes[1]) {
		return ""
	}
	out := s.logoPath(fmt.Sprintf("%s-%d.png", strings.TrimSuffix(src, filepath.Ext(src)), size))
	in := s.logoPath(src)
	return s.remade(in, out, []string{"/usr/bin/sips", "-s", "format", "png", "-Z", strconv.Itoa(size), in, "--out", out})
}

// keyed is the path of the photo's keyed copy (icon: logo-icon.png, else favicon.png),
// made when stale; "" when the raster logo is no photo or the copy cannot be made.
func (s *Server) keyed(icon bool) string {
	src := s.raster()
	if !slices.Contains(logoKeyed, src) {
		return ""
	}
	out, scale := s.logoPath("favicon.png"), "scale=64:64:force_original_aspect_ratio=decrease:flags=lanczos,pad=64:64:(ow-iw)/2:(oh-ih)/2:color=0x00000000,format=rgba"
	if icon {
		out, scale = s.logoPath("logo-icon.png"), "scale=-1:144:flags=lanczos,format=rgba"
	}
	ffmpeg := s.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	in := s.logoPath(src)
	vf := "crop=" + logoCrop + ",colorkey=0xFFFFFF:0.18:0.12," + scale
	return s.remade(in, out, []string{ffmpeg, "-loglevel", "error", "-y", "-i", in, "-vf", vf, out})
}

// remade is out, remade from in by argv first when it is missing or its time is not in's;
// "" when it cannot be made. The copy takes in's time.
func (s *Server) remade(in, out string, argv []string) string {
	s.derive.Lock()
	defer s.derive.Unlock()
	src, err := os.Stat(in)
	if err != nil {
		return ""
	}
	if o, err := os.Stat(out); err == nil && o.ModTime().Equal(src.ModTime()) {
		return out
	}
	convert := s.Convert
	if convert == nil {
		convert = runTool
	}
	if convert(argv) != nil || os.Chtimes(out, src.ModTime(), src.ModTime()) != nil {
		return ""
	}
	return out
}

// runTool runs an image tool, its output discarded, bounded by 30 s.
func runTool(argv []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

// logoDirSig is the logo directory's part of the build number: each logo file's name,
// time and size, so a new logo reloads an open page.
func (s *Server) logoDirSig() string {
	if s.LogoDir == "" {
		return ""
	}
	var b strings.Builder
	for _, n := range append([]string{"logo.svg"}, logoRasters...) {
		if fi, err := os.Stat(s.logoPath(n)); err == nil {
			fmt.Fprintf(&b, "|%s:%d:%d", n, fi.ModTime().UnixNano(), fi.Size())
		}
	}
	return b.String()
}

// logoDirSlot is the page's logo slot and favicon links from the logo directory, as
// server.py wrote them.
func (s *Server) logoDirSlot(v string) (slot, icon string) {
	if vb, inner, ok := s.logoSVG(); ok {
		return fmt.Sprintf(`<svg id="logo" viewBox="%s" width="32" height="32" fill="currentColor" aria-hidden="true">%s</svg>`, pyEscape(vb), inner),
			`<link rel="icon" type="image/svg+xml" href="/favicon.svg">`
	}
	if s.tileCopy(192) != "" && s.tileCopy(384) != "" {
		slot = fmt.Sprintf(`<img id="logo" class="logo-tile" src="/logo-tile-192.png?v=%s" srcset="/logo-tile-192.png?v=%s 1x, /logo-tile-384.png?v=%s 2x" alt="">`, v, v, v)
		icon = fmt.Sprintf(`<link rel="icon" type="image/png" sizes="192x192" href="/logo-tile-192.png?v=%s">`+
			`<link rel="apple-touch-icon" href="/logo-tile-192.png?v=%s">`+
			`<link rel="preload" as="image" href="/logo-tile-192.png?v=%s" imagesrcset="/logo-tile-192.png?v=%s 1x, /logo-tile-384.png?v=%s 2x">`, v, v, v, v, v)
		return slot, icon
	}
	if s.keyed(true) != "" {
		slot = fmt.Sprintf(`<img id="logo" class="raster" src="/logo-icon.png?v=%s" alt="" height="48">`, v)
		if s.keyed(false) != "" {
			icon = fmt.Sprintf(`<link rel="icon" type="image/png" href="/favicon.png?v=%s">`, v)
		}
	}
	return slot, icon
}

// pyEscape is Python's html.escape: & < > " and ' as entities.
func pyEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;").Replace(s)
}

// serveLogoFile answers a logo path as server.py did: the file, or a 404 that says which
// logo there is none of.
func (s *Server) serveLogoFile(w http.ResponseWriter, path string) {
	notFound := func(why string) { s.sendCode(w, http.StatusNotFound, "text/plain", []byte(why+"\n")) }
	image := func(f string) {
		body, err := os.ReadFile(f)
		if err != nil {
			notFound("not found")
			return
		}
		s.send(w, imageType(body), body)
	}
	switch path {
	case "/logo-tile-192.png", "/logo-tile-384.png":
		size, _ := strconv.Atoi(path[len("/logo-tile-") : len(path)-len(".png")]) // ignored: both paths carry a number
		if f := s.tileCopy(size); f != "" {
			image(f)
			return
		}
		notFound("no tile logo")
	case "/logo-icon.png", "/favicon.png":
		if f := s.keyed(path == "/logo-icon.png"); f != "" {
			image(f)
			return
		}
		notFound("no raster logo")
	case "/logo.webp", "/logo.png":
		f := s.logoPath(path[1:])
		if s.LogoDir == "" || !exists(f) {
			notFound("not found")
			return
		}
		body, err := os.ReadFile(f)
		if err != nil {
			notFound("not found")
			return
		}
		ctype := "image/png"
		if strings.HasSuffix(path, "webp") {
			ctype = "image/webp"
		}
		s.send(w, ctype, body)
	case "/favicon.svg":
		vb, inner, ok := s.logoSVG()
		if !ok {
			notFound("no logo.svg")
			return
		}
		s.send(w, "image/svg+xml", []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s" fill="currentColor">%s%s</svg>`, pyEscape(vb), faviconStyle, inner)))
	}
}

// imageType is an image's media type by its bytes (a tile named .png may be WebP inside).
func imageType(body []byte) string {
	switch {
	case len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP":
		return "image/webp"
	case len(body) >= 8 && string(body[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(body) >= 3 && string(body[:3]) == "\xff\xd8\xff":
		return "image/jpeg"
	}
	return "application/octet-stream"
}

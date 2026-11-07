package main

import (
	"bytes"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitDiffAndRegionsFromFixturePNGs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "fixture-a.png")
	pathB := filepath.Join(dir, "fixture-b.png")
	buildFixturePair(t, pathA, pathB)

	imgA, err := readPNG(pathA)
	if err != nil {
		t.Fatalf("readPNG A: %v", err)
	}
	imgB, err := readPNG(pathB)
	if err != nil {
		t.Fatalf("readPNG B: %v", err)
	}
	domA := []DOMRegion{
		{Label: "Hero: Cost", X: 20, Y: 80, W: 60, H: 30, Text: "$28.62", Structural: false},
		{Label: "Work table header", X: 10, Y: 20, W: 200, H: 24, Text: "waiting ready working review fix merging landed cost", Structural: true},
	}
	domB := []DOMRegion{
		{Label: "Hero: Cost", X: 20, Y: 80, W: 60, H: 30, Text: "$29.10", Structural: false},
		{Label: "Work table header", X: 10, Y: 20, W: 200, H: 24, Text: "status landed", Structural: true},
	}
	res := CompareImages(2000, imgA, imgB, domA, domB, 8, 64)
	wantDiff := 200*24 + 60*30
	if res.DiffPixels != wantDiff || !res.Exceeded {
		t.Fatalf("got DiffPixels=%d Exceeded=%v, want %d true", res.DiffPixels, res.Exceeded, wantDiff)
	}
	if len(res.Regions) != 2 {
		t.Fatalf("got %d regions, want 2: %+v", len(res.Regions), res.Regions)
	}
	line0 := FormatRegionLine(2000, res.Regions[0])
	wantLine0 := "2000: Work table header (x=10, y=20, w=200, h=24, diff_px=4800): A has 'waiting ready working review fix merging landed cost', B has 'status landed'"
	if line0 != wantLine0 {
		t.Fatalf("got line0:\n  %s\nwant:\n  %s", line0, wantLine0)
	}
	report := FormatReport("http://a.local", "http://b.local", []WidthResult{res})
	if !strings.Contains(report, wantLine0) {
		t.Fatalf("report missing expected line:\n%s", report)
	}
}

func buildFixturePair(t *testing.T, pathA, pathB string) {
	t.Helper()
	imgA := image.NewRGBA(image.Rect(0, 0, 240, 140))
	imgB := image.NewRGBA(image.Rect(0, 0, 240, 140))
	fillRect(imgA, image.Rect(0, 0, 240, 140), color.RGBA{R: 13, G: 15, B: 18, A: 255})
	fillRect(imgB, image.Rect(0, 0, 240, 140), color.RGBA{R: 17, G: 19, B: 22, A: 255}) // delta 4 <= channelTol 8
	fillRect(imgA, image.Rect(10, 20, 210, 44), color.RGBA{R: 220, G: 220, B: 230, A: 255})
	fillRect(imgB, image.Rect(10, 20, 210, 44), color.RGBA{R: 40, G: 90, B: 180, A: 255})
	fillRect(imgA, image.Rect(20, 80, 80, 110), color.RGBA{R: 34, G: 179, B: 94, A: 255})
	fillRect(imgB, image.Rect(20, 80, 80, 110), color.RGBA{R: 224, G: 82, B: 82, A: 255})
	if err := writePNG(pathA, imgA); err != nil {
		t.Fatalf("writePNG A: %v", err)
	}
	if err := writePNG(pathB, imgB); err != nil {
		t.Fatalf("writePNG B: %v", err)
	}
}

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func TestUnitToleranceAndUnlabeledClustering(t *testing.T) {
	t.Parallel()
	imgA := image.NewRGBA(image.Rect(0, 0, 100, 100))
	imgB := image.NewRGBA(image.Rect(0, 0, 100, 100))
	fillRect(imgA, image.Rect(0, 0, 100, 100), color.RGBA{R: 20, G: 20, B: 20, A: 255})
	fillRect(imgB, image.Rect(0, 0, 100, 100), color.RGBA{R: 25, G: 25, B: 25, A: 255})
	fillRect(imgB, image.Rect(40, 50, 45, 54), color.RGBA{R: 250, G: 250, B: 250, A: 255}) // 20 px > channelTol
	resPass := CompareImages(1440, imgA, imgB, nil, nil, 8, 25)
	if resPass.Exceeded || resPass.DiffPixels != 20 {
		t.Fatalf("expected pass within pixelTol=25: DiffPixels=%d Exceeded=%v", resPass.DiffPixels, resPass.Exceeded)
	}
	if len(resPass.Regions) != 1 || resPass.Regions[0].Bounds != image.Rect(40, 50, 45, 54) {
		t.Fatalf("unexpected cluster bounds: %+v", resPass.Regions)
	}
}

func TestFunctionalBrowserABShot(t *testing.T) {
	t.Parallel()
	if os.Getenv("ABSHOT_BROWSER_TEST") == "" {
		t.Skip("set ABSHOT_BROWSER_TEST=1 to run headless browser functional test")
	}
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(staticBoardHTML("stream", "status", "waiting", "ready", "working", "review", "fix", "merging", "landed", "cost")))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(staticBoardHTML("stream", "status", "landed")))
	}))
	defer srvB.Close()
	verifyBrowserDiffAndIdentical(t, srvA.URL, srvB.URL)
}

func verifyBrowserDiffAndIdentical(t *testing.T, urlA, urlB string) {
	t.Helper()
	outDiff := filepath.Join(t.TempDir(), "diff")
	var outBuf, errBuf bytes.Buffer
	code := run([]string{
		"--a", urlA, "--b", urlB, "--widths", "1440,2000", "--settle", "150ms", "--out", outDiff,
	}, &outBuf, &errBuf)
	if code != 1 {
		t.Fatalf("expected exit code 1 on differing pages, got %d (stderr: %s)", code, errBuf.String())
	}
	for _, name := range []string{"1440-a.png", "1440-b.png", "1440-diff.png", "2000-a.png", "2000-b.png", "2000-diff.png"} {
		if _, err := readPNG(filepath.Join(outDiff, name)); err != nil {
			t.Fatalf("verify output %s: %v", name, err)
		}
	}
	rep, err := os.ReadFile(filepath.Join(outDiff, "report.md"))
	if err != nil {
		t.Fatalf("read report.md: %v", err)
	}
	wantSub := "A has 'stream status waiting ready working review fix merging landed cost', B has 'stream status landed'"
	if !strings.Contains(string(rep), "2000: Work table header") || !strings.Contains(string(rep), wantSub) {
		t.Fatalf("report.md missing expected Work table header diff:\n%s", string(rep))
	}
	outSame := filepath.Join(t.TempDir(), "same")
	codeSame := run([]string{
		"--a", urlA, "--b", urlA, "--widths", "1440", "--settle", "150ms", "--out", outSame,
	}, &outBuf, &errBuf)
	if codeSame != 0 {
		t.Fatalf("expected exit code 0 on identical pages, got %d (stderr: %s)", codeSame, errBuf.String())
	}
}

func staticBoardHTML(headers ...string) string {
	var cells strings.Builder
	for _, h := range headers {
		cells.WriteString("<div>" + h + "</div>")
	}
	return `<!doctype html><html><head><meta charset="utf-8"><style>
body { margin: 0; background: #0d0f12; color: #eef0f3; font: 24px sans-serif; padding: 24px; }
.top { padding: 12px; }
.panel.streams { margin-top: 24px; border: 1px solid #23272e; }
.row.head { display: flex; gap: 24px; padding: 16px; background: #14171b; }
</style></head><body>
<header class="top">nova-sprint test</header>
<section class="panel streams"><div class="panel-head"><h2>Work</h2></div>
<div id="streams"><div class="row head">` + cells.String() + `</div></div>
</section></body></html>`
}

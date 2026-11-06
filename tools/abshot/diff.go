package main

import (
	"image"
	"image/color"
	"sort"
	"strings"
)

// DOMRegion describes a rendered DOM element's bounding box and visible text.
type DOMRegion struct {
	Label      string `json:"label"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	W          int    `json:"w"`
	H          int    `json:"h"`
	Text       string `json:"text"`
	Structural bool   `json:"structural"`
}

// Rect returns the region's bounding box in page coordinates.
func (r DOMRegion) Rect() image.Rectangle {
	return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
}

// RegionDiff summarizes a differing region between page A and page B.
type RegionDiff struct {
	Label      string
	Bounds     image.Rectangle
	DiffPixels int
	TextA      string
	TextB      string
	Structural bool
}

// WidthResult holds the comparison outcome for a single viewport width.
type WidthResult struct {
	Width       int
	SizeA       image.Point
	SizeB       image.Point
	TotalPixels int
	DiffPixels  int
	ChannelTol  int
	PixelTol    int
	Exceeded    bool
	DiffImage   *image.RGBA
	Regions     []RegionDiff
}

// CompareImages computes the pixel diff and DOM region attributions for one width.
func CompareImages(width int, imgA, imgB image.Image, domA, domB []DOMRegion, channelTol, pixelTol int) WidthResult {
	bA, bB := imgA.Bounds(), imgB.Bounds()
	w := max(bA.Dx(), bB.Dx())
	h := max(bA.Dy(), bB.Dy())
	diffImg := image.NewRGBA(image.Rect(0, 0, w, h))
	mask := make([]bool, w*h)
	diffPixels := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if pixelDiffers(imgA, imgB, x, y, channelTol) {
				mask[y*w+x] = true
				diffPixels++
				diffImg.SetRGBA(x, y, color.RGBA{R: 255, G: 48, B: 80, A: 255})
			} else {
				diffImg.SetRGBA(x, y, dimPixel(imgA.At(bA.Min.X+x, bA.Min.Y+y)))
			}
		}
	}
	regions := buildRegions(w, h, mask, domA, domB)
	return WidthResult{
		Width:       width,
		SizeA:       image.Pt(bA.Dx(), bA.Dy()),
		SizeB:       image.Pt(bB.Dx(), bB.Dy()),
		TotalPixels: w * h,
		DiffPixels:  diffPixels,
		ChannelTol:  channelTol,
		PixelTol:    pixelTol,
		Exceeded:    diffPixels > pixelTol,
		DiffImage:   diffImg,
		Regions:     regions,
	}
}

func pixelDiffers(imgA, imgB image.Image, x, y, tol int) bool {
	bA, bB := imgA.Bounds(), imgB.Bounds()
	if x >= bA.Dx() || y >= bA.Dy() || x >= bB.Dx() || y >= bB.Dy() {
		return true
	}
	r1, g1, bl1, a1 := toRGBA8(imgA.At(bA.Min.X+x, bA.Min.Y+y))
	r2, g2, bl2, a2 := toRGBA8(imgB.At(bB.Min.X+x, bB.Min.Y+y))
	return absDiff(r1, r2) > tol || absDiff(g1, g2) > tol || absDiff(bl1, bl2) > tol || absDiff(a1, a2) > tol
}

func toRGBA8(c color.Color) (int, int, int, int) {
	r, g, b, a := c.RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8), int(a >> 8)
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

func dimPixel(c color.Color) color.RGBA {
	r, g, b, _ := toRGBA8(c)
	gray := (r*77 + g*150 + b*29) >> 8
	v := uint8(18 + gray/3)
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

func buildRegions(w, h int, mask []bool, domA, domB []DOMRegion) []RegionDiff {
	regions, covered := matchNamedRegions(w, h, mask, domA, domB)
	if len(regions) == 0 {
		regions = clusterUnmatchedPixels(w, h, mask, covered, domA, domB)
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].Structural != regions[j].Structural {
			return regions[i].Structural
		}
		if regions[i].Bounds.Min.Y != regions[j].Bounds.Min.Y {
			return regions[i].Bounds.Min.Y < regions[j].Bounds.Min.Y
		}
		return regions[i].Bounds.Min.X < regions[j].Bounds.Min.X
	})
	return regions
}

func matchNamedRegions(w, h int, mask []bool, domA, domB []DOMRegion) ([]RegionDiff, []bool) {
	covered := make([]bool, w*h)
	mapA, mapB, order := indexByLabel(domA, domB)
	canvas := image.Rect(0, 0, w, h)
	var out []RegionDiff
	for _, label := range order {
		rA, okA := mapA[label]
		rB, okB := mapB[label]
		if !okA && !okB {
			continue
		}
		union := unionRect(rA, okA, rB, okB).Intersect(canvas)
		cnt, tight := rectDiffStats(w, mask, covered, union)
		if cnt == 0 {
			continue
		}
		tA, tB := regionText(rA, okA), regionText(rB, okB)
		out = append(out, RegionDiff{
			Label:      label,
			Bounds:     tight,
			DiffPixels: cnt,
			TextA:      truncateText(tA, 140),
			TextB:      truncateText(tB, 140),
			Structural: (rA.Structural || rB.Structural) && tA != tB,
		})
	}
	return out, covered
}

func indexByLabel(domA, domB []DOMRegion) (map[string]DOMRegion, map[string]DOMRegion, []string) {
	mapA := make(map[string]DOMRegion, len(domA))
	mapB := make(map[string]DOMRegion, len(domB))
	seen := make(map[string]bool, len(domA)+len(domB))
	var order []string
	for _, r := range domA {
		if r.Label == "" {
			continue
		}
		if _, ok := mapA[r.Label]; !ok {
			mapA[r.Label] = r
		}
		if !seen[r.Label] {
			seen[r.Label] = true
			order = append(order, r.Label)
		}
	}
	for _, r := range domB {
		if r.Label == "" {
			continue
		}
		if _, ok := mapB[r.Label]; !ok {
			mapB[r.Label] = r
		}
		if !seen[r.Label] {
			seen[r.Label] = true
			order = append(order, r.Label)
		}
	}
	return mapA, mapB, order
}

func unionRect(rA DOMRegion, okA bool, rB DOMRegion, okB bool) image.Rectangle {
	if okA && okB {
		return rA.Rect().Union(rB.Rect())
	}
	if okA {
		return rA.Rect()
	}
	return rB.Rect()
}

func regionText(r DOMRegion, ok bool) string {
	if !ok {
		return "(absent)"
	}
	return strings.TrimSpace(r.Text)
}

func rectDiffStats(w int, mask, covered []bool, rect image.Rectangle) (int, image.Rectangle) {
	if rect.Empty() {
		return 0, image.Rectangle{}
	}
	minX, minY := rect.Max.X, rect.Max.Y
	maxX, maxY, cnt := rect.Min.X, rect.Min.Y, 0
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		row := y * w
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if mask[row+x] {
				covered[row+x] = true
				cnt++
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x+1), max(maxY, y+1)
			}
		}
	}
	if cnt == 0 {
		return 0, image.Rectangle{}
	}
	return cnt, image.Rect(minX, minY, maxX, maxY)
}

func clusterUnmatchedPixels(w, h int, mask, covered []bool, domA, domB []DOMRegion) []RegionDiff {
	const cell = 16
	cw, ch := (w+cell-1)/cell, (h+cell-1)/cell
	grid := buildCellGrid(w, h, cell, cw, ch, mask, covered)
	visited := make([]bool, cw*ch)
	var out []RegionDiff
	for cy := 0; cy < ch; cy++ {
		for cx := 0; cx < cw; cx++ {
			if !grid[cy*cw+cx] || visited[cy*cw+cx] {
				continue
			}
			cRect := floodCluster(cw, ch, cx, cy, grid, visited)
			pxRect := image.Rect(cRect.Min.X*cell, cRect.Min.Y*cell, min(w, cRect.Max.X*cell), min(h, cRect.Max.Y*cell))
			cnt, tight := rectDiffStats(w, mask, covered, pxRect)
			if cnt == 0 {
				continue
			}
			label, tA, tB := lookupDOMText(tight, domA, domB)
			out = append(out, RegionDiff{
				Label:      label,
				Bounds:     tight,
				DiffPixels: cnt,
				TextA:      truncateText(tA, 140),
				TextB:      truncateText(tB, 140),
			})
		}
	}
	return out
}

func buildCellGrid(w, h, cell, cw, ch int, mask, covered []bool) []bool {
	grid := make([]bool, cw*ch)
	for y := 0; y < h; y++ {
		row := y * w
		cy := y / cell
		for x := 0; x < w; x++ {
			if mask[row+x] && !covered[row+x] {
				grid[cy*cw+(x/cell)] = true
			}
		}
	}
	return grid
}

func floodCluster(cw, ch, sx, sy int, grid, visited []bool) image.Rectangle {
	q := []image.Point{{X: sx, Y: sy}}
	visited[sy*cw+sx] = true
	minX, minY, maxX, maxY := sx, sy, sx+1, sy+1
	for len(q) > 0 {
		p := q[len(q)-1]
		q = q[:len(q)-1]
		minX, minY = min(minX, p.X), min(minY, p.Y)
		maxX, maxY = max(maxX, p.X+1), max(maxY, p.Y+1)
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				nx, ny := p.X+dx, p.Y+dy
				if nx >= 0 && nx < cw && ny >= 0 && ny < ch && grid[ny*cw+nx] && !visited[ny*cw+nx] {
					visited[ny*cw+nx] = true
					q = append(q, image.Point{X: nx, Y: ny})
				}
			}
		}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func lookupDOMText(box image.Rectangle, domA, domB []DOMRegion) (string, string, string) {
	lA, tA := bestOverlap(box, domA)
	lB, tB := bestOverlap(box, domB)
	label := lA
	if label == "" {
		label = lB
	}
	if label == "" {
		label = "Unlabeled region"
	}
	return label, tA, tB
}

func bestOverlap(box image.Rectangle, regions []DOMRegion) (string, string) {
	bestArea := 0
	var best DOMRegion
	for _, r := range regions {
		inter := box.Intersect(r.Rect())
		area := inter.Dx() * inter.Dy()
		if area > bestArea {
			bestArea = area
			best = r
		}
	}
	if bestArea == 0 {
		return "", ""
	}
	return best.Label, strings.TrimSpace(best.Text)
}

func truncateText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= limit {
		return s
	}
	return s[:limit-3] + "..."
}

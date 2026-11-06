package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
)

// FormatRegionLine formats a single differing region line for report.md.
func FormatRegionLine(width int, r RegionDiff) string {
	b := r.Bounds
	return fmt.Sprintf(
		"%d: %s (x=%d, y=%d, w=%d, h=%d, diff_px=%d): A has '%s', B has '%s'",
		width, r.Label, b.Min.X, b.Min.Y, b.Dx(), b.Dy(), r.DiffPixels, r.TextA, r.TextB,
	)
}

// FormatReport renders the complete markdown comparison report across all widths.
func FormatReport(urlA, urlB string, results []WidthResult) string {
	ordered := prioritize2000(results)
	var sb strings.Builder
	sb.WriteString("# A/B Screenshot Report\n\n")
	fmt.Fprintf(&sb, "- **A**: `%s`\n", urlA)
	fmt.Fprintf(&sb, "- **B**: `%s`\n", urlB)
	if len(ordered) > 0 {
		fmt.Fprintf(&sb, "- **Tolerance**: per-channel delta <= %d, max differing pixels <= %d\n\n",
			ordered[0].ChannelTol, ordered[0].PixelTol)
	}
	writeSummaryTable(&sb, ordered)
	for _, res := range ordered {
		writeWidthSection(&sb, res)
	}
	return sb.String()
}

func prioritize2000(results []WidthResult) []WidthResult {
	out := make([]WidthResult, 0, len(results))
	for _, r := range results {
		if r.Width == 2000 {
			out = append(out, r)
		}
	}
	for _, r := range results {
		if r.Width != 2000 {
			out = append(out, r)
		}
	}
	return out
}

func writeSummaryTable(sb *strings.Builder, results []WidthResult) {
	sb.WriteString("| Width | Size A | Size B | Diff Pixels | Total Pixels | Diff % | Status |\n")
	sb.WriteString("|---:|---|---|---:|---:|---:|---|\n")
	for _, r := range results {
		pct := 0.0
		if r.TotalPixels > 0 {
			pct = float64(r.DiffPixels) * 100.0 / float64(r.TotalPixels)
		}
		status := "PASS"
		if r.Exceeded {
			status = "DIFF"
		}
		fmt.Fprintf(sb, "| %d | %dx%d | %dx%d | %d | %d | %.2f%% | %s |\n",
			r.Width, r.SizeA.X, r.SizeA.Y, r.SizeB.X, r.SizeB.Y, r.DiffPixels, r.TotalPixels, pct, status)
	}
	sb.WriteString("\n")
}

func writeWidthSection(sb *strings.Builder, r WidthResult) {
	pct := 0.0
	if r.TotalPixels > 0 {
		pct = float64(r.DiffPixels) * 100.0 / float64(r.TotalPixels)
	}
	fmt.Fprintf(sb, "## Width %d\n\n", r.Width)
	fmt.Fprintf(sb, "- **Differing pixels**: %d / %d (%.2f%%, tolerance <= %d px at channel delta <= %d)\n",
		r.DiffPixels, r.TotalPixels, pct, r.PixelTol, r.ChannelTol)
	fmt.Fprintf(sb, "- **Viewport captures**: `%d-a.png` (%dx%d), `%d-b.png` (%dx%d), `%d-diff.png`\n\n",
		r.Width, r.SizeA.X, r.SizeA.Y, r.Width, r.SizeB.X, r.SizeB.Y, r.Width)
	if len(r.Regions) == 0 {
		sb.WriteString("No differing regions beyond anti-aliasing tolerance.\n\n")
		return
	}
	for _, reg := range r.Regions {
		sb.WriteString("- ")
		sb.WriteString(FormatRegionLine(r.Width, reg))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	encErr := png.Encode(f, img)
	closeErr := f.Close()
	if encErr != nil {
		return encErr
	}
	return closeErr
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

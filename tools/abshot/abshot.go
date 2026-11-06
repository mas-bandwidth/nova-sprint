package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type config struct {
	urlA       string
	urlB       string
	widths     []int
	outDir     string
	channelTol int
	pixelTol   int
	settle     time.Duration
	tz         string
	chromeBin  string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "abshot: %v\n", err)
		return 2
	}
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "abshot: mkdir %s: %v\n", cfg.outDir, err)
		return 2
	}
	chromeBin, err := findChrome(cfg.chromeBin)
	if err != nil {
		fmt.Fprintf(stderr, "abshot: %v\n", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	results, anyDiff, err := runAllWidths(ctx, cfg, chromeBin)
	if err != nil {
		fmt.Fprintf(stderr, "abshot: %v\n", err)
		return 2
	}
	report := FormatReport(cfg.urlA, cfg.urlB, results)
	reportPath := filepath.Join(cfg.outDir, "report.md")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		fmt.Fprintf(stderr, "abshot: write report: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, report)
	if anyDiff {
		return 1
	}
	return 0
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet("abshot", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg config
	var widthsRaw string
	fs.StringVar(&cfg.urlA, "a", "", "URL of board A (baseline)")
	fs.StringVar(&cfg.urlB, "b", "", "URL of board B (candidate)")
	fs.StringVar(&widthsRaw, "widths", "1440,2000,2800", "comma-separated viewport widths")
	fs.StringVar(&cfg.outDir, "out", "", "output directory for PNGs and report.md")
	fs.IntVar(&cfg.channelTol, "channel-tol", 8, "per-channel anti-aliasing delta tolerance (0..255)")
	fs.IntVar(&cfg.pixelTol, "pixel-tol", 64, "max differing pixels allowed per width for anti-aliasing")
	fs.DurationVar(&cfg.settle, "settle", 1500*time.Millisecond, "wait duration after page load for 1s refresh to settle")
	fs.StringVar(&cfg.tz, "tz", "America/New_York", "IANA timezone for browser rendering")
	fs.StringVar(&cfg.chromeBin, "chrome", "", "path to headless chrome/chromium binary")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.urlA == "" || cfg.urlB == "" || cfg.outDir == "" {
		return config{}, errors.New("--a, --b, and --out are required")
	}
	widths, err := parseWidths(widthsRaw)
	if err != nil {
		return config{}, err
	}
	cfg.widths = widths
	return cfg, nil
}

func parseWidths(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	var out []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		w, err := strconv.Atoi(p)
		if err != nil || w < 200 || w > 10000 {
			return nil, fmt.Errorf("invalid width %q", p)
		}
		out = append(out, w)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one width is required")
	}
	return out, nil
}

func runAllWidths(ctx context.Context, cfg config, chromeBin string) ([]WidthResult, bool, error) {
	results := make([]WidthResult, 0, len(cfg.widths))
	anyDiff := false
	for _, w := range cfg.widths {
		res, err := runSingleWidth(ctx, cfg, chromeBin, w)
		if err != nil {
			return nil, false, err
		}
		if res.Exceeded {
			anyDiff = true
		}
		results = append(results, res)
	}
	return results, anyDiff, nil
}

func runSingleWidth(ctx context.Context, cfg config, chromeBin string, w int) (WidthResult, error) {
	imgA, domA, err := capturePage(ctx, cfg.urlA, w, cfg.settle, cfg.tz, chromeBin)
	if err != nil {
		return WidthResult{}, fmt.Errorf("width %d page A: %w", w, err)
	}
	imgB, domB, err := capturePage(ctx, cfg.urlB, w, cfg.settle, cfg.tz, chromeBin)
	if err != nil {
		return WidthResult{}, fmt.Errorf("width %d page B: %w", w, err)
	}
	if err := writePNG(filepath.Join(cfg.outDir, fmt.Sprintf("%d-a.png", w)), imgA); err != nil {
		return WidthResult{}, err
	}
	if err := writePNG(filepath.Join(cfg.outDir, fmt.Sprintf("%d-b.png", w)), imgB); err != nil {
		return WidthResult{}, err
	}
	res := CompareImages(w, imgA, imgB, domA, domB, cfg.channelTol, cfg.pixelTol)
	if err := writePNG(filepath.Join(cfg.outDir, fmt.Sprintf("%d-diff.png", w)), res.DiffImage); err != nil {
		return WidthResult{}, err
	}
	return res, nil
}

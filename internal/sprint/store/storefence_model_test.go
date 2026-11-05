package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

func checkStoreFenceModelRecorded(root string) error {
	tlaDir := filepath.Join(root, "tla")
	for _, req := range []string{"StoreFence.tla", "MCStoreFence.tla", "MCStoreFence.cfg", "CASES.tsv", "RUNS.tsv", "COVERAGE.tsv"} {
		if _, err := os.Stat(filepath.Join(tlaDir, req)); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(tlaDir)
	if err != nil {
		return err
	}
	var cfgs []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "MCStoreFence") && strings.HasSuffix(name, ".cfg") {
			cfgs = append(cfgs, name)
		}
	}
	if len(cfgs) == 0 {
		return &os.PathError{Op: "scan", Path: tlaDir, Err: os.ErrNotExist}
	}

	casesBytes, err := os.ReadFile(filepath.Join(tlaDir, "CASES.tsv"))
	if err != nil {
		return err
	}
	type caseRow struct {
		module   string
		kind     string
		property string
		group    string
	}
	casesMap := make(map[string]caseRow)
	for _, line := range strings.Split(strings.TrimSpace(string(casesBytes)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "config\t") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 6 {
			continue
		}
		casesMap[cols[0]] = caseRow{
			module:   cols[1],
			kind:     cols[2],
			property: cols[3],
			group:    cols[5],
		}
	}

	runsBytes, err := os.ReadFile(filepath.Join(tlaDir, "RUNS.tsv"))
	if err != nil {
		return err
	}
	type runRow struct {
		module   string
		status   string
		kind     string
		property string
	}
	runsMap := make(map[string]runRow)
	for _, line := range strings.Split(strings.TrimSpace(string(runsBytes)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "config\t") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 17 {
			continue
		}
		runsMap[cols[0]] = runRow{
			module:   cols[1],
			status:   cols[14],
			kind:     cols[15],
			property: cols[16],
		}
	}

	for _, cfg := range cfgs {
		c, ok := casesMap[cfg]
		if !ok {
			return &storeFenceRecordError{"tla/CASES.tsv: missing row for " + cfg}
		}
		if c.module != "MCStoreFence.tla" {
			return &storeFenceRecordError{"tla/CASES.tsv: " + cfg + " module=" + c.module + ", want MCStoreFence.tla"}
		}
		if c.group != "storefence" {
			return &storeFenceRecordError{"tla/CASES.tsv: " + cfg + " group=" + c.group + ", want storefence"}
		}
		r, ok := runsMap[cfg]
		if !ok {
			return &storeFenceRecordError{"tla/RUNS.tsv: missing row for " + cfg}
		}
		if r.module != "MCStoreFence.tla" {
			return &storeFenceRecordError{"tla/RUNS.tsv: " + cfg + " module=" + r.module + ", want MCStoreFence.tla"}
		}
		if r.status != "PASS" || r.kind != c.kind || r.property != c.property {
			return &storeFenceRecordError{"tla/RUNS.tsv: " + cfg + " has status=" + r.status + " kind=" + r.kind + " property=" + r.property + ", want PASS " + c.kind + " " + c.property}
		}
	}

	cases, err := tlc.LoadCases(root)
	if err != nil {
		return err
	}
	src, err := tlc.SourceAt(root)
	if err != nil {
		return err
	}
	records, err := tlc.ReadRecordsFile(filepath.Join(tlaDir, tlc.RunsFile))
	if err != nil {
		return err
	}
	stale, err := tlc.StaleGroups(src, cases, records)
	if err != nil {
		return err
	}
	if slices.Contains(stale, "storefence") {
		return &storeFenceRecordError{"tla/RUNS.tsv: group storefence has stale records"}
	}

	covBytes, err := os.ReadFile(filepath.Join(tlaDir, "COVERAGE.tsv"))
	if err != nil {
		return err
	}
	foundPendingFencedOp := false
	for _, line := range strings.Split(strings.TrimSpace(string(covBytes)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "area\t") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 6 || cols[0] != "pending-fenced-op" {
			continue
		}
		foundPendingFencedOp = true
		files := strings.Split(cols[1], ",")
		if !slices.Contains(files, "internal/sprint/store/lock.go") {
			return &storeFenceRecordError{"tla/COVERAGE.tsv: pending-fenced-op files missing internal/sprint/store/lock.go"}
		}
		if !slices.Contains(files, "internal/sprint/store/engine.go") {
			return &storeFenceRecordError{"tla/COVERAGE.tsv: pending-fenced-op files missing internal/sprint/store/engine.go"}
		}
		models := strings.Split(cols[2], ",")
		if !slices.Contains(models, "StoreFence") {
			return &storeFenceRecordError{"tla/COVERAGE.tsv: pending-fenced-op models missing StoreFence"}
		}
		desc := cols[5]
		for _, token := range []string{
			"lock.go",
			"takeLock",
			"Relock",
			"engine.go",
			"callerOp",
			"fencedRead",
			"apply",
			"committed",
			"finishOp",
		} {
			if !strings.Contains(desc, token) {
				return &storeFenceRecordError{"tla/COVERAGE.tsv: pending-fenced-op description missing " + token}
			}
		}
	}
	if !foundPendingFencedOp {
		return &storeFenceRecordError{"tla/COVERAGE.tsv: missing pending-fenced-op row"}
	}
	return nil
}

type storeFenceRecordError struct {
	msg string
}

func (e *storeFenceRecordError) Error() string {
	return e.msg
}

func TestStoreFenceModelRecorded(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", "..", ".."))
	if err := checkStoreFenceModelRecorded(root); err != nil {
		t.Fatalf("%v", err)
	}

	// Negative controls: verify checkStoreFenceModelRecorded fails when a
	// required config row is missing in CASES.tsv or RUNS.tsv, or when
	// COVERAGE.tsv omits lock.go or the required function names.
	tmp := t.TempDir()
	tmpTLA := filepath.Join(tmp, "tla")
	if err := os.MkdirAll(tmpTLA, 0o755); err != nil {
		t.Fatal(err)
	}
	srcTLA := filepath.Join(root, "tla")
	entries, err := os.ReadDir(srcTLA)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tla") || strings.HasSuffix(name, ".cfg") || strings.HasSuffix(name, ".tsv") {
			b, err := os.ReadFile(filepath.Join(srcTLA, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tmpTLA, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := checkStoreFenceModelRecorded(tmp); err != nil {
		t.Fatalf("clean copy failed check: %v", err)
	}

	origRuns, err := os.ReadFile(filepath.Join(tmpTLA, "RUNS.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var filteredRuns []string
	for _, line := range strings.Split(string(origRuns), "\n") {
		if !strings.HasPrefix(line, "MCStoreFence.cfg\t") {
			filteredRuns = append(filteredRuns, line)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpTLA, "RUNS.tsv"), []byte(strings.Join(filteredRuns, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when MCStoreFence.cfg is missing from RUNS.tsv")
	}
	if err := os.WriteFile(filepath.Join(tmpTLA, "RUNS.tsv"), origRuns, 0o644); err != nil {
		t.Fatal(err)
	}

	origCov, err := os.ReadFile(filepath.Join(tmpTLA, "COVERAGE.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	brokenCov := strings.ReplaceAll(string(origCov), "internal/sprint/store/lock.go,", "")
	if err := os.WriteFile(filepath.Join(tmpTLA, "COVERAGE.tsv"), []byte(brokenCov), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when internal/sprint/store/lock.go is missing from pending-fenced-op files")
	}
}

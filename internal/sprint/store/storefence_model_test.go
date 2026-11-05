package store

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// storeFenceHoles are the two defects the fence model found: each stays in the
// plan as a case that must violate its named invariant.
var storeFenceHoles = []struct{ cfg, property string }{
	{"MCStoreFenceSharedOpIds.cfg", "NoPartialOutsideFence"},
	{"MCStoreFenceRevivedRefusal.cfg", "AbandonedAppliedNothing"},
}

// readStoreFenceTSV reads a tab-separated ledger by its header row; a line that
// is blank or starts with '#' is skipped.
func readStoreFenceTSV(path string) (header []string, rows [][]string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if header == nil {
			header = cols
			continue
		}
		rows = append(rows, cols)
	}
	if header == nil {
		return nil, nil, fmt.Errorf("%s: no header row", path)
	}
	return header, rows, nil
}

func storeFenceColumns(path string, header []string, want ...string) (map[string]int, error) {
	col := map[string]int{}
	for i, name := range header {
		col[name] = i
	}
	for _, name := range want {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("%s: no %q column", path, name)
		}
	}
	return col, nil
}

// storeFenceCfgs lists the fence configurations in the model directory.
func storeFenceCfgs(tlaDir string) ([]string, error) {
	entries, err := os.ReadDir(tlaDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "MCStoreFence") || !strings.HasSuffix(name, ".cfg") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

// checkStoreFenceModelRecorded is the whole record of the fence model: the
// model, its plan and its records; every configuration's declared outcome; the
// two failing holes kept as cases; each record's fingerprint against the
// configuration and model it names now; and the COVERAGE row that names
// engine.go and lock.go by function.
func checkStoreFenceModelRecorded(root string) error {
	tlaDir := filepath.Join(root, "tla")
	for _, req := range []string{
		"StoreFence.tla", "MCStoreFence.tla", "MCStoreFence.cfg",
		"CASES.tsv", "RUNS.tsv", "COVERAGE.tsv",
	} {
		if _, err := os.Stat(filepath.Join(tlaDir, req)); err != nil {
			return err
		}
	}

	cfgs, err := storeFenceCfgs(tlaDir)
	if err != nil {
		return err
	}
	if !slices.Contains(cfgs, "MCStoreFence.cfg") {
		return fmt.Errorf("tla: MCStoreFence.cfg is the model's control instance and is absent")
	}
	for _, hole := range storeFenceHoles {
		if !slices.Contains(cfgs, hole.cfg) {
			return fmt.Errorf("tla: %s is absent (the fence suite keeps every reversed witness and both holes)", hole.cfg)
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
	runs := make(map[string]tlc.Record, len(records))
	for _, r := range records {
		runs[r.Config] = r
	}

	inPlan := make(map[string]tlc.Case, len(cases))
	for _, c := range cases {
		inPlan[c.Config] = c
	}
	for _, cfg := range cfgs {
		c, ok := inPlan[cfg]
		if !ok {
			return fmt.Errorf("tla/CASES.tsv: missing row for %s", cfg)
		}
		r, ok := runs[cfg]
		if !ok {
			return fmt.Errorf("tla/RUNS.tsv: missing row for %s", cfg)
		}
		if c.Expected == "" || c.Property == "" {
			return fmt.Errorf("tla/CASES.tsv: %s has no declared outcome", cfg)
		}
		if r.Result != "PASS" {
			return fmt.Errorf("tla/RUNS.tsv: %s result=%s, want PASS (the declared outcome was not reached)", cfg, r.Result)
		}
		if r.Module != c.Module || r.Expected != c.Expected || r.Property != c.Property {
			return fmt.Errorf("tla/RUNS.tsv: %s record holds %s %s %s but the plan declares %s %s %s",
				cfg, r.Module, r.Expected, r.Property, c.Module, c.Expected, c.Property)
		}
		// A record is the measurement of these inputs: its fingerprint must be
		// the one the configuration, the module and the plan row hash to now.
		fp, files, err := src.Fingerprint(cfg)
		if err != nil {
			return err
		}
		if r.InputSHA256 != fp || r.InputFiles != files {
			return fmt.Errorf("tla/RUNS.tsv: %s records fingerprint %s over %d files, but its inputs now hash to %s over %d",
				cfg, r.InputSHA256, r.InputFiles, fp, files)
		}
	}

	// The two holes the model found are cases that must fail an invariant; a
	// case that silently starts passing would hide the defect they witness.
	for _, hole := range storeFenceHoles {
		c, ok := inPlan[hole.cfg]
		if !ok || c.Expected != "invariant" || c.Property != hole.property {
			return fmt.Errorf("tla/CASES.tsv: %s must declare invariant %s (the hole it witnesses), got %+v", hole.cfg, hole.property, c)
		}
	}

	return checkStoreFenceCoverage(filepath.Join(tlaDir, "COVERAGE.tsv"))
}

// checkStoreFenceCoverage holds the fence row to the code it models: engine.go
// by the functions the model follows and lock.go by the path it leaves to
// DirtyTickRead, named by function so a moved line cannot stale it.
func checkStoreFenceCoverage(path string) error {
	header, rows, err := readStoreFenceTSV(path)
	if err != nil {
		return err
	}
	col, err := storeFenceColumns(path, header, "machine", "code", "module", "note")
	if err != nil {
		return err
	}
	var row []string
	for _, cols := range rows {
		if len(cols) >= len(header) && cols[col["machine"]] == "pending-fenced-op" {
			row = cols
			break
		}
	}
	if row == nil {
		return fmt.Errorf("%s: missing pending-fenced-op row", path)
	}
	if !strings.Contains(row[col["module"]], "StoreFence") {
		return fmt.Errorf("%s: pending-fenced-op module=%q, want StoreFence", path, row[col["module"]])
	}
	for _, want := range []string{"internal/sprint/store/engine.go"} {
		if !slices.Contains(strings.Split(row[col["code"]], ","), want) {
			return fmt.Errorf("%s: pending-fenced-op code missing %s", path, want)
		}
	}
	note := row[col["note"]]
	for _, token := range []string{
		"engine.go", "callerOp", "fencedRead", "apply", "committed", "finishOp",
		"lock.go", "takeLock", "Relock",
	} {
		if !strings.Contains(note, token) {
			return fmt.Errorf("%s: pending-fenced-op note does not name %s", path, token)
		}
	}
	return nil
}

// copyStoreFenceTree copies the model directory into a scratch root, so a
// negative control changes only its own copy.
func copyStoreFenceTree(t *testing.T, src, dst string) {
	t.Helper()
	from := filepath.Join(src, "tla")
	to := filepath.Join(dst, "tla")
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreFenceModelRecorded(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", "..", ".."))
	if err := checkStoreFenceModelRecorded(root); err != nil {
		t.Fatalf("%v", err)
	}

	// Negative controls: the check must fail when a witness case loses its row,
	// when a witness's configuration changes under its record, when a witness
	// record did not reach its declared outcome, and when the fence row stops
	// naming lock.go.
	tmp := t.TempDir()
	copyStoreFenceTree(t, root, tmp)
	if err := checkStoreFenceModelRecorded(tmp); err != nil {
		t.Fatalf("clean copy failed the check: %v", err)
	}

	casesPath := filepath.Join(tmp, "tla", "CASES.tsv")
	origCases, err := os.ReadFile(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	var withoutHole []string
	for _, line := range strings.Split(string(origCases), "\n") {
		if strings.HasPrefix(line, "MCStoreFenceSharedOpIds.cfg\t") {
			continue
		}
		withoutHole = append(withoutHole, line)
	}
	if err := os.WriteFile(casesPath, []byte(strings.Join(withoutHole, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when a witness case has no CASES row")
	}
	if err := os.WriteFile(casesPath, origCases, 0o644); err != nil {
		t.Fatal(err)
	}

	witnessCfg := filepath.Join(tmp, "tla", "MCStoreFenceBrokenAcquireNoGen.cfg")
	origCfg, err := os.ReadFile(witnessCfg)
	if err != nil {
		t.Fatal(err)
	}
	changedCfg := append(append([]byte(nil), origCfg...), []byte("\n\\* changed under its record\n")...)
	if err := os.WriteFile(witnessCfg, changedCfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when a witness configuration changes under its record")
	}
	if err := os.WriteFile(witnessCfg, origCfg, 0o644); err != nil {
		t.Fatal(err)
	}

	runsPath := filepath.Join(tmp, "tla", "RUNS.tsv")
	origRuns, err := os.ReadFile(runsPath)
	if err != nil {
		t.Fatal(err)
	}
	var failed []string
	for _, line := range strings.Split(string(origRuns), "\n") {
		if strings.HasPrefix(line, "MCStoreFenceBrokenRerunOldEpoch.cfg\t") {
			cols := strings.Split(line, "\t")
			if len(cols) > 14 {
				cols[14] = "FAIL"
			}
			line = strings.Join(cols, "\t")
		}
		failed = append(failed, line)
	}
	if err := os.WriteFile(runsPath, []byte(strings.Join(failed, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when a witness record did not reach its declared outcome")
	}
	if err := os.WriteFile(runsPath, origRuns, 0o644); err != nil {
		t.Fatal(err)
	}

	covPath := filepath.Join(tmp, "tla", "COVERAGE.tsv")
	origCov, err := os.ReadFile(covPath)
	if err != nil {
		t.Fatal(err)
	}
	noLock := strings.Replace(string(origCov), "lock.go takeLock, Relock", "the lock path", 1)
	if noLock == string(origCov) {
		t.Fatal("negative control: pending-fenced-op no longer names lock.go takeLock, Relock")
	}
	if err := os.WriteFile(covPath, []byte(noLock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkStoreFenceModelRecorded(tmp); err == nil {
		t.Fatal("expected failure when the fence row does not name lock.go")
	}
}

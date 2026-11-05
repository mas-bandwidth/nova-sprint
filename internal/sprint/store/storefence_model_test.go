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

// storeFenceWitnessDir holds the fence model's reversed witnesses and its two
// failing holes: every case but the control instance. The plan and records live
// beside the configurations so the whole suite is reproducible from tla/testdata.
const storeFenceWitnessDir = "tla/testdata/storefence"

// storeFenceWitnesses are the witness cases the fence model must keep: six
// reversed witnesses that break one invariant each and three more cases (the
// timing-free control, the shared-operation-id hole and the revived-refusal
// hole) that must stay in the suite.
var storeFenceWitnesses = []string{
	"MCStoreFenceBrokenAbandonApplied.cfg",
	"MCStoreFenceBrokenAcquireNoGen.cfg",
	"MCStoreFenceBrokenReleaseAnyOp.cfg",
	"MCStoreFenceBrokenRepairNoSend.cfg",
	"MCStoreFenceBrokenReplayIgnoresArgs.cfg",
	"MCStoreFenceBrokenRerunOldEpoch.cfg",
	"MCStoreFenceLateWriter.cfg",
	"MCStoreFenceRevivedRefusal.cfg",
	"MCStoreFenceSharedOpIds.cfg",
}

type storeFenceCase struct {
	expected string
	property string
	group    string
}

type storeFenceRun struct {
	result   string
	expected string
	property string
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

// readStoreFenceCases reads a case plan's rows by configuration: the declared
// outcome (expected and property) and the group.
func readStoreFenceCases(path string) (map[string]storeFenceCase, error) {
	header, rows, err := readStoreFenceTSV(path)
	if err != nil {
		return nil, err
	}
	col, err := storeFenceColumns(path, header, "config", "expected", "property", "group")
	if err != nil {
		return nil, err
	}
	cases := make(map[string]storeFenceCase)
	for _, cols := range rows {
		if len(cols) < len(header) {
			return nil, fmt.Errorf("%s: row %q has %d fields, want %d", path, cols[0], len(cols), len(header))
		}
		cases[cols[col["config"]]] = storeFenceCase{
			expected: cols[col["expected"]],
			property: cols[col["property"]],
			group:    cols[col["group"]],
		}
	}
	return cases, nil
}

// readStoreFenceRuns reads a records file's rows by configuration: TLC's result
// and the outcome the record was judged against.
func readStoreFenceRuns(path string) (map[string]storeFenceRun, error) {
	header, rows, err := readStoreFenceTSV(path)
	if err != nil {
		return nil, err
	}
	col, err := storeFenceColumns(path, header, "config", "result", "expected", "property")
	if err != nil {
		return nil, err
	}
	runs := make(map[string]storeFenceRun)
	for _, cols := range rows {
		if len(cols) < len(header) {
			return nil, fmt.Errorf("%s: row %q has %d fields, want %d", path, cols[0], len(cols), len(header))
		}
		runs[cols[col["config"]]] = storeFenceRun{
			result:   cols[col["result"]],
			expected: cols[col["expected"]],
			property: cols[col["property"]],
		}
	}
	return runs, nil
}

// checkStoreFenceCases holds every configuration in cfgs to a plan row and a
// current record that says the outcome the plan declares.
func checkStoreFenceCases(cfgs []string, cases map[string]storeFenceCase, runs map[string]storeFenceRun, casesPath, runsPath string) error {
	for _, cfg := range cfgs {
		c, ok := cases[cfg]
		if !ok {
			return fmt.Errorf("%s: missing row for %s", casesPath, cfg)
		}
		if c.expected == "" || c.property == "" {
			return fmt.Errorf("%s: %s has no declared outcome", casesPath, cfg)
		}
		r, ok := runs[cfg]
		if !ok {
			return fmt.Errorf("%s: missing row for %s", runsPath, cfg)
		}
		if r.result != "PASS" {
			return fmt.Errorf("%s: %s result=%s, want PASS (the declared outcome was not reached)", runsPath, cfg, r.result)
		}
		if r.expected != c.expected || r.property != c.property {
			return fmt.Errorf("%s: %s declares %s %s but its record holds %s %s", runsPath, cfg, c.expected, c.property, r.expected, r.property)
		}
	}
	return nil
}

// storeFenceCfgs lists the fence configurations in dir that the test owns.
func storeFenceCfgs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
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
// two failing holes kept as cases; and the COVERAGE row that names engine.go
// and lock.go by function.
func checkStoreFenceModelRecorded(root string) error {
	tlaDir := filepath.Join(root, "tla")
	witnessDir := filepath.Join(root, storeFenceWitnessDir)
	for _, req := range []string{
		"StoreFence.tla", "MCStoreFence.tla", "MCStoreFence.cfg",
		"CASES.tsv", "RUNS.tsv", "COVERAGE.tsv",
	} {
		if _, err := os.Stat(filepath.Join(tlaDir, req)); err != nil {
			return err
		}
	}
	for _, req := range []string{"CASES.tsv", "RUNS.tsv"} {
		if _, err := os.Stat(filepath.Join(witnessDir, req)); err != nil {
			return err
		}
	}

	mainCfgs, err := storeFenceCfgs(tlaDir)
	if err != nil {
		return err
	}
	if !slices.Contains(mainCfgs, "MCStoreFence.cfg") {
		return fmt.Errorf("tla: MCStoreFence.cfg is the model's control instance and is absent")
	}
	witnessCfgs, err := storeFenceCfgs(witnessDir)
	if err != nil {
		return err
	}
	for _, want := range storeFenceWitnesses {
		if !slices.Contains(witnessCfgs, want) {
			return fmt.Errorf("%s: %s is absent (the fence suite keeps every reversed witness and both holes)", storeFenceWitnessDir, want)
		}
	}

	mainCases, err := readStoreFenceCases(filepath.Join(tlaDir, "CASES.tsv"))
	if err != nil {
		return err
	}
	mainRuns, err := readStoreFenceRuns(filepath.Join(tlaDir, "RUNS.tsv"))
	if err != nil {
		return err
	}
	if err := checkStoreFenceCases(mainCfgs, mainCases, mainRuns, "tla/CASES.tsv", "tla/RUNS.tsv"); err != nil {
		return err
	}

	witnessCases, err := readStoreFenceCases(filepath.Join(witnessDir, "CASES.tsv"))
	if err != nil {
		return err
	}
	witnessRuns, err := readStoreFenceRuns(filepath.Join(witnessDir, "RUNS.tsv"))
	if err != nil {
		return err
	}
	if err := checkStoreFenceCases(witnessCfgs, witnessCases, witnessRuns, storeFenceWitnessDir+"/CASES.tsv", storeFenceWitnessDir+"/RUNS.tsv"); err != nil {
		return err
	}

	// The two holes the model found are cases that must fail an invariant; a
	// case that silently starts passing would hide the defect they witness.
	for _, hole := range []struct{ cfg, property string }{
		{"MCStoreFenceSharedOpIds.cfg", "NoPartialOutsideFence"},
		{"MCStoreFenceRevivedRefusal.cfg", "AbandonedAppliedNothing"},
	} {
		c, ok := witnessCases[hole.cfg]
		if !ok || c.expected != "invariant" || c.property != hole.property {
			return fmt.Errorf("%s: %s must declare invariant %s (the hole it witnesses), got %+v", storeFenceWitnessDir+"/CASES.tsv", hole.cfg, hole.property, c)
		}
	}

	// The control instance's record must be current for this tree, not merely
	// present: a stale record names a fingerprint the merged model no longer has.
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
		return fmt.Errorf("tla/RUNS.tsv: group storefence has stale records")
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

// copyStoreFenceTree copies the ledgers and models the check reads into a
// scratch root, so a negative control changes only its own copy.
func copyStoreFenceTree(t *testing.T, src, dst string) {
	t.Helper()
	for _, rel := range []string{"tla"} {
		if err := filepath.WalkDir(filepath.Join(src, rel), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" && filepath.Dir(path) == filepath.Join(src, "tla") {
					return nil
				}
				if filepath.Dir(path) == filepath.Join(src, "tla") && path != filepath.Join(src, "tla") {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if !(strings.HasSuffix(name, ".tla") || strings.HasSuffix(name, ".cfg") || strings.HasSuffix(name, ".tsv")) {
				return nil
			}
			to := filepath.Join(dst, strings.TrimPrefix(path, src+string(os.PathSeparator)))
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(to, raw, 0o644)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyStoreFenceTreeDir(t, filepath.Join(src, storeFenceWitnessDir), filepath.Join(dst, storeFenceWitnessDir)); err != nil {
		t.Fatal(err)
	}
}

func copyStoreFenceTreeDir(t *testing.T, src, dst string) error {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := copyStoreFenceTreeDir(t, filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
			continue
		}
		raw, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestStoreFenceModelRecorded(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", "..", ".."))
	if err := checkStoreFenceModelRecorded(root); err != nil {
		t.Fatalf("%v", err)
	}

	// Negative controls: the check must fail when a witness case loses its row
	// or its declared outcome, and when the fence row stops naming lock.go.
	tmp := t.TempDir()
	copyStoreFenceTree(t, root, tmp)
	if err := checkStoreFenceModelRecorded(tmp); err != nil {
		t.Fatalf("clean copy failed the check: %v", err)
	}

	casesPath := filepath.Join(tmp, storeFenceWitnessDir, "CASES.tsv")
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

	runsPath := filepath.Join(tmp, storeFenceWitnessDir, "RUNS.tsv")
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

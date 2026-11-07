package enforced

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type treeIndex struct {
	topTests    map[string]bool
	subTests    map[string]bool
	invariants  map[string]bool
	witnesses   map[string]bool
	schemaCheck map[string]bool
}

func loadTree(t *testing.T, root string) *treeIndex {
	t.Helper()
	idx := &treeIndex{
		topTests:    map[string]bool{},
		subTests:    map[string]bool{},
		invariants:  map[string]bool{},
		witnesses:   map[string]bool{},
		schemaCheck: map[string]bool{},
	}

	goBin := "go"
	if gr := runtime.GOROOT(); gr != "" {
		goBin = filepath.Join(gr, "bin", "go")
	}
	cmd := exec.Command(goBin, "test", "-list=^(Test|Fuzz)", "./store", "./card", "./steps", "./policy")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test -list failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Test") || strings.HasPrefix(line, "Fuzz") {
			idx.topTests[line] = true
		}
	}

	for _, pkg := range []string{"store", "card", "steps", "policy"} {
		files, err := filepath.Glob(filepath.Join(root, pkg, "*_test.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", pkg, err)
		}
		for _, f := range files {
			collectStringLiterals(t, f, idx.subTests)
		}
	}
	// TestProducerTable and TestConsumerTable derive subtest names from card
	// Event type names (and "<Event> work" / "<Event> read").
	collectCardEventSubtests(t, filepath.Join(root, "card", "card.go"), idx.subTests)

	loadTLA(t, filepath.Join(root, "tla"), idx)
	loadSchemaChecks(t, filepath.Join(root, "store", "schema.sql"), idx.schemaCheck)
	return idx
}

func collectStringLiterals(t *testing.T, path string, out map[string]bool) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile(path, fset.Base(), len(src))
	s.Init(file, src, nil, 0)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.STRING {
			if unq, err := strconv.Unquote(lit); err == nil {
				out[unq] = true
				out[strings.ReplaceAll(unq, " ", "_")] = true
			}
		}
	}
}

func collectCardEventSubtests(t *testing.T, path string, out map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			name := ts.Name.Name
			out[name] = true
			out[name+"_work"] = true
			out[name+"_read"] = true
			out[name+" work"] = true
			out[name+" read"] = true
		}
	}
}

var (
	reTLAOpDef = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*==`)
	reCFGInv   = regexp.MustCompile(`(?m)^\s*INVARIANT\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
)

func loadTLA(t *testing.T, tlaDir string, idx *treeIndex) {
	t.Helper()
	tlaFiles, err := filepath.Glob(filepath.Join(tlaDir, "*.tla"))
	if err != nil {
		t.Fatalf("glob tla: %v", err)
	}
	defsByMod := map[string]map[string]bool{}
	for _, f := range tlaFiles {
		mod := strings.TrimSuffix(filepath.Base(f), ".tla")
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		defs := map[string]bool{}
		for _, m := range reTLAOpDef.FindAllStringSubmatch(string(b), -1) {
			defs[m[1]] = true
		}
		defsByMod[mod] = defs
	}
	if sr, ok := defsByMod["StoreRecords"]; ok {
		for k := range defsByMod["Store"] {
			sr[k] = true
		}
	}

	cfgFiles, err := filepath.Glob(filepath.Join(tlaDir, "*.cfg"))
	if err != nil {
		t.Fatalf("glob cfg: %v", err)
	}
	for _, cf := range cfgFiles {
		base := strings.TrimSuffix(filepath.Base(cf), ".cfg")
		mod := base
		if strings.HasPrefix(base, "CardMachine") {
			mod = "CardMachine"
		} else if strings.HasPrefix(base, "Steps") {
			mod = "Steps"
		} else if strings.HasPrefix(base, "Store") {
			mod = "Store"
		}
		b, err := os.ReadFile(cf)
		if err != nil {
			t.Fatalf("read %s: %v", cf, err)
		}
		for _, m := range reCFGInv.FindAllStringSubmatch(string(b), -1) {
			inv := m[1]
			if defsByMod[mod][inv] {
				idx.invariants[mod+"."+inv] = true
				idx.invariants[inv] = true
			}
		}
	}

	wConfigs, err := filepath.Glob(filepath.Join(tlaDir, "witnesses", "*.cfg"))
	if err != nil {
		t.Fatalf("glob witnesses: %v", err)
	}
	for _, wc := range wConfigs {
		name := strings.TrimSuffix(filepath.Base(wc), ".cfg")
		wtla := filepath.Join(tlaDir, "witnesses", name+".tla")
		tb, err := os.ReadFile(wtla)
		if err != nil {
			continue
		}
		cb, err := os.ReadFile(wc)
		if err != nil {
			continue
		}
		wDefs := map[string]bool{}
		for _, m := range reTLAOpDef.FindAllStringSubmatch(string(tb), -1) {
			wDefs[m[1]] = true
		}
		invs := reCFGInv.FindAllStringSubmatch(string(cb), -1)
		if len(invs) > 0 {
			allExist := true
			for _, m := range invs {
				if !wDefs[m[1]] {
					allExist = false
				}
			}
			if allExist {
				idx.witnesses[name] = true
			}
		}
	}

	rConfigs, err := filepath.Glob(filepath.Join(tlaDir, "reach", "*.cfg"))
	if err != nil {
		t.Fatalf("glob reach: %v", err)
	}
	for _, rc := range rConfigs {
		name := strings.TrimSuffix(filepath.Base(rc), ".cfg")
		cb, err := os.ReadFile(rc)
		if err != nil {
			continue
		}
		mod := "Store"
		if strings.HasPrefix(name, "Card") {
			mod = "CardMachine"
		}
		invs := reCFGInv.FindAllStringSubmatch(string(cb), -1)
		if len(invs) > 0 {
			allExist := true
			for _, m := range invs {
				if !defsByMod[mod][m[1]] {
					allExist = false
				}
			}
			if allExist {
				idx.witnesses[name] = true
			}
		}
	}
}

var (
	reCreateTable     = regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS sprint\.([a-z_]+)\s*\((.*?)\n\);`)
	reNamedConstraint = regexp.MustCompile(`\bCONSTRAINT\s+([a-z0-9_]+)\s+(?:CHECK|UNIQUE|PRIMARY KEY|FOREIGN KEY)\b`)
	reTrigger         = regexp.MustCompile(`CREATE\s+(?:OR\s+REPLACE\s+)?TRIGGER\s+([a-z0-9_]+)\b`)
	reUniqueIndex     = regexp.MustCompile(`CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)\b`)
	reRaisedCheck     = regexp.MustCompile(`CONSTRAINT\s*=\s*'([a-z0-9_]+)'`)
	reTableFKey       = regexp.MustCompile(`^\s*FOREIGN KEY\s*\(([^)]+)\)\s*REFERENCES\b`)
	reColDef          = regexp.MustCompile(`^\s*"?([a-z_]+)"?\s+(?:integer|bigint|text|boolean|timestamptz|jsonb)\b(.*)$`)
)

func loadSchemaChecks(t *testing.T, schemaPath string, out map[string]bool) {
	t.Helper()
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	sql := string(b)

	for _, m := range reNamedConstraint.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}
	for _, m := range reTrigger.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}
	for _, m := range reUniqueIndex.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}
	for _, m := range reRaisedCheck.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}

	for _, tm := range reCreateTable.FindAllStringSubmatch(sql, -1) {
		table, body := tm[1], tm[2]
		for _, line := range strings.Split(body, "\n") {
			if idx := strings.Index(line, "--"); idx >= 0 {
				line = line[:idx]
			}
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "CONSTRAINT ") || strings.HasPrefix(line, "PRIMARY KEY") {
				continue
			}
			if fm := reTableFKey.FindStringSubmatch(line); len(fm) == 2 {
				cols := strings.Split(fm[1], ",")
				for i := range cols {
					cols[i] = strings.Trim(strings.TrimSpace(cols[i]), `"`)
				}
				out[table+"_"+strings.Join(cols, "_")+"_fkey"] = true
				continue
			}
			if cm := reColDef.FindStringSubmatch(line); len(cm) == 3 {
				col, rest := cm[1], cm[2]
				if strings.Contains(rest, "CHECK") {
					out[table+"_"+col+"_check"] = true
				}
				if strings.Contains(rest, "UNIQUE") {
					out[table+"_"+col+"_key"] = true
				}
			}
		}
	}
}

var (
	reCeiling     = regexp.MustCompile(`(?m)^Nothing ceiling:\s*(\d+)\s*$`)
	reTotalCount  = regexp.MustCompile(`(?m)^Total rules audited:\s*(\d+)\s*$`)
	reModelCount  = regexp.MustCompile(`(?m)^-\s*held by a model:\s*(\d+)\s*$`)
	reTestCount   = regexp.MustCompile(`(?m)^-\s*held by a test:\s*(\d+)\s*$`)
	reSchemaCount = regexp.MustCompile(`(?m)^-\s*held by the schema:\s*(\d+)\s*$`)
	reNothingCnt  = regexp.MustCompile(`(?m)^-\s*held by nothing:\s*(\d+)\s*$`)
	reRemediation = regexp.MustCompile("(?m)^-\\s*`([A-Z0-9-]+)`:\\s+(.+)$")
)

func verifyEnforced(doc string, idx *treeIndex) []string {
	var errs []string
	cm := reCeiling.FindStringSubmatch(doc)
	if len(cm) != 2 {
		return []string{"missing 'Nothing ceiling: <N>' header at top of docs/ENFORCED.md"}
	}
	ceiling, err := strconv.Atoi(cm[1])
	if err != nil {
		return []string{fmt.Sprintf("invalid Nothing ceiling: %v", err)}
	}

	seenIDs := map[string]bool{}
	nothingIDs := map[string]bool{}
	var totalRows, modelRows, testRows, schemaRows, nothingRows int

	for lineNo, raw := range strings.Split(doc, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		cells := strings.Split(line[1:len(line)-1], "|")
		if len(cells) != 4 {
			continue
		}
		id := strings.TrimSpace(cells[0])
		holdersRaw := strings.TrimSpace(cells[3])
		if id == "ID" || strings.HasPrefix(id, ":---") || strings.HasPrefix(id, "---") {
			continue
		}
		if id == "" {
			errs = append(errs, fmt.Sprintf("line %d: empty rule ID", lineNo+1))
			continue
		}
		if seenIDs[id] {
			errs = append(errs, fmt.Sprintf("line %d: duplicate rule ID %q", lineNo+1, id))
		}
		seenIDs[id] = true
		totalRows++

		if holdersRaw == "`nothing`" || holdersRaw == "nothing" {
			nothingRows++
			nothingIDs[id] = true
			continue
		}

		parts := strings.Split(holdersRaw, ",")
		var hasModel, hasTest, hasSchema bool
		for _, p := range parts {
			tok := strings.TrimSpace(p)
			if !strings.HasPrefix(tok, "`") || !strings.HasSuffix(tok, "`") || len(tok) < 3 {
				errs = append(errs, fmt.Sprintf("line %d (%s): holder token %q must be backticked", lineNo+1, id, tok))
				continue
			}
			inner := strings.TrimSpace(tok[1 : len(tok)-1])
			kind, arg, ok := strings.Cut(inner, " ")
			arg = strings.TrimSpace(arg)
			if !ok || arg == "" {
				errs = append(errs, fmt.Sprintf("line %d (%s): malformed holder token %q", lineNo+1, id, inner))
				continue
			}
			switch kind {
			case "invariant":
				hasModel = true
				if !idx.invariants[arg] {
					errs = append(errs, fmt.Sprintf("line %d (%s): missing TLA+ invariant %q", lineNo+1, id, arg))
				}
			case "witness":
				hasModel = true
				if !idx.witnesses[arg] {
					errs = append(errs, fmt.Sprintf("line %d (%s): missing TLA+ witness %q", lineNo+1, id, arg))
				}
			case "test":
				hasTest = true
				top, sub, hasSub := strings.Cut(arg, "/")
				if !idx.topTests[top] {
					errs = append(errs, fmt.Sprintf("line %d (%s): missing top-level test %q (from %q)", lineNo+1, id, top, arg))
				} else if hasSub && !idx.subTests[sub] {
					errs = append(errs, fmt.Sprintf("line %d (%s): missing subtest scenario %q in *_test.go (from %q)", lineNo+1, id, sub, arg))
				}
			case "check":
				hasSchema = true
				if !idx.schemaCheck[arg] {
					errs = append(errs, fmt.Sprintf("line %d (%s): missing schema check/trigger %q in store/schema.sql", lineNo+1, id, arg))
				}
			default:
				errs = append(errs, fmt.Sprintf("line %d (%s): unknown holder kind %q in %q", lineNo+1, id, kind, inner))
			}
		}
		if !hasModel && !hasTest && !hasSchema {
			errs = append(errs, fmt.Sprintf("line %d (%s): row has no valid holders", lineNo+1, id))
		}
		if hasModel {
			modelRows++
		}
		if hasTest {
			testRows++
		}
		if hasSchema {
			schemaRows++
		}
	}

	if totalRows == 0 {
		errs = append(errs, "no ledger rows parsed from docs/ENFORCED.md")
	}
	if nothingRows > ceiling {
		errs = append(errs, fmt.Sprintf("nothing rows (%d) exceed Nothing ceiling (%d)", nothingRows, ceiling))
	}

	checkSummary := func(label string, re *regexp.Regexp, want int) {
		m := re.FindStringSubmatch(doc)
		if len(m) != 2 {
			errs = append(errs, fmt.Sprintf("missing summary line for %s", label))
			return
		}
		got, err := strconv.Atoi(m[1])
		if err != nil || got != want {
			errs = append(errs, fmt.Sprintf("summary %s records %s, but ledger has %d", label, m[1], want))
		}
	}
	checkSummary("Total rules audited", reTotalCount, totalRows)
	checkSummary("held by a model", reModelCount, modelRows)
	checkSummary("held by a test", reTestCount, testRows)
	checkSummary("held by the schema", reSchemaCount, schemaRows)
	checkSummary("held by nothing", reNothingCnt, nothingRows)

	remediated := map[string]bool{}
	for _, m := range reRemediation.FindAllStringSubmatch(doc, -1) {
		id := m[1]
		remediated[id] = true
		if !nothingIDs[id] {
			errs = append(errs, fmt.Sprintf("Nothing Rows section lists %q, which is not a nothing row", id))
		}
	}
	for id := range nothingIDs {
		if !remediated[id] {
			errs = append(errs, fmt.Sprintf("nothing row %q is missing from 'Nothing Rows — What Would Hold Each'", id))
		}
	}

	return errs
}

func TestEnforcedLedger(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	idx := loadTree(t, root)
	docBytes, err := os.ReadFile(filepath.Join(root, "docs", "ENFORCED.md"))
	if err != nil {
		t.Fatalf("read docs/ENFORCED.md: %v", err)
	}
	errs := verifyEnforced(string(docBytes), idx)
	for _, e := range errs {
		t.Errorf("%s", e)
	}
}

func TestEnforcedRejectsBrokenLedger(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	idx := loadTree(t, root)
	docBytes, err := os.ReadFile(filepath.Join(root, "docs", "ENFORCED.md"))
	if err != nil {
		t.Fatalf("read docs/ENFORCED.md: %v", err)
	}
	baseDoc := string(docBytes)

	cases := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{
			name: "missing_top_level_test",
			mutate: func(s string) string {
				return strings.Replace(s, "`test TestFunctionalLoad/build`", "`test TestDoesNotExistAnywhere/build`", 1)
			},
			wantErr: `missing top-level test "TestDoesNotExistAnywhere"`,
		},
		{
			name: "missing_subtest_scenario",
			mutate: func(s string) string {
				return strings.Replace(s, "`test TestProducerTable/Resolve`", "`test TestProducerTable/NoSuchSubtestScenario`", 1)
			},
			wantErr: `missing subtest scenario "NoSuchSubtestScenario"`,
		},
		{
			name: "missing_tla_invariant",
			mutate: func(s string) string {
				return strings.Replace(s, "`invariant Store.SingleStateSingleHolder`", "`invariant Store.NoSuchInvariant`", 1)
			},
			wantErr: `missing TLA+ invariant "Store.NoSuchInvariant"`,
		},
		{
			name: "missing_tla_witness",
			mutate: func(s string) string {
				return strings.Replace(s, "`witness StoreProducerHolds`", "`witness StoreNoSuchWitness`", 1)
			},
			wantErr: `missing TLA+ witness "StoreNoSuchWitness"`,
		},
		{
			name: "missing_schema_check",
			mutate: func(s string) string {
				return strings.Replace(s, "`check cards_state_check`", "`check cards_nonexistent_check`", 1)
			},
			wantErr: `missing schema check/trigger "cards_nonexistent_check"`,
		},
		{
			name: "nothing_exceeds_ceiling",
			mutate: func(s string) string {
				return strings.Replace(s, "Nothing ceiling: 20", "Nothing ceiling: 19", 1)
			},
			wantErr: "nothing rows (20) exceed Nothing ceiling (19)",
		},
		{
			name: "summary_count_mismatch",
			mutate: func(s string) string {
				return regexp.MustCompile(`(?m)^- held by a test: \d+$`).ReplaceAllString(s, "- held by a test: 999")
			},
			wantErr: "summary held by a test records 999, but ledger has",
		},
		{
			name: "missing_nothing_remediation",
			mutate: func(s string) string {
				return strings.Replace(s, "- `L2-U17`:", "- `L2-U99`:", 1)
			},
			wantErr: `nothing row "L2-U17" is missing`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := verifyEnforced(tc.mutate(baseDoc), idx)
			matched := false
			for _, e := range errs {
				if strings.Contains(e, tc.wantErr) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, errs)
			}
		})
	}
}

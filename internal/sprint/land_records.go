package sprint

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
)

// The lander's run-record check (docs/SPEC-SPRINT.md section 7, the lander's checks; tla/README.md,
// "Refreshing the records after a model edit"): a merge that edits a model (tla/*.tla) or a
// configuration (tla/*.cfg) lands only with a current record in tla/RUNS.tsv for every case
// whose inputs it edits, written by `make tlc` on a bench and merged by `tlacheck merge`, never
// by hand. A case's inputs are what its TLC run reads (tlc.Source.Inputs), so an edit to a
// module another case extends touches that case too. A record is current when it names the
// fingerprint the merged tree gives the case (tlc.StaleGroups). Each case without one is a
// refusal with one line naming the case, its group and the command that makes the record.
// A tree with no tla/CASES.tsv holds no models and is not checked.

// recordsRefusals is the check of a merge in the clone dir whose diff against the batch
// branch's tip is diff: the case plan is held to the merged tree by tlacheck's loader
// (tlc.LoadCases), and a case the merge touches with no current record is refused. A plan
// edit alone is enough to hold a tree that carries models (some MC*.cfg) to its plan.
// RepairMerge calls it, so the lander refuses with it as with a document's fault.
func recordsRefusals(dir, diff string) ([]DocFix, error) {
	edited := map[string]bool{}
	planEdited := false
	for _, f := range diffcheck.Parse(diff) {
		for _, p := range []string{f.Old, f.New} {
			if modelFile(p) {
				edited[p] = true
			}
			if p == "tla/"+tlc.CasesFile {
				planEdited = true
			}
		}
	}
	if len(edited) == 0 && !planEdited {
		return nil, nil
	}
	tla := filepath.Join(dir, "tla")
	plan, err := os.ReadFile(filepath.Join(tla, tlc.CasesFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A tree that carries no configuration (no MC*.cfg) holds no models, as one with no
	// tla/CASES.tsv holds none: a tla/CASES.tsv edit alone holds only a tree that carries
	// models to its plan (docs/SPEC-SPRINT.md section 7, "The run records").
	if len(edited) == 0 {
		cfgs, _ := filepath.Glob(filepath.Join(tla, "MC*.cfg"))
		if len(cfgs) == 0 {
			return nil, nil
		}
	}
	runsFile := "tla/" + tlc.RunsFile
	cases, err := tlc.ParseCases(bytes.NewReader(plan))
	if err != nil {
		return []DocFix{{File: "tla/" + tlc.CasesFile, Line: 1, What: "cannot be read, so no edited model's run record can be checked: " + err.Error()}}, nil
	}
	// the loader holds the plan to the tree, not just to its own fields (docs/SPEC-SPRINT.md
	// section 7, "The run records")
	if _, err := tlc.LoadCases(dir); err != nil {
		return []DocFix{{File: "tla/" + tlc.CasesFile, Line: 1, What: "the case plan does not match the tree: " + err.Error() +
			"; add the missing row (config, module, expected pass or the reversed witness's kind and property, deadlock, group, gate, debt) or take the file out, then run make tlc TLC_GROUP=<group> (tla/README.md)"}}, nil
	}
	runner, err := treeRunner(dir)
	if err != nil {
		return nil, err
	}
	src := tlc.Source{TLADir: tla, Plan: plan, Runner: runner}
	rawRuns, err := os.ReadFile(filepath.Join(tla, tlc.RunsFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	records, err := tlc.ReadRecords(bytes.NewReader(rawRuns))
	if err != nil && len(rawRuns) > 0 {
		return []DocFix{{File: runsFile, Line: 1, What: "cannot be read, so no edited model's run record can be checked: " + err.Error()}}, nil
	}
	rows := recordLines(rawRuns)
	var out []DocFix
	for _, c := range cases {
		touched, why := caseTouched(src, c, edited)
		if len(touched) == 0 {
			continue
		}
		line, ok := rows[c.Config]
		if !ok {
			line = len(rows) + 2 // where the owed row goes: after the header and every row
		}
		if why == "" {
			stale, err := tlc.StaleGroups(src, []tlc.Case{c}, records)
			if err != nil {
				why = err.Error()
			} else if len(stale) == 0 {
				continue
			}
		}
		out = append(out, DocFix{File: runsFile, Line: line, What: recordOwed(c, touched, why)})
	}
	return out, nil
}

// recordOwed is the one line a case without a current run record is refused with: the case,
// its group, the inputs of it the merge edited, and the command that writes the record.
func recordOwed(c tlc.Case, files []string, why string) string {
	s := fmt.Sprintf("has no current run record for the case %s (group %s) and the change edits %s", c.Config, c.Group, strings.Join(files, ", "))
	if why != "" {
		s += " (" + why + ")"
	}
	return s + fmt.Sprintf("; run make tlc TLC_GROUP=%s on a TLC bench, merge its RUNS.tsv with tlacheck merge --keep tla/RUNS.tsv and commit it (tla/README.md)", c.Group)
}

// modelFile says p is a model or a configuration under tla/.
func modelFile(p string) bool {
	return path.Dir(p) == "tla" && (path.Ext(p) == ".tla" || path.Ext(p) == ".cfg")
}

// caseTouched is the case's inputs the merge edited, in path order, none when it edited
// none. A case whose inputs cannot be read is touched when the merge edited its
// configuration or its module, and why is the error.
func caseTouched(src tlc.Source, c tlc.Case, edited map[string]bool) (touched []string, why string) {
	inputs, err := src.Inputs(c.Config)
	if err != nil {
		for _, f := range []string{"tla/" + c.Config, "tla/" + c.Module} {
			if edited[f] {
				touched = append(touched, f)
			}
		}
		if len(touched) == 0 {
			return nil, ""
		}
		return touched, err.Error()
	}
	for _, in := range inputs {
		if edited[in.Path] {
			touched = append(touched, in.Path)
		}
	}
	return touched, ""
}

// treeRunner is the runner's result files as the merged tree holds them, so the fingerprint
// is the one the tree's own tlacheck computes; a tree with no pkg/tlc takes the bytes
// this binary was built with.
func treeRunner(dir string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range tlc.ResultFiles {
		p := tlc.RunnerDir + "/" + name
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if errors.Is(err, fs.ErrNotExist) {
			return tlc.RunnerFiles()
		}
		if err != nil {
			return nil, err
		}
		out[p] = raw
	}
	return out, nil
}

// recordLines is the line (1-based) of each case's row in a RUNS.tsv.
func recordLines(raw []byte) map[string]int {
	out := map[string]int{}
	for i, l := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if i == 0 || l == "" {
			continue
		}
		config, _, _ := strings.Cut(l, "\t")
		out[config] = i + 1
	}
	return out
}

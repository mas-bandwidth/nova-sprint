package tlc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LoadCases holds the plan to the tree (docs/SPEC-SPRINT.md section 7, the lander's
// checks): when the tree's MC*.cfg set and the plan's configs differ, or a row's module
// is absent, its error names every file, in name order, after the opening words the
// lander and tlacheck match on.
func TestLoadCasesNamesTheFilesThePlanDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tla")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"CASES.tsv": "config\tmodule\texpected\tproperty\tdeadlock\tgroup\tgate\tdebt\n" +
			"MCA.cfg\tMCA.tla\tpass\t-\tcheck\talpha\trequired\t-\n" +
			"MCBar.cfg\tMCMissing.tla\tpass\t-\tcheck\tbeta\trequired\t-\n",
		"MCFoo.cfg": "INIT Init\nNEXT Next\n",
		"MCA.cfg":   "INIT Init\nNEXT Next\n",
		"MCA.tla":   "---- MODULE MCA ----\n====\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, err := LoadCases(root)
	if err == nil {
		t.Fatal("LoadCases accepted a plan that does not match the tree")
	}
	want := "CASES.tsv must name every MC*.cfg exactly once: " +
		"MCBar.cfg is in CASES.tsv but has no file; " +
		"MCBar.cfg names a module MCMissing.tla that has no file; " +
		"MCFoo.cfg is under tla/ but not in CASES.tsv"
	if err.Error() != want {
		t.Fatalf("LoadCases error:\n got %q\nwant %q", err.Error(), want)
	}
	if !strings.HasPrefix(err.Error(), "CASES.tsv must name every MC*.cfg exactly once") {
		t.Fatalf("the opening words changed: %q", err.Error())
	}
}

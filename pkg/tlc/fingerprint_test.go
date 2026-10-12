package tlc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckedFilesListsTheResultAndInputListFiles: CheckedFiles is ResultFiles
// plus InputListFiles.
func TestCheckedFilesListsTheResultAndInputListFiles(t *testing.T) {
	t.Parallel()
	want := append(append([]string{}, ResultFiles...), InputListFiles...)
	got := CheckedFiles()
	if !equalStrings(got, want) {
		t.Fatalf("CheckedFiles()=%v want %v", got, want)
	}
}

// TestCheckRunnerAcceptsMatchingRunnerFiles: a root whose runner files are the
// ones this binary was built from passes.
func TestCheckRunnerAcceptsMatchingRunnerFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(RunnerDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range CheckedFiles() {
		raw, err := sources.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CheckRunner(root); err != nil {
		t.Fatalf("CheckRunner: %v", err)
	}
}

// TestCheckRunnerNoticesAChangedRunnerFile: a changed runner file is refused,
// naming the file.
func TestCheckRunnerNoticesAChangedRunnerFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(RunnerDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := sources.ReadFile("outcome.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outcome.go"), append(raw, []byte("\n// changed\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	err = CheckRunner(root)
	if err == nil {
		t.Fatal("CheckRunner accepted a changed runner file")
	}
	if !strings.Contains(err.Error(), "outcome.go") {
		t.Fatalf("CheckRunner error does not name the file: %v", err)
	}
}

// TestCheckRunnerNoopsWithoutARunnerDir: a root with no pkg/tlc has nothing to
// check.
func TestCheckRunnerNoopsWithoutARunnerDir(t *testing.T) {
	t.Parallel()
	if err := CheckRunner(t.TempDir()); err != nil {
		t.Fatalf("CheckRunner on an empty root: %v", err)
	}
}

// TestFingerprintNoticesAChangedModule: editing a case's module changes its
// fingerprint, so the group it belongs to becomes stale.
func TestFingerprintNoticesAChangedModule(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fresh := currentRecord(t, src, c)

	path := filepath.Join(root, "tla", c.Module)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\n\\* edited\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	fp, _, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}
	if fp == fresh.InputSHA256 {
		t.Fatal("editing a module did not change the fingerprint")
	}
	stale, err := StaleGroups(src, cases, []Record{fresh})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0] != c.Group {
		t.Fatalf("StaleGroups after editing a module = %v, want [%s]", stale, c.Group)
	}
}

// TestFingerprintNoticesAChangedConfig: editing a case's configuration changes
// its fingerprint.
func TestFingerprintNoticesAChangedConfig(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fp0, _, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tla", c.Config)
	if err := os.WriteFile(path, []byte("SPECIFICATION Spec\nCONSTANT N = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp1, _, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp0 {
		t.Fatal("editing a configuration did not change the fingerprint")
	}
}

// TestFingerprintNoticesAnAddedModule: adding a module the case extends covers a
// new input and changes its fingerprint.
func TestFingerprintNoticesAnAddedModule(t *testing.T) {
	t.Parallel()
	root, cases, src := recordsPlan(t)
	c := cases[0]
	fp0, files0, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}

	modulePath := filepath.Join(root, "tla", c.Module)
	raw, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	header, body, _ := strings.Cut(string(raw), "\n")
	edited := header + "\nEXTENDS AddedModule\n" + body
	if err := os.WriteFile(modulePath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tla", "AddedModule.tla"),
		[]byte("---- MODULE AddedModule ----\nVARIABLE y\n====\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fp1, files1, err := src.Fingerprint(c.Config)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 == fp0 {
		t.Fatal("adding a module did not change the fingerprint")
	}
	if files1 <= files0 {
		t.Fatalf("adding a module covered %d inputs, want more than %d", files1, files0)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

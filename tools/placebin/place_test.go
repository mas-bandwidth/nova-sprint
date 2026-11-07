package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// TestPlaceReplacesNeverOverwrites: the destination is a different file
// afterwards (a rename of a new file, not a write into the old one), the
// old file keeps its content for whoever holds it open, the new one is
// signed before it is renamed in, and no temporary file stays.
func TestPlaceReplacesNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "bin", "nova-sprint")
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, src, "new")
	write(t, dst, "old")
	old, err := os.Open(dst) // a running service holds its file
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	before, _ := os.Stat(dst)
	var signed string
	sign := func(path string) error {
		signed = path
		if got, _ := os.ReadFile(path); string(got) != "new" || path == dst {
			t.Errorf("signing %s holding %q: not the new file", path, got)
		}
		if got, _ := os.ReadFile(dst); string(got) != "old" {
			t.Errorf("the destination changed before the rename: %q", got)
		}
		return nil
	}
	if err := Place(src, dst, sign); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(dst)
	if os.SameFile(before, after) {
		t.Fatal("the destination is the same file: it was overwritten in place")
	}
	if got, _ := os.ReadFile(dst); string(got) != "new" {
		t.Fatalf("destination holds %q", got)
	}
	if after.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", after.Mode())
	}
	buf := make([]byte, 3)
	if n, _ := old.Read(buf); string(buf[:n]) != "old" {
		t.Fatalf("the open old file holds %q", buf[:n])
	}
	if filepath.Dir(signed) != filepath.Dir(dst) || signed == dst {
		t.Fatalf("signed %s, want a new file beside %s", signed, dst)
	}
	if got := entries(t, filepath.Dir(dst)); !slices.Equal(got, []string{"nova-sprint"}) {
		t.Fatalf("directory holds %v", got)
	}
}

// TestPlaceFailureLeavesTheDestination: a signing that fails, a missing
// source and a missing directory each leave the old file and no new one, and
// the error carries its cause.
func TestPlaceFailureLeavesTheDestination(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "nova-sprint")
	write(t, src, "new")
	write(t, dst, "old")
	boom := errors.New("no identity")
	if err := Place(src, dst, func(string) error { return boom }); !errors.Is(err, boom) || !strings.Contains(err.Error(), "sign") {
		t.Fatalf("sign failure: %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Fatalf("destination holds %q", got)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"nova-sprint", "src"}) {
		t.Fatalf("directory holds %v", got)
	}
	if err := Place(filepath.Join(dir, "missing"), dst, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing source: %v", err)
	}
	if err := Place(src, filepath.Join(dir, "nodir", "x"), nil); err == nil || !strings.Contains(err.Error(), "a new file beside it") {
		t.Fatalf("missing directory: %v", err)
	}
}

// TestCodesignArgv: the signing is ad hoc, of the new file.
func TestCodesignArgv(t *testing.T) {
	if got := codesignArgv("/x/y"); !slices.Equal(got, []string{"codesign", "--force", "--sign", "-", "/x/y"}) {
		t.Fatalf("%v", got)
	}
}

package ci

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSchemaPinCarriesTextLists holds every generate.go of this repository and its
// schemas/README.md to one pinned schema compiler, and that pin to the first one
// carrying text lists and array-root tables. The pseudo-version's middle field is
// the commit time; pins move forward, never back below the text-list compiler.
func TestSchemaPinCarriesTextLists(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root := filepath.Join(cwd, "..", "..")

	pinRe := regexp.MustCompile(`github.com/mas-bandwidth/schema/v2/cmd/schema@(v\S+)`)

	var versions []string
	for _, dir := range []string{"internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "generate.go" {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(src), "\n") {
				if !strings.Contains(line, "//go:generate") {
					continue
				}
				m := pinRe.FindStringSubmatch(line)
				if m == nil {
					t.Fatalf("%s: go:generate line does not name a schema pin: %q", path, line)
				}
				versions = append(versions, m[1])
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	if len(versions) == 0 {
		t.Fatal("found no generate.go pin lines under internal/ or pkg/")
	}
	want := versions[0]
	for _, v := range versions[1:] {
		if v != want {
			t.Fatalf("generate.go pins disagree: want %s, got %s", want, v)
		}
	}

	readme, err := os.ReadFile(filepath.Join(root, "schemas", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	if !strings.Contains(string(readme), want) {
		t.Fatalf("schemas/README.md does not name the pinned version %s", want)
	}

	fields := strings.Split(want, "-")
	if len(fields) < 3 {
		t.Fatalf("pseudo-version %s has no commit-time field", want)
	}
	dateField := fields[1]
	if i := strings.IndexByte(dateField, '.'); i >= 0 {
		dateField = dateField[i+1:]
	}
	if dateField < "20261017153557" {
		t.Fatalf("schema pin %s predates the text-list compiler 20261017153557", want)
	}
}

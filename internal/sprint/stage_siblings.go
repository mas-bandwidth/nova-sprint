package sprint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
)

// Sibling is one repository a brief names in its SIBLINGS header
// (docs/SPEC-CARD-CONTRACT.md, the SIBLINGS header). Name is the repository's own
// name (the part of owner/name@ref after the slash); Ref is the ref to clone at.
type Sibling struct {
	// Name is the repository's own name (the part of owner/name@ref after the slash).
	Name string
	// Path is the owner/name part of the entry, used for the forge URL.
	Path string
	// Ref is the ref to clone at.
	Ref string
}

// ParseSiblings reads the SIBLINGS header of a brief: each comma-separated entry is
// owner/name@ref. Empty when the brief has no header.
func ParseSiblings(brief string) []Sibling {
	var out []Sibling
	for _, line := range strings.Split(brief, "\n") {
		if key, val, ok := cardhdr.KeyValue(line); ok && key == "SIBLINGS" {
			for _, entry := range strings.Split(val, ",") {
				e := strings.TrimSpace(entry)
				if e == "" {
					continue
				}
				s := Sibling{Name: e, Path: e, Ref: e}
				if i := strings.IndexByte(e, '@'); i >= 0 {
					s.Path = e[:i]
					s.Ref = e[i+1:]
				}
				if i := strings.LastIndex(s.Path, "/"); i >= 0 {
					s.Name = s.Path[i+1:]
				}
				out = append(out, s)
			}
		}
	}
	return out
}

// cloneRepo clones src into dst at ref. It is a var so a test can stub it: a
// sibling that cannot be staged must refuse the stage without touching the forge.
var cloneRepo = func(dst, src, ref string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return exec.Command("git", "clone", "--quiet", "--branch", ref, src, dst).Run()
}

// StageSiblings clones each SIBLINGS repository at its ref into $JOB/siblings/<name>,
// from the bench mirror (~/nova-bench/mirror/<name>.git) where one exists, else from
// the forge from outside the wall (docs/SPEC-STAGING.md, the siblings). It returns the
// JOB.md lines naming each sibling (SIBLING_<NAME>=$JOB/siblings/<name>), and refuses
// with the first sibling that cannot be staged, so the card returns to ready (a bench
// fault, not the worker's).
func StageSiblings(job, mirror string, siblings []Sibling) ([]string, error) {
	lines := make([]string, 0, len(siblings))
	for _, s := range siblings {
		name := s.Name
		if name == "" {
			return nil, fmt.Errorf("sibling %q has no name", s.Ref)
		}
		dst := filepath.Join(job, "siblings", name)
		var src string
		if mirror != "" && dirExists(filepath.Join(mirror, name+".git")) {
			src = filepath.Join(mirror, name+".git")
		} else {
			src = "https://github.com/" + s.Path
		}
		if err := cloneRepo(dst, src, s.Ref); err != nil {
			return nil, fmt.Errorf("sibling %s: cannot stage: %w", name, err)
		}
		lines = append(lines, fmt.Sprintf("SIBLING_%s=$JOB/siblings/%s", strings.ToUpper(name), name))
	}
	return lines, nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

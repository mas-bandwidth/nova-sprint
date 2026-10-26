package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// applyHeaderFix applies a brief's corrected PATHS, NEW and SHARED lines to its header only:
// the text before sprint.HeaderEnd. A header line of the key is replaced; a NEW or SHARED
// line the header lacks is inserted after the PATHS line. Nothing at or after the header
// changes. It returns the fixed brief and, per changed line, the old and new line.
func applyHeaderFix(brief string, fix swarm.BriefFix) (string, [][2]string) {
	lines := strings.Split(brief, "\n")
	end := sprint.HeaderEnd(brief)
	have := map[string]bool{}
	paths := -1
	for i := 1; i < end; i++ {
		k, _, ok := cardhdr.KeyValue(lines[i])
		if !ok {
			continue
		}
		have[k] = true
		if k == "PATHS" && paths < 0 {
			paths = i
		}
	}
	var applied [][2]string
	for _, f := range fix {
		k, _, ok := strings.Cut(f, ": ")
		if !ok {
			continue
		}
		switch k {
		case "PATHS", "NEW", "SHARED":
		default:
			continue // never the tier line or any other fix
		}
		if have[k] {
			for i := 1; i < end; i++ {
				if kk, _, ok := cardhdr.KeyValue(lines[i]); ok && kk == k {
					applied = append(applied, [2]string{lines[i], f})
					lines[i] = f
					break
				}
			}
			continue
		}
		at := end - 1
		if paths >= 0 {
			at = paths
		}
		applied = append(applied, [2]string{"", f})
		lines = slices.Insert(lines, at+1, f)
		end++
		have[k] = true
		if k == "PATHS" && paths < 0 {
			paths = at + 1
		}
	}
	return strings.Join(lines, "\n"), applied
}

// fileBrief is one brief of an add read from a file, as --apply-fix fixes it.
type fileBrief struct {
	id, brief, file, stream string
}

// fixWrite is one file a --apply-fix pass has fixed and the checks accepted: written only
// once every check of the add has passed, so a refusal leaves every file as it was.
type fixWrite struct {
	id, file, brief string
	applied         [][2]string
}

// applyFixFiles is add's --apply-fix for the cards of a file-based add: for each brief, it
// lints at its base, applies the corrected PATHS/NEW/SHARED header lines to the header, and
// re-lints the fixed brief. Only a brief whose re-lint is clean is fixed: its corrected
// brief replaces the one the entry carries, and a fixWrite is returned so the file is
// rewritten once the add's checks have accepted the whole call. A brief whose re-lint still
// has a finding is left as it is, for the add checks to refuse.
func (a *app) applyFixFiles(verbName string, st *store.Store, allowPersonal bool, stderr io.Writer, briefs []fileBrief) ([]fixWrite, int) {
	checks := make([]briefCheck, len(briefs))
	for i, b := range briefs {
		checks[i] = briefCheck{id: b.id, brief: b.brief, stream: b.stream}
	}
	env, code := a.baseEnv(verbName, st, stderr, checks)
	if code != 0 {
		return nil, code
	}
	evidence := map[key]swarm.BriefBase{}
	var writes []fixWrite
	for i := range briefs {
		b := &briefs[i]
		bb := env.bb(a, allowPersonal, checks[i], evidence)
		fs, fix := lintAt(b.stream, env.s, b.brief, bb)
		if len(fs) == 0 {
			continue
		}
		fixed, applied := applyHeaderFix(b.brief, fix)
		if len(applied) == 0 {
			continue
		}
		fs2, _ := lintAt(b.stream, env.s, fixed, bb)
		if len(fs2) > 0 {
			continue
		}
		writes = append(writes, fixWrite{id: b.id, file: b.file, brief: fixed, applied: applied})
		b.brief = fixed
	}
	return writes, 0
}

// writeFixWrites rewrites the files a --apply-fix pass fixed, printing one APPLY-FIX pair per
// changed line, after the add's checks have accepted the whole call.
func (a *app) writeFixWrites(verbName string, stderr io.Writer, writes []fixWrite) int {
	for _, w := range writes {
		if err := rewriteBriefFile(w.file, w.brief); err != nil {
			return a.readFailed(verbName, err, stderr)
		}
		for _, p := range w.applied {
			fmt.Fprintf(stderr, "APPLY-FIX card=%s file=%s -%s\n", oneline.Field(w.id), oneline.Escape(w.file), oneline.Escape(p[0]))
			fmt.Fprintf(stderr, "APPLY-FIX card=%s file=%s +%s\n", oneline.Field(w.id), oneline.Escape(w.file), oneline.Escape(p[1]))
		}
	}
	return 0
}

// rewriteBriefFile rewrites path with the fixed brief, preserving the original's trailing
// newline, in place via a temp file in the same directory and a rename.
func rewriteBriefFile(path, fixed string) error {
	orig, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	b := []byte(fixed)
	if bytes.HasSuffix(orig, []byte("\n")) {
		b = append(b, '\n')
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apply-fix-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

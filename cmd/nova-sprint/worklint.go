package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The work lint's binding (docs/SPEC-SPRINT.md section 6, the work lint): every tick this
// process runs holds each finished attempt to sprint.WorkLint before its first read, in
// the clone the lander keeps of the card's repository (land.go, lander.clone). A
// repository the lander keeps no clone of yet is not linted: the attempt is asked as it
// was before the lint, and the lander's first landing of the repository makes the clone.
// rules lists every check of the lint beside the answers (rulesWithLint). This file's
// init runs after verbs.go's, which builds the verbs table it wraps.
func init() {
	sprint.DefaultWorkLint = sprint.NewWorkLinter(sprint.WorkLintGit{Clone: landClone})
	for i := range verbs {
		if verbs[i].name == "rules" {
			verbs[i].run = rulesWithLint(verbs[i].run)
			return
		}
	}
	panic("work lint: no rules verb to list its checks in")
}

// landClone is the clone the lander keeps of repo under its default root, an error when
// it keeps none.
func landClone(repo string) (string, error) {
	root, err := defaultLandRoot()
	if err != nil {
		return "", fmt.Errorf("work lint: no land directory: %w", err)
	}
	dir := filepath.Join(root, repoDirName(repo))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return "", fmt.Errorf("work lint: the lander keeps no clone of %s under %s yet", repo, root)
	}
	return dir, nil
}

// rulesWithLint is rules with the work lint's checks listed (sprint.WorkLintRules, in the
// order they run): one LINT line per check, `LINT <token>: <what it refuses> (remedy:
// <remedy>)`, the token and remedy in the words a finding prints, before the RULES line;
// under --json the object's lint field. A refused rules prints as it did.
func rulesWithLint(run func(*app, []string, io.Writer, io.Writer) int) func(*app, []string, io.Writer, io.Writer) int {
	return func(a *app, args []string, stdout, stderr io.Writer) int {
		var buf bytes.Buffer
		code := run(a, args, &buf, stderr)
		out := buf.String()
		if code != 0 {
			_, _ = io.WriteString(stdout, out)
			return code
		}
		var obj map[string]any
		if json.Unmarshal(buf.Bytes(), &obj) == nil {
			obj["lint"] = sprint.WorkLintRules
			b, _ := json.Marshal(obj)
			fmt.Fprintln(stdout, string(b))
			return 0
		}
		head, last := "", out
		if i := strings.LastIndex(strings.TrimSuffix(out, "\n"), "\n"); i >= 0 {
			head, last = out[:i+1], out[i+1:]
		}
		_, _ = io.WriteString(stdout, head)
		for _, r := range sprint.WorkLintRules {
			fmt.Fprintf(stdout, "LINT %s: %s (remedy: %s)\n", r.Token, r.Refuses, r.Remedy)
		}
		_, _ = io.WriteString(stdout, last)
		return 0
	}
}

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// The work lint's binding (docs/SPEC-SPRINT.md section 6, the work lint): every tick this
// process runs holds each finished attempt to sprint.WorkLint before its first read, in
// the clone the lander keeps of the card's repository (land.go, lander.clone). A
// repository the lander keeps no clone of yet is not linted: the attempt is asked as it
// was before the lint, and the lander's first landing of the repository makes the clone.
func init() {
	sprint.DefaultWorkLint = sprint.NewWorkLinter(sprint.WorkLintGit{Clone: landClone})
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

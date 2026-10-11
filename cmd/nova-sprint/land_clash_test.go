package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// TestTwoGreenHeadsRedTogetherRefuseTheSecondAsAClash verifies the clash detection
// for unit test failures. Two heads, each green alone with different file changes,
// when merged together cause unit tests to fail. The second head should be refused
// with way "clash".
func TestTwoGreenHeadsRedTogetherRefuseTheSecondAsAClash(t *testing.T) {
	// Create a temporary directory for the test repo
	tmpDir, err := os.MkdirTemp("", "land-clash-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// A clone does not inherit the repository config, so each commit made in a clone
	// needs an identity from the environment (the way the package's other git-using
	// tests set it); otherwise git commit fails with exit 128 on a machine without a
	// global git identity (CI, a bench).
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@test.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@test.com")

	// Initialize git repo
	runCmd(t, tmpDir, "git", "init")
	runCmd(t, tmpDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, tmpDir, "git", "config", "user.name", "Test User")

	// Create go.mod
	os.WriteFile(tmpDir+"/go.mod", []byte("module github.com/mas-bandwidth/nova-sprint\n"), 0644)

	// Create internal/sprint directory
	os.MkdirAll(tmpDir+"/internal/sprint", 0755)

	// Create base files
	os.WriteFile(tmpDir+"/internal/sprint/a.go", []byte("package sprint\nconst A = 1\n"), 0644)
	os.WriteFile(tmpDir+"/internal/sprint/b.go", []byte("package sprint\nconst B = 1\n"), 0644)

	// Create test that will fail when A+B >= 4
	os.WriteFile(tmpDir+"/internal/sprint/store_test.go", []byte(`package sprint
import "testing"
func TestAHeldReportCrossesTheStopAndIsTaken(t *testing.T) {
	if A+B >= 4 {
		t.Fatalf("A+B=%d, want <4", A+B)
	}
}
`), 0644)

	// Create cmd/nova-sprint directory
	os.MkdirAll(tmpDir+"/cmd/nova-sprint", 0755)
	os.WriteFile(tmpDir+"/cmd/nova-sprint/main.go", []byte("package main\nimport \"github.com/mas-bandwidth/nova-sprint/internal/sprint\"\nfunc main(){_ = sprint.A}\n"), 0644)

	// Create initial commit
	runCmd(t, tmpDir, "git", "add", ".")
	runCmd(t, tmpDir, "git", "commit", "-m", "base")
	baseSha := runGit(t, tmpDir, "rev-parse", "HEAD")

	// Create head X: set A = 2
	xDir := tmpDir + "/x"
	os.MkdirAll(xDir, 0755)
	runCmd(t, xDir, "git", "clone", tmpDir, ".")
	runCmd(t, xDir, "git", "checkout", "-b", "x")
	os.WriteFile(xDir+"/internal/sprint/a.go", []byte("package sprint\nconst A = 2\n"), 0644)
	runCmd(t, xDir, "git", "add", ".")
	runCmd(t, xDir, "git", "commit", "-m", "head x")

	// Verify X alone is green
	out, err := runGo(t, xDir, "test", "./internal/sprint/")
	if err != nil {
		t.Fatalf("X alone should pass: %v\n%s", err, out)
	}

	// Create head Y: set B = 2 (from same base)
	yDir := tmpDir + "/y"
	os.MkdirAll(yDir, 0755)
	runCmd(t, yDir, "git", "clone", tmpDir, ".")
	runCmd(t, yDir, "git", "checkout", "-b", "y")
	runCmd(t, yDir, "git", "reset", "--hard", baseSha)
	os.WriteFile(yDir+"/internal/sprint/b.go", []byte("package sprint\nconst B = 2\n"), 0644)
	runCmd(t, yDir, "git", "add", ".")
	runCmd(t, yDir, "git", "commit", "-m", "head y")

	// Verify Y alone is green
	out, err = runGo(t, yDir, "test", "./internal/sprint/")
	if err != nil {
		t.Fatalf("Y alone should pass: %v\n%s", err, out)
	}

	// Merge X onto main
	runCmd(t, tmpDir, "git", "fetch", xDir, "x:refs/remotes/x")
	runCmd(t, tmpDir, "git", "merge", "--no-ff", "-m", "merge x", "x")

	// Now merge Y - this creates the clash (both A=2 and B=2)
	runCmd(t, tmpDir, "git", "fetch", yDir, "y:refs/remotes/y")
	runCmd(t, tmpDir, "git", "merge", "--no-ff", "-m", "merge y", "y")

	// Get changed files from Y against base
	changed := runGit(t, tmpDir, "diff", "--name-only", baseSha, "HEAD")

	// Test UnitPackages - Y's changes should give internal/sprint packages
	changedFiles := strings.Split(changed, "\n")
	unitPkgs := sprint.UnitPackages("github.com/mas-bandwidth/nova-sprint", changedFiles)

	// Verify internal/sprint package is in unitPkgs
	found := false
	for _, p := range unitPkgs {
		if p == "internal/sprint" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected internal/sprint in unitPkgs, got %v", unitPkgs)
	}

	// Try running tests with Y's changes applied
	out, err = runGo(t, tmpDir, "test", "./internal/sprint/")
	if err != nil {
		// Expected: tests fail when both changes are present
		// Check if it's a clash (green at base alone, red at merged)
		if !strings.Contains(out, "FAIL") {
			t.Fatalf("expected tests to fail after merge")
		}
	} else {
		t.Fatalf("expected tests to fail after merging both heads")
	}

	// Verify RefusalWay maps clash
	what := "the head " + baseSha[:12] + " of card-id clashes at the merged tip: internal/sprint TestAHeldReportCrossesTheStopAndIsTaken fail on base 12345678 + y 87654321 and pass on each alone"
	if sprint.RefusalWay("", what) != sprint.RefusedClash {
		t.Errorf("RefusalWay should map clash to RefusedClash, got %s", sprint.RefusalWay("", what))
	}
}

func runCmd(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func runGo(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

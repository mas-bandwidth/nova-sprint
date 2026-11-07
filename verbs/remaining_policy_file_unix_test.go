//go:build unix

package verbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestRemainingPolicyRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := (LocalPolicyFiles{}).ReadPolicy(context.Background(), path, 64)
	if err == nil {
		t.Fatal("FIFO accepted")
	}
}

func TestRemainingPolicyBoundedReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.star")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 65537)), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := (LocalPolicyFiles{}).ReadPolicy(context.Background(), path, 16)
	if err != nil || len(data) != 17 {
		t.Fatal(len(data), err)
	}
	_, e := policyFixture(t)
	e.PolicyFiles = LocalPolicyFiles{}
	r := e.policy(context.Background(), []string{"set", path, "--reason", "x"})
	if r.Exit != 1 || !strings.Contains(r.Output, "exceeds 64 KiB") {
		t.Fatal(r)
	}
	if _, err = (LocalPolicyFiles{}).ReadPolicy(context.Background(), path+"missing", 16); err == nil || !errors.As(err, new(*OperationError)) {
		t.Fatal(err)
	}
}

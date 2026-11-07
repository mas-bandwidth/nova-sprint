//go:build linux || darwin

package verbs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallLockCrashHelper(t *testing.T) {
	path := os.Getenv("NOVA_INSTALL_LOCK_TEST_PATH")
	if path == "" {
		return
	}
	lock, err := installLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("locked")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestInstallLockReacquiredAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstallLockCrashHelper$")
	cmd.Env = append(os.Environ(), "NOVA_INSTALL_LOCK_TEST_PATH="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("child did not acquire lock: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child timed out acquiring lock")
	}
	if f, err := installLock(path); err == nil {
		f.Close()
		t.Fatal("concurrent lock succeeded")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	f, err := installLock(path)
	if err != nil {
		t.Fatalf("lock survived process death: %v", err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("stable lock file missing: %v", err)
	}
}

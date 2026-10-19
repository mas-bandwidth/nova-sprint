//go:build functional && !windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gateServerChildEnv marks the child TestGateServerChild starts: in it, a process holds
// a land context and runs one gate whose shim go execs a long sleep.
const gateServerChildEnv = "NOVA_SPRINT_TEST_GATE_SERVER_CHILD"

const gateServerDirEnv = "NOVA_SPRINT_TEST_GATE_DIR"

// TestServerSIGTERMLeavesNoGateProcess: a process holding a land gate, stopped with
// SIGTERM as a launchd restart stops the server, leaves no process of the gate's group
// behind. The gate's shim go writes its pid (its pgid, Setpgid) then execs sleep 300;
// the parent sends SIGTERM, waits 5 s, and wants signal 0 to -pgid to be ESRCH.
func TestServerSIGTERMLeavesNoGateProcess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shim := filepath.Join(dir, "go")
	pidfile := filepath.Join(dir, "gate.pid")
	script := "#!/bin/sh\nsleep 300 &\necho \"$$ $!\" > \"" + pidfile + "\"\nwait\n"
	require.NoError(t, os.WriteFile(shim, []byte(script), 0o755))

	cmd := exec.Command(os.Args[0], "-test.run=^TestGateServerChild$", "-test.count=1")
	cmd.Env = append(envWithout(os.Environ(), []string{gateServerChildEnv, gateServerDirEnv, "PATH"}),
		gateServerChildEnv+"=1", gateServerDirEnv+"="+dir,
		"PATH="+dir+":"+os.Getenv("PATH"))
	var logs strings.Builder
	cmd.Stdout, cmd.Stderr = &logs, &logs
	require.NoError(t, cmd.Start())
	server := cmd.Process.Pid
	var pgid, child int
	t.Cleanup(func() {
		_ = syscall.Kill(server, syscall.SIGKILL) // ignored: it may have exited already
		if pgid > 0 {
			_ = syscall.Kill(-pgid, syscall.SIGKILL) // ignored: the test's own group, stopped at last
			_ = syscall.Kill(pgid, syscall.SIGKILL)  // ignored: or the child's own pid when it never got a group
			_ = syscall.Kill(child, syscall.SIGKILL) // ignored: and the descendant the group holds
		}
	})
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(pidfile)
		if err != nil {
			return false
		}
		_, err = fmt.Sscan(strings.TrimSpace(string(b)), &pgid, &child)
		return err == nil && pgid > 0 && child > 0
	}, 60*time.Second, 50*time.Millisecond, "the gate's shim never wrote its pid; the server's output: %s", &logs)

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	time.Sleep(5 * time.Second)
	if err := syscall.Kill(-pgid, 0); err != syscall.ESRCH {
		t.Fatalf("the gate's process group -%d still exists %v after the server's SIGTERM: %v; the server's output: %s", pgid, 5*time.Second, err, &logs)
	}
	if err := syscall.Kill(pgid, 0); err != syscall.ESRCH {
		t.Fatalf("the gate's child %d still exists %v after the server's SIGTERM: %v; the server's output: %s", pgid, 5*time.Second, err, &logs)
	}
	if err := syscall.Kill(child, 0); err != syscall.ESRCH {
		t.Fatalf("the gate's descendant %d still exists %v after the server's SIGTERM: %v; the server's output: %s", child, 5*time.Second, err, &logs)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, "the server exited: %s", &logs)
	case <-time.After(10 * time.Second):
		t.Fatalf("the server did not exit after SIGTERM: %s", &logs)
	}
}

// TestGateServerChild is the child of TestServerSIGTERMLeavesNoGateProcess: a process
// holding the land context an interrupt cancels, running one gate through the lander's
// goRun over the shim go.
func TestGateServerChild(t *testing.T) {
	if os.Getenv(gateServerChildEnv) == "" {
		t.Skip("the child of TestServerSIGTERMLeavesNoGateProcess; it runs only there")
	}
	ctx, stop := newApp(func(k string) string { return os.Getenv(k) }).notify(context.Background())
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = (&lander{}).goRun(ctx, os.Getenv(gateServerDirEnv), []string{"go", "test", "-count=1", "./..."})
	}()
	<-ctx.Done()
	<-done
}

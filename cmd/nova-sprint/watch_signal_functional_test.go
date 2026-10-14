//go:build functional && !windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// interruptChildEnv marks the child process TestTheInterruptOfTheCommandIsSIGINTAndSIGTERM
// starts: in it, TestInterruptChildWaitsForItsSignal holds a command's interrupt
// context and says when it ends.
const interruptChildEnv = "NOVA_SPRINT_TEST_INTERRUPT_CHILD"

// The interrupt a command gets when nothing has replaced it is the terminal's
// (SIGINT) and a termination (SIGTERM), each of which ends its context. The
// signal is a real one, so it goes to a child process of this test binary and
// never to this process: a signal sent to the test binary reaches every
// command of every test running beside this one, and a held hook (TestAHookStays
// LiveAcrossThreeBoundsWhileItsSeatActs, TestHookCompactPrintsOnlyWhatTheSeatActsOn)
// ended at it, its socket gone, while its seat still acted (CI, 2026-10-12).
// internal/ci's TestNoTestSignalsItsOwnProcess keeps it so.
func TestTheInterruptOfTheCommandIsSIGINTAndSIGTERM(t *testing.T) {
	t.Parallel()
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInterruptChildWaitsForItsSignal$", "-test.count=1", "-test.v")
			cmd.WaitDelay = 5 * time.Second
			cmd.Env = append(envWithout(os.Environ(), []string{interruptChildEnv}), interruptChildEnv+"="+sig.String())
			out, err := cmd.StdoutPipe()
			require.NoError(t, err)
			cmd.Stderr = cmd.Stdout
			require.NoError(t, cmd.Start())
			var seen []string
			sc := bufio.NewScanner(out)
			ready := false
			for !ready && sc.Scan() {
				seen = append(seen, sc.Text())
				ready = sc.Text() == "INTERRUPT CHILD READY"
			}
			require.True(t, ready, "the child never held its context:\n%s", strings.Join(seen, "\n"))
			require.NoError(t, cmd.Process.Signal(sig))
			for sc.Scan() {
				seen = append(seen, sc.Text())
			}
			err = cmd.Wait()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				require.FailNowf(t, "", "%v did not end the context of the command: the child %v:\n%s", sig, exit.ProcessState, strings.Join(seen, "\n"))
			}
			require.NoError(t, err, strings.Join(seen, "\n"))
			require.Contains(t, seen, "INTERRUPT CHILD ENDED", "%v did not end the context of the command", sig)
		})
	}
}

// TestInterruptChildWaitsForItsSignal is the child's half of TestTheInterruptOfTheCommand
// IsSIGINTAndSIGTERM, run only in the child that test starts (interruptChildEnv): a
// command's interrupt context, held until a signal ends it. A signal it does not catch
// ends the child by that signal, which the parent reads as the failure.
func TestInterruptChildWaitsForItsSignal(t *testing.T) {
	t.Parallel()
	if os.Getenv(interruptChildEnv) == "" {
		t.Skip("the child of TestTheInterruptOfTheCommandIsSIGINTAndSIGTERM; it runs only there")
	}
	ctx, stop := newApp(func(string) string { return "" }).notify(context.Background())
	defer stop()
	fmt.Println("INTERRUPT CHILD READY")
	select {
	case <-ctx.Done():
		fmt.Println("INTERRUPT CHILD ENDED")
	case <-time.After(25 * time.Second):
		require.Failf(t, "", "%s did not end the context of the command", os.Getenv(interruptChildEnv))
	}
}

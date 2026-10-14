package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The streaming verbs besides hook flush every line to stdout as it is
// written: where --watch draws a frame at once and watch --wake prints its
// WAKE line, whether stdout is a terminal, a pipe or a file. Each guard runs
// the verb with stdout a real OS pipe and reads the line from the pipe while
// the verb runs. A regression that wrapped stdout in a user-space buffer
// without flushing it would hide the line and time the guard out (see
// TestHookFlushesAfterChallenge for the hook's own guard).

// TestWhereWatchFlushesEveryFrame runs where --watch against the test store
// with stdout a real OS pipe and asserts the first frame is readable within
// 1s while the watch is still running; then it interrupts the watch and waits
// for it to end.
func TestWhereWatchFlushesEveryFrame(t *testing.T) {
	t.Parallel()
	ta := whereFixture(t)

	// the interrupt is the test's, so the watch can be ended from here
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ta.a.notify = func(context.Context) (context.Context, context.CancelFunc) {
		return ctx, cancel
	}

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		code := ta.a.cmdWhere(split("--watch --every 20ms"), pw, &errb)
		_ = pw.Close()
		done <- code
	}()

	// keep reading so a watch blocked on a full pipe can always finish
	frame := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := sc.Text()
			if strings.Contains(line, "\x1b[H") {
				select {
				case frame <- line:
				default:
				}
			}
		}
	}()

	select {
	case line := <-frame:
		assert.Contains(t, line, "SPRINT TABLE", "the first frame's line: %q", line)
	case <-time.After(time.Second):
		require.FailNow(t, "a where --watch frame was not readable on the OS pipe within 1s while the watch ran")
	}

	// the watch is still running when the frame was read: it did not exit
	select {
	case code := <-done:
		require.FailNowf(t, "", "where --watch exited (code %d) before a frame was read", code)
	default:
	}

	cancel()
	select {
	case code := <-done:
		require.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "where --watch did not end after the interrupt")
	}
}

// TestWatchWakeFlushesTheWake runs watch --wake with stdout a real OS pipe and
// asserts the WAKE line is readable on the pipe within 1s. A regression that
// buffered stdout would let the line sit in the buffer until the verb exited
// rather than reaching a reader's pipe as it is written.
func TestWatchWakeFlushesTheWake(t *testing.T) {
	t.Parallel()
	ta := whereFixture(t)

	pr, pw, err := os.Pipe()
	require.NoError(t, err)
	defer pr.Close()
	done := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		state := filepath.Join(t.TempDir(), "wake.json")
		code := ta.a.cmdWatch(split("--wake --every 1ms --check 1ms --state "+state), pw, &errb)
		_ = pw.Close()
		done <- code
	}()

	wake := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "WAKE ") {
				wake <- sc.Text()
				return
			}
		}
	}()

	select {
	case line := <-wake:
		assert.Contains(t, line, "WAKE ", "the wake line: %q", line)
	case <-time.After(time.Second):
		require.FailNow(t, "a watch --wake WAKE line was not readable on the OS pipe within 1s")
	}

	select {
	case code := <-done:
		require.Equal(t, 0, code)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "watch --wake did not end after its wake")
	}
}

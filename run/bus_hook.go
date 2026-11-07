package run

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type busLease struct {
	bus  *NovaBus
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	err  error
	stop sync.Once
}

// Acquire starts a real hook. It runs only the fixed bus-reply entry point;
// no message body is interpolated into a shell command.
func (b *NovaBus) Acquire(ctx context.Context, name string) (HookLease, error) {
	if name != "sprint" {
		return nil, fmt.Errorf("run: chooser hook must be sprint")
	}
	reply := shellWord(b.cfg.ReplyExecutable) + " bus-reply"
	cmd := exec.Command(b.cfg.Binary, "hook", "--as", name, "--exec", reply)
	cmd.Env = b.env()
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("run: prepare bus hook: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("run: start bus hook: %w", err)
	}
	lease := &busLease{bus: b, cmd: cmd, done: make(chan struct{})}
	ready := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "HOOK OK name="+name+" ") {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}
	}()
	go func() { err := cmd.Wait(); lease.mu.Lock(); lease.err = err; lease.mu.Unlock(); close(lease.done) }()
	select {
	case <-ready:
		return lease, nil
	case <-lease.done:
		return nil, fmt.Errorf("run: sprint hook exited before acquiring its name")
	case <-ctx.Done():
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lease.Close(cleanup)
		return nil, fmt.Errorf("run: sprint hook: %w", ctx.Err())
	}
}

// Check observes process exit; Scheduler's one Who call verifies presence.
func (l *busLease) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-l.done:
		return fmt.Errorf("sprint hook exited")
	default:
		return nil
	}
}
func (l *busLease) Close(ctx context.Context) error {
	l.stop.Do(func() { _ = l.cmd.Process.Signal(os.Interrupt) })
	select {
	case <-l.done:
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.err
	case <-ctx.Done():
		_ = l.cmd.Process.Kill()
		return ctx.Err()
	}
}

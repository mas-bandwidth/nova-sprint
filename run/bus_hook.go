package run

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// SprintHook holds the nova-bus hook for the name sprint while run holds
// the chooser lock (LAYER-6 section 1): Bin is the nova-bus binary, Env its
// bus connection only (no store or provider secrets), Reply the absolute
// nova-sprint whose `bus-reply` answers a message sent to sprint.
type SprintHook struct {
	Bin   string
	Env   []string
	Reply string
}

type busLease struct {
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	err  error
	tail []string // the hook's first stderr lines, for its failure
	stop sync.Once
}

// Acquire starts a real hook. It runs only the fixed bus-reply entry point;
// no message body is interpolated into a shell command.
func (b SprintHook) Acquire(ctx context.Context, name string) (HookLease, error) {
	if name != "sprint" {
		return nil, fmt.Errorf("run: chooser hook must be sprint")
	}
	if !filepath.IsAbs(b.Bin) || !filepath.IsAbs(b.Reply) {
		return nil, fmt.Errorf("run: the sprint hook needs absolute nova-bus and nova-sprint paths; pass them in run.Config")
	}
	cmd := exec.Command(b.Bin, "hook", "--as", name, "--exec", shellWord(b.Reply)+" bus-reply")
	cmd.Env = b.Env
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("run: prepare bus hook: %w", err)
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("run: start bus hook: %w", err)
	}
	lease := &busLease{cmd: cmd, done: make(chan struct{})}
	ready := make(chan struct{}, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lease.mu.Lock()
			if len(lease.tail) < 8 {
				lease.tail = append(lease.tail, scanner.Text())
			}
			lease.mu.Unlock()
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
		return nil, fmt.Errorf("run: sprint hook exited before acquiring its name: %w", lease.failure())
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
		return fmt.Errorf("sprint hook exited: %w", l.failure())
	default:
		return nil
	}
}

// failure is the ended hook as an edge.CommandError: its command, exit code
// and first stderr lines (its env is the bus connection: never printed).
func (l *busLease) failure() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	code := -1
	if l.cmd.ProcessState != nil {
		code = l.cmd.ProcessState.ExitCode()
	}
	var runErr error
	var x *exec.ExitError
	if l.err != nil && !errors.As(l.err, &x) {
		runErr = l.err
	}
	return edge.Failure(l.cmd.Args, nil, code, "", strings.Join(l.tail, "\n"), runErr)
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

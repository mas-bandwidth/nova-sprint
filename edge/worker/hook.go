package worker

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

type (
	// WakeHook is the row's long-lived `nova-bus hook --as <row>` child. A
	// line it prints is only a wake (LAYER-4, Wake): the runner asks the
	// sprint what to do, so a lost, late or repeated line changes nothing.
	// edge.Exec returns only when its program ends, so the hook cannot be
	// run through it; this is the one child the worker watches itself.
	WakeHook interface {
		// Wake receives one value per burst of lines (capacity one: wakes
		// that arrive while a pass runs coalesce into the next pass).
		Wake() <-chan struct{}
		// Done is closed when the hook process has ended, for any reason.
		Done() <-chan struct{}
		// Err says why it ended; only meaningful after Done is closed.
		Err() error
		// Stop ends the process (SIGINT, then SIGKILL) and waits for it.
		Stop()
	}
	// BusHookStarter claims the row's name on the bus and returns the hook
	// once nova-bus says it holds the name.
	BusHookStarter func(ctx context.Context, row string) (WakeHook, error)
	// OSBusHook starts the real nova-bus hook. Env is the bus connection
	// only (NOVA_BUS_*): the hook never sees the store, a provider key or
	// the worker's other environment.
	OSBusHook struct {
		Bin                 string
		Env                 []string
		ClaimWait, StopWait time.Duration
	}
	osWake_internal struct {
		cmd      *exec.Cmd
		wake     chan struct{}
		done     chan struct{}
		ready    chan struct{}
		readyOne sync.Once
		stopOne  sync.Once
		stopWait time.Duration
		mu       sync.Mutex
		err      error
		tail     []byte
		row      string
	}
)

// BusEnv picks the bus connection variables out of an environment.
func BusEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, "NOVA_BUS_") {
			out = append(out, kv)
		}
	}
	return out
}
func (b OSBusHook) Start(ctx context.Context, row string) (WakeHook, error) {
	cmd := exec.Command(resolveToolBin_internal(cmp.Or(b.Bin, "nova-bus")), "hook", "--as", row)
	cmd.Env = append([]string{"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin"), "HOME=" + os.Getenv("HOME")}, b.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("worker refused: cannot start nova-bus hook for %s: %w", row, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("worker refused: cannot start nova-bus hook for %s: %w", row, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("worker refused: cannot start nova-bus hook for %s: %w: install nova-tools on PATH or run: nova-fleet up", row, err)
	}
	w := &osWake_internal{cmd: cmd, wake: make(chan struct{}, 1), done: make(chan struct{}), ready: make(chan struct{}), stopWait: cmp.Or(b.StopWait, 3*time.Second), row: row}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		rd := bufio.NewReader(stdout)
		for {
			if _, _, err := rd.ReadLine(); err != nil {
				return
			}
			select {
			case w.wake <- struct{}{}:
			default:
			}
		}
	}()
	go func() {
		defer readers.Done()
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			w.mu.Lock()
			w.tail = append(w.tail, line+"\n"...)
			if len(w.tail) > 4096 {
				w.tail = w.tail[len(w.tail)-4096:]
			}
			w.mu.Unlock()
			if strings.HasPrefix(line, "HOOK OK name="+row+" ") {
				w.readyOne.Do(func() { close(w.ready) })
			}
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	go func() {
		readers.Wait()
		status := "exit 0"
		if werr := cmd.Wait(); werr != nil {
			status = werr.Error()
		}
		w.mu.Lock()
		w.err = fmt.Errorf("nova-bus hook --as %s ended (%s): stderr %q: the row is no longer reachable on the bus, so the worker exits; the service restarts it", row, status, edge.RedactN(TailBytes(w.tail, 512), edge.FailureMax))
		w.mu.Unlock()
		close(w.done)
	}()
	claim := time.NewTimer(cmp.Or(b.ClaimWait, 60*time.Second))
	defer claim.Stop()
	select {
	case <-w.ready:
		return w, nil
	case <-w.done:
		return nil, fmt.Errorf("worker refused: %w", w.Err())
	case <-ctx.Done():
		w.Stop()
		return nil, ctx.Err()
	case <-claim.C:
		w.Stop()
		w.mu.Lock()
		tail := edge.RedactN(TailBytes(w.tail, 512), edge.FailureMax)
		w.mu.Unlock()
		return nil, fmt.Errorf("worker refused: nova-bus hook --as %s did not claim the name in time (stderr %q): check the bus with nova-bus who, then run nova-worker again", row, tail)
	}
}
func (w *osWake_internal) Wake() <-chan struct{} { return w.wake }
func (w *osWake_internal) Done() <-chan struct{} { return w.done }
func (w *osWake_internal) Err() error            { w.mu.Lock(); defer w.mu.Unlock(); return w.err }
func (w *osWake_internal) Stop() {
	w.stopOne.Do(func() {
		_ = w.cmd.Process.Signal(os.Interrupt)
		grace := time.NewTimer(w.stopWait)
		defer grace.Stop()
		select {
		case <-w.done:
		case <-grace.C:
			_ = syscall.Kill(-w.cmd.Process.Pid, syscall.SIGKILL)
			<-w.done
		}
	})
}

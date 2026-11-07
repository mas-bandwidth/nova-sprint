package verbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// SecretTerminal reads without echo. Implementations must never log its value.
type SecretTerminal interface {
	ReadHidden(context.Context, string) ([]byte, error)
}

// SecretSink checks presence and saves a key through stdin, never argv or env.
type SecretSink interface {
	Has(context.Context, string) (bool, error)
	Set(context.Context, string, io.Reader) error
}

// EnsureKey asks for service's key, saved under name, only when it is not
// saved; it never asks after a durable save. A crash before save may
// re-prompt.
func EnsureKey(ctx context.Context, service, name string, terminal SecretTerminal, sink SecretSink) error {
	if terminal == nil || sink == nil {
		return fmt.Errorf("secret prompt is unavailable: no terminal or no secret store configured")
	}
	present, err := sink.Has(ctx, name)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	key, err := terminal.ReadHidden(ctx, "Enter "+service+" API key: ")
	if err != nil {
		return err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	present, err = sink.Has(ctx, name)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	if len(key) == 0 {
		return &OperationError{Problem: "key is empty", Fix: "run nova-sprint up --key " + service, Exit: 1}
	}
	return sink.Set(ctx, name, bytes.NewReader(key))
}

// TTY reads from the controlling terminal and restores its original mode.
type TTY struct{}

func (TTY) ReadHidden(ctx context.Context, prompt string) (key []byte, resultErr error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, RefusalOr(err, &OperationError{Problem: "hidden prompt needs a terminal (" + errText(err) + ")", Fix: "run nova-sprint up --key <service> in a visible terminal", Exit: 1})
	}
	defer tty.Close()
	restoreTTY, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer restoreTTY.Close()
	state, err := ttyState(tty)
	if err != nil {
		return nil, err
	}
	sigctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sigctx.Err(); err != nil {
		return nil, err
	}
	defer func() {
		if err := ttyMode(restoreTTY, state); err != nil {
			for i := range key {
				key[i] = 0
			}
			key = nil
			resultErr = errors.Join(resultErr, cause("terminal mode could not be restored", err))
		}
	}()
	if err = ttyMode(tty, "-echo"); err != nil {
		return nil, err
	}
	if _, err = io.WriteString(tty, prompt); err != nil {
		return nil, err
	}
	fd := int(tty.Fd())
	if err = syscall.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	key, err = readHiddenLine(sigctx, fd)
	if _, werr := io.WriteString(restoreTTY, "\n"); werr != nil { // the newline after a hidden prompt is cosmetic: said, not fatal
		fmt.Fprintln(os.Stderr, "nova-sprint: the terminal did not take the newline after the hidden prompt: "+Redact(werr.Error()))
	}
	if err != nil {
		for i := range key {
			key[i] = 0
		}
		return nil, err
	}
	key = bytes.TrimSuffix(key, []byte("\n"))
	key = bytes.TrimSuffix(key, []byte("\r"))
	return key, nil
}
func readHiddenLine(ctx context.Context, fd int) ([]byte, error) {
	data := make([]byte, 0, 128)
	var chunk [1]byte
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return data, err
		}
		n, err := syscall.Read(fd, chunk[:])
		if n > 0 {
			if chunk[0] == '\n' {
				return data, nil
			}
			data = append(data, chunk[0])
			if len(data) > 65536 {
				return data, fmt.Errorf("key is too long: over 65536 bytes")
			}
			continue
		}
		if n == 0 && err == nil {
			return data, io.EOF
		}
		if err != nil && err != syscall.EAGAIN && err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return data, cause("key could not be read", err)
		}
		select {
		case <-ctx.Done():
			return data, ctx.Err()
		case <-ticker.C:
		}
	}
}

func ttyState(tty *os.File) (string, error) {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = tty
	var out bytes.Buffer
	if err := runTool(cmd, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
func ttyMode(tty *os.File, mode string) error {
	cmd := exec.Command("stty", mode)
	cmd.Stdin = tty
	return runTool(cmd, nil)
}

// NovaSecrets uses the configured local secret store and passes values only on stdin.
type NovaSecrets struct{ Store, KeyFile, Pins, Seat string }

func (n NovaSecrets) Has(ctx context.Context, name string) (bool, error) {
	args := []string{"names", "--store", n.Store}
	if n.Seat != "" {
		args = append(args, "--as", n.Seat)
	}
	out, err := Output(ctx, "nova-secrets", args...)
	if err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "nova-secrets could not list names (" + errText(err) + ")", Fix: "run nova-sprint up", Exit: 1})
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true, nil
		}
	}
	return false, nil
}
func (n NovaSecrets) Set(ctx context.Context, name string, key io.Reader) error {
	args := []string{"set", name, "--store", n.Store, "--key", n.KeyFile, "--pins", n.Pins}
	if n.Seat != "" {
		args = append(args, "--as", n.Seat)
	}
	cmd := exec.CommandContext(ctx, "nova-secrets", args...)
	cmd.Stdin = key // the key goes on stdin only: never argv, env or output
	if err := runTool(cmd, nil); err != nil {
		return RefusalOr(err, &OperationError{Problem: "nova-secrets could not save the key (" + errText(err) + ")", Fix: "run nova-sprint up --key <service>", Exit: 1})
	}
	return nil
}

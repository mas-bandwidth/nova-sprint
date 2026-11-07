package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/run/filestore"
)

// OpenFakeChooser is for explicit local test mode only. The file lock is a
// whole-process chooser lease; its separate simulated hook does not use a bus.
func OpenFakeChooser(ctx context.Context, path string) (*Chooser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := filestore.AcquireChooser(path)
	if err != nil {
		return nil, err
	}
	return acquireChooser(ctx, &fakeFileSession{file: f}, simulatedHook{})
}

type fakeFileSession struct{ file *os.File }

func (s *fakeFileSession) Try(context.Context) (bool, error)      { return true, nil }
func (s *fakeFileSession) Holder(context.Context) (string, error) { return "local fake chooser", nil }
func (s *fakeFileSession) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.file.Stat()
	return err
}
func (s *fakeFileSession) Close(context.Context) error { return s.file.Close() }

type simulatedHook struct{}

func (simulatedHook) Acquire(context.Context, string) (HookLease, error) {
	return simulatedHookLease{}, nil
}

type simulatedHookLease struct{}

func (simulatedHookLease) Check(ctx context.Context) error { return ctx.Err() }
func (simulatedHookLease) Close(context.Context) error     { return nil }

// FakePresence exposes only names supplied by the test-mode composition.
type FakePresence struct{ Names []string }

func (p FakePresence) Who(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]string{"sprint"}, p.Names...), nil
}

// FakeSender prints simulated delivery, without invoking nova-bus.
type FakeSender struct {
	mu   sync.Mutex
	Out  io.Writer
	next uint64
}

func (s *FakeSender) Send(ctx context.Context, to, subject, body string) (Delivery, error) {
	if err := ctx.Err(); err != nil {
		return Delivery{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Out == nil {
		return Delivery{}, fmt.Errorf("fake push: output is required")
	}
	if _, err := fmt.Fprintf(s.Out, "PUSH to=%s subject=%s\n%s\n", to, subject, body); err != nil {
		return Delivery{}, err
	}
	s.next++
	return Delivery{Recipient: to, ID: fmt.Sprintf("1-%d", s.next)}, nil
}

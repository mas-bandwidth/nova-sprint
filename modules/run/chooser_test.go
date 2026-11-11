package run

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSession struct {
	locked           bool
	holder           string
	tryErr, checkErr error
	closed           bool
}

func (s *fakeSession) Try(context.Context) (bool, error)      { return s.locked, s.tryErr }
func (s *fakeSession) Holder(context.Context) (string, error) { return s.holder, nil }
func (s *fakeSession) Check(context.Context) error            { return s.checkErr }
func (s *fakeSession) Close(context.Context) error            { s.closed = true; return nil }

type fakeLease struct {
	checkErr error
	closed   bool
}

func (l *fakeLease) Check(context.Context) error { return l.checkErr }
func (l *fakeLease) Close(context.Context) error { l.closed = true; return nil }

type fakeHook struct {
	lease *fakeLease
	err   error
	name  string
}

func (h *fakeHook) Acquire(_ context.Context, name string) (HookLease, error) {
	h.name = name
	if h.err != nil {
		return nil, h.err
	}
	return h.lease, nil
}

func TestChooserRefusalAndCleanup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &fakeSession{holder: "nova-sprint run studio 4121"}
	h := &fakeHook{lease: &fakeLease{}}
	_, err := acquireChooser(ctx, s, h)
	want := "SPRINT REFUSED run: run already chooses on nova-sprint run studio 4121: stop it there, or nova-sprint show run"
	if err == nil || err.Error() != want || !s.closed || h.name != "" {
		t.Fatalf("err=%v closed=%v hook=%q", err, s.closed, h.name)
	}
	s = &fakeSession{locked: true}
	h.err = errors.New("name is held")
	_, err = acquireChooser(ctx, s, h)
	if err == nil || !strings.Contains(err.Error(), "sprint is already hooked") || !s.closed || h.name != "sprint" {
		t.Fatalf("err=%v session=%+v hook=%+v", err, s, h)
	}
}
func TestChooserHealthAndClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := &fakeSession{locked: true}
	lease := &fakeLease{}
	h := &fakeHook{lease: lease}
	c, err := acquireChooser(ctx, s, h)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	s.checkErr = errors.New("disconnected")
	if err = c.Check(ctx); err == nil || !strings.Contains(err.Error(), "LOCK LOST") {
		t.Fatalf("err=%v", err)
	}
	s.checkErr = nil
	lease.checkErr = errors.New("hook lost")
	if err = c.Check(ctx); err == nil || !strings.Contains(err.Error(), "LOCK LOST") {
		t.Fatalf("err=%v", err)
	}
	if err = c.Close(ctx); err != nil || !s.closed || !lease.closed {
		t.Fatalf("close=%v session=%v hook=%v", err, s.closed, lease.closed)
	}
}

func TestDescribeHolder(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 6, 14, 2, 0, 0, time.Local)
	if got := describeHolder("nova-sprint run studio 4121", start); got != "studio (pid 4121, since 14:02)" {
		t.Fatal(got)
	}
	if got := describeHolder("nova-sprint run studio 4121 v2.0.38", start); got != "studio (pid 4121, since 14:02)" {
		t.Fatal(got)
	}
	if got := describeHolder("unknown", start); got != "unknown" {
		t.Fatal(got)
	}
}

func TestChooserConfigErrorHidesDSN(t *testing.T) {
	t.Parallel()
	const secret = "secret-token-for-test"
	_, err := OpenChooser(context.Background(), "postgres://u:"+secret+"@%broken/db", "", "host", 1, "v2.0.0", &fakeHook{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe config error: %v", err)
	}
}

type contextLease struct{ closed bool }

func (l *contextLease) Check(context.Context) error { return nil }
func (l *contextLease) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.closed = true
	return nil
}
func TestChooserCleanupSurvivesCancelledCaller(t *testing.T) {
	t.Parallel()
	s := &fakeSession{locked: true}
	lease := &contextLease{}
	c := &Chooser{session: s, hook: lease}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Close(ctx); err != nil || !lease.closed || !s.closed {
		t.Fatalf("err=%v hook=%v session=%v", err, lease.closed, s.closed)
	}
}

// The holder's build is the fifth word of its name; PostgreSQL cuts a name
// at NameMax bytes, so a name that long holds no readable build (a long host
// could leave "v2.0.3" of v2.0.38, an older release) and is VersionUnknown.
func TestChooserNameVersion(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"nova-sprint run studio 4121 v2.0.38":                                      "v2.0.38",
		"nova-sprint run studio 4121":                                              "",
		("nova-sprint run " + strings.Repeat("h", 39) + " 4121 v2.0.38")[:NameMax]: VersionUnknown,
		("nova-sprint run " + strings.Repeat("h", 50) + " 4121 v2.0.38")[:NameMax]: VersionUnknown,
	} {
		s := ChooserStatus{ApplicationName: name}
		s.parseName()
		if s.Version != want {
			t.Errorf("%q: version %q, want %q", name, s.Version, want)
		}
	}
}

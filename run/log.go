package run

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

// TickMetrics is one tick's line: its number, the seq it folded to, how
// long it took, its counts and its error.
type TickMetrics struct {
	Number int
	Seq    int64
	Took   time.Duration
	Counts TickCounts
	Err    error
}

// WriterLogger serializes short tick, push and edge lines. It never logs
// message bodies; an error's text is logged redacted and bounded
// (edge.RedactN: no DSN password, provider key or token value, names only),
// because a failure that is not said cannot be fixed (audit row 28).
type WriterLogger struct {
	mu  sync.Mutex
	out io.Writer
	err error
}

func NewWriterLogger(out io.Writer) *WriterLogger { return &WriterLogger{out: out} }
func (l *WriterLogger) Log(m TickMetrics) {
	l.mu.Lock()
	defer l.mu.Unlock()
	status := "ok"
	if m.Err != nil {
		status = "error"
	}
	_, err := fmt.Fprintf(l.out, "TICK n=%d seq=%d took=%s folded=%d ops=%d/%d edges=%d status=%s%s\n", m.Number, m.Seq, m.Took, m.Counts.Folded, m.Counts.Applied, m.Counts.Refused, m.Counts.Edges, status, errField(m.Err))
	if err == nil && m.Took > time.Second {
		_, err = fmt.Fprintf(l.out, "OVERRUN n=%d took=%s\n", m.Number, m.Took)
	}
	if err == nil && errors.Is(m.Err, ErrLockLost) {
		_, err = fmt.Fprintln(l.out, "LOCK LOST")
	}
	if err != nil {
		l.err = err
	}
}
func (l *WriterLogger) PushFailed(recipient string, cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := fmt.Fprintf(l.out, "PUSH FAILED to=%s%s\n", recipient, errField(cause))
	if err != nil {
		l.err = err
	}
}

// EdgeFault is an edge request or read that failed: what it was and why.
func (l *WriterLogger) EdgeFault(what string, cause error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := fmt.Fprintf(l.out, "EDGE FAULT %s%s\n", edge.RedactN(what, 120), errField(cause))
	if err != nil {
		l.err = err
	}
}

// errField is ` err="<the cause, redacted and bounded>"`, or "" for nil.
func errField(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf(" err=%q", edge.RedactN(err.Error(), edge.FailureMax))
}
func (l *WriterLogger) Err() error { l.mu.Lock(); defer l.mu.Unlock(); return l.err }

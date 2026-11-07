package run

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
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

// WriterLogger serializes short tick and push lines without logging message
// bodies, DSNs, provider keys or untrusted error text.
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
	_, err := fmt.Fprintf(l.out, "TICK n=%d seq=%d took=%s folded=%d ops=%d/%d edges=%d status=%s\n", m.Number, m.Seq, m.Took, m.Counts.Folded, m.Counts.Applied, m.Counts.Refused, m.Counts.Edges, status)
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
func (l *WriterLogger) PushFailed(recipient string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := fmt.Fprintf(l.out, "PUSH FAILED to=%s\n", recipient)
	if err != nil {
		l.err = err
	}
}
func (l *WriterLogger) Err() error { l.mu.Lock(); defer l.mu.Unlock(); return l.err }

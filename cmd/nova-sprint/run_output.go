package main

import "io"

// runOutput gives the run loop and every helper one stdout write at a time.
// It is independent of the store's control line and of stderr: a blocked
// goroutine-stack dump must never hold up the watchdog's stdout diagnostics.
type runOutput struct {
	writing chan struct{}
	w       io.Writer
}

func newRunOutput(w io.Writer) *runOutput {
	return &runOutput{writing: make(chan struct{}, 1), w: w}
}

func (w *runOutput) Write(p []byte) (int, error) {
	w.writing <- struct{}{}
	defer func() { <-w.writing }()
	return w.w.Write(p)
}

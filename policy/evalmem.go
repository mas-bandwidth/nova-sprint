package policy

import (
	"fmt"
	"os"
	"runtime/debug"
	"runtime/metrics"
	"strconv"
	"time"
)

// EvalMemory is the evaluator child's memory cap in bytes (the owner's ruling,
// 2026-11-07: a cap, the policy layer's number, default 512 MiB).
//
// The child caps itself before it reads a request. RLIMIT_AS is not used:
// the Go runtime reserves address space far past its heap (and a -race
// build reserves terabytes of shadow), so an address-space cap near the heap
// cap stops the child before it starts, and darwin does not enforce one.
// The platform-independent equivalent is the Go runtime's own: the memory
// limit (the collector works to stay under it) and a watchdog reading the
// live heap every evalMemoryEvery, which ends the child with "over the
// evaluator's memory cap" on stderr when the heap passes the cap. The
// parent reads that as the child dying and fails every version it ran,
// naming the line. A single allocation is bounded by Starlark's own limit
// (1 GiB), so the heap can pass the cap by at most that before the watchdog
// sees it.
const EvalMemory = 512 << 20

const (
	evalMemoryEnv   = "NOVA_POLICY_EVAL_MEMORY" // the cap, parent to child
	evalMemoryEvery = 2 * time.Millisecond
	heapMetric      = "/memory/classes/heap/objects:bytes"
)

// capMemory applies the cap the parent passed (EvalMemory when absent) and
// starts the watchdog.
func capMemory() error {
	limit := uint64(EvalMemory)
	if v := os.Getenv(evalMemoryEnv); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("%s is %q, want a positive byte count: %w", evalMemoryEnv, v, err)
		}
		if n == 0 {
			return fmt.Errorf("%s is %q, want a positive byte count", evalMemoryEnv, v)
		}
		limit = n
	}
	debug.SetMemoryLimit(int64(limit))
	go watchMemory(limit)
	return nil
}

// watchMemory ends the process when the live heap passes limit.
func watchMemory(limit uint64) {
	s := []metrics.Sample{{Name: heapMetric}}
	for range time.Tick(evalMemoryEvery) {
		metrics.Read(s)
		if s[0].Value.Kind() == metrics.KindUint64 && s[0].Value.Uint64() > limit {
			fmt.Fprintf(os.Stderr, "over the evaluator's memory cap of %d MiB\n", limit>>20)
			os.Exit(3)
		}
	}
}

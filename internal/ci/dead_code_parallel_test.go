package ci

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

const (
	// deadCodeMaxMemBytes bounds the total memory budget for concurrent deadcode runs.
	deadCodeMaxMemBytes int64 = 4 << 30

	// deadCodePerRunBytes is the measured peak RSS (~1.8 GiB under GOGC=25) of one
	// whole-program deadcode -test -json invocation across the repository's mains.
	deadCodePerRunBytes int64 = 1800 << 20
)

// deadCodeParallelFor returns the number of concurrent deadcode processes to
// run given the machine's CPU cores, total memory, and per-run memory cost.
func deadCodeParallelFor(cores int, memBytes, perRunBytes int64) int {
	if cores <= 1 || memBytes <= 0 || perRunBytes <= 0 {
		return 1
	}
	budget := min(memBytes/8, deadCodeMaxMemBytes-1)
	byMem := int(budget / perRunBytes)
	return max(1, min(cores, byMem))
}

func deadCodeHostMemBytes() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					if kb, err := strconv.ParseInt(fields[0], 10, 64); err == nil && kb > 0 {
						return kb << 10
					}
				}
			}
		}
	}
	return 30 << 30
}

func TestDeadCodeParallelBounded(t *testing.T) {
	t.Parallel()
	const (
		cores16 = 16
		mem30GB = int64(30) << 30
		max4GB  = int64(4) << 30
	)
	n := deadCodeParallelFor(cores16, mem30GB, deadCodePerRunBytes)
	if n < 1 {
		t.Fatalf("deadCodeParallelFor(%d, %d, %d) = %d, want >= 1", cores16, mem30GB, deadCodePerRunBytes, n)
	}
	if n > cores16 {
		t.Fatalf("deadCodeParallelFor(%d, %d, %d) = %d, want <= %d", cores16, mem30GB, deadCodePerRunBytes, n, cores16)
	}
	if got := int64(n) * deadCodePerRunBytes; got >= max4GB {
		t.Fatalf("int64(%d) * %d = %d bytes (>= 4 GiB %d)", n, deadCodePerRunBytes, got, max4GB)
	}

	// Never goes below 1 even when per-run cost exceeds the memory budget or cores <= 0.
	for _, tc := range []struct {
		cores       int
		memBytes    int64
		perRunBytes int64
	}{
		{0, mem30GB, deadCodePerRunBytes},
		{-1, mem30GB, deadCodePerRunBytes},
		{16, 1 << 30, 5 << 30},
		{16, 0, deadCodePerRunBytes},
		{16, mem30GB, 10 << 30},
	} {
		if got := deadCodeParallelFor(tc.cores, tc.memBytes, tc.perRunBytes); got < 1 {
			t.Fatalf("deadCodeParallelFor(%d, %d, %d) = %d, want >= 1", tc.cores, tc.memBytes, tc.perRunBytes, got)
		}
	}

	// Never exceeds cores even when per-run cost is tiny.
	for _, cores := range []int{1, 2, 4, 8} {
		if got := deadCodeParallelFor(cores, mem30GB, 64<<20); got > cores {
			t.Fatalf("deadCodeParallelFor(%d, %d, 64 MiB) = %d, want <= %d", cores, mem30GB, got, cores)
		}
	}

	// Scales with per-run cost within the 4 GiB ceiling.
	if got := deadCodeParallelFor(16, mem30GB, 2500<<20); got != 1 {
		t.Fatalf("deadCodeParallelFor(16, 30 GiB, 2500 MiB) = %d, want 1", got)
	}
}

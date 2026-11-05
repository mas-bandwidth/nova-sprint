//go:build !unix

package worker

import (
	"errors"
	"os"
)

// ExecProc refuses: the worker runs its lanes on unix machines only (WORKER-INVENTORY.md
// row 78); captain stays on nova-swarm.
func ExecProc(argv, env []string, dir string, log *os.File) (Proc, error) {
	return nil, errors.New("nova-sprint worker runs lanes on unix only")
}

// FreeBytes refuses off unix: the worker runs on unix only.
func FreeBytes(dir string) (uint64, error) { return 0, errors.New("unix only") }

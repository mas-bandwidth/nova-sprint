package dogfood

import (
	"context"
)

// Runner runs one command and returns its standard output. It is the only way
// this package reaches outside the process, and it is a parameter so the tests
// can answer without a git of their own. Nothing here reaches the network.
type Runner func(ctx context.Context, dir string, args ...string) (string, error)

// Progress is told how far along a slow read is. The CLI prints it on stderr
// once the read has taken longer than a tenth of a second: a program says what
// it is doing when what it is doing takes long enough to look like a hang.
type Progress func(done, total int)

package friend

import (
	"context"

	"regexp"
	"strings"
)

// A lane runs its harness in the card's job directory and names every path absolute (the
// finding of 2026-10-07: a model read a relative brief path from the friend's working
// directory, found nothing and wrote no report; docs/SPEC-FRIEND.md, the lane's paths).

// laneDirKey marks a context with the working directory a lane's harness runs in.
type laneDirKey struct{}

// LaneDirOf is the job directory ctx carries, else def: a harness's run in a lane goes there.
func LaneDirOf(ctx context.Context, def string) string {
	if d, _ := ctx.Value(laneDirKey{}).(string); d != "" {
		return d
	}
	return def
}

var harnessErrorLine = regexp.MustCompile(`(?i)\b(error|errors|not found|no such file|failed|failure|exception|denied|refused|cannot|could not)\b`)

// HarnessFirstError is the first line of a harness's output that says an error, one line, at
// most 200 bytes; "" when none does.
func HarnessFirstError(out string) string {
	for _, line := range strings.Split(stripANSI(out), "\n") {
		if line = strings.TrimSpace(line); line != "" && harnessErrorLine.MatchString(line) {
			return oneLine(line, 200)
		}
	}
	return ""
}

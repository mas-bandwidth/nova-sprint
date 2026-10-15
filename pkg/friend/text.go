package friend

import (
	"strings"
	"time"
)

// RecheckEvery is how long a deferred delivery waits before it is tried again
// (Deferred: the session cannot take a turn now and nothing is wrong).
const RecheckEvery = 10 * time.Second

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// oneLine is s on one line, at most n bytes: a reason fit for a subject.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = s[:n]
	}
	return s
}

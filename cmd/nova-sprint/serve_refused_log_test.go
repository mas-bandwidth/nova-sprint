package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A refused verb is recorded on the server's own log (serve.go; docs/SPEC-SPRINT.md
// section 14, The server): on the v1.2.10 server every done-path finish was refused for
// about 40 minutes and the log held no line for a refused verb, so the refusal could not
// be read. These tests pin the line's shape, its rate bound, its tally and that it carries
// none of the verb's own argv or stdout.

// refusedLines is the SERVE REFUSED lines of a captured server log.
func refusedLines(log string) []string {
	var out []string
	for _, l := range strings.Split(log, "\n") {
		if strings.Contains(l, "SERVE REFUSED") {
			out = append(out, l)
		}
	}
	return out
}

// A verb refused exit 1, one the server does not run exit 2, and a lane verb refused each
// write one SERVE REFUSED line naming the verb, the worker, the exit and the card when the
// verb names one, with the verb's own first stderr line; a verb that exits 0 writes
// nothing.
func TestARefusedVerbIsLoggedWithItsRefusal(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log

	ok := serveOne(ta.a, false, "queue", "--as", "m1")
	require.Equal(t, 0, ok.Code, ok.Stderr)
	assert.Empty(t, log.String(), "a verb that exits 0 writes nothing")

	fin := serveOne(ta.a, false, "finish", "--as", "m1", "no-such-card@1", "--epoch", "0")
	require.Equal(t, 1, fin.Code, fin.Stderr)
	lines := refusedLines(log.String())
	require.Len(t, lines, 1, log.String())
	first := strings.SplitN(strings.TrimSpace(fin.Stderr), "\n", 2)[0]
	assert.Contains(t, lines[0], "SERVE REFUSED verb=finish as=m1 exit=1 card=no-such-card@1: ")
	assert.Contains(t, lines[0], first, "the line carries the verb's own words, never a summary")

	log.Reset()
	un := serveOne(ta.a, false, "clear", "--confirm", "sprint")
	require.Equal(t, 2, un.Code, un.Stderr)
	lines = refusedLines(log.String())
	require.Len(t, lines, 1, log.String())
	assert.Contains(t, lines[0], "SERVE REFUSED verb=clear as=- exit=2: ")

	log.Reset()
	beat := serveOne(ta.a, false, "friend", "beat", "zed")
	require.Equal(t, 1, beat.Code, beat.Stderr)
	lines = refusedLines(log.String())
	require.Len(t, lines, 1, log.String())
	assert.Contains(t, lines[0], "SERVE REFUSED verb=friend as=zed exit=1: ")
}

// The same verb, worker, exit and first stderr line repeated within a minute is written
// once; when it changes the line that follows says how many repeats it stood for, so 40
// minutes of refused finishes from ten members is a few hundred lines, never one per verb.
func TestTheRefusedLogIsRateBounded(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log
	refuse := func() {
		res := serveOne(ta.a, false, "finish", "--as", "m1", "no-such-card@1", "--epoch", "0")
		require.Equal(t, 1, res.Code, res.Stderr)
	}
	for range 20 {
		refuse()
	}
	lines := refusedLines(log.String())
	require.Len(t, lines, 1, "the same refusal within a minute is written once: %s", log.String())

	ta.a.sleep(ServeSayEvery)
	refuse()
	lines = refusedLines(log.String())
	require.Len(t, lines, 2, log.String())
	assert.Contains(t, lines[1], "(repeated 19 times since ", log.String())

	beat := serveOne(ta.a, false, "friend", "beat", "zed")
	require.Equal(t, 1, beat.Code, beat.Stderr)
	assert.Len(t, refusedLines(log.String()), 3, "a refusal that changes is written at once: %s", log.String())
}

// The tally counts refused verbs by verb word beside its counts, and the SERVE line it
// prints says refused=<n> by verb: the server's own status line, and no new verb.
func TestTheServeTallyCountsRefusedVerbsByVerb(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log
	require.Equal(t, 1, serveOne(ta.a, false, "finish", "--as", "m1", "no-such-card@1", "--epoch", "0").Code)
	require.Equal(t, 1, serveOne(ta.a, false, "friend", "beat", "zed").Code)
	ta.a.sleep(ServeSayEvery)
	require.Equal(t, 0, serveOne(ta.a, false, "queue", "--as", "m1").Code)
	assert.Contains(t, log.String(), "refused=2 refused-by=finish:1,friend:1", log.String())
}

// The line carries the verb word, the worker, the exit, the card argument and stderr's
// first line only: never the verb's whole argv (a brief, a report) or its stdout. A first
// line longer than the bound is cut and marked.
func TestTheRefusedLogHoldsNoSecret(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log
	secret := "REPORTSECRET" + strings.Repeat("z", 2000)
	fin := serveOne(ta.a, false, "finish", "--as", "m1", "no-such-card@1", "--epoch", "0", "--report", secret)
	require.Equal(t, 1, fin.Code, fin.Stderr)
	assert.NotContains(t, log.String(), "REPORTSECRET", "the verb's argv is not logged")
	assert.Contains(t, log.String(), "SERVE REFUSED verb=finish as=m1 exit=1 card=no-such-card@1: ")

	log.Reset()
	ta.a.logRefused([]string{"read"}, "reader-a", 1, strings.Repeat("y", 1000))
	assert.Contains(t, log.String(), "...+", "the verb's first stderr line is cut to the bound")
}

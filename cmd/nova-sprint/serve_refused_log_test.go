package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A refused verb is logged with its own refusal (docs/SPEC-SPRINT.md section 14, The
// server). On the v1.2.10 server every done-path finish was refused exit 1 from 13:11Z on
// for about 40 minutes, and the server's log had no line for a refused verb; these tests
// pin the line that makes the next refusal readable, its one-line bound, the count beside
// the SERVE line, and that no secret of the verb's argument list reaches the log.

// firstOf is the first line of a verb's stderr, the text the refusal line carries.
func firstOf(s string) string {
	s = strings.TrimRight(s, "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// A worker's finish the sprint refuses (exit 1) writes one server log line naming the
// verb, the worker, the exit and the card, with the verb's own first stderr line; a verb
// the server does not run (exit 2) writes the same shape with its own verb word; a verb
// that exits 0 writes nothing.
func TestARefusedVerbIsLoggedWithItsRefusal(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, twoLanes()...)
	var log bytes.Buffer
	r.a.serveLog = &log

	fin := r.one("finish", "--as", "m1", "no-such-card@1", "--epoch", "0")
	require.Equal(t, 1, fin.Code, fin.Stderr)
	want := "SERVE REFUSED verb=finish as=m1 exit=1 card=no-such-card@1: " + firstOf(fin.Stderr)
	assert.Equal(t, want, strings.TrimRight(log.String(), "\n"), "the verb's own first stderr line, never a summary")
	assert.False(t, strings.Contains(log.String(), "nothing was changed\n;"), "one line")

	log.Reset()
	not := r.one("clear", "--confirm", "sprint")
	require.Equal(t, 2, not.Code, not.Stderr)
	assert.Equal(t, "SERVE REFUSED verb=clear as=- exit=2: "+firstOf(not.Stderr), strings.TrimRight(log.String(), "\n"))

	log.Reset()
	ok := r.one("queue", "--as", "m1")
	require.Equal(t, 0, ok.Code, ok.Stderr)
	assert.Empty(t, log.String(), "a verb that exits 0 writes nothing")
}

// A lane verb refused writes the same shape with its own verb word: a friend's beat the
// roster lacks (exit 1, on the beat lane) and a read the sprint refuses (exit 1, on the
// read lane; servelanes.go).
func TestARefusedLaneVerbIsLoggedWithItsRefusal(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log

	beat := serveOne(ta.a, false, "friend", "beat", "zed")
	require.Equal(t, 1, beat.Code, beat.Stderr)
	assert.Equal(t, "SERVE REFUSED verb=friend beat as=zed exit=1: "+firstOf(beat.Stderr), strings.TrimRight(log.String(), "\n"))

	log.Reset()
	card := serveOne(ta.a, true, "card", "s1-9")
	require.Equal(t, 1, card.Code, card.Stdout+card.Stderr)
	assert.Equal(t, "SERVE REFUSED verb=card as=- exit=1: "+firstOf(card.Stderr), strings.TrimRight(log.String(), "\n"))
}

// The line carries stderr's first line alone, one line, cut to 300 bytes: a verb whose
// stderr has several lines logs only the first, and one whose first line is long is cut.
func TestARefusedLogIsOneLineCutToThreeHundredBytes(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	t.Cleanup(a.close)
	var log bytes.Buffer
	a.serveLog = &log

	a.serveRefused("finish", "m1", 1, "c@1", "first\nsecond\nthird\n")
	assert.Equal(t, "SERVE REFUSED verb=finish as=m1 exit=1 card=c@1: first", strings.TrimRight(log.String(), "\n"))
	assert.NotContains(t, log.String(), "second")

	log.Reset()
	long := strings.Repeat("x", 1000)
	a.serveRefused("finish", "m1", 1, "c@1", long+"\n")
	line := strings.TrimRight(log.String(), "\n")
	text := strings.TrimPrefix(line, "SERVE REFUSED verb=finish as=m1 exit=1 card=c@1: ")
	assert.LessOrEqual(t, len(text), 300, "the first stderr line is cut to 300 bytes")
	assert.Less(t, len(text), len(long), "the long line was cut")
}

// The same verb, worker, exit and first line repeated within a minute is written once,
// and the next written line carries how many repeats there were since the last, so 40
// minutes of refused finishes from ten members is a few hundred lines, never one per verb.
func TestARefusedVerbRepeatedIsBoundedToOneLineAMinute(t *testing.T) {
	t.Parallel()
	a := newApp(func(string) string { return "" })
	t.Cleanup(a.close)
	var log bytes.Buffer
	a.serveLog = &log
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	a.now = func() time.Time { return at }

	for range 50 {
		a.serveRefused("finish", "m1", 1, "c@1", "no card c@1\n")
	}
	assert.Equal(t, 1, strings.Count(log.String(), "SERVE REFUSED"), "50 of one refusal within the minute are one line")

	at = at.Add(ServeRefusedEvery)
	a.serveRefused("finish", "m1", 1, "c@1", "no card c@1\n")
	assert.Equal(t, 2, strings.Count(log.String(), "SERVE REFUSED"), "the minute's line carries the repeats")
	assert.Contains(t, log.String(), "(repeated 49 times since 03:04:05)")

	// a different refusal is its own line at once, within the same minute
	a.serveRefused("finish", "m1", 1, "c@2", "no card c@2\n")
	assert.Equal(t, 3, strings.Count(log.String(), "SERVE REFUSED"))
}

// The line carries the verb word, the worker, the exit, the card and stderr's first line
// only: never the verb's whole argument list or its stdout, so a finish whose --report
// holds a long text logs none of it.
func TestARefusedVerbLogsNoSecretOfItsArgumentList(t *testing.T) {
	t.Parallel()
	r := newServerRig(t, twoLanes()...)
	var log bytes.Buffer
	r.a.serveLog = &log
	secret := "SECRET-" + strings.Repeat("z", 500)

	fin := r.one("finish", "--as", "m1", "no-such-card@1", "--epoch", "0", "--report", secret, "--head", "0123456")
	require.Equal(t, 1, fin.Code, fin.Stderr)
	assert.Contains(t, log.String(), "SERVE REFUSED verb=finish as=m1 exit=1 card=no-such-card@1: ")
	assert.NotContains(t, log.String(), secret, "the report is not logged")
	assert.NotContains(t, log.String(), "SECRET-", "the report is not logged")
	assert.NotContains(t, log.String(), "--report", "the argument list is not logged")
}

// The server's own SERVE line counts the refused verbs by verb word beside its counts
// (servelanes.go), so the operator reads which verb is being refused without a new verb.
func TestTheServeLineCountsRefusedByVerb(t *testing.T) {
	t.Parallel()
	ta := laneRig(t)
	var log bytes.Buffer
	ta.a.serveLog = &log
	require.Equal(t, 1, serveOne(ta.a, false, "finish", "--as", "m1", "no-such@1", "--epoch", "0").Code)
	require.Equal(t, 2, serveOne(ta.a, true, "tick").Code)
	assert.NotContains(t, log.String(), "SERVE batches=", "the cost line is not said before the minute")

	ta.a.sleep(ServeSayEvery)
	serveOne(ta.a, false, "friend", "beat", "amy")
	assert.Contains(t, log.String(), " SERVE batches=")
	assert.Contains(t, log.String(), "refused=finish:1,tick:1", "the refused verbs by verb word")
}

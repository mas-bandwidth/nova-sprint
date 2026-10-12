package swarm

// WHAT THE CARD IS SAYING, READ WHILE IT IS STILL SAYING IT.
//
// Until this file every wall question was asked of a file AFTER the child was gone:
// cmd/nova-swarm/native.go re-opened <job>/harness-output.log once the process had exited.
// A run that stopped
// making progress therefore cost its WHOLE deadline before anybody looked, and the
// coordinator watching it saw nothing at all in the meantime.
//
// MEASURED, inside the real wall (landlock abi 4) and on one dead card:
//
//   - A wall refusal does NOT stop a card. Three probe cards inside the real wall
//     (`wallprobewrite`, `wallprobeexec`, `wallprobehang`) each took a refusal -- rc=2 and
//     `Permission denied` on a write to /etc, rc=1 on `sudo`, rc=127 on a missing binary,
//     rc=2 and `Permission denied` on a write to /tmp -- and each read the tool error, went
//     on, and published a correct RESULT.md in 13-35 seconds. The model routes around the
//     wall; killing a card at its first refusal would kill working cards.
//   - The card that DID die (rc=-1, wall=1200.04s, no RESULT.md) was not stopped by the
//     wall at all. Its ONE `Operation not permitted` is
//     line 5 of its log -- the harness's own startup banner, before STEP 1 -- and the card
//     then worked for SIXTEEN more model steps past it. Its last three log lines are a
//     provider stream opening at 14:53:36.958Z and then nothing: it died waiting on a model
//     turn that never answered, and the post-mortem scan named the banner line as the cause.
//
// So two things are wrong and both are here. A refusal must be NAMED THE MOMENT IT HAPPENS,
// on one typed line the coordinator reads live instead of at the reap; and a refusal the
// card MOVED PAST is not what killed it, whatever order the lines are in.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// wallMovedOn reports whether the card made another tool call after the line that named
// path. A path it cannot find again is treated as not moved on: the caller already has a
// refusal, and inventing a recovery it did not see would throw the classification away.
// wallRefusalLine reports whether one already-trimmed, already-stripped line is a refusal in
// the three grammars this package knows: the sandbox's own word, and the kernel's two, each
// of the last on a path.
// WallKind is the ONE WORD a refusal is reported by -- the `<what>` of the typed line -- and
// it is read out of the refusal's own sentence rather than guessed: `home` for the wall's
// own home_outside refusal, `write` and `read` for a path the kernel refused in one
// direction, `exec` for a refused execution, `fence` for the harness's own auto-reject, and
// `denied` when the sentence names no direction at all.
func WallKind(line string) string {
	l := strings.ToLower(stripPaint(line))
	switch {
	case strings.Contains(l, "reason=home_outside"):
		return "home"
	case strings.Contains(l, "auto-reject"), strings.Contains(l, "permission requested"):
		return "fence"
	case strings.Contains(l, "cannot execute"), strings.Contains(l, "exec format"), strings.Contains(l, "permission denied") && strings.Contains(l, "./"):
		return "exec"
	case strings.Contains(l, "cannot create"), strings.Contains(l, "cannot write"), strings.Contains(l, "cannot touch"),
		strings.Contains(l, "cannot make"), strings.Contains(l, "cannot create directory"), strings.Contains(l, "cannot remove"),
		strings.Contains(l, "read-only file system"), strings.Contains(l, "mkdir"), strings.Contains(l, "mkdtemp"):
		return "write"
	case strings.Contains(l, "unable to read"), strings.Contains(l, "cannot open"), strings.Contains(l, "cannot read"),
		strings.Contains(l, "cannot access"), strings.Contains(l, "no such file or directory"):
		return "read"
	}
	return "denied"
}

// WallRefusedLine is the typed line a refusal is announced on THE MOMENT IT IS SEEN, before
// anything is decided about it and while the card is still running:
//
//	WALL REFUSED <what> <path> task=<id> step=<n>
//
// It is a note, never a verdict: a card that goes on to publish is done, and this line
// having been printed takes nothing away from it.
func WallRefusedLine(task, kind, path, step string) string {
	return fmt.Sprintf("WALL REFUSED %s %s task=%s step=%s",
		oneline.Field(dashOr(kind)), oneline.Field(dashOr(path)), oneline.Field(task), oneline.Field(dashOr(step)))
}

// WallReader is an io.Writer over the child's own output, put in the capture chain beside
// the log files and the timeline so every byte is seen AS IT ARRIVES rather than re-read
// from a file once the child is gone. It holds a partial line until its newline, the way
// Timeline does, and it is safe for the child's concurrent stdout and stderr copies.
//
// It decides nothing. It remembers the first refusal, the last STEP the card reached, and
// whether the card made another tool call after the refusal -- and it calls announce ONCE,
// with the typed line, the first time a refusal appears.
type WallReader struct {
	mu       sync.Mutex
	buf      []byte
	refusal  WallRefusal
	kind     string
	refused  bool
	movedOn  bool
	step     string
	announce func(string)
	task     string
}

// NewWallReader returns a reader for one card. announce is called once, from the writing
// goroutine, with the typed line of the first refusal; a nil announce says nothing.
func NewWallReader(task string, announce func(string)) *WallReader {
	return &WallReader{task: task, announce: announce}
}

// Write consumes the child's output and never fails: a reader that could fail a write would
// break the MultiWriter it sits in and take the card's own log down with it.
// Stopped is the refusal that stopped this card -- a refusal with no tool call after it --
// and false when there was no refusal or the card moved past the one there was.
func (r *WallReader) Stopped() (WallRefusal, string, bool) {
	if r == nil {
		return WallRefusal{}, "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.refused || r.movedOn {
		return WallRefusal{}, "", false
	}
	return r.refusal, r.kind, true
}

// Step is the last `STEP <n>` the card printed, or "" when it printed none.
func (r *WallReader) Step() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.step
}

// BlockedResultName is the report a run writes FOR a card that never published one, so the
// coordinator's harvest reads a named end instead of an absence. It is `RESULT.md` because
// that is the one file every gather already looks for, and its first line carries no
// findings head, so it can be scored `plan-only` and NEVER `ok` or `clean`: a report the
// machinery wrote can never be counted as work a worker did.
const BlockedResultName = "RESULT.md"

// WriteBlockedResult writes the card's report when the run ended it and it published none.
// It REFUSES to overwrite a result that exists -- a card that published owns its report --
// and it says who wrote it on its own line, because a reader must never have to guess
// whether a worker or the machinery wrote what they are reading.
//
// When kind is empty (idle end with no refusal), the report uses CardIdleLine.
// When kind is set (idle end after a refusal), the report uses WallRefusedLine.
func WriteBlockedResult(jobDir, task, kind, path, step, reason string) (string, bool, error) {
	if _, found := FindCardResult(jobDir); found {
		return "", false, nil
	}
	dest := filepath.Join(jobDir, BlockedResultName)
	var statusLine string
	if kind == "" {
		// Idle end with no refusal: use CARD IDLE line
		statusLine = CardIdleLine(task, IdleEnd{Step: step})
	} else {
		// Idle end with a refusal: use WALL REFUSED line
		statusLine = WallRefusedLine(task, kind, path, step)
	}
	body := strings.Join([]string{
		"RESULT: BLOCKED " + oneline.Field(task),
		statusLine,
		"blocked: " + oneline.Escape(reason),
		"written-by: nova-swarm native (the card published no report of its own)",
		"",
	}, "\n")
	if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
		return "", false, err
	}
	return dest, true, nil
}

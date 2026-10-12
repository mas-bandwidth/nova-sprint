package friend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Hosted in tmux (docs/SPEC-FRIEND.md, "Hosted in tmux"): a terminal harness
// (OpenCode, Grok, Aider, any TUI) started by `nova-friend host` runs in a
// detached tmux session named friend-<name>. The friend's session is the TUI
// in that pane; the daemon types into it as a person would and a person can
// attach and watch. The adapter is a function of captured screens and a
// clock: every tmux call goes through the Exec seam.

// TmuxPrefix starts the name of every hosted session: friend-<name>.
const TmuxPrefix = "friend-"

// Timing of a typed delivery: the pane is polled each TmuxPoll for up to
// TmuxAcceptWithin for the prompt line to go (the turn started).
const (
	TmuxPoll         = 500 * time.Millisecond
	TmuxAcceptWithin = time.Minute
)

// genericPrompt is the idle prompt of a pane whose harness names none: a lone
// prompt character on the last line.
const genericPrompt = `^\s*[>❯]\s*$`

// HostPrompts is each hostable harness's idle prompt pattern, matched against
// the last non-empty line of the captured pane. It is data: --prompt on host
// overrides it for a harness that draws its prompt differently.
var HostPrompts = map[string]string{
	"opencode": `^\s*[┃>]?\s*Ask anything`,
	"grok":     genericPrompt,
	"aider":    `^(\S+ )?>\s*$`,
}

// PromptPattern compiles a prompt pattern, panicking on a bad constant.
func PromptPattern(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// HostFile is the friend's host state in its state directory.
const HostFile = "host.json"

// Hosted is what host saves so run and install need no flag: the tmux
// session, the harness it hosts and the idle prompt pattern (source text).
type Hosted struct {
	Session string `json:"session"`
	Harness string `json:"harness"`
	Prompt  string `json:"prompt"`
}

// Tmux delivers into a TUI hosted in a tmux pane. Deliver captures the pane
// (`tmux capture-pane -p -t <session>`); when its last non-empty line matches
// the idle prompt it types the text literally (`send-keys -l`), then Enter as
// a second call, and is accepted once the prompt line has gone, the turn
// having started. While the prompt is absent a turn runs: Deferred, so no
// second turn lands beside one. A missing session is Deferred with the host
// line, never a failure.
type Tmux struct {
	Dir     string         // the friend's directory, the working directory of each tmux call
	Session string         // the tmux session, friend-<name>
	Prompt  *regexp.Regexp // the idle prompt, matched against the last non-empty line
	Run     Exec
	Out     io.Writer
	Now     func() time.Time                           // time.Now when nil
	Sleep   func(ctx context.Context, d time.Duration) // a real wait when nil
}

func (t *Tmux) prompt() *regexp.Regexp {
	if t.Prompt == nil {
		return PromptPattern(genericPrompt)
	}
	return t.Prompt
}

func (t *Tmux) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

func (t *Tmux) sleep(ctx context.Context, d time.Duration) {
	if t.Sleep != nil {
		t.Sleep(ctx, d)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// errNoSession is capture's answer when the tmux session does not exist.
var errNoSession = errors.New("no tmux session")

// capture is the pane's screen as text (SPEC-FRIEND.md, "Hosted in tmux").
func (t *Tmux) capture(ctx context.Context) (string, error) {
	out, exit, err := t.Run(ctx, t.Dir, "tmux", []string{"capture-pane", "-p", "-t", t.Session}, "")
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %w", err)
	}
	if exit != 0 {
		low := strings.ToLower(out)
		if strings.Contains(low, "can't find") || strings.Contains(low, "no server") || strings.Contains(low, "no sessions") || strings.Contains(low, "error connecting") {
			return "", errNoSession
		}
		return "", fmt.Errorf("tmux capture-pane exited %d: %s", exit, strings.TrimSpace(Head(out, 200)))
	}
	return out, nil
}

// idle says whether the screen's last non-empty line is the idle prompt: the
// idle rule of SPEC-FRIEND.md, "Hosted in tmux".
func (t *Tmux) idle(screen string) bool {
	lines := strings.Split(strings.TrimRight(screen, " \t\r\n"), "\n")
	return t.prompt().MatchString(strings.TrimRight(lines[len(lines)-1], " \t\r"))
}

// HostLine is the line a person runs to host the friend again.
func (t *Tmux) hostLine() string {
	return fmt.Sprintf("nova-friend host --as %s --harness <h> --dir %s -- <launch command...>", strings.TrimPrefix(t.Session, TmuxPrefix), t.Dir)
}

// Busy says whether a turn runs in the pane now: the session exists and its
// prompt is absent (SPEC-FRIEND.md, "Hosted in tmux"). A missing session is
// not busy.
func (t *Tmux) Busy(ctx context.Context) (bool, error) {
	screen, err := t.capture(ctx)
	if errors.Is(err, errNoSession) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !t.idle(screen), nil
}

// Deliver types text into the idle pane (SPEC-FRIEND.md, "Hosted in tmux";
// Delivery.tla: the pane's prompt is the session's free state, the typed line
// starts the turn that makes it busy).
func (t *Tmux) Deliver(ctx context.Context, text string) (int, error) {
	screen, err := t.capture(ctx)
	if errors.Is(err, errNoSession) {
		return 0, Deferred{Reason: fmt.Sprintf("the tmux session %s is not running; run: %s", t.Session, t.hostLine())}
	}
	if err != nil {
		return 0, err
	}
	if !t.idle(screen) {
		return 0, Deferred{Reason: fmt.Sprintf("a turn runs in %s: its prompt is not shown", t.Session)}
	}
	for _, keys := range [][]string{{"send-keys", "-t", t.Session, "-l", TypedLine(text)}, {"send-keys", "-t", t.Session, "Enter"}} {
		out, exit, err := t.Run(ctx, t.Dir, "tmux", keys, "")
		if err != nil {
			return 0, fmt.Errorf("tmux send-keys: %w", err)
		}
		if exit != 0 {
			return 0, fmt.Errorf("tmux send-keys exited %d: %s", exit, strings.TrimSpace(Head(out, 200)))
		}
	}
	deadline := t.now().Add(TmuxAcceptWithin)
	for {
		screen, err := t.capture(ctx)
		if err != nil {
			return 0, err
		}
		if !t.idle(screen) {
			if t.Out != nil {
				fmt.Fprintln(t.Out, "typed into "+t.Session+"; the turn runs after this")
			}
			return 0, nil
		}
		if !t.now().Before(deadline) || ctx.Err() != nil {
			return 0, fmt.Errorf("typed into %s and its prompt is still shown after %s: no turn started", t.Session, TmuxAcceptWithin)
		}
		t.sleep(ctx, TmuxPoll)
	}
}

// TypedLine is text as the one line a TUI is typed: each newline shown as
// " ⏎ ", as the Grok adapter does (SPEC-FRIEND.md, "Hosted in tmux").
func TypedLine(text string) string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	return strings.ReplaceAll(text, "\n", " ⏎ ")
}

// HostSpec is one host: the friend, its directory, the launch command and
// whether to only print.
type HostSpec struct {
	Name, Dir string
	Command   []string
	DryRun    bool
}

// HostResult is what host started (or, on a dry run, would start).
type HostResult struct {
	Session, Attach, Line string
}

// HostRefused is host's refusal: the session runs already.
type HostRefused struct{ Session string }

// Alive: the hosted tmux session exists (`tmux has-session`); the TUI inside
// it may still be at any state, which the session check alone answers.
func (t *Tmux) Alive(ctx context.Context) Liveness {
	if t.Run == nil {
		return cannotTell("tmux: no runner")
	}
	out, exit, err := t.Run(ctx, t.Dir, "tmux", []string{"has-session", "-t", t.Session}, "")
	switch {
	case err != nil:
		return cannotTell("tmux has-session: " + err.Error())
	case exit == 0:
		return running("the tmux session " + t.Session + " runs")
	case strings.Contains(strings.ToLower(out), "can't find") || strings.Contains(strings.ToLower(out), "no server"):
		return notRunning("the tmux session " + t.Session + " is not running")
	}
	return cannotTell(fmt.Sprintf("tmux has-session exited %d", exit))
}

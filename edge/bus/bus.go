// Package bus is the nova-bus edge of layer 4 (docs/LAYER-4-EDGES.md): it sends
// one message and reads who is present, through the nova-bus binary. What to
// send, to whom and when the cursor moves is run's (layer 6); this package
// holds no cursor and reads no store.
package bus

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
)

const (
	DefaultSender   = "sprint"
	DefaultTimeout  = 5 * time.Second
	MaxMessageBytes = 4096
)

type (
	// Bus is the seam run sends and asks presence through.
	Bus interface {
		Send(ctx context.Context, to, subject, body string) (msgID string, err error)
		Who(ctx context.Context) (map[string]Presence, error)
	}
	Presence struct{ Name, Uptime, Detail string }
	Config   struct {
		Exec        edge.Exec
		Bin, Sender string
		Timeout     time.Duration
		Env         []string
		AllowNew    bool
	}
	Edge struct {
		exec        edge.Exec
		bin, sender string
		timeout     time.Duration
		env         []string
		allowNew    bool
	}
)

func New(cfg Config) *Edge {
	return &Edge{
		exec: cmp.Or[edge.Exec](cfg.Exec, edge.NewOSExec()), bin: cmp.Or(cfg.Bin, "nova-bus"),
		sender: cmp.Or(cfg.Sender, DefaultSender), timeout: cmp.Or(cfg.Timeout, DefaultTimeout),
		env: slices.Clone(cfg.Env), allowNew: cfg.AllowNew,
	}
}

// Send runs `nova-bus send` and returns the message id only when nova-bus
// exited 0 and printed `<to> <id>`; anything else is an error and the caller's
// cursor does not move.
func (e *Edge) Send(ctx context.Context, to, subject, body string) (string, error) {
	return e.send(ctx, to, subject, body)
}

func (e *Edge) send(ctx context.Context, to, subject, body string, extra ...string) (string, error) {
	switch {
	case strings.TrimSpace(to) == "" || strings.TrimSpace(subject) == "":
		return "", fmt.Errorf("bus send: recipient and subject are required; pass --to <name> and a non-empty subject")
	case len(body) > MaxMessageBytes:
		return "", fmt.Errorf("bus send to %s (%s): body is %d bytes, exceeding the %d-byte cap; truncate before sending", to, subject, len(body), MaxMessageBytes)
	}
	argv := []string{e.bin, "send", "--as", e.sender, "--to", to}
	if e.allowNew {
		argv = append(argv, "--new")
	}
	argv = append(argv, extra...)
	res, err := e.exec.Run(ctx, edge.Cmd{Argv: append(argv, subject, body), Env: e.env, Timeout: e.timeout})
	if err != nil {
		return "", fmt.Errorf("bus send to %s (%s): %w", to, subject, err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("bus send to %s (%s): nova-bus exited %d (%s); verify %s is hooked in (`nova-bus who`)", to, subject, res.ExitCode, strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout)), e.sender)
	}
	if msgID, ok := parseSendConfirmation_internal(res.Stdout, to); ok {
		return msgID, nil
	}
	return "", fmt.Errorf("bus send to %s (%s): missing `<recipient> <id>` confirmation in stdout %q; leaving cursor unmoved", to, subject, strings.TrimSpace(res.Stdout))
}
// Reply answers message re from to (`nova-bus send --re`), with the same
// confirmation rule as Send. The sprint hook's bus-reply uses it to point a
// sender at its verbs; it reads no body.
func (e *Edge) Reply(ctx context.Context, to, re, subject, body string) (string, error) {
	if strings.TrimSpace(re) == "" {
		return "", fmt.Errorf("bus reply to %s: the message id is required; pass NOVA_BUS_ID", to)
	}
	return e.send(ctx, to, subject, body, "--re", re)
}

func parseSendConfirmation_internal(stdout, to string) (string, bool) {
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(strings.TrimSpace(line)); len(fields) == 2 && fields[0] == to && fields[1] != "" {
			return fields[1], true
		}
	}
	return "", false
}
func (e *Edge) Who(ctx context.Context) (map[string]Presence, error) {
	res, err := edge.RunArgv(ctx, e.exec, e.timeout, "", e.env, e.bin, "who")
	if err != nil {
		return nil, fmt.Errorf("bus who: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("bus who: nova-bus exited %d (%s); check NOVA_BUS_REDIS and daemon status", res.ExitCode, strings.TrimSpace(cmp.Or(res.Stderr, res.Stdout)))
	}
	return parseWhoOutput_internal(res.Stdout)
}
func parseWhoOutput_internal(stdout string) (map[string]Presence, error) {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "WHO OK") {
		return nil, fmt.Errorf("bus who: unexpected header %q; expected `WHO OK present=<n>`", strings.TrimSpace(stdout))
	}
	out := make(map[string]Presence)
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 || parts[0] == "" {
			return nil, fmt.Errorf("bus who: malformed presence line %q", line)
		}
		p := Presence{Name: parts[0], Uptime: parts[1]}
		if len(parts) == 3 {
			p.Detail = parts[2]
		}
		out[p.Name] = p
	}
	return out, nil
}

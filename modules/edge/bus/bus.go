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
	"strconv"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
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

func (e *Edge) send(ctx context.Context, to, subject, body string) (string, error) {
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
	cmd := edge.Cmd{Argv: append(argv, subject, body), Env: e.env, Timeout: e.timeout}
	res, err := e.exec.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("bus send to %s (%s): %w", to, subject, edge.Failed(cmd, res, err))
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("bus send to %s (%s): %w; verify %s is hooked in (`nova-bus who`)", to, subject, edge.Failed(cmd, res, nil), e.sender)
	}
	if msgID, ok := parseSendConfirmation_internal(res.Stdout, to); ok {
		return msgID, nil
	}
	return "", fmt.Errorf("bus send to %s (%s): missing `<recipient> <id>` confirmation: %s; leaving cursor unmoved", to, subject, edge.ParseRefused("no `"+to+" <id>` line", 0, []byte(res.Stdout)))
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
	cmd := edge.Cmd{Argv: []string{e.bin, "who"}, Env: e.env}
	res, err := edge.RunArgv(ctx, e.exec, e.timeout, "", cmd.Env, cmd.Argv...)
	if err != nil {
		return nil, fmt.Errorf("bus who: %w", edge.Failed(cmd, res, err))
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("bus who: %w; check NOVA_BUS_REDIS and daemon status", edge.Failed(cmd, res, nil))
	}
	return ParseWho([]byte(res.Stdout))
}

// ParseWho is the one parse of `nova-bus who`: "WHO OK present=<n>", then
// n lines of name<TAB>uptime[<TAB>detail], each name once.
func ParseWho(out []byte) (map[string]Presence, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if !strings.HasPrefix(lines[0], "WHO OK present=") {
		return nil, fmt.Errorf("bus who: expected `WHO OK present=<n>`: %s", edge.ParseRefused("unexpected header", 0, out))
	}
	n, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if err != nil {
		return nil, fmt.Errorf("bus who: expected `WHO OK present=<n>`: %s", edge.ParseFailure(err, out))
	}
	if n != len(lines)-1 {
		return nil, fmt.Errorf("bus who: %s", edge.ParseRefused(fmt.Sprintf("present=%d but %d name lines", n, len(lines)-1), len(lines[0]), out))
	}
	who := make(map[string]Presence, n)
	at := len(lines[0]) + 1
	for _, line := range lines[1:] {
		parts := strings.SplitN(line, "\t", 3)
		if _, seen := who[parts[0]]; len(parts) < 2 || parts[0] == "" || seen {
			return nil, fmt.Errorf("bus who: %s", edge.ParseRefused("a name line is not a new name<TAB>uptime", at, out))
		}
		at += len(line) + 1
		p := Presence{Name: parts[0], Uptime: parts[1]}
		if len(parts) == 3 {
			p.Detail = parts[2]
		}
		who[p.Name] = p
	}
	return who, nil
}

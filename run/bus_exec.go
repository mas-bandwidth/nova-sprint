package run

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// BusConfig contains only the bus connection, not store or provider secrets.
type BusConfig struct{ Binary, Redis, User, Password, ReplyExecutable string }
type NovaBus struct{ cfg BusConfig }

func NewNovaBus(c BusConfig) (*NovaBus, error) {
	if !filepath.IsAbs(c.Binary) || c.Redis == "" || !filepath.IsAbs(c.ReplyExecutable) {
		return nil, fmt.Errorf("run: nova-bus binary, Redis and reply executable are required")
	}
	return &NovaBus{cfg: c}, nil
}
func (b *NovaBus) env() []string {
	env := []string{"NOVA_BUS_REDIS=" + b.cfg.Redis, "PATH=" + filepath.Dir(b.cfg.Binary) + ":/usr/bin:/bin"}
	if b.cfg.User != "" {
		env = append(env, "NOVA_BUS_USER="+b.cfg.User)
	}
	if b.cfg.Password != "" {
		env = append(env, "NOVA_BUS_PASSWORD="+b.cfg.Password)
	}
	return env
}
func (b *NovaBus) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	cmd.Env = b.env()
	return cmd
}
func (b *NovaBus) Who(ctx context.Context) ([]string, error) {
	out, err := b.command(ctx, "who").Output()
	if err != nil {
		return nil, fmt.Errorf("run: nova-bus who failed: %w", err)
	}
	return parseWho(string(out))
}
func parseWho(output string) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "WHO OK present=") {
		return nil, fmt.Errorf("run: malformed nova-bus who receipt")
	}
	count, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if err != nil || count < 0 {
		return nil, fmt.Errorf("run: malformed nova-bus who count")
	}
	names := []string{}
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || !busName(fields[0]) || seen[fields[0]] {
			return nil, fmt.Errorf("run: malformed nova-bus who row")
		}
		seen[fields[0]] = true
		names = append(names, fields[0])
	}
	if len(names) != count {
		return nil, fmt.Errorf("run: nova-bus who count mismatch")
	}
	return names, nil
}
func busName(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-") == ""
}
func (b *NovaBus) Send(ctx context.Context, to, subject, body string) (Delivery, error) {
	if !busName(to) || subject == "" {
		return Delivery{}, fmt.Errorf("run: invalid bus recipient or subject")
	}
	cmd := b.command(ctx, "send", "--as", "sprint", "--to", to, subject, "-")
	cmd.Stdin = bytes.NewBufferString(body)
	out, err := cmd.Output()
	if err != nil {
		return Delivery{}, fmt.Errorf("run: nova-bus send failed: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] != to || !validDeliveryID(fields[1]) {
		return Delivery{}, fmt.Errorf("run: invalid nova-bus send receipt")
	}
	return Delivery{Recipient: fields[0], ID: fields[1]}, nil
}

// ReplyTo answers a hook delivery without examining its body. The correlation
// ID is supplied by nova-bus, and the reply names the sender's next verb.
func (b *NovaBus) ReplyTo(ctx context.Context, to, id string) (Delivery, error) {
	if !busName(to) || to == "sprint" || !validDeliveryID(id) {
		return Delivery{}, fmt.Errorf("run: invalid bus reply target or id")
	}
	body := "replies are verbs: run nova-sprint next --as " + to
	cmd := b.command(ctx, "send", "--as", "sprint", "--to", to, "--re", id, "REPLY", "-")
	cmd.Stdin = bytes.NewBufferString(body)
	out, err := cmd.Output()
	if err != nil {
		return Delivery{}, fmt.Errorf("run: nova-bus reply failed: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] != to || !validDeliveryID(fields[1]) {
		return Delivery{}, fmt.Errorf("run: invalid nova-bus reply receipt")
	}
	return Delivery{Recipient: fields[0], ID: fields[1]}, nil
}

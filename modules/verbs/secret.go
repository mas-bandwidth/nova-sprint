package verbs

import (
	"context"
	"io"
	"os/exec"
	"strings"
)

// SecretSink checks presence and saves a key through stdin, never argv or env.
type SecretSink interface {
	Has(context.Context, string) (bool, error)
	Set(context.Context, string, io.Reader) error
}

// NovaSecrets uses the configured local secret store and passes values only on stdin.
type NovaSecrets struct{ Store, KeyFile, Pins, Seat string }

// ExecReady refuses before a `nova-secrets exec` argv is built when the
// store, key or seat it needs is empty, naming the variable to set.
func (n NovaSecrets) ExecReady() error {
	for _, v := range []struct{ name, value string }{{"NOVA_SECRETS_STORE", n.Store}, {"NOVA_SECRETS_KEY", n.KeyFile}, {"NOVA_SECRETS_SEAT", n.Seat}} {
		if v.value == "" {
			return &OperationError{Problem: "control-seat secrets configuration is incomplete (" + v.name + " is not set)", Fix: "set NOVA_SECRETS_STORE, NOVA_SECRETS_KEY, NOVA_SECRETS_PINS and NOVA_SECRETS_SEAT; run nova-secrets -h", Exit: 1}
		}
	}
	return nil
}

func (n NovaSecrets) Has(ctx context.Context, name string) (bool, error) {
	args := []string{"names", "--store", n.Store}
	if n.Seat != "" {
		args = append(args, "--as", n.Seat)
	}
	out, err := Output(ctx, "nova-secrets", args...)
	if err != nil {
		return false, RefusalOr(err, &OperationError{Problem: "nova-secrets could not list names (" + errText(err) + ")", Fix: "run nova-sprint up", Exit: 1})
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true, nil
		}
	}
	return false, nil
}
func (n NovaSecrets) Set(ctx context.Context, name string, key io.Reader) error {
	args := []string{"set", name, "--store", n.Store, "--key", n.KeyFile, "--pins", n.Pins}
	if n.Seat != "" {
		args = append(args, "--as", n.Seat)
	}
	cmd := exec.CommandContext(ctx, "nova-secrets", args...)
	cmd.Stdin = key // the key goes on stdin only: never argv, env or output
	if err := runTool(cmd, nil); err != nil {
		return RefusalOr(err, &OperationError{Problem: "nova-secrets could not save the key (" + errText(err) + ")", Fix: "run nova-sprint up --key <service>", Exit: 1})
	}
	return nil
}

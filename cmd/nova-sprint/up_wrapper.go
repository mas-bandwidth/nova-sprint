package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/verbs"
)

// upWithCredentials is up's refusal of a process holding a database
// credential (under nova-secrets exec, say): up is the one verb run bare,
// and the refusal ends with the command that runs it so (L6.4).
func upWithCredentials(actor string) string {
	if actor == "" {
		actor = "<you>"
	}
	return "SPRINT REFUSED up: up is the one verb run without database credentials (each step's child gets its own from nova-secrets): env -u NOVA_PG_ADMIN_PASSWORD -u NOVA_SPRINT_PG_PASSWORD nova-sprint up --as " + actor
}

// runUpWrapper is up in a process that holds no database credential: it
// walks steps 1 and 2 itself (each credential only in that step's
// nova-secrets child), then the whole walk again in one child holding the
// sprint's credential and nothing else (LAYER-6 section 5).
func runUpWrapper(ctx context.Context, args []string) int {
	input, ok := verbs.ParseSetup(args, os.Getenv("NOVA_SPRINT_AS"))
	if !ok {
		r := (&verbs.Engine{Actor: os.Getenv("NOVA_SPRINT_AS")}).Execute(ctx, append([]string{"up"}, args...))
		return emit(r.Output, r.Exit)
	}
	if os.Getenv("NOVA_PG_ADMIN_PASSWORD") != "" || os.Getenv("NOVA_SPRINT_PG_PASSWORD") != "" {
		return emit(upWithCredentials(input.Actor)+"\n", 1)
	}
	cfg, err := productionSetupConfigFromEnv()
	if err != nil {
		return emit("SPRINT REFUSED up: setup configuration unavailable: "+edge.RedactN(err.Error(), edge.CommandMax)+"\n", 1)
	}
	if err = (productionSetup{cfg: cfg}).configError(); err != nil {
		return emit("SPRINT REFUSED up: "+edge.RedactN(err.Error(), edge.CommandMax)+"\n", 1)
	}
	if _, err = setupRoleDSN(cfg.DSN, "nova_sprint"); err != nil {
		return emit("SPRINT REFUSED up: configure a passwordless NOVA_PG_DSN ("+edge.RedactN(err.Error(), edge.CommandMax)+")\n", 1)
	}
	e, closeAll := newEngine(append([]string{"up"}, args...), "store")
	r := e.Execute(ctx, append([]string{"up"}, args...))
	closeAll()
	if r.Exit != 0 || r.Output != "" {
		return emit(r.Output, r.Exit)
	}
	return runUpChild(ctx, cfg, args)
}

// emit writes output on stdout and returns code; when stdout cannot take
// it, the output and why go to stderr and the exit is 2.
func emit(output string, code int) int {
	if _, err := fmt.Fprint(os.Stdout, output); err != nil {
		fmt.Fprintf(os.Stderr, "SPRINT REFUSED up: cannot write to stdout (%s); the output was: %s", edge.RedactN(err.Error(), edge.CommandMax), output)
		return 2
	}
	return code
}

// runUpChild is the walk under the sprint's credential: nova-secrets exec
// with only NOVA_SPRINT_PG_PASSWORD, its output passed through, its exit
// up's. nova-secrets exec drops NOVA_SECRETS_* from its child; the walk
// reads them (productionSetupConfigFromEnv), so env puts the paths back.
func runUpChild(ctx context.Context, cfg productionSetupConfig, args []string) int {
	s := cfg.Secrets
	flags := []string{"exec", "--only", "NOVA_SPRINT_PG_PASSWORD", "--store", s.Store, "--key", s.KeyFile, "--as", s.Seat, "--", "/usr/bin/env",
		"NOVA_SECRETS_STORE=" + s.Store, "NOVA_SECRETS_KEY=" + s.KeyFile, "NOVA_SECRETS_PINS=" + s.Pins, "NOVA_SECRETS_SEAT=" + s.Seat, cfg.Binary, "--internal-up"}
	cmd := exec.CommandContext(ctx, "nova-secrets", append(flags, args...)...)
	cmd.Env = append(setupWrapperEnv(os.Environ()), "NOVA_PG_PASSWORD_ENV=NOVA_SPRINT_PG_PASSWORD")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode()
	}
	return emit("SPRINT REFUSED up: the walk under the sprint credential did not run: "+edge.Failure(cmd.Args, cmd.Env, -1, "", "", err).Error()+"\n", 2)
}

func setupWrapperEnv(source []string) []string {
	names := map[string]bool{"PATH": true, "HOME": true, "SSH_AUTH_SOCK": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true, "NOVA_PG_DSN": true, "NOVA_BUS_REDIS": true, "NOVA_BUS_USER": true, "NOVA_SPRINT_BACKUP_DIR": true, "NOVA_SECRETS_STORE": true, "NOVA_SECRETS_KEY": true, "NOVA_SECRETS_PINS": true, "NOVA_SECRETS_SEAT": true, "NOVA_SPRINT_AS": true}
	out := []string{}
	for _, entry := range source {
		name, _, ok := strings.Cut(entry, "=")
		if ok && names[name] {
			out = append(out, entry)
		}
	}
	return out
}

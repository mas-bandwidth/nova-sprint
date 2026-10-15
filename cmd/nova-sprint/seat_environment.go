package main

import (
	"errors"
	"path/filepath"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-tools/pkg/seatcred"
)

// seatEnvironment is the per-user v1 seat record as nova-tools pkg/seatcred reads
// it, nova-up's writer's own reader. This command is its consumer: it holds no
// second reader, path rule, owner check or field allowlist of the record.
type seatEnvironment = seatcred.SeatEnvironment

func (a *app) seatEnvironmentPath() (string, error) {
	if x := a.getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "nova-sprint", "seat-environment.json"), nil
	}
	h, err := a.home()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(h) {
		return "", errors.New("seat environment has no absolute home directory")
	}
	return filepath.Join(h, ".config", "nova-sprint", "seat-environment.json"), nil
}

func (a *app) loadSeatEnvironment() error {
	record, ok, err := seatcred.ReadSeatEnvironment(a.getenv)
	if err != nil || !ok {
		return err
	}
	a.seatEnvironment = &record
	env := a.getenv
	a.getenv = func(k string) string {
		if v := env(k); v != "" {
			return v
		}
		if k == "NOVA_SPRINT_REDIS" {
			if v := env("NOVA_REDIS_ADDR"); v != "" {
				return v
			}
		}
		return record.Environment[k]
	}
	return nil
}

func seatEnvironmentNeeded(args []string) bool {
	if len(args) == 0 || args[0] == "help" || args[0] == "version" || args[0] == "--version" || verbflag.IsHelp(args[0]) {
		return false
	}
	v := readVerb(args)
	return v.words != 0 && v.err == nil && !v.help
}

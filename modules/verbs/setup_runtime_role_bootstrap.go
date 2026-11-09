package verbs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
)

// SprintRoleBootstrap stores one generated credential before distributing it
// and invokes a child that rereads the durable values under nova-secrets exec.
// The child may set only the up-owned nova_sprint role. Every run proves the
// role's password is the stored credential, and sets it from the stored
// credential when it is not (defect 37: the seat's value is the one every
// service reads), so a run interrupted after saving and before setting
// recovers by running again.
type SprintRoleBootstrap struct {
	Sink    SecretSink
	Grants  FleetGrants
	Secrets NovaSecrets
	Binary  string
	Random  io.Reader
	Execute func(context.Context, string, ...string) error
}
type SprintSeat struct{ Seat, Machine string }

func (s SprintRoleBootstrap) Run(ctx context.Context, seats []SprintSeat) error {
	if s.Sink == nil || !filepath.IsAbs(s.Binary) || len(seats) == 0 || s.Secrets.Store == "" || s.Secrets.KeyFile == "" || s.Secrets.Seat == "" {
		return fmt.Errorf("role setup configuration unavailable: secret sink set %t, binary %q absolute %t, %d seat(s), secrets store, key file and seat set %t/%t/%t", s.Sink != nil, Redact(s.Binary), filepath.IsAbs(s.Binary), len(seats), s.Secrets.Store != "", s.Secrets.KeyFile != "", s.Secrets.Seat != "")
	}
	if s.Secrets.Seat != s.Grants.ControlSeat || s.Secrets.Store != s.Grants.Store || s.Secrets.KeyFile != s.Grants.KeyFile {
		return fmt.Errorf("role setup configuration unavailable: the secrets seat %q, store and key file are not the grants' (control seat %q; store same %t, key file same %t)", s.Secrets.Seat, s.Grants.ControlSeat, s.Secrets.Store == s.Grants.Store, s.Secrets.KeyFile == s.Grants.KeyFile)
	}
	for _, seat := range seats {
		if !s.Grants.valid(seat.Seat, seat.Machine, sprintPasswordName) {
			return fmt.Errorf("role setup configuration unavailable: seat %q on machine %q is not a valid grant target for %s", seat.Seat, seat.Machine, sprintPasswordName)
		}
	}
	has, err := s.Sink.Has(ctx, sprintPasswordName)
	if err != nil {
		return err
	}
	if !has {
		if err = s.savePassword(ctx); err != nil {
			return err
		}
	}

	mode := "--internal-setup-role"
	if !has {
		mode = "--internal-setup-role-fresh"
	}
	args := []string{"exec", "--only", "NOVA_PG_ADMIN_PASSWORD," + sprintPasswordName, "--store", s.Secrets.Store, "--key", s.Secrets.KeyFile, "--as", s.Secrets.Seat, "--", s.Binary, mode}
	run := s.Execute
	if run == nil {
		run = RunQuiet
	}
	if err = run(ctx, "nova-secrets", args...); err != nil {
		if r := AsRefusal(err); r != nil {
			return r // the role child's own refusal (an ADMIN OPTION grant to make, a role it may not create)
		}
		return fmt.Errorf("role setup failed: %w", err)
	}
	for _, seat := range seats {
		if err = s.GrantSeat(ctx, seat); err != nil {
			return err
		}
	}

	return nil
}
func (s SprintRoleBootstrap) savePassword(ctx context.Context) error {
	source := s.Random
	if source == nil {
		source = rand.Reader
	}
	raw := make([]byte, 32)
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	if _, err := io.ReadFull(source, raw); err != nil {
		return cause("role credential unavailable", err)
	}
	encoded := make([]byte, base64.RawURLEncoding.EncodedLen(len(raw)))
	defer func() {
		for i := range encoded {
			encoded[i] = 0
		}
	}()
	base64.RawURLEncoding.Encode(encoded, raw)
	return s.Sink.Set(ctx, sprintPasswordName, bytes.NewReader(encoded))
}

// GrantSeat never grants a value to its own control-seat grants file.
func (s SprintRoleBootstrap) GrantSeat(ctx context.Context, seat SprintSeat) error {
	if s.Secrets.Seat == "" || s.Secrets.Seat != s.Grants.ControlSeat || s.Secrets.Store != s.Grants.Store || s.Secrets.KeyFile != s.Grants.KeyFile {
		return fmt.Errorf("control seat mismatch")
	}
	if seat.Seat != s.Secrets.Seat {
		if err := s.Grants.Grant(ctx, seat.Seat, seat.Machine, sprintPasswordName); err != nil {
			return err
		}
	}
	return s.Grants.Refresh(ctx, seat.Seat, seat.Machine, sprintPasswordName)
}

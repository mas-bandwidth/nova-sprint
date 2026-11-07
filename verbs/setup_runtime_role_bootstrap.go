package verbs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

// SprintRoleBootstrap stores one generated credential before distributing it
// and invokes a child that rereads the durable values under nova-secrets exec.
type SprintRoleBootstrap struct {
	Sink    SecretSink
	Grants  FleetGrants
	Secrets NovaSecrets
	Binary  string
	Random  io.Reader
	Execute func(context.Context, string, ...string) error
	Inspect CommandOutput
}
type SprintSeat struct{ Seat, Machine string }

func (s SprintRoleBootstrap) Run(ctx context.Context, seats []SprintSeat) error {
	if s.Sink == nil || !filepath.IsAbs(s.Binary) || len(seats) == 0 || s.Secrets.Store == "" || s.Secrets.KeyFile == "" || s.Secrets.Seat == "" {
		return fmt.Errorf("role setup configuration unavailable")
	}
	if s.Secrets.Seat != s.Grants.ControlSeat || s.Secrets.Store != s.Grants.Store || s.Secrets.KeyFile != s.Grants.KeyFile {
		return fmt.Errorf("role setup configuration unavailable")
	}
	for _, seat := range seats {
		if !s.Grants.valid(seat.Seat, seat.Machine, sprintPasswordName) {
			return fmt.Errorf("role setup configuration unavailable")
		}
	}
	has, err := s.Sink.Has(ctx, sprintPasswordName)
	if err != nil {
		return err
	}
	if !has {
		exists, err := s.roleExists(ctx)
		if err != nil {
			return err
		}
		if exists {
			return &OperationError{Problem: "sprint role exists but its credential is missing", Fix: "recover NOVA_SPRINT_PG_PASSWORD in the control seat", Exit: 1}
		}
		if err = s.savePassword(ctx); err != nil {
			return err
		}
	}
	for _, seat := range seats {
		if err = s.GrantSeat(ctx, seat); err != nil {
			return err
		}
	}

	args := []string{"exec", "--only", "NOVA_PG_ADMIN_PASSWORD," + sprintPasswordName, "--store", s.Secrets.Store, "--key", s.Secrets.KeyFile, "--as", s.Secrets.Seat, "--", s.Binary, "--internal-setup-role"}
	run := s.Execute
	if run == nil {
		run = quietCommand
	}
	if err = run(ctx, "nova-secrets", args...); err != nil {
		return fmt.Errorf("role setup failed")
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
		return fmt.Errorf("role credential unavailable")
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

func (s SprintRoleBootstrap) roleExists(ctx context.Context) (bool, error) {
	args := []string{"exec", "--only", "NOVA_PG_ADMIN_PASSWORD", "--store", s.Secrets.Store, "--key", s.Secrets.KeyFile, "--as", s.Secrets.Seat, "--", s.Binary, "--internal-role-exists"}
	cmd := s.Inspect
	if cmd == nil {
		cmd = toolOutput
	}
	out, err := cmd(ctx, "nova-secrets", args...)
	if err != nil {
		return false, fmt.Errorf("role inspection unavailable")
	}
	var result struct {
		Exists *bool `json:"exists"`
	}
	if len(out) > 1024 || json.Unmarshal(out, &result) != nil || result.Exists == nil {
		return false, fmt.Errorf("role inspection unavailable")
	}
	return *result.Exists, nil
}

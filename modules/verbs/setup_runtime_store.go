package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// PGSetupStore is step 2 in children that reread the durable credentials
// for each probe and migration; its caller never receives a value.
type PGSetupStore struct {
	Role           SprintRoleBootstrap
	ControlMachine string
	Secrets        NovaSecrets
	Binary         string
	Command        CommandOutput
}

func (p PGSetupStore) configured() bool {
	return p.Role.Sink != nil && p.Role.Binary == p.Binary && p.Role.Secrets.Seat == p.Secrets.Seat && p.Role.Secrets.Store == p.Secrets.Store && p.Role.Secrets.KeyFile == p.Secrets.KeyFile
}
func (p PGSetupStore) Ready(ctx context.Context) (bool, error) {
	if !p.configured() {
		return false, fmt.Errorf("role setup unavailable: the store and role setup do not share one binary, seat, store and key")
	}
	has, err := p.Role.Sink.Has(ctx, sprintPasswordName)
	if err != nil || !has {
		return false, err
	}
	// The role's password first, set from the seat (defect 37): a store
	// probe as nova_sprint passes on a pg_hba trust host whatever it is.
	ok, err := p.childWith(ctx, "--internal-setup-role-password", "NOVA_PG_ADMIN_PASSWORD,"+sprintPasswordName)
	if err != nil || !ok {
		return false, err
	}
	return p.child(ctx, "--internal-store-probe")
}
func (p PGSetupStore) Install(ctx context.Context) error {
	if !p.configured() || !fleetName.MatchString(p.ControlMachine) || !fleetName.MatchString(p.Secrets.Seat) {
		return fmt.Errorf("control seat unavailable: store and role setup share one binary, seat, store and key %t; control machine %q and seat %q are fleet names %t/%t", p.configured(), Redact(p.ControlMachine), Redact(p.Secrets.Seat), fleetName.MatchString(p.ControlMachine), fleetName.MatchString(p.Secrets.Seat))
	}
	if err := p.Role.Run(ctx, []SprintSeat{{Seat: p.Secrets.Seat, Machine: p.ControlMachine}}); err != nil {
		return err
	}

	ok, err := p.child(ctx, "--internal-store-setup")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("store setup did not verify: the store child answered ready false after --internal-store-setup with no error (a bug: take it to the project's own tracker with this line)")
	}
	return nil
}
func (p PGSetupStore) child(ctx context.Context, mode string) (bool, error) {
	return p.childWith(ctx, mode, sprintPasswordName)
}

// childWith runs mode under nova-secrets exec with only the names in only
// (comma-separated) and reads its {"ready": bool}.
func (p PGSetupStore) childWith(ctx context.Context, mode, only string) (bool, error) {
	if !filepath.IsAbs(p.Binary) || p.Secrets.Store == "" || p.Secrets.KeyFile == "" || p.Secrets.Seat == "" {
		return false, fmt.Errorf("store child configuration unavailable: binary %q absolute %t; secrets store, key file and seat set %t/%t/%t", Redact(p.Binary), filepath.IsAbs(p.Binary), p.Secrets.Store != "", p.Secrets.KeyFile != "", p.Secrets.Seat != "")
	}
	args := []string{"exec", "--only", only, "--store", p.Secrets.Store, "--key", p.Secrets.KeyFile, "--as", p.Secrets.Seat, "--", p.Binary, mode}
	cmd := p.Command
	if cmd == nil {
		cmd = toolOutput
	}
	out, err := cmd(ctx, "nova-secrets", args...)
	if err != nil {
		if r := AsRefusal(err); r != nil {
			return false, r // the child's own refusal: its Problem and Fix as it wrote them
		}
		return false, fmt.Errorf("store child %s failed: %w", mode, err)
	}
	var result struct {
		Ready *bool `json:"ready"`
	}
	if len(out) > 1024 {
		return false, fmt.Errorf("store child returned invalid result: %d bytes, over 1024; first bytes %q", len(out), firstBytes(out))
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return false, fmt.Errorf("store child returned invalid result: %s", ParseFailure(err, out))
	}
	if result.Ready == nil {
		return false, fmt.Errorf("store child returned invalid result: no \"ready\"; first bytes %q", firstBytes(out))
	}
	return *result.Ready, nil
}

// SprintStoreChild runs only in the dedicated nova-secrets child. Its caller
// receives a boolean and no credential or raw command output. Ready is the
// schema at this build and owned by nova_sprint, and a pg_dump on PATH at
// least the server's major (backup and clear need it, SETUP section 1).
func SprintStoreChild(ctx context.Context, install bool) (bool, error) {
	dsn, password := os.Getenv("NOVA_PG_DSN"), os.Getenv(sprintPasswordName)
	if dsn == "" || password == "" {
		return false, fmt.Errorf("store child configuration unavailable: %s", missingNames(map[string]string{"NOVA_PG_DSN": dsn, sprintPasswordName: password}))
	}
	dsn, err := sprintDSN(dsn)
	if err != nil {
		return false, err
	}
	pg, err := store.Open(ctx, dsn, password)
	if err != nil {
		var pe *pgconn.PgError
		if !install && errors.As(err, &pe) && pe.Code == "28P01" {
			return false, nil
		}
		return false, cause("sprint store unavailable", err)
	}
	defer pg.Close()
	ok, err := ensureStoreSchema(ctx, pg, install)
	if err != nil || !ok {
		return false, err
	}
	return true, PGBackups{DSN: dsn, Password: password}.checkDumpVersion(ctx)
}

func ensureStoreSchema(ctx context.Context, pg *store.PG, install bool) (bool, error) {
	id, err := pg.Schema(ctx)
	if err != nil {
		missing := (!id.Present && strings.Contains(err.Error(), "there is no schema sprint in this database")) || (id.Present && strings.Contains(err.Error(), "schema sprint is empty"))
		if !missing {
			return false, cause("schema identity unavailable", err)
		}
	}
	if !id.Current || id.Owner != "nova_sprint" {
		if !install {
			return false, nil
		}
		if err = pg.Migrate(ctx); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) {
				return false, cause(fmt.Sprintf("migration failed (SQLSTATE %s)", pe.Code), err)
			}
			return false, cause("migration failed", err)
		}
	}
	id, err = pg.Schema(ctx)
	if err != nil {
		return false, cause("schema ownership unavailable", err)
	}
	if !id.Current || id.Owner != "nova_sprint" {
		return false, fmt.Errorf("schema ownership unavailable: current %v, owner %q (want nova_sprint)", id.Current, id.Owner)
	}
	return true, nil
}

func sprintDSN(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", cause("invalid postgres configuration", err)
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", fmt.Errorf("invalid postgres configuration: scheme %q host %q, want postgres://<host>/<db>", u.Scheme, u.Host)
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			return "", fmt.Errorf("postgres dsn must not contain a password")
		}
	}
	for key := range u.Query() {
		if strings.EqualFold(key, "password") {
			return "", fmt.Errorf("postgres dsn must not contain a password")
		}
		if strings.EqualFold(key, "user") {
			return "", fmt.Errorf("postgres dsn must not contain a user query")
		}
	}
	u.User = url.User("nova_sprint")
	return u.String(), nil
}

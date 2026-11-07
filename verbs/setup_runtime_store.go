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
	"github.com/mas-bandwidth/nova-sprint/store"
)

// PGSetupStore uses a child to reread the durable sprint credential for each
// probe and migration; its caller never receives that value.
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
		return false, fmt.Errorf("role setup unavailable")
	}
	has, err := p.Role.Sink.Has(ctx, sprintPasswordName)
	if err != nil || !has {
		return false, err
	}
	return p.child(ctx, "--internal-store-probe")
}
func (p PGSetupStore) Install(ctx context.Context) error {
	if !p.configured() || !fleetName.MatchString(p.ControlMachine) || !fleetName.MatchString(p.Secrets.Seat) {
		return fmt.Errorf("control seat unavailable")
	}
	if err := p.Role.Run(ctx, []SprintSeat{{Seat: p.Secrets.Seat, Machine: p.ControlMachine}}); err != nil {
		return err
	}

	ok, err := p.child(ctx, "--internal-store-setup")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("store setup did not verify")
	}
	return nil
}
func (p PGSetupStore) child(ctx context.Context, mode string) (bool, error) {
	if !filepath.IsAbs(p.Binary) || p.Secrets.Store == "" || p.Secrets.KeyFile == "" || p.Secrets.Seat == "" {
		return false, fmt.Errorf("store child configuration unavailable")
	}
	args := []string{"exec", "--only", sprintPasswordName, "--store", p.Secrets.Store, "--key", p.Secrets.KeyFile, "--as", p.Secrets.Seat, "--", p.Binary, mode}
	cmd := p.Command
	if cmd == nil {
		cmd = toolOutput
	}
	out, err := cmd(ctx, "nova-secrets", args...)
	if err != nil {
		return false, fmt.Errorf("store child failed")
	}
	var result struct {
		Ready *bool `json:"ready"`
	}
	if len(out) > 1024 || json.Unmarshal(out, &result) != nil || result.Ready == nil {
		return false, fmt.Errorf("store child returned invalid result")
	}
	return *result.Ready, nil
}

// SprintStoreChild runs only in the dedicated nova-secrets child. Its caller
// receives a boolean and no credential or raw command output.
func SprintStoreChild(ctx context.Context, install bool, version string) (bool, error) {
	dsn, password, dir := os.Getenv("NOVA_PG_DSN"), os.Getenv(sprintPasswordName), os.Getenv("NOVA_SPRINT_BACKUP_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return false, fmt.Errorf("backup directory unavailable")
		}
		dir = filepath.Join(home, ".local", "state", "nova-sprint", "backups")
	}
	if dsn == "" || password == "" || !filepath.IsAbs(dir) {
		return false, fmt.Errorf("store child configuration unavailable")
	}
	var err error
	dsn, err = sprintDSN(dsn)
	if err != nil {
		return false, err
	}
	pg, err := store.Open(ctx, dsn, password)
	if err != nil {
		var pe *pgconn.PgError
		if !install && errors.As(err, &pe) && pe.Code == "28P01" {
			return false, nil
		}
		return false, fmt.Errorf("sprint store unavailable")
	}
	defer pg.Close()
	ok, err := ensureStoreSchema(ctx, pg, install)
	if err != nil || !ok {
		return false, err
	}
	archive := PGBackups{DSN: dsn, Password: password, Directory: dir, Version: version, Loader: PGSQLLoader{}}
	ok, err = verifiedFirstBackup(ctx, pg, archive, dir)
	if err != nil {
		return false, err
	}
	if ok || !install {
		return ok, nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, fmt.Errorf("backup directory unavailable")
	}
	attempt, err := os.MkdirTemp(dir, "attempt-")
	if err != nil {
		return false, fmt.Errorf("backup directory unavailable")
	}
	if _, err = archive.Backup(ctx, attempt); err != nil {
		_ = os.Remove(attempt) // only removes an empty attempt, never evidence
		return false, fmt.Errorf("first backup failed")
	}
	return verifiedFirstBackup(ctx, pg, archive, dir)
}

func ensureStoreSchema(ctx context.Context, pg *store.PG, install bool) (bool, error) {
	id, err := pg.Schema(ctx)
	if err != nil {
		missing := (!id.Present && strings.Contains(err.Error(), "there is no schema sprint in this database")) || (id.Present && strings.Contains(err.Error(), "schema sprint is empty"))
		if !missing {
			return false, fmt.Errorf("schema identity unavailable")
		}
	}
	if !id.Current || id.Owner != "nova_sprint" {
		if !install {
			return false, nil
		}
		if err = pg.Migrate(ctx); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) {
				return false, fmt.Errorf("migration failed (SQLSTATE %s)", pe.Code)
			}
			return false, fmt.Errorf("migration failed")
		}
	}
	id, err = pg.Schema(ctx)
	if err != nil || !id.Current || id.Owner != "nova_sprint" {
		return false, fmt.Errorf("schema ownership unavailable")
	}
	return true, nil
}

func sprintDSN(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", fmt.Errorf("invalid postgres configuration")
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
func verifiedFirstBackup(ctx context.Context, pg *store.PG, archive PGBackups, dir string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "sprint-e*-s*.sql"))
	if err != nil {
		return false, fmt.Errorf("backup directory unavailable")
	}
	attempts, err := filepath.Glob(filepath.Join(dir, "attempt-*", "sprint-e*-s*.sql"))
	if err != nil {
		return false, fmt.Errorf("backup directory unavailable")
	}
	files = append(files, attempts...)
	id, err := pg.Schema(ctx)
	if err != nil || !id.Current {
		return false, fmt.Errorf("schema identity unavailable")
	}
	snap, err := pg.Snapshot(ctx)
	if err != nil {
		return false, fmt.Errorf("store snapshot unavailable")
	}
	for _, file := range files {
		info, err := archive.Verify(ctx, file)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			continue
		}
		if info.Seq > snap.Seq || info.PrefixDigest == "" || info.SchemaHash != id.Hash {
			continue
		}
		digest, err := EventPrefixDigest(ctx, pg, info.Seq)
		if err != nil {
			return false, fmt.Errorf("event history unavailable")
		}
		if digest == info.PrefixDigest {
			return true, nil
		}
	}
	return false, nil
}

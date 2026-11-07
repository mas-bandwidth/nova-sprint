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
		return false, fmt.Errorf("role setup unavailable: the store and role setup do not share one binary, seat, store and key")
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
// receives a boolean and no credential or raw command output.
func SprintStoreChild(ctx context.Context, install bool, version string) (bool, error) {
	dsn, password, dir := os.Getenv("NOVA_PG_DSN"), os.Getenv(sprintPasswordName), os.Getenv("NOVA_SPRINT_BACKUP_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, cause("backup directory unavailable", err)
		}
		if !filepath.IsAbs(home) {
			return false, fmt.Errorf("backup directory unavailable: the home directory is not absolute")
		}
		dir = filepath.Join(home, ".local", "state", "nova-sprint", "backups")
	}
	if dsn == "" || password == "" {
		return false, fmt.Errorf("store child configuration unavailable: %s", missingNames(map[string]string{"NOVA_PG_DSN": dsn, sprintPasswordName: password}))
	}
	if !filepath.IsAbs(dir) {
		return false, fmt.Errorf("store child configuration unavailable: NOVA_SPRINT_BACKUP_DIR is not absolute")
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
		return false, cause("sprint store unavailable", err)
	}
	defer pg.Close()
	ok, err := ensureStoreSchema(ctx, pg, install)
	if err != nil || !ok {
		return false, err
	}
	archive := PGBackups{DSN: dsn, Password: password, Directory: dir, Version: version, Loader: PGSQLLoader{}}
	ok, _, err = verifiedFirstBackup(ctx, pg, archive, dir)
	if err != nil {
		return false, err
	}
	if ok || !install {
		return ok, nil
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return false, cause("backup directory unavailable", err)
	}
	attempt, err := os.MkdirTemp(dir, "attempt-")
	if err != nil {
		return false, cause("backup directory unavailable", err)
	}
	if _, err = archive.Backup(ctx, attempt); err != nil {
		_ = os.Remove(attempt) // only removes an empty attempt, never evidence; the backup's failure is the one reported
		if r := AsRefusal(err); r != nil {
			return false, r // pg_dump's or the build version's refusal: its Problem and Fix as they are
		}
		return false, fmt.Errorf("first backup failed: %w", err)
	}
	ok, why, err := verifiedFirstBackup(ctx, pg, archive, dir)
	if err == nil && !ok {
		return false, fmt.Errorf("first backup did not verify: %s", why)
	}
	return ok, err
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

// verifiedFirstBackup is true when a backup in dir verifies against the
// store; false with why none did (the last file's reason), for the caller
// that must say it.
func verifiedFirstBackup(ctx context.Context, pg *store.PG, archive PGBackups, dir string) (bool, string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "sprint-e*-s*.sql"))
	if err != nil {
		return false, "", cause("backup directory unavailable", err)
	}
	attempts, err := filepath.Glob(filepath.Join(dir, "attempt-*", "sprint-e*-s*.sql"))
	if err != nil {
		return false, "", cause("backup directory unavailable", err)
	}
	files = append(files, attempts...)
	id, err := pg.Schema(ctx)
	if err != nil {
		return false, "", cause("schema identity unavailable", err)
	}
	if !id.Current {
		return false, "", fmt.Errorf("schema identity unavailable: the store's schema is not this build's")
	}
	snap, err := pg.Snapshot(ctx)
	if err != nil {
		return false, "", cause("store snapshot unavailable", err)
	}
	why := "no backup file in the backup directory"
	for _, file := range files {
		name := filepath.Base(file)
		info, err := archive.Verify(ctx, file)
		if err != nil {
			if ctx.Err() != nil {
				return false, "", ctx.Err()
			}
			why = name + ": " + errText(err)
			continue
		}
		if info.Seq > snap.Seq || info.PrefixDigest == "" || info.SchemaHash != id.Hash {
			why = fmt.Sprintf("%s: seq %d (store at %d), schema %s (store %s), digest %q", name, info.Seq, snap.Seq, info.SchemaHash, id.Hash, info.PrefixDigest)
			continue
		}
		digest, err := EventPrefixDigest(ctx, pg, info.Seq)
		if err != nil {
			return false, "", cause("event history unavailable", err)
		}
		if digest == info.PrefixDigest {
			return true, "", nil
		}
		why = name + ": its event prefix digest differs from the store's"
	}
	return false, why, nil
}

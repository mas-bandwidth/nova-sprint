package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// PGSetupStore uses a child to reread the durable sprint credential for each
// probe and migration; its caller never receives that value.
type PGSetupStore struct {
	Role           SprintRoleBootstrap
	ControlMachine string
	Secrets        NovaSecrets
	Binary         string
	Command        CommandOutput
	Out            io.Writer // where Install names the rows the store setup put back; nil: stdout
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
	// The role's password first (defect 37): a store probe as nova_sprint
	// passes on a pg_hba trust host whatever the password is.
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

	return p.storeSetup(ctx)
}

// storeSetup runs the store child's setup and prints, on Out, each row it put
// back from the log and each friend whose settings it copied from nova-config:
// the child's stderr is shown only when it fails.
func (p PGSetupStore) storeSetup(ctx context.Context) error {
	r, err := p.childResult(ctx, "--internal-store-setup", sprintPasswordName)
	if err != nil {
		return err
	}
	out := p.Out
	if out == nil {
		out = os.Stdout
	}
	for _, name := range r.Restored {
		if _, err := fmt.Fprintf(out, "UP store: row %s was missing from sprint.rows; put back from the log\n", Redact(name)); err != nil {
			return cause("write the restored rows", err)
		}
	}
	for _, line := range r.Friends {
		if _, err := fmt.Fprintln(out, "UP store: "+Redact(line)); err != nil {
			return cause("write the copied friends", err)
		}
	}
	if !r.Ready {
		return fmt.Errorf("store setup did not verify: the store child answered ready false after --internal-store-setup with no error (a bug: take it to the project's own tracker with this line)")
	}
	return nil
}
func (p PGSetupStore) child(ctx context.Context, mode string) (bool, error) {
	return p.childWith(ctx, mode, sprintPasswordName)
}

// storeChildResult is a store child's answer: ready, and on setup the rows
// it put back from the log and the friends whose settings it copied.
type storeChildResult struct {
	Ready    bool
	Restored []string
	Friends  []string
}

func (p PGSetupStore) childWith(ctx context.Context, mode, only string) (bool, error) {
	r, err := p.childResult(ctx, mode, only)
	return r.Ready, err
}

// childResult runs mode under nova-secrets exec with only the names in only
// (comma-separated) and reads its {"ready": bool, "restored": [names], "friends": [lines]}.
func (p PGSetupStore) childResult(ctx context.Context, mode, only string) (storeChildResult, error) {
	if !filepath.IsAbs(p.Binary) || p.Secrets.Store == "" || p.Secrets.KeyFile == "" || p.Secrets.Seat == "" {
		return storeChildResult{}, fmt.Errorf("store child configuration unavailable: binary %q absolute %t; secrets store, key file and seat set %t/%t/%t", Redact(p.Binary), filepath.IsAbs(p.Binary), p.Secrets.Store != "", p.Secrets.KeyFile != "", p.Secrets.Seat != "")
	}
	args := []string{"exec", "--only", only, "--store", p.Secrets.Store, "--key", p.Secrets.KeyFile, "--as", p.Secrets.Seat, "--", p.Binary, mode}
	cmd := p.Command
	if cmd == nil {
		cmd = toolOutput
	}
	out, err := cmd(ctx, "nova-secrets", args...)
	if err != nil {
		if r := AsRefusal(err); r != nil {
			return storeChildResult{}, r // the child's own refusal: its Problem and Fix as it wrote them
		}
		return storeChildResult{}, fmt.Errorf("store child %s failed: %w", mode, err)
	}
	var result struct {
		Ready    *bool    `json:"ready"`
		Restored []string `json:"restored"`
		Friends  []string `json:"friends"`
	}
	if len(out) > 16384 {
		return storeChildResult{}, fmt.Errorf("store child returned invalid result: %d bytes, over 16384; first bytes %q", len(out), firstBytes(out))
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return storeChildResult{}, fmt.Errorf("store child returned invalid result: %s", ParseFailure(err, out))
	}
	if result.Ready == nil {
		return storeChildResult{}, fmt.Errorf("store child returned invalid result: no \"ready\"; first bytes %q", firstBytes(out))
	}
	return storeChildResult{Ready: *result.Ready, Restored: result.Restored, Friends: result.Friends}, nil
}

// SprintStoreChild runs only in the dedicated nova-secrets child. Its caller
// receives a boolean and no credential or raw command output.
func SprintStoreChild(ctx context.Context, install bool, version string) (bool, error) {
	return sprintStoreChild(ctx, install, version, nil)
}

// SprintStoreSetup is SprintStoreChild installing, with the names of the rows
// it put back from the log (store.PG.RestoreLostRows) and a line per friend
// whose settings it copied from nova-config (CopyFriendSettings), for the
// child's result: its stderr is shown only when it fails.
func SprintStoreSetup(ctx context.Context, version string) (bool, StoreSetupResult, error) {
	var r StoreSetupResult
	ok, err := sprintStoreChild(ctx, true, version, &r)
	return ok, r, err
}

// StoreSetupResult is what the store setup child did that the parent prints:
// the rows it put back from the log, and a line per friend whose settings it
// copied from nova-config (or left at the defaults).
type StoreSetupResult struct {
	Restored []string `json:"restored,omitempty"`
	Friends  []string `json:"friends,omitempty"`
}

func sprintStoreChild(ctx context.Context, install bool, version string, done *StoreSetupResult) (bool, error) {
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
	if install { // before the first backup, whose restore verification refuses a store with a lost row
		lost, err := pg.RestoreLostRows(ctx)
		if err != nil {
			return false, cause("restoring the rows lost from the table", err)
		}
		for _, name := range lost { // stderr is shown when the child fails later (the backup); its result carries them when it does not
			fmt.Fprintf(os.Stderr, "nova-sprint: store: row %s was missing from sprint.rows; put back from the log\n", name)
		}
		copied, err := copyFriends(ctx, dsn, password, pg)
		if err != nil {
			return false, err
		}
		if done != nil {
			done.Restored, done.Friends = lost, copied
		}
	} else if owed, err := friendCopyOwedOn(ctx, dsn, password, pg); err != nil || owed {
		return false, err // owed: not ready, so up's store step installs, which copies (the store may be otherwise ready)
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

// copyFriends is CopyFriendSettings on its own connection as nova_sprint,
// which reads config.friends as the roster does.
func copyFriends(ctx context.Context, dsn, password string, pg *store.PG) ([]string, error) {
	conn, err := friendConn(ctx, dsn, password)
	if err != nil {
		return nil, err
	}
	defer closeSetupConnection(conn)
	return CopyFriendSettings(ctx, conn, pg, "up", time.Now())
}

// friendCopyOwedOn is FriendCopyOwed on its own connection as nova_sprint.
func friendCopyOwedOn(ctx context.Context, dsn, password string, pg *store.PG) (bool, error) {
	conn, err := friendConn(ctx, dsn, password)
	if err != nil {
		return false, err
	}
	defer closeSetupConnection(conn)
	return FriendCopyOwed(ctx, conn, pg)
}

// friendConn connects to dsn as nova_sprint with password, for the friend
// copy's read of config.friends.
func friendConn(ctx context.Context, dsn, password string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, cause("the friends' settings from nova-config: the dsn", err)
	}
	cfg.Password = password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, cause("the friends' settings from nova-config: connect", err)
	}
	return conn, nil
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

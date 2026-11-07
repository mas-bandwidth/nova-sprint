package verbs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// PGBackups makes plain SQL schema dumps and verifies them in a private
// throwaway PostgreSQL before publishing the dump. DSN is never put in argv.
type SQLLoader interface {
	Load(context.Context, string, *VerifiedDump) error
}

type PGBackups struct {
	DSN, Password, Directory, Version string
	Loader                            SQLLoader
}

func (p PGBackups) Backup(ctx context.Context, out string) (BackupInfo, error) {
	if !validBuildVersion(p.Version) {
		return BackupInfo{}, &OperationError{Problem: fmt.Sprintf("this nova-sprint build's version %q cannot be recorded in a backup (a build with no version stamp)", Redact(p.Version)), Fix: "build nova-sprint with make (it stamps git describe), or install a release with nova-fleet, then run nova-sprint up again", Exit: exitRefused}
	}
	if err := p.checkDumpVersion(ctx); err != nil {
		return BackupInfo{}, err
	}
	if out == "" {
		out = p.Directory
	}
	if out == "" {
		out = "."
	}
	tmp, err := os.CreateTemp(out, ".sprint-*.sql")
	if err != nil {
		return BackupInfo{}, err
	}
	path := tmp.Name()
	if err = tmp.Close(); err != nil {
		os.Remove(path)
		return BackupInfo{}, err
	}
	defer os.Remove(path)
	if err = runPGPassword(ctx, p.DSN, p.Password, "pg_dump", "--schema=sprint", "--data-only", "--format=plain", "--inserts", "--no-owner", "--no-privileges", "--file", path); err != nil {
		return BackupInfo{}, fmt.Errorf("pg_dump failed: %w", err)
	}
	source, err := store.Open(ctx, p.DSN, p.Password)
	if err != nil {
		return BackupInfo{}, err
	}
	schema, err := source.Schema(ctx)
	source.Close()
	if err != nil {
		return BackupInfo{}, cause("source schema is not current", err)
	}
	if !schema.Current {
		return BackupInfo{}, fmt.Errorf("source schema is not current (schema %s)", schema.Hash)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return BackupInfo{}, err
	}
	header := fmt.Sprintf("-- nova-sprint-backup schema=%s version=%s\n", schema.Hash, p.Version)
	body := append([]byte(header), raw...)
	info, err := p.verifyBytes(ctx, path, body)
	if err != nil {
		return BackupInfo{}, err
	}
	target := filepath.Join(out, fmt.Sprintf("sprint-e%d-s%d.sql", info.Epoch, info.Seq))
	if err = publishDump(body, target); err != nil {
		return BackupInfo{}, err
	}
	info.Path = target
	return info, nil
}
func publishDump(body []byte, target string) error {
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = out.Write(body)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(target)
	}
	return err
}
func (p PGBackups) checkDumpVersion(ctx context.Context) error {
	out, dumpErr := Output(ctx, "pg_dump", "--version")
	cfg, err := pgx.ParseConfig(p.DSN)
	if err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL: %w", err)
	}
	cfg.Password = p.Password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL: %w", err)
	}
	defer closeSetupConnection(conn)
	var server string
	if err = conn.QueryRow(ctx, "SHOW server_version_num").Scan(&server); err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL version: %w", err)
	}
	n, err := strconv.Atoi(server)
	if err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL version: server_version_num: %s", edge.ParseFailure(err, []byte(server)))
	}
	if dumpErr != nil {
		return RefusalOr(dumpErr, &OperationError{Problem: "pg_dump --version failed (" + errText(dumpErr) + ")", Fix: dumpFix(n / 10000), Exit: exitRefused})
	}
	return dumpVersionRefusal(string(out), n/10000)
}

// dumpFix names the install that resolves a pg_dump refusal.
func dumpFix(serverMajor int) string {
	return fmt.Sprintf("put PostgreSQL %d's bin directory (or a newer one's) first on PATH so pg_dump is at least %d, then run nova-sprint up again", serverMajor, serverMajor)
}

// dumpVersionRefusal is nil when the `pg_dump --version` line names a major
// at least serverMajor; else a refusal quoting the line it saw. The major is
// the first word whose leading integer parses: "pg_dump (PostgreSQL) 18.6
// (Homebrew)" and "pg_dump (PostgreSQL) 16.4 (Debian 16.4-1.pgdg120+1)" are
// 18 and 16.
func dumpVersionRefusal(out string, serverMajor int) error {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	major := -1
	for _, field := range strings.Fields(line) {
		digits := field[:len(field)-len(strings.TrimLeft(field, "0123456789"))]
		if n, err := strconv.Atoi(digits); err == nil {
			major = n
			break
		}
	}
	switch {
	case major < 0:
		return &OperationError{Problem: fmt.Sprintf("pg_dump version is unknown: no version number in %q", Redact(line)), Fix: dumpFix(serverMajor), Exit: exitRefused}
	case major < serverMajor:
		return &OperationError{Problem: fmt.Sprintf("pg_dump is older than the server (pg_dump %d vs server %d; pg_dump --version said %q)", major, serverMajor, Redact(line)), Fix: dumpFix(serverMajor), Exit: exitRefused}
	}
	return nil
}
func runPG(ctx context.Context, dsn, program string, args ...string) error {
	return runPGPassword(ctx, dsn, "", program, args...)
}
// runPGPassword runs a PostgreSQL program with the password in PGPASSWORD
// (never argv); a failure is an edge.CommandError carrying the program, its
// arguments, its exit and its whole stderr, redacted (the env, with
// PGPASSWORD, is never printed: only its names).
func runPGPassword(ctx context.Context, dsn, password, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	env, err := pgProcessEnv(os.Environ(), dsn, password)
	if err != nil {
		return fmt.Errorf("%s: %w", program, err)
	}
	cmd.Env = env
	return runTool(cmd, nil)
}
func pgProcessEnv(source []string, dsn, password string) ([]string, error) {
	env := safeProcessEnv(source)
	if dsn == "" {
		return env, nil
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, cause("invalid postgres configuration", err)
	}
	cfg.Password = password
	mode := "disable"
	if cfg.TLSConfig != nil {
		mode = "require"
		if !cfg.TLSConfig.InsecureSkipVerify {
			mode = "verify-full"
		}
	}
	if cfg.TLSConfig != nil && len(cfg.TLSConfig.Certificates) > 0 {
		return nil, fmt.Errorf("client certificates are not supported by the backup subprocess")
	}
	env = append(env, "PGHOST="+cfg.Host, fmt.Sprintf("PGPORT=%d", cfg.Port), "PGUSER="+cfg.User, "PGPASSWORD="+cfg.Password, "PGPASSFILE=/dev/null", "PGDATABASE="+cfg.Database, "PGSSLMODE="+mode)
	return env, nil
}

// safeProcessEnv is the allowed entries of source; with neither LANG nor
// LC_ALL among them it adds LC_ALL=C (PostgreSQL 18 on macOS refuses to
// start without a locale: "postmaster became multithreaded during startup").
func safeProcessEnv(source []string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true}
	out := []string{}
	locale := false
	for _, entry := range source {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			out = append(out, entry)
			locale = locale || name == "LANG" || name == "LC_ALL"
		}
	}
	if !locale {
		out = append(out, "LC_ALL=C")
	}
	return out
}

// Verify restores into an isolated temporary cluster and compares every rebuilt
// event to the restored state; no caller-provided server is modified.
func (p PGBackups) Verify(ctx context.Context, path string) (BackupInfo, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return BackupInfo{}, err
	}
	return p.verifyBytes(ctx, path, body)
}

func (p PGBackups) verifyBytes(ctx context.Context, path string, dumpBytes []byte) (info BackupInfo, resultErr error) {
	dir, err := os.MkdirTemp("", "nova-sprint-verify-")
	if err != nil {
		return BackupInfo{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	dataDir := filepath.Join(dir, "pg")
	if err = runPG(ctx, "", "initdb", "-D", dataDir, "-A", "trust", "--no-instructions"); err != nil {
		return BackupInfo{}, fmt.Errorf("initdb failed: %w", err)
	}
	// The stop is set before the start: a start that fails or times out may
	// still leave a postmaster, and the stop always runs, on a fresh context.
	started := false
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := runPG(stopCtx, "", "pg_ctl", "-D", dataDir, "-m", "immediate", "-w", "stop")
		if err != nil && (started || pidFileExists(dataDir)) {
			cleanup = false
			resultErr = errors.Join(resultErr, fmt.Errorf("temporary PostgreSQL could not stop (its directory is kept): %w", err))
		}
	}()
	if err = runPG(ctx, "", "pg_ctl", "-D", dataDir, "-o", verifyServerOptions(dir), "-w", "start"); err != nil {
		return BackupInfo{}, fmt.Errorf("temporary PostgreSQL failed to start: %w", err)
	}
	started = true
	dsn := "host=" + dir + " dbname=postgres sslmode=disable"
	if p.Loader == nil {
		return BackupInfo{}, fmt.Errorf("verified SQL loader unavailable")
	}
	schema, err := store.NewFake().Schema(ctx)
	if err != nil {
		return BackupInfo{}, err
	}

	statements, err := ConfineData(dumpBytes, schema.Expected)
	if err != nil {
		return BackupInfo{}, RefusalOr(err, &OperationError{Problem: "backup rejected: " + errText(err), Fix: "use a compatible verified nova-sprint backup", Exit: 1})
	}
	sum := sha256.Sum256(dumpBytes)
	validated := &VerifiedDump{statements: statements, digest: fmt.Sprintf("%x", sum)}
	if err = p.Loader.Load(ctx, dsn, validated); err != nil {
		return BackupInfo{}, fmt.Errorf("dump restore verification failed: %w", err)
	}
	snap, digest, schemaHash, err := verifyDatabase(ctx, dsn)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Path: path, Epoch: snap.Epoch, Seq: snap.Seq, PrefixDigest: digest, SchemaHash: schemaHash, Verified: validated}, nil
}
// verifyServerOptions are the verification cluster's server options: a
// socket in dir only, no TCP, and no System V shared memory (mmap and POSIX
// segments), so it starts beside other clusters on a host whose SysV limits
// are small (macOS).
func verifyServerOptions(dir string) string {
	return "-k " + dir + " -c listen_addresses='' -c shared_memory_type=mmap -c dynamic_shared_memory_type=posix"
}

// pidFileExists is whether a postmaster may still own dataDir.
func pidFileExists(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "postmaster.pid"))
	return err == nil
}

func verifyDatabase(ctx context.Context, dsn string) (store.Snapshot, string, string, error) {
	pg, err := store.Open(ctx, dsn, "")
	if err != nil {
		return store.Snapshot{}, "", "", err
	}
	defer pg.Close()
	schema, err := pg.Schema(ctx)
	if err != nil {
		return store.Snapshot{}, "", "", err
	}
	if !schema.Current {
		return store.Snapshot{}, "", "", fmt.Errorf("backup schema is not current")
	}
	snap, err := pg.Snapshot(ctx)
	if err != nil {
		return store.Snapshot{}, "", "", err
	}
	fold := newEventFold()
	digest, err := digestEvents(ctx, pg, snap.Seq, fold)
	if err != nil {
		return store.Snapshot{}, "", "", err
	}
	if !reflect.DeepEqual(fold.result(), snap) {
		return store.Snapshot{}, "", "", fmt.Errorf("rebuild does not match snapshot")
	}
	return snap, digest, schema.Hash, nil
}

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
		return BackupInfo{}, fmt.Errorf("nova-sprint version is unknown")
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
		return BackupInfo{}, cause("pg_dump failed", err)
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
	out, err := Output(ctx, "pg_dump", "--version")
	if err != nil {
		return &OperationError{Problem: "pg_dump is missing (" + Redact(err.Error()) + ")", Fix: "install pg_dump at least as new as the server", Exit: 1}
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return &OperationError{Problem: "pg_dump version is unknown (" + edge.ParseRefused("fewer than 3 words", 0, out) + ")", Fix: "install pg_dump at least as new as the server", Exit: 1}
	}
	version := fields[len(fields)-1]
	major, err := strconv.Atoi(strings.Split(version, ".")[0])
	if err != nil {
		return fmt.Errorf("pg_dump version is unknown: %s", edge.ParseFailure(err, out))
	}
	cfg, err := pgx.ParseConfig(p.DSN)
	if err != nil {
		return cause("cannot inspect PostgreSQL", err)
	}
	cfg.Password = p.Password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return cause("cannot inspect PostgreSQL", err)
	}
	defer conn.Close(ctx)
	var server string
	if err = conn.QueryRow(ctx, "SHOW server_version_num").Scan(&server); err != nil {
		return cause("cannot inspect PostgreSQL version", err)
	}
	n, err := strconv.Atoi(server)
	if err != nil {
		return fmt.Errorf("cannot inspect PostgreSQL version: server_version_num: %s", edge.ParseFailure(err, []byte(server)))
	}
	if major < n/10000 {
		return &OperationError{Problem: fmt.Sprintf("pg_dump is older than the server (pg_dump %s, server %d)", version, n/10000), Fix: "install pg_dump at least as new as the server", Exit: 1}
	}
	return nil
}
func runPG(ctx context.Context, dsn, program string, args ...string) error {
	return runPGPassword(ctx, dsn, "", program, args...)
}
func runPGPassword(ctx context.Context, dsn, password, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	env, err := pgProcessEnv(os.Environ(), dsn, password)
	if err != nil {
		return err
	}
	cmd.Env = env
	// Output is kept only for a failure, which carries the command, its exit
	// code and its first lines (the env, with PGPASSWORD, is never printed).
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

func safeProcessEnv(source []string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "TZ": true, "TMPDIR": true, "SSL_CERT_DIR": true, "SSL_CERT_FILE": true}
	out := []string{}
	for _, entry := range source {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			out = append(out, entry)
		}
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
		return BackupInfo{}, cause("initdb failed", err)
	}
	opts := "-k " + dir + " -c listen_addresses=''"
	if err = runPG(ctx, "", "pg_ctl", "-D", dataDir, "-o", opts, "-w", "start"); err != nil {
		return BackupInfo{}, cause("temporary PostgreSQL failed to start", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runPG(stopCtx, "", "pg_ctl", "-D", dataDir, "-m", "immediate", "-w", "stop"); err != nil {
			cleanup = false
			resultErr = errors.Join(resultErr, cause("temporary PostgreSQL could not stop", err))
		}
	}()
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
		return BackupInfo{}, &OperationError{Problem: "backup rejected: " + err.Error(), Fix: "use a compatible verified nova-sprint backup", Exit: 1}
	}
	sum := sha256.Sum256(dumpBytes)
	validated := &VerifiedDump{statements: statements, digest: fmt.Sprintf("%x", sum)}
	if err = p.Loader.Load(ctx, dsn, validated); err != nil {
		return BackupInfo{}, cause("dump restore verification failed", err)
	}
	snap, digest, schemaHash, err := verifyDatabase(ctx, dsn)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Path: path, Epoch: snap.Epoch, Seq: snap.Seq, PrefixDigest: digest, SchemaHash: schemaHash, Verified: validated}, nil
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

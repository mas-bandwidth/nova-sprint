package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

func gitEdge() *git.Client { return git.New(git.Config{}) }

// forgeEdge is GitHub through gh, as the identity nova-secrets exec gave
// this process (GH_TOKEN or GH_CONFIG_DIR; never Glenn's).
func forgeEdge() *forge.Client {
	login := os.Getenv("NOVA_SPRINT_FORGE_LOGIN")
	return forge.New(forge.Config{Identity: forge.Identity{Name: cmp.Or(login, "sprint"), ExpectedLogin: login, Token: os.Getenv("GH_TOKEN"), ConfigDir: os.Getenv("GH_CONFIG_DIR")}})
}

// origin is a stream's repository at origin, read through one clone per
// repository under the state dir (the git edge has no clone-free
// ls-remote): run keeps its own the same way.
type origin struct {
	git   *git.Client
	forge *forge.Client
	dir   string
}

func (o *origin) clone(ctx context.Context, repo string) (string, error) {
	sum := sha256.Sum256([]byte(repo))
	dir := filepath.Join(o.dir, fmt.Sprintf("%x", sum[:8]))
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return "", err
	}
	return dir, o.git.Clone(ctx, repo, dir)
}

func (o *origin) Tip(ctx context.Context, repo, branch string) (string, bool, error) {
	dir, err := o.clone(ctx, repo)
	if err != nil {
		return "", false, err
	}
	return o.git.LsRemote(ctx, dir, branch)
}

func (o *origin) Contains(ctx context.Context, repo, ancestor, head string) (bool, error) {
	dir, err := o.clone(ctx, repo)
	if err == nil {
		err = o.git.Fetch(ctx, dir)
	}
	if err != nil {
		return false, err
	}
	return o.git.IsAncestor(ctx, dir, ancestor, head)
}

func (o *origin) PR(ctx context.Context, repo, branch, base string) (string, bool, error) {
	pr, ok, err := o.forge.PR(ctx, repo, branch, base)
	return pr.HeadSHA, ok, err
}

// doctor is check's and show run's view outside the verbs' own reads.
type doctor struct{ creds credentials }

// Fleet is nova-fleet check, green or a refusal naming it.
func (d doctor) Fleet(ctx context.Context) error {
	if _, err := toolCommand(ctx, "nova-fleet", "check"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &verbs.OperationError{Problem: "nova-fleet check is not green", Fix: "run nova-fleet check and fix what it names", Exit: 1}
	}
	return nil
}

func (d doctor) ToolsVersion(ctx context.Context) (string, error) {
	out, err := toolCommand(ctx, "nova-version", "version")
	return string(out), err
}

// Schema is the store at this build's schema; a store not set up is false.
func (d doctor) Schema(ctx context.Context) (bool, error) {
	dsn, err := d.creds.sprint()
	if err != nil {
		return false, err
	}
	return verbs.ExecEnvironment{DSN: dsn, Password: d.creds.password}.Schema(ctx)
}

// History rebuilds the tables from the event log in one read-only
// repeatable-read transaction and compares them (store.ValidateHistory).
func (d doctor) History(ctx context.Context) error {
	dsn, err := d.creds.sprint()
	if err != nil {
		return err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("integrity: NOVA_PG_DSN does not parse")
	}
	cfg.Password = d.creds.password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("integrity: PostgreSQL cannot be reached")
	}
	defer conn.Close(context.WithoutCancel(ctx))
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("integrity: no read-only transaction")
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = store.ValidateHistory(ctx, tx); err != nil {
		return &verbs.OperationError{Problem: "the history does not rebuild the tables (" + err.Error() + ")", Fix: "take it to the project's own tracker with this line", Exit: 2}
	}
	return nil
}

// Run is who holds the chooser lock.
func (d doctor) Run(ctx context.Context) (run.ChooserStatus, error) {
	dsn, err := d.creds.sprint()
	if err != nil {
		return run.ChooserStatus{}, err
	}
	return run.InspectChooser(ctx, dsn, d.creds.password)
}

// setupPresence is the bus as up reads it: nova-bus who through the setup
// walk's command, which takes the bus credential from the control seat.
type setupPresence struct{}

func (p setupPresence) Who(ctx context.Context) (map[string]bus.Presence, error) {
	cfg, err := productionSetupConfigFromEnv()
	if err != nil {
		return nil, err
	}
	out, err := setupCommandWithBus(cfg, productionFleetCommand(cfg))(ctx, "nova-bus", "who")
	if err != nil {
		return nil, err
	}
	return parseWho(out)
}

// parseWho is `nova-bus who`: "WHO OK present=<n>", then one tab-separated
// line per name.
func parseWho(out []byte) (map[string]bus.Presence, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	n, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if !strings.HasPrefix(lines[0], "WHO OK present=") || err != nil || n != len(lines)-1 {
		return nil, fmt.Errorf("nova-bus who printed something else")
	}
	who := map[string]bus.Presence{}
	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" {
			return nil, fmt.Errorf("nova-bus who printed something else")
		}
		who[f[0]] = bus.Presence{Name: f[0], Uptime: f[1]}
	}
	return who, nil
}

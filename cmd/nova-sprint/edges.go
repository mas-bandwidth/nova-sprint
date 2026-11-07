package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/edge/git"
	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/verbs"
)

// gitEdge is git as the sprint. The landing's forge work runs as the host's
// gh login (intent rule 32), and so does git: gh is its one credential
// helper for https://github.com (the address git.CloneURL gives a github
// stream), run with no GitHub credential of the process, so a GH_TOKEN or
// GH_CONFIG_DIR in the environment never makes it someone else (directive
// 59). GH_GATE_TOKEN, when nova-secrets exec puts it in the process, is the
// optional override: every git call then authenticates with it.
func gitEdge() *git.Client {
	return git.New(git.Config{Token: os.Getenv(forge.TokenEnv), GitHubCredential: "gh"})
}

// forgeEdge is GitHub through the host's gh login (intent rule 32); a
// GH_GATE_TOKEN in the process is an optional override gh then acts as.
// The identity's name in messages is the choice, never a login.
func forgeEdge() *forge.Client {
	return forge.New(forge.Config{Identity: identity(os.Getenv)})
}

// identity is the forge's identity: the host's gh login, or the override.
func identity(env func(string) string) forge.Identity {
	if tok := env(forge.TokenEnv); tok != "" {
		return forge.Identity{Name: forge.TokenEnv + " (the override of " + forge.HostLogin + ")", Token: tok}
	}
	return forge.Identity{Name: forge.HostLogin}
}

// origin is a stream's repository at origin, by its clone address
// (git.CloneURL): a tip is one ls-remote of the address; a containment is
// read in one clone per address under the state dir (run keeps its own the
// same way).
type origin struct {
	git *git.Client
	dir string
}

func (o *origin) clone(ctx context.Context, repo string) (string, error) {
	sum := sha256.Sum256([]byte(repo))
	dir := filepath.Join(o.dir, fmt.Sprintf("%x", sum[:8]))
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return "", err
	}
	return dir, o.git.Clone(ctx, repo, dir)
}

// Contains is whether head contains ancestor, read in the clone of the
// repository at url (admission: the work is not already at the base).
func (o *origin) Contains(ctx context.Context, url, ancestor, head string) (bool, error) {
	dir, err := o.clone(ctx, url)
	if err == nil {
		err = o.git.Fetch(ctx, dir)
	}
	if err != nil {
		return false, err
	}
	return o.git.IsAncestor(ctx, dir, ancestor, head)
}

func (o *origin) Tip(ctx context.Context, url, branch string) (string, bool, error) {
	return o.git.Tip(ctx, url, branch)
}

// doctor is check's and show run's view outside the verbs' own reads.
type doctor struct{ creds credentials }

// Fleet is nova-fleet check, green or a refusal naming it.
func (d doctor) Fleet(ctx context.Context) error {
	if out, err := toolCommand(ctx, "nova-fleet", "check"); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		missing := verbs.FleetMissing(out) // names, never values: not redacted
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: strings.TrimPrefix(missing+"; ", "; ") + edge.RedactN("nova-fleet check is not green: "+err.Error(), edge.CommandMax), Fix: "nova-fleet check (each CHECK MISSING line is the command that fixes it)", Exit: 1})
	}
	return nil
}

// ToolsVersion is nova-tools' release, read from the installed binaries'
// build info (nova-version is retired).
func (d doctor) ToolsVersion(ctx context.Context) (string, error) {
	return verbs.InstalledTools(ctx)
}

// Schema is the store at this build's schema; a store not set up is false.
// A process without the sprint's credential cannot look, and says so
// (verbs.NoCredential), never "not set up".
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
		return edge.Cause("integrity: NOVA_PG_DSN does not parse", err)
	}
	cfg.Password = d.creds.password
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return edge.Cause("integrity: PostgreSQL cannot be reached", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return edge.Cause("integrity: no read-only transaction", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = store.ValidateHistory(ctx, tx); err != nil {
		return verbs.RefusalOr(err, &verbs.OperationError{Problem: edge.RedactN("the history does not rebuild the tables ("+err.Error()+")", edge.CommandMax), Fix: "take it to the project's own tracker with this line", Exit: 2})
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
	if !strings.HasPrefix(lines[0], "WHO OK present=") {
		return nil, fmt.Errorf("nova-bus who printed something else: %s", edge.ParseRefused("no WHO OK present= header", 0, out))
	}
	n, err := strconv.Atoi(strings.TrimPrefix(lines[0], "WHO OK present="))
	if err != nil {
		return nil, fmt.Errorf("nova-bus who printed something else: %s", edge.ParseFailure(err, out))
	}
	if n != len(lines)-1 {
		return nil, fmt.Errorf("nova-bus who printed something else: %s", edge.ParseRefused(fmt.Sprintf("present=%d but %d name lines", n, len(lines)-1), len(lines[0]), out))
	}
	who := map[string]bus.Presence{}
	at := len(lines[0]) + 1
	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) < 2 || f[0] == "" {
			return nil, fmt.Errorf("nova-bus who printed something else: %s", edge.ParseRefused("a name line is not name<TAB>uptime", at, out))
		}
		at += len(line) + 1
		who[f[0]] = bus.Presence{Name: f[0], Uptime: f[1]}
	}
	return who, nil
}

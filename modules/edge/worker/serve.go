package worker

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
)

// Config is one worker as a binary starts it: `nova-worker` and
// `nova-sprint worker` both build it from their flags and call Serve, so the
// two are one worker (drift rows 14, 15, 20: no host, model, deadline or
// provider literal; those come from the flags and the take receipt). Any
// machine nova-config gives the bench role runs one, the coordinator's too.
type Config struct {
	Row, Host, Root, StateDir, Harness string
	// WorkRoot holds a work clone per stream repo (default <root>/clones),
	// MirrorRoot its bare mirror, <name>.git, as mirror-refresh keeps them
	// (default ~/nova-bench/mirror). Both are made on first use.
	WorkRoot, MirrorRoot string
	SprintBin            string
	// Providers is --providers: the key proxy's routes (ParseProviders).
	Providers string
	// WallReads is --wall-read, repeatable: directories every job and probe
	// wall reads, never writes (a toolchain and its module cache;
	// CheckWallReads refuses the rest).
	WallReads []string
	Once      bool
	// LookupEnv reads the worker's environment (os.LookupEnv when nil).
	LookupEnv func(string) (string, bool)
}

// lookupEnv_internal is c's environment reader.
func (c Config) lookupEnv_internal(name string) string {
	lookup := c.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	v, _ := lookup(name)
	return strings.TrimSpace(v)
}

// Check refuses a config that cannot run, one line naming the fix.
func (c Config) Check() error {
	if strings.TrimSpace(c.Row) == "" {
		return fmt.Errorf("worker refused: missing --as <row>: pass --as <row>")
	}
	if _, err := NewHarness(cmp.Or(c.Harness, "claude")); err != nil {
		return err
	}
	if _, err := ParseProviders(c.Providers); err != nil {
		return err
	}
	for _, d := range c.WallReads {
		if err := checkWallReadForm_internal(strings.TrimSpace(d)); err != nil {
			return err
		}
	}
	if c.lookupEnv_internal(forge.TokenEnv) == "" {
		return fmt.Errorf("worker refused: %s is not in the worker's environment, and it is the one github credential of the worker's git clones, fetches and pushes: on the control machine run nova-secrets grant %s --to %s --pins <dir> --store <dir> --key <file>, push the secrets store (grant does not), then nova-sprint up --as <coordinator>", forge.TokenEnv, forge.TokenEnv, strings.TrimSpace(c.Row))
	}
	return nil
}

// Serve builds the OS adapters and serves the row (Runner.ServeRow: hook,
// probe, then one pass or the wake loop) until ctx ends or the bus hook
// dies. It writes the one-pass receipt to out.
func Serve(ctx context.Context, c Config, out func(string)) error {
	if err := c.Check(); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("worker refused: no home directory for the worker's state: %w", err)
	}
	providers, err := ParseProviders(c.Providers)
	if err != nil {
		return err
	}
	proxy := NewKeyProxy(providers, os.LookupEnv)
	srv, proxyAddr, err := proxy.ListenLoopback("127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { reportTo(out, "key proxy close", srv.Close()) }()
	r, err := newOSRunner_internal(ctx, c, home, proxy, proxyAddr, out)
	if err != nil {
		return err
	}
	if err := r.ServeRow(ctx, c.Row, cmp.Or(c.Harness, "claude"), c.Once); err != nil || !c.Once {
		return err
	}
	out(fmt.Sprintf("WORKER PASS %s host=%s\nnext: nothing; wait for the bus", c.Row, c.Host))
	return nil
}

// newOSRunner_internal is the runner on the real clock, git, wall and bus.
// It holds no forge: the worker's contract ends at a pushed head, and the
// run's landing opens the PR with the host's identity (intent rules 16, 32).
func newOSRunner_internal(ctx context.Context, c Config, home string, proxy *KeyProxy, proxyAddr string, out func(string)) (*Runner, error) {
	h, err := NewHarness(cmp.Or(c.Harness, "claude"))
	if err != nil {
		return nil, err
	}
	ex, timeouts := edge.NewOSExec(), DefaultTimeouts()
	root := cmp.Or(c.Root, filepath.Join(home, ".local", "state", "nova-worker", c.Row))
	workRoot, mirrorRoot := cmp.Or(c.WorkRoot, filepath.Join(root, "clones")), cmp.Or(c.MirrorRoot, filepath.Join(home, "nova-bench", "mirror"))
	stateDir := cmp.Or(c.StateDir, filepath.Join(home, ".local", "state"))
	wall, err := osWallConfig_internal(ctx, c, ex, timeouts.Stage, home, [][2]string{{"--root", root}, {"--state-dir", stateDir}, {"--work-root", workRoot}, {"--mirror-root", mirrorRoot}, {"secrets store", c.lookupEnv_internal("NOVA_SECRETS_STORE")}}, out)
	if err != nil {
		return nil, err
	}
	wall.ReserveBin, wall.SandboxBin, wall.ReserveWait, wall.NetDeny, wall.WorkRoot, wall.MirrorRoot = "nova-reserve", "nova-sandbox", timeouts.ReserveWait, true, workRoot, mirrorRoot
	sprint := &CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SprintBin: cmp.Or(c.SprintBin, "nova-sprint")}
	return NewRunner(Runner{
		Host: c.Host, RootDir: root,
		StateDir: stateDir,
		Sprint:   sprint, Beater: sprint,
		Git: &OSGitOps{WorkRoot: workRoot, MirrorRoot: mirrorRoot, Exec: ex, Timeouts: timeouts,
			Token: c.lookupEnv_internal(forge.TokenEnv), GitHubCredential: "gh"},
		Now: time.Now, Sleep: time.Sleep, After: time.After, WakeEvery: WakeEvery,
		BusHook: OSBusHook{Env: BusEnv(os.Environ()), ClaimWait: timeouts.BusClaim}.Start,
		Wall:    &OSReserveWall{UseSandbox: true, Exec: ex, KillGroup: OSKillGroup, Config: wall},
		Procs:   OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
		Warn: out, Meter: OSMeter{Exec: ex, ReserveBin: "nova-reserve", CPU: NewOSCPUSampler(ex)},
	})
}

// osWallConfig_internal is the wall's read grants: the operator's
// --wall-read (CheckWallReads against the worker's own directories; a
// refusal stops the start), then the GOROOT and GOMODCACHE `go env` names
// on the worker's PATH (DetectGo), each granted by the same checks or
// skipped with one line saying why; and the start line naming what go the
// wall reads (goWallGrant_internal). A go that is missing, refused or does
// not answer is said, never fatal: cards that need no go still run.
func osWallConfig_internal(ctx context.Context, c Config, ex edge.Exec, timeout time.Duration, home string, own [][2]string, out func(string)) (WallConfig, error) {
	reads, err := CheckWallReads(c.WallReads, home, own)
	if err != nil {
		return WallConfig{}, err
	}
	cfg := WallConfig{ReadPaths: reads}
	if cfg.GoRoot, cfg.GoModCache, err = DetectGo(ctx, ex, timeout); err != nil {
		out("worker: go is on the worker's PATH and go env failed, so the wall is given no Go toolchain: " + edge.RedactN(err.Error(), edge.FailureMax))
	}
	for _, d := range [][2]string{{"GOROOT", cfg.GoRoot}, {"GOMODCACHE", cfg.GoModCache}} {
		if d[1] == "" {
			continue
		}
		got, err := CheckWallReads([]string{d[1]}, home, own)
		if err != nil {
			out(fmt.Sprintf("worker: go env's %s %s is skipped, not granted to the wall: %s", d[0], d[1], edge.RedactN(err.Error(), edge.FailureMax)))
			continue
		}
		if !slices.Contains(cfg.ReadPaths, got[0]) {
			cfg.ReadPaths = append(cfg.ReadPaths, got[0])
		}
	}
	_, notes := goWallGrant_internal(cfg)
	for _, n := range notes {
		out(n)
	}
	return cfg, nil
}

// SplitList is a comma-separated flag as a list, blanks dropped.
func SplitList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

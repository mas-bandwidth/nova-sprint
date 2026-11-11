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

// Config is one worker as `nova-sprint worker` starts it from its flags
// (drift rows 14, 15, 20: no host, model, deadline or provider literal;
// those come from the flags and the take receipt). Any
// machine nova-config gives the bench role runs one, the coordinator's too.
type Config struct {
	Row, Host, Root, StateDir, Harness string
	// WorkRoot holds a work clone per stream repo (default <root>/clones),
	// MirrorRoot its bare mirror, <name>.git, as mirror-refresh keeps them
	// (default ~/nova-bench/mirror). Both are made on first use.
	WorkRoot, MirrorRoot string
	SprintBin            string
	// Version is this worker process's own build, handed to every sprint
	// verb it runs (BuildEnv): nova-fleet install replaces SprintBin on disk
	// under a running worker, so its beat reports the process, not the file.
	Version string
	// Providers is --providers: the key proxy's routes (ParseProviders).
	Providers string
	// WallReads is --wall-read, repeatable: directories every job and probe
	// wall reads, never writes (a toolchain and its module cache;
	// CheckWallReads refuses the rest).
	WallReads []string
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
// probe, then the wake loop) until ctx ends or the bus hook dies.
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
	return r.ServeRow(ctx, c.Row, cmp.Or(c.Harness, "claude"))
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
	own := [][2]string{{"--root", root}, {"--state-dir", stateDir}, {"--work-root", workRoot}, {"--mirror-root", mirrorRoot}, {"secrets store", c.lookupEnv_internal("NOVA_SECRETS_STORE")}}
	c = withBenchSDK_internal(c, home, own, out)
	wall, err := osWallConfig_internal(ctx, c, ex, timeouts.Stage, home, own, out)
	if err != nil {
		return nil, err
	}
	wall.ReserveBin, wall.SandboxBin, wall.ReserveWait, wall.NetDeny, wall.WorkRoot, wall.MirrorRoot = "nova-reserve", "nova-sandbox", timeouts.ReserveWait, true, workRoot, mirrorRoot
	if envSh := filepath.Join(home, "sdk", "env.sh"); slices.Contains(c.WallReads, filepath.Join(home, "sdk")) {
		if st, err := os.Stat(envSh); err == nil && st.Mode().IsRegular() {
			wall.SDKEnv = envSh
			out("worker: every job gets what " + envSh + " exports")
		}
	}
	sprint := &CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SprintBin: cmp.Or(c.SprintBin, "nova-sprint"), Version: c.Version}
	killJob := func(jobDir string) (int, error) { return OSKillJob(root, jobDir, timeouts.TerminateGrace) }
	return NewRunner(Runner{
		Host: c.Host, RootDir: root,
		StateDir: stateDir,
		Sprint:   sprint, Beater: sprint,
		Git: &OSGitOps{WorkRoot: workRoot, MirrorRoot: mirrorRoot, Exec: ex, Timeouts: timeouts,
			Token: c.lookupEnv_internal(forge.TokenEnv), GitHubCredential: "gh"},
		Now: time.Now, Sleep: time.Sleep, After: time.After, WakeEvery: WakeEvery,
		BusHook: OSBusHook{Env: BusEnv(os.Environ()), ClaimWait: timeouts.BusClaim}.Start,
		Wall:    &OSReserveWall{UseSandbox: true, Exec: ex, KillGroup: OSKillGroup, KillJob: killJob, Config: wall},
		KillJob: killJob,
		Procs:   OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
		Warn: out, Meter: OSMeter{Exec: ex, ReserveBin: "nova-reserve", CPU: NewOSCPUSampler(ex)},
	})
}

// osWallConfig_internal is the wall's read grants: the operator's
// --wall-read (CheckWallReads against the worker's own directories; a
// refusal stops the start), then the GOROOT and GOMODCACHE `go env` names
// on the worker's PATH (DetectGo), each granted by the same checks or
// skipped with one line saying why; and the start line naming what go the
// wall reads (goWallGrant_internal); then ~/.cargo/registry when it is
// there (CargoWallEnv). A go that is missing, refused or does
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
	// Cargo's registry holds only downloaded public crates, so it alone of
	// ~/.cargo is granted (CheckWallReads with no home: the dot-directory
	// rule would refuse it; the worker's own directories still do). One that
	// resolves anywhere else gets every home check.
	if reg := filepath.Join(home, ".cargo", "registry"); checkWallReadForm_internal(reg) == nil {
		got, err := CheckWallReads([]string{reg}, "", own)
		if err == nil && got[0] != filepath.Join(resolvePath_internal(home), ".cargo", "registry") {
			got, err = CheckWallReads([]string{reg}, home, own)
		}
		if err != nil {
			out("worker: cargo's registry " + reg + " is skipped, not granted to the wall: " + edge.RedactN(err.Error(), edge.FailureMax))
		} else {
			cfg.CargoRegistry, cfg.ReadPaths = got[0], append(cfg.ReadPaths, got[0])
			out("worker: the wall reads cargo's registry " + got[0] + " (CARGO_NET_OFFLINE=true)")
		}
	}
	return cfg, nil
}

// withBenchSDK_internal wires the bench's ~/sdk and ~/go toolchains when
// ~/sdk/bin exists on the bench: prepending ~/sdk/bin and ~/go/bin to PATH so
// DetectGo and the wall's commands find go, elixir, luau, sbcl, dart, javac,
// rustc and dotnet under systemd's minimal PATH, and granting ~/sdk and ~/go
// read-only to the wall when CheckWallReads admits them. What ~/sdk/env.sh
// exports reaches each job through WallConfig.SDKEnv.
func withBenchSDK_internal(c Config, home string, own [][2]string, out func(string)) Config {
	sdkBin := filepath.Join(home, "sdk", "bin")
	if checkWallReadForm_internal(sdkBin) != nil {
		return c
	}
	curPath := cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin:/bin")
	pathEntries := filepath.SplitList(curPath)
	var prepend []string
	for _, binDir := range []string{sdkBin, filepath.Join(home, "go", "bin")} {
		if checkWallReadForm_internal(binDir) == nil && !slices.Contains(pathEntries, binDir) {
			prepend = append(prepend, binDir)
		}
	}
	if len(prepend) > 0 {
		reportTo(out, "setting PATH for bench SDK", os.Setenv("PATH", strings.Join(append(prepend, curPath), string(os.PathListSeparator))))
	}
	reportTo(out, "setting DOTNET_CLI_TELEMETRY_OPTOUT", os.Setenv("DOTNET_CLI_TELEMETRY_OPTOUT", "1"))
	reportTo(out, "setting DOTNET_NOLOGO", os.Setenv("DOTNET_NOLOGO", "1"))
	for _, dir := range []string{filepath.Join(home, "sdk"), filepath.Join(home, "go")} {
		if _, err := CheckWallReads([]string{dir}, home, own); err == nil && !slices.Contains(c.WallReads, dir) {
			c.WallReads = append(c.WallReads, dir)
		}
	}
	return c
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

package worker

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/edge/forge"
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
	Once      bool
}

// Check refuses a config that cannot run, one line naming the fix.
func (c Config) Check() error {
	if strings.TrimSpace(c.Row) == "" {
		return fmt.Errorf("worker refused: missing --as <row>: pass --as <row>")
	}
	if _, err := NewHarness(cmp.Or(c.Harness, "claude")); err != nil {
		return err
	}
	_, err := ParseProviders(c.Providers)
	return err
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
	r, err := newOSRunner_internal(c, home, proxy, proxyAddr, out)
	if err != nil {
		return err
	}
	if err := r.ServeRow(ctx, c.Row, cmp.Or(c.Harness, "claude"), c.Once); err != nil || !c.Once {
		return err
	}
	out(fmt.Sprintf("WORKER PASS %s host=%s\nnext: nothing; wait for the bus", c.Row, c.Host))
	return nil
}

// newOSRunner_internal is the runner on the real clock, git, forge, wall and
// bus.
func newOSRunner_internal(c Config, home string, proxy *KeyProxy, proxyAddr string, out func(string)) (*Runner, error) {
	h, err := NewHarness(cmp.Or(c.Harness, "claude"))
	if err != nil {
		return nil, err
	}
	ex, timeouts := edge.NewOSExec(), DefaultTimeouts()
	root := cmp.Or(c.Root, filepath.Join(home, ".local", "state", "nova-worker", c.Row))
	fg := forge.New(forge.Config{Identity: forge.Identity{Name: c.Row, Token: os.Getenv("GH_TOKEN"), ConfigDir: os.Getenv("GH_CONFIG_DIR")}, Exec: ex, Timeout: timeouts.GH})
	return NewRunner(Runner{
		Host: c.Host, RootDir: root,
		StateDir: cmp.Or(c.StateDir, filepath.Join(home, ".local", "state")),
		Sprint:   &CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SprintBin: cmp.Or(c.SprintBin, "nova-sprint")},
		Git: &OSGitOps{WorkRoot: cmp.Or(c.WorkRoot, filepath.Join(root, "clones")),
			MirrorRoot: cmp.Or(c.MirrorRoot, filepath.Join(home, "nova-bench", "mirror")), Exec: ex, Timeouts: timeouts},
		Forges: map[string]ForgeOps{"github": &ForgeAdapter{Client: fg}},
		Now:    time.Now, Sleep: time.Sleep,
		BusHook: OSBusHook{Env: BusEnv(os.Environ()), ClaimWait: timeouts.BusClaim}.Start,
		Wall:    &OSReserveWall{UseSandbox: true, Exec: ex, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: timeouts.ReserveWait, NetDeny: true}},
		Procs:   OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
		Warn: out,
	})
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

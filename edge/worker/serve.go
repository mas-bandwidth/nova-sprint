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
// provider literal; those come from the flags and the take receipt).
type Config struct {
	Row, Host, Root, StateDir, Harness string
	WorkClone, Mirror, Origin          string
	SecretsBin, SprintBin              string
	CoordinatorHosts                   []string
	ProviderUpstream, ProviderKey      string
	ProviderHosts                      []string
	Once                               bool
}

// Check refuses a config that cannot run, one line naming the fix.
func (c Config) Check() error {
	if err := CheckHost(c.Host, c.CoordinatorHosts); err != nil {
		return err
	}
	if strings.TrimSpace(c.Row) == "" {
		return fmt.Errorf("worker refused: missing --as <row>: pass --as <row>")
	}
	if _, err := NewHarness(cmp.Or(c.Harness, "claude")); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(c.WorkClone) == "":
		return fmt.Errorf("worker refused: missing --work-clone <dir>: pass --work-clone <dir>")
	case c.ProviderUpstream == "" && (len(c.ProviderHosts) > 0 || c.ProviderKey != ""):
		return fmt.Errorf("worker refused: --provider-hosts and --provider-key need --provider-upstream <url>: pass it, or drop them")
	case c.ProviderUpstream != "" && (len(c.ProviderHosts) == 0 || c.ProviderKey == ""):
		return fmt.Errorf("worker refused: --provider-upstream needs --provider-hosts <host,host> and --provider-key <nova-secrets name>: pass both")
	}
	return nil
}

// Serve builds the OS adapters, probes the row and runs one pass (Once) or
// the wake loop until ctx ends or the bus hook dies. It writes the one-pass
// receipt to out.
func Serve(ctx context.Context, c Config, out func(string)) error {
	if err := c.Check(); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	harness := cmp.Or(c.Harness, "claude")
	h, _ := NewHarness(harness)
	ex, timeouts := edge.NewOSExec(), DefaultTimeouts()
	proxy := &KeyProxy{DefaultRoute: "provider", Routes: map[string]ProxyRoute{}}
	if c.ProviderUpstream != "" {
		proxy.Routes["provider"] = ProxyRoute{Name: "provider", UpstreamURL: c.ProviderUpstream, AllowedHosts: c.ProviderHosts,
			Key: func() (string, error) {
				return ReadProviderKey(ex, cmp.Or(c.SecretsBin, "nova-secrets"), c.ProviderKey)
			}}
	}
	srv, proxyAddr, err := proxy.ListenLoopback("127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()
	fg := forge.New(forge.Config{Identity: forge.Identity{Name: c.Row, Token: os.Getenv("GH_TOKEN")}, Exec: ex, Timeout: timeouts.GH})
	r, err := NewRunner(Runner{
		Host: c.Host, CoordinatorHosts: c.CoordinatorHosts,
		RootDir:  cmp.Or(c.Root, filepath.Join(home, ".local", "state", "nova-worker", c.Row)),
		StateDir: cmp.Or(c.StateDir, filepath.Join(home, ".local", "state")),
		Sprint:   &CLISprintClient{Exec: ex, Timeout: timeouts.Stage, SecretsBin: cmp.Or(c.SecretsBin, "nova-secrets"), SprintBin: cmp.Or(c.SprintBin, "nova-sprint")},
		Git:      &OSGitOps{WorkCloneDir: c.WorkClone, MirrorDir: c.Mirror, OriginURL: c.Origin, Exec: ex, Timeouts: timeouts},
		Forges:   map[string]ForgeOps{"github": &ForgeAdapter{Client: fg}},
		Now:      time.Now, Sleep: time.Sleep,
		BusHook: OSBusHook{Env: BusEnv(os.Environ()), ClaimWait: timeouts.BusClaim}.Start,
		Wall:    &OSReserveWall{UseSandbox: true, Exec: ex, Config: WallConfig{ReserveBin: "nova-reserve", SandboxBin: "nova-sandbox", ReserveWait: timeouts.ReserveWait, NetDeny: true}},
		Procs:   OSProcessTable{}, Proxy: proxy, Exec: ex, ProxyAddr: proxyAddr, DefaultHarness: h, Timeouts: timeouts,
		Warn: out,
	})
	if err != nil {
		return err
	}
	if err := r.ProbeRow(ctx, c.Row, harness, ""); err != nil {
		return err
	}
	if !c.Once {
		return r.RunWakeLoop(ctx, c.Row)
	}
	if err := r.RunOnce(ctx, c.Row); err != nil {
		return err
	}
	out(fmt.Sprintf("WORKER PASS %s host=%s\nnext: nothing; wait for the bus", c.Row, c.Host))
	return nil
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

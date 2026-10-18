package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
)

// DashboardListen is the page's address when --listen names none, and DashboardPull the
// pull routes': this machine only, each on a port of its own.
const (
	DashboardListen = "127.0.0.1:7390"
	DashboardPull   = "127.0.0.1:7395"
)

// noListener is the --listen or --pull word that serves nothing there.
const noListener = "none"

// tailnetRange is the addresses a tailnet hands out (100.64.0.0/10, the shared range):
// with the private and loopback ranges, the only ones the dashboard listens on.
var tailnetRange = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// cmdDashboard serves the sprint dashboard (docs/SPEC-SPRINT-DASHBOARD.md) until it is
// interrupted: the page from the files embedded in the binary, and /api/sprint, the
// sprint as where --json --cards prints it, read in this process once per --every whoever
// is looking, with one freshness check on it; on the --pull listeners, the pull routes a
// worker reads its own view from (docs/SPEC-SPRINT.md, the dashboard). Given --pull <url>,
// it is a puller: it reads that dashboard's copy in place of the sprint, and serves the
// page only.
func (a *app) cmdDashboard(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("dashboard")
	listen := fs.String("listen", DashboardListen, "serve the page on each `address:port` of a comma-separated list, one listener each and one cached copy of the sprint: loopback, or this machine's address on the fleet's private network (the tailnet); 0.0.0.0, :: and public addresses are refused; default "+DashboardListen+"; none: no page")
	pull := fs.String("pull", DashboardPull, "serve the pull routes (/friend/<name>, /machine/<name>, their /api/ JSON and /events/ stream forms, /team and /api/team, /api/sprint, /events) on each `address:port` of a comma-separated list, the same addresses --listen takes, from the same cached copy, read-only; default "+DashboardPull+"; none: no pull routes; an http:// or https:// URL of another dashboard: be its puller, reading its /api/sprint once per --every in place of the sprint and serving the page alone (the public copy)")
	logo := fs.String("logo", "", "an image `file` served as the page's logo and favicon in place of the live page's robot tile, embedded; none: the robot tile")
	every := fs.Duration("every", time.Second, "read the sprint at most once per this `duration`, above 0")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "dashboard", argErr("takes no words ", err, pos...))
	}
	if *every <= 0 {
		return refuse(stderr, "dashboard", "--every wants a duration above 0, got "+every.String())
	}
	pages, err := dashboardAddrs("--listen", *listen)
	if err != nil {
		return refuse(stderr, "dashboard", err.Error())
	}
	from, err := dashboardUpstream(*pull)
	if err != nil {
		return refuse(stderr, "dashboard", err.Error())
	}
	if from == "" && a.server(fs) == "" && strings.TrimSpace(c.redis) == "" {
		// no sprint to read: refused here, on stderr, before any line goes to stdout or any
		// listener opens (refusal grammar); a store that is named but down is a read that
		// fails and is logged, the page holding the last good copy
		if _, _, err := a.recordedLogin(); err != nil {
			return refuse(stderr, "dashboard", err.Error())
		}
		return refuse(stderr, "dashboard", dashboardNoStore)
	}
	var pulls []string
	if from == "" {
		if pulls, err = dashboardAddrs("--pull", *pull); err != nil {
			return refuse(stderr, "dashboard", err.Error())
		}
	}
	if len(pages)+len(pulls) == 0 {
		return refuse(stderr, "dashboard", "--listen none and --pull none serve nothing: name an address:port for one of them")
	}
	if *logo != "" {
		if fi, err := os.Stat(*logo); err != nil || fi.IsDir() {
			return refuse(stderr, "dashboard", "--logo "+*logo+" is not a file this process can read")
		}
	}
	redis := (verbArgs{fs: fs}).given("redis")
	stdout = &lockedWriter{w: stdout} // the listeners and the reads write lines from their own goroutines
	srv := a.dashboardServer(c.redis, redis, from, *every, *logo, stdout)
	a.keepDashboardSamples(srv, c.redis, from)
	ctx, stop := a.notify(context.Background())
	defer stop()
	// one poller: the first read before any listener opens, then, while someone is looking
	// (dashViewers), one each --every (back to back when a read takes longer), each new copy
	// pushed to the /events clients as it is read and its freshness checked; a page reads
	// the copy only. While no one is looking nothing is read: the first request after reads
	// the sprint itself and starts the poller again
	srv.Tick()
	tick := time.NewTicker(*every)
	defer tick.Stop()
	viewers := newDashViewers(a.now)
	runCtx, endRun := context.WithCancel(ctx)
	defer endRun()
	go viewers.poll(runCtx, srv, tick.C)
	var ls []listener
	for _, addr := range pages {
		ls = append(ls, listener{"--listen", addr, viewers.watch(srv), "DASHBOARD listening on http://%s/\n"})
	}
	for _, addr := range pulls {
		ls = append(ls, listener{"--pull", addr, viewers.watch(srv.Pull()), "DASHBOARD pull routes on http://%s/ (friend/<name>, machine/<name>, team, api/..., events/...)\n"})
	}
	servers, err := a.serveDashboard(ls, stdout)
	if err != nil {
		return refuse(stderr, "dashboard", err.Error())
	}
	defer func() {
		for _, s := range servers {
			// ignored: the dashboard is ending; a page asking now asks again
			_ = s.Close()
		}
	}()
	began := a.binaryStamp()
	for a.pause(ctx, *every) {
		if now := a.binaryStamp(); began != "" && now != began {
			fmt.Fprintln(stdout, "DASHBOARD STOP the binary this dashboard runs was replaced; its supervisor starts the new one")
			return exitReplaced
		}
	}
	fmt.Fprintln(stdout, "DASHBOARD STOP interrupted")
	return 0
}

// dashboardNoStore is the dashboard's refusal when it is given nothing to read: no store, no
// sprint's server, no dashboard to pull from.
const dashboardNoStore = "the dashboard reads the sprint: --redis <addr> (or NOVA_SPRINT_REDIS, NOVA_REDIS_ADDR, or a login recorded by nova-sprint seat login), the sprint's server (NOVA_SPRINT_SERVER), or another dashboard (--pull <url>); with no Redis, --redis mem:<file> serves the in-memory twin kept in that file"

// dashboardServer is the dashboard's one server: it reads the sprint as where --json
// --dashboard does (whereJSONForDashboard), or, given from (a puller), that dashboard's /api/sprint; each
// read and the freshness check go to log.
func (a *app) dashboardServer(redis string, given bool, from string, every time.Duration, logo string, log io.Writer) *sprintdash.Server {
	cmd := "nova-sprint where --json --dashboard"
	if redis != "" && given {
		cmd += " --redis " + redis
	}
	srv := &sprintdash.Server{
		Read:    func() ([]byte, error) { return a.whereJSONForDashboard(redis, given) },
		Now:     a.now,
		Every:   every,
		Logo:    logo,
		Version: version,
		Command: cmd,
		Log:     log,
	}
	if from != "" {
		srv.From = &sprintdash.Upstream{URL: from}
	}
	return srv
}

// dashboardUpstream is the /api/sprint URL of the dashboard --pull names when it names a
// URL (a puller), "" when it names listeners.
func dashboardUpstream(pull string) (string, error) {
	pull = strings.TrimSpace(pull)
	if !strings.Contains(pull, "://") {
		return "", nil
	}
	u, err := url.Parse(pull)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		// the URL is not echoed: it may carry a credential
		return "", errors.New("--pull: a puller wants the http:// or https:// URL of another dashboard, with no credential, query or fragment")
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/api/sprint") + "/api/sprint"
	return u.String(), nil
}

// whereJSON reads the sprint as `nova-sprint where --json --cards --rows --archived` does, in this
// process: through the sprint's server when NOVA_SPRINT_SERVER names one, else on the
// store; --redis is handed on when the dashboard was given it, as where would have been.
func (a *app) whereJSON(addr string, given bool) ([]byte, error) {
	return a.whereJSONWith([]string{"where", "--json", "--cards", "--rows", "--archived"}, addr, given)
}

// whereJSONForDashboard reads the sprint as `nova-sprint where --json --dashboard` does, in this
// process: through the sprint's server when NOVA_SPRINT_SERVER names one, else on the
// store; --redis is handed on when the dashboard was given it, as where would have been.
func (a *app) whereJSONForDashboard(addr string, given bool) ([]byte, error) {
	return a.whereJSONWith([]string{"where", "--json", "--dashboard"}, addr, given)
}

func (a *app) whereJSONWith(argv []string, addr string, given bool) ([]byte, error) {
	if given {
		argv = append(argv, "--redis", addr)
	}
	var out, errb bytes.Buffer
	if code := a.run(argv, &out, &errb); code != 0 {
		// the page is shown the exit alone; where's own line goes to the log
		line, _, _ := strings.Cut(strings.TrimSpace(errb.String()), "\n")
		return nil, &sprintdash.ReadError{Why: fmt.Sprintf("where exited %d", code), Detail: line}
	}
	return out.Bytes(), nil
}

// dashboardAddrs is the --listen or --pull list (flag), each a host:port whose host is an
// IP address of loopback, a private range or the tailnet's (or localhost), each once; none
// is no address.
func dashboardAddrs(flag, list string) ([]string, error) {
	if strings.TrimSpace(list) == noListener {
		return nil, nil
	}
	var out []string
	for _, addr := range strings.Split(list, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("%s wants address:port (or none), found %s", flag, addr)
		}
		if host != "localhost" {
			if why := listenable(net.ParseIP(host)); why != "" {
				return nil, fmt.Errorf("%s %s: %s", flag, addr, why)
			}
		}
		if !slices.Contains(out, addr) {
			out = append(out, addr)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s names no address:port (none serves nothing there)", flag)
	}
	return out, nil
}

// listenable is why the dashboard does not listen on ip, "" when it does: loopback, a
// private range or the tailnet's, never every network and never a public address.
func listenable(ip net.IP) string {
	switch {
	case ip == nil:
		return "the address is an IP address of this machine (loopback, or its address on the fleet's private network), never a name"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "a link-local address; the page checks no credential, so it listens on loopback or the fleet's private network (the tailnet) only"
	case ip.IsUnspecified():
		return "the page shows the sprint and checks no credential, so it does not listen on every network; name loopback or this machine's tailnet address"
	case !ip.IsLoopback() && !ip.IsPrivate() && !tailnetRange.Contains(ip):
		return "a public address; the page shows the sprint and checks no credential, so it listens on loopback or the fleet's private network (the tailnet) only"
	}
	return ""
}

// listener is an address the dashboard listens on (and the flag that named it), the handler it serves there (the page
// or the pull routes, one server behind both), and the line that says so.
type listener struct {
	flag, addr string
	h          http.Handler
	says       string
}

// serveDashboard starts one listener per address and says each address it listens on; a
// listener that cannot start closes the others.
func (a *app) serveDashboard(ls []listener, stdout io.Writer) ([]*http.Server, error) {
	listen := net.Listen
	if a.dashListen != nil {
		listen = a.dashListen
	}
	var lns []net.Listener
	for _, l := range ls {
		ln, err := listen("tcp", l.addr)
		if err != nil {
			for _, l := range lns {
				// ignored: closing the listeners already open on the failure path; err is the one returned
				_ = l.Close()
			}
			return nil, fmt.Errorf("%s %s: %w", l.flag, l.addr, err)
		}
		lns = append(lns, ln)
	}
	var servers []*http.Server
	for i, ln := range lns {
		s := &http.Server{Handler: ls[i].h, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 2 * time.Minute}
		servers = append(servers, s)
		at := ln.Addr().String()
		go func() {
			if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(stdout, "DASHBOARD STOPPED %s: %s\n", at, oneline.Escape(err.Error()))
			}
		}()
		fmt.Fprintf(stdout, ls[i].says, at)
	}
	return servers, nil
}

// lockedWriter writes each line whole, one writer at a time.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// keepDashboardSamples has srv keep its throughput samples in a file beside the store login's, one
// file a store (and a puller's upstream), so a restart, which every release adoption is, carries
// the rolling hour on and the figure never waits to warm up again. An app with the login off (a
// test's) keeps none.
func (a *app) keepDashboardSamples(srv *sprintdash.Server, redis, from string) {
	if a.loginFile == nil {
		return
	}
	login, err := a.loginFile()
	if err != nil {
		return
	}
	sum := sha256.Sum256([]byte(redis + "\x00" + from))
	path := filepath.Join(filepath.Dir(login), fmt.Sprintf("dashboard-samples-%x.json", sum[:6]))
	srv.LoadSamples = func() ([]byte, error) {
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return b, err
	}
	srv.SaveSamples = func(b []byte) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
}

// DashboardViewerWindow is how long the dashboard keeps reading the sprint once a --every
// after it last saw a viewer: a request on the page's or the pull routes' listeners (but
// /healthz), seen as it begins and as it ends, and an /events stream for as long as it is
// open. The owner, 2026-10-17: the dashboard updates every second while someone is looking.
// It read the sprint once a second whoever was looking, 86,400 reads a day of where --json
// --dashboard (the work, merge and fleet tables, the inbox, the routes, the machine and the
// server's record) with no one looking.
const DashboardViewerWindow = 30 * time.Second

// dashPoller is the dashboard server's poller (sprintdash.Server): Run reads the sprint on
// each tick until ctx ends, and Refresh is a request's own read while Run is not running.
type dashPoller interface {
	Refresh()
	Run(ctx context.Context, tick <-chan time.Time)
}

// dashViewers is who is looking at the dashboard: the requests open and when one was last
// seen, on the clock now; back is one slot, filled when a viewer comes while no one was
// looking.
type dashViewers struct {
	mu   sync.Mutex
	now  func() time.Time
	open int
	seen time.Time
	back chan struct{}
}

func newDashViewers(now func() time.Time) *dashViewers {
	return &dashViewers{now: now, back: make(chan struct{}, 1)}
}

// watch is h, each request but /healthz (a check, not a viewer) counted as a viewer while
// it runs and seen as it ends.
func (v *dashViewers) watch(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			h.ServeHTTP(w, r)
			return
		}
		v.enter()
		defer v.leave()
		h.ServeHTTP(w, r)
	})
}

// enter counts a viewer in, and fills back when no one was looking.
func (v *dashViewers) enter() {
	v.mu.Lock()
	now := v.now()
	idle := !v.watchingAt(now)
	v.open++
	v.seen = now
	v.mu.Unlock()
	if idle {
		select {
		case v.back <- struct{}{}:
		default:
		}
	}
}

// leave counts a viewer out, seen now.
func (v *dashViewers) leave() {
	v.mu.Lock()
	v.open--
	v.seen = v.now()
	v.mu.Unlock()
}

// watching says someone is looking now: a request open, or one seen within
// DashboardViewerWindow.
func (v *dashViewers) watching() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.watchingAt(v.now())
}

// watchingAt is watching at now; the caller holds v.mu.
func (v *dashViewers) watchingAt(now time.Time) bool {
	if v.open > 0 {
		return true
	}
	return !v.seen.IsZero() && now.Sub(v.seen) < DashboardViewerWindow && !now.Before(v.seen)
}

// poll runs srv's poller on the clock's ticks while someone is looking, and stops it at
// the first tick no one is (DashboardViewerWindow after the last viewer went); stopped, it
// reads nothing, a request reading the sprint itself, at most once a --every (Refresh).
// When a viewer comes it reads the sprint in full (Refresh: the poller's first freshness
// check is of a fresh read, never of the copy left from before no one looked) and runs the
// poller again. It returns when ctx ends, the poller stopped.
func (v *dashViewers) poll(ctx context.Context, srv dashPoller, clock <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock: // no one is looking: the tick reads nothing
			continue
		case <-v.back:
		}
		srv.Refresh()
		ticks := make(chan time.Time)
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			srv.Run(runCtx, ticks)
		}()
		for looking := true; looking; {
			select {
			case <-ctx.Done():
				looking = false
			case <-v.back: // a viewer came while the poller runs: it already reads
			case t := <-clock:
				if !v.watching() {
					looking = false
					break
				}
				select {
				case ticks <- t:
				case <-ctx.Done():
					looking = false
				}
			}
		}
		stop()
		<-done
	}
}

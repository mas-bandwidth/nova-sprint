package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// seat install and seat uninstall (docs/SPEC-SPRINT.md, "Handing over the seat"; the
// owner, 2026-10-04: what is manual needs a verb): the seat's push loop, inbox --wait
// --push seat, as the tool's own service on this machine, in place of a unit written by
// hand. The unit's text and its install are sprint.SeatUnit and sprint.SeatInstaller;
// here are the machine's facts (its OS, the binary, the home, the store this verb was
// given) and the loader, launchctl or systemctl --user. Neither verb is the server's
// (notServed): a unit is installed where the verb is typed.

// seatOS is the OS the unit is written for: the test's, else this machine's.
func (a *app) seatOS() string {
	if a.goos != "" {
		return a.goos
	}
	return runtime.GOOS
}

// seatDir is where the unit goes when --dir names nowhere: the user's LaunchAgents on
// macOS, the systemd user directory on Linux.
func (a *app) seatDir(goos string) (string, error) {
	home, err := a.home()
	if err != nil {
		return "", err
	}
	return sprint.UnitDir(goos, home, a.getenv), nil
}

// seatInstaller is the installer into dir, its loader the test's when one is set.
func (a *app) seatInstaller(goos, dir string) sprint.SeatInstaller {
	load := a.seatLoad
	if load == nil {
		load = loadSeatUnit
	}
	return sprint.SeatInstaller{Dir: dir,
		Load:   func(p string) error { return load(goos, "load", p) },
		Unload: func(p string) error { return load(goos, "unload", p) }}
}

func (a *app) cmdSeatInstall(args []string, stdout, stderr io.Writer) int {
	const name = "seat install"
	fs, c := a.verbSetup(name)
	dir := fs.String("dir", "", "the directory the unit is written into (default: ~/Library/LaunchAgents on macOS, ~/.config/systemd/user on Linux)")
	logf := fs.String("log", "", "the file the loop's lines go to, macOS (default: ~/Library/Logs/nova-sprint-seat-push.log); on Linux they are in the journal")
	dry := fs.Bool("dry-run", false, "print the unit and where it would go, and write and load nothing")
	harness := fs.String("harness", "", "the harness the seat's AI runs in (required): the push loop delivers each judgment, and the push proof, into the session through its adapter; a harness with no deliver command (claude) gets the folder adapter, each one a file written into --target")
	target := fs.String("target", "", "the session's directory, where the harness's adapter delivers (required); for the folder adapter, the directory the session watches with a Monitor, which must be there")
	session := fs.String("session", "", "the session's id, for a harness that names one (default: the adapter's newest in --target)")
	server := fs.String("server", "", "the sprint's server, `host:port` (default NOVA_SPRINT_SERVER): the unit's, and recorded in the seat beside the store login, where seat check reads it when NOVA_SPRINT_SERVER is not set; the push loop holds the seat's hook on its loopback listener, so a --redis unit names it too")
	busAddr := fs.String("bus", "", "the bus store, `host:port` (default NOVA_BUS_REDIS): the unit's, where the push loop holds the seat's hook (the bus push's record and subscription); without one the loop pushes judgments and holds no hook, and the seat hooks in by hand under a Monitor")
	cs := addConfigSeatFlags(fs)
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if _, _, err := cs.profile(); err != nil {
		return refuse(stderr, name, err.Error())
	}
	// the push target first: a loop that cannot reach the session is no push (pushproof.go)
	// (a dry run with no --harness still prints the unit, and says the install wants one)
	push, why := seatPushTarget(sprint.PushRecord{Name: c.actor, Harness: *harness, Target: *target, Session: *session})
	if why != "" && (!*dry || push.Harness != "") {
		return refuse(stderr, name, why+"; run: "+sprint.PushSetup(c.actor, push, push.Harness != ""))
	}
	goos := a.seatOS()
	u := sprint.SeatUnit{OS: goos, Log: *logf}
	if srv := strings.TrimSpace(*server); srv != "" {
		u.Server = srv
	} else if srv := a.server(fs); srv != "" {
		u.Server = srv
	}
	if u.Server == "" || (verbArgs{fs: fs}).given("redis") {
		u.Redis = strings.TrimSpace(c.redis)
	}
	if u.Redis != "" && u.Server == "" {
		// a store unit reads the store itself; the hook is the server's loopback listener,
		// which the seat's record names when the environment does not (namedServer)
		if srv, _ := a.namedServer(); srv != "" {
			u.Server = srv
		}
	}
	// the bus store, where the loop holds the seat's hook: --bus, else the environment's,
	// with the names of its login's variables as typed (never a password)
	bus := strings.TrimSpace(*busAddr)
	if bus == "" {
		bus = a.getenv(busRedisEnv)
	}
	if bus != "" {
		u.Env = append(u.Env, [2]string{busRedisEnv, bus})
		for _, k := range sprint.SeatEnv[1:] {
			if v := a.getenv(k); v != "" {
				u.Env = append(u.Env, [2]string{k, v})
			}
		}
	}
	exe := a.executable
	if exe == nil {
		exe = os.Executable
	}
	if u.Exe, err = exe(); err != nil {
		return refuse(stderr, name, "the path of this nova-sprint cannot be read: "+err.Error()+"; nothing was written")
	}
	if *dir == "" {
		if *dir, err = a.seatDir(goos); err != nil {
			return refuse(stderr, name, "--dir names no directory and the home cannot be read: "+err.Error())
		}
	}
	if u.Log == "" && goos == "darwin" {
		home, err := a.home()
		if err != nil {
			return refuse(stderr, name, "--log names no file and the home cannot be read: "+err.Error())
		}
		u.Log = filepath.Join(home, "Library", "Logs", "nova-sprint-seat-push.log")
	}
	// A Redis unit dials in its own process. Its file carries no user and no secret
	// (SeatUnit.env), so with no login recorded for that address it crash-loops on
	// NOAUTH. A twin is refused by the unit text, which names it. A server unit does
	// not dial Redis and is installed as it is.
	if u.Redis != "" && !isTwin(u.Redis) {
		if why := a.redisUnitLogin(u.Redis); why != "" {
			return refuse(stderr, name, why+"; nothing was written")
		}
	}
	text, err := u.Text()
	if err != nil {
		return refuse(stderr, name, err.Error()+"; nothing was written")
	}
	path := filepath.Join(*dir, sprint.SeatUnitFile(goos))
	if *dry {
		if c.json {
			b, _ := json.Marshal(map[string]any{"path": path, "unit": text, "args": u.Args(), "dry_run": true}) // ignored: strings always encode
			fmt.Fprintln(stdout, string(b))
			return 0
		}
		fmt.Fprintf(stdout, "SEAT INSTALL DRY-RUN unit=%s; nothing was written or loaded\n%s", oneline.Field(path), text)
		if push.Harness == "" {
			fmt.Fprintf(stdout, "NOTE the install wants the seat's push target: %s\n", sprint.PushSetup(c.actor, push, false))
		}
		if err := a.installSeat(cs, u.Server, true, stdout); err != nil {
			return refuse(stderr, name, err.Error())
		}
		return 0
	}
	if code := a.recordPushTarget(u.Server, *c, push, stdout, stderr); code != 0 {
		return code
	}
	r, err := a.seatInstaller(goos, *dir).Install(u)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: %s\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	var seat strings.Builder
	if err := a.installSeat(cs, u.Server, false, &seat); err != nil {
		fmt.Fprintf(stderr, "%s %s FAILED: the unit is installed, and %s\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	if c.json {
		b, _ := json.Marshal(map[string]any{"path": r.Path, "written": r.Changed, "loaded": true, "args": u.Args(), "adapter": push.AdapterName(), "seat": strings.Split(strings.TrimSpace(seat.String()), "\n")}) // ignored: strings and bools always encode
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprintf(stdout, "SEAT INSTALL OK unit=%s written=%t loaded=true\n", oneline.Field(r.Path), r.Changed)
	fmt.Fprintf(stdout, "  runs: %s\n", strings.Join(u.Args(), " "))
	if push.Adapter == sprint.AdapterFolder {
		fmt.Fprintf(stdout, "  push: %s into %s adapter=folder: each judgment is written there as a file\n", oneline.Field(push.Harness), oneline.Field(push.Target))
	} else {
		fmt.Fprintf(stdout, "  push: %s into %s\n", oneline.Field(push.Harness), oneline.Field(push.Target))
	}
	if bus != "" {
		fmt.Fprintf(stdout, "  hook: the push loop holds the seat's hook (bus=%s): its HOOK CHALLENGE <c> is delivered into %s, and every coordinator command is refused until it is answered: nova-sprint hook --prove <c> --actor %s; to hook in again after unhook or a missed challenge: %s\n", oneline.Field(bus), oneline.Field(push.Target), oneline.Field(push.Name), hookMonitor(push.Name))
	} else {
		fmt.Fprintf(stdout, "  monitor: %s\n", hookMonitor(push.Name))
		fmt.Fprintf(stdout, "  hook: every coordinator command is refused until the Monitor's HOOK CHALLENGE <c> is answered: nova-sprint hook --prove <c> --actor %s (no bus store named: the push loop holds no hook; install with --bus <host:port> or %s set)\n", oneline.Field(push.Name), busRedisEnv)
	}
	if push.Adapter != sprint.AdapterFolder {
		fmt.Fprint(stdout, seat.String())
	}
	return 0
}

// redisUnitLogin is why a Redis seat unit is not installed, "" when a recorded
// login matches addr. A shell user with no matching login is unitLogin's sentence.
// No shell user is seat login --check's sentence. Nothing here is copied into the unit.
func (a *app) redisUnitLogin(addr string) string {
	if why := a.unitLogin(addr); why != "" {
		return why
	}
	l, ok, err := a.recordedLogin()
	if err != nil {
		return err.Error()
	}
	if ok && l.Redis == addr {
		return ""
	}
	file := a.loginFile
	if file == nil {
		file = a.defaultLoginFile
	}
	path, err := file()
	if err != nil {
		return err.Error()
	}
	return noSeatLogin(path)
}

// recordPushTarget writes the push record seat install was given, on the sprint's
// server when there is one (--server, else NOVA_SPRINT_SERVER), else on the
// store: the push loop reads it to reach the session.
func (a *app) recordPushTarget(srv string, c common, push sprint.PushRecord, stdout, stderr io.Writer) int {
	const name = "seat install"
	words := []string{"push", "--actor", push.Name, "--harness", push.Harness, "--target", push.Target} // the server derives the adapter again
	if push.Session != "" {
		words = append(words, "--session", push.Session)
	}
	if srv != "" {
		res, err := a.ask(context.Background(), srv, []string{"seat"}, words)
		if err != nil {
			return a.unanswered(name, srv, err, stderr)
		}
		if res.Code == 2 {
			return refuse(stderr, name, "the server refused the push record: "+strings.TrimSpace(res.Stderr))
		}
		return 0
	}
	st, err := a.store(c)
	if err != nil {
		return refuse(stderr, name, err.Error()+"; nothing was written")
	}
	if err := writePush(context.Background(), st, push); err != nil {
		return a.readFailed(name, err, stderr)
	}
	return 0
}

// cmdSeatUninstall is seat uninstall (docs/SPEC-SPRINT.md, "Handing over the
// seat"): the shared uninstall body, the push loop's unit (friendsync_install.go
// cmdFriendSyncUninstall is the other caller).
func (a *app) cmdSeatUninstall(args []string, stdout, stderr io.Writer) int {
	return a.uninstallUnit(args, stdout, stderr, "seat uninstall", "SEAT UNINSTALL",
		sprint.SeatUnitFile,
		func(in sprint.SeatInstaller, goos string) (sprint.SeatResult, error) { return in.Uninstall(goos) })
}

// loadSeatUnit loads (op load: unloaded first, so a changed unit is read again) or
// unloads the unit at path on this machine, the push loop's or the friend sync loop's
// (friendsync_install.go), named by its file: launchctl in the user's gui domain on
// macOS, its label the file's name without .plist, and systemctl --user on Linux, its
// service the file's name. Under NOVA_TEST_NO_HOST it refuses: a test gives its own
// loader.
func loadSeatUnit(goos, op, path string) error { return sprint.LoadUnit(goos, op, path) }

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// seat install and seat uninstall (docs/SPEC-SPRINT.md, "Handing over the seat"; the
// owner, 2026-10-04: what is manual needs a verb). seat install records the seat on this
// machine: the sprint's server it uses and the nova-config seat profile (installSeat,
// seat.go). The seat's push loop it once installed as a service is retired (v1.3.6): the
// server sends the coordinator's judgments to its name on the bus, and the coordinator
// hooks in with nova-hook. seat uninstall unloads and removes that loop's unit where an
// older build left it. Neither verb is the server's (notServed): a seat is recorded, and a
// unit removed, where the verb is typed.

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
	dry := fs.Bool("dry-run", false, "print the seat record (and the nova-config seat profile) it would write, and write nothing")
	server := fs.String("server", "", "the sprint's server, `host:port` (default NOVA_SPRINT_SERVER): recorded in the seat beside the store login, where the coordinator's verbs and seat check read it when NOVA_SPRINT_SERVER is not set")
	cs := addConfigSeatFlags(fs)
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	_, named, err := cs.profile()
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	srv := strings.TrimSpace(*server)
	if srv == "" {
		srv = a.server(fs)
	}
	if srv == "" && !named {
		return refuse(stderr, name, "seat install records the seat's server (--server <host:port>, or NOVA_SPRINT_SERVER) and the nova-config seat profile (--config-seat <name> --config-dsn <dsn> --config-password-env <NAME>), and was given neither; nothing was written")
	}
	var seat strings.Builder
	if err := a.installSeat(cs, srv, *dry, &seat); err != nil {
		return refuse(stderr, name, err.Error())
	}
	if c.json {
		b, _ := json.Marshal(map[string]any{"server": srv, "dry_run": *dry, "seat": strings.Split(strings.TrimSpace(seat.String()), "\n")}) // ignored: strings and bools always encode
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprint(stdout, seat.String())
	return 0
}

// cmdSeatUninstall is seat uninstall (docs/SPEC-SPRINT.md, "Handing over the
// seat"): the shared uninstall body, the retired push loop's unit where an older build
// installed it (friendsync_install.go cmdFriendSyncUninstall is the other caller).
func (a *app) cmdSeatUninstall(args []string, stdout, stderr io.Writer) int {
	return a.uninstallUnit(args, stdout, stderr, "seat uninstall", "SEAT UNINSTALL",
		sprint.SeatUnitFile,
		func(in sprint.SeatInstaller, goos string) (sprint.SeatResult, error) { return in.Uninstall(goos) })
}

// loadSeatUnit loads (op load: unloaded first, so a changed unit is read again) or
// unloads the unit at path on this machine, the friend sync loop's, any unit kind's, or
// the retired push loop's
// (friendsync_install.go), named by its file: launchctl in the user's gui domain on
// macOS, its label the file's name without .plist, and systemctl --user on Linux, its
// service the file's name. Under NOVA_TEST_NO_HOST it refuses: a test gives its own
// loader.
func loadSeatUnit(goos, op, path string) error { return sprint.LoadUnit(goos, op, path) }

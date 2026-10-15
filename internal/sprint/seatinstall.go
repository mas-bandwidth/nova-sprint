package sprint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mas-bandwidth/nova-sprint/pkg/units"
)

// seat uninstall (docs/SPEC-SPRINT.md, "Handing over the seat"): the seat's push loop,
// inbox --wait with the seat's push, ran as a service of the machine the seat worked on,
// a launchd agent on macOS and a systemd user unit on Linux. The loop is retired (v1.3.6:
// the sprint's server sends the coordinator's judgments to its name on the bus, and the
// coordinator hooks in with nova-hook), and nothing installs it now; uninstall unloads and
// removes the unit where an older build installed it. The unit's directory and its loader
// are the caller's, so a test removes from a directory of its own and unloads nothing.
// SeatInstaller is the installer every unit of a running sprint shares (units.go,
// seat_unit_friendsync.go).

// SeatLabel is the retired push loop's launchd label; its plist is SeatLabel.plist.
const SeatLabel = units.SeatLabel

// SeatService is the retired push loop's systemd user unit.
const SeatService = units.SeatService

// SeatResult is what install or uninstall did: the unit's path, and whether the file
// there was written or removed.
type SeatResult struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
}

// SeatInstaller writes units into Dir; Load loads (or loads again) the unit at a path
// and Unload unloads it.
type SeatInstaller struct {
	Dir          string
	Load, Unload func(path string) error
}

// SeatUnitFile is the retired push loop's unit file name on goos, "" where there is no
// service manager it was installed under.
func SeatUnitFile(goos string) string {
	switch goos {
	case "darwin":
		return SeatLabel + ".plist"
	case "linux":
		return SeatService
	}
	return ""
}

// Uninstall unloads the unit and removes its file; with no file there it does
// nothing, and says so by Changed false.
func (in SeatInstaller) Uninstall(goos string) (SeatResult, error) {
	name := SeatUnitFile(goos)
	if name == "" {
		return SeatResult{}, errors.New("seat uninstall removes a launchd agent (macOS) or a systemd user unit (Linux), and " + orDash(goos) + " has neither")
	}
	r := SeatResult{Path: filepath.Join(in.Dir, name)}
	if _, err := os.Stat(r.Path); errors.Is(err, fs.ErrNotExist) {
		return r, nil
	} else if err != nil {
		return r, err
	}
	if err := in.Unload(r.Path); err != nil {
		return r, fmt.Errorf("the unit at %s did not unload, and is kept: %w", r.Path, err)
	}
	if err := os.Remove(r.Path); err != nil {
		return r, err
	}
	r.Changed = true
	return r, nil
}

package sprint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/buildinfo"
)

// DefaultRollbackWindow is how long server switch watches for a failed land before
// the switch is considered permanent (docs/SPEC-SPRINT.md section 14; item 14).
const DefaultRollbackWindow = 15 * time.Minute

// ServerSwitchOptions configures ServerSwitch.
type ServerSwitchOptions struct {
	Binary   string        // path to candidate binary
	Target   string        // path to target binary (default: os.Executable() or NOVA_SPRINT_SERVER_BIN)
	Rollback bool          // keep previous binary and roll back on land failure in window
	Window   time.Duration // rollback window duration (default DefaultRollbackWindow)
	Now      func() time.Time
	Stdout   io.Writer
	Stderr   io.Writer
}

// SwitchState is recorded at <target>.switch.json when rollback protection is enabled.
type SwitchState struct {
	Target      string    `json:"target"`
	Previous    string    `json:"previous"`
	SwitchedAt  time.Time `json:"switched_at"`
	WindowNanos int64     `json:"window_nanos"`
	Rollback    bool      `json:"rollback"`
}

// copyBinary copies a binary file atomically and ensures executable permission.
func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() // ignored: a read-only source; the copy's write side is checked

	fi, err := in.Stat()
	if err != nil {
		return err
	}

	mode := fi.Mode() | 0o111 // ensure executable

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// ignored: best-effort cleanup of temporary file
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		// ignored: best-effort close on copy error
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		// ignored: best-effort close on chmod error
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, dst)
}

// ServerSwitch replaces the server binary with candidate binary, keeping previous binary
// for rollback (docs/SPEC-SPRINT.md section 14; item 14).
func ServerSwitch(ctx context.Context, opts ServerSwitchOptions) error {
	target := opts.Target
	if target == "" {
		target = os.Getenv("NOVA_SPRINT_SERVER_BIN")
		if target == "" {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("server switch: cannot determine target binary: %w", err)
			}
			target = exe
		}
	}

	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	// Case 1: Rollback requested without new binary
	if opts.Binary == "" && opts.Rollback {
		return ServerRollback(ctx, target)
	}

	if opts.Binary == "" {
		return errors.New("server switch: candidate binary path required")
	}

	// Verify candidate binary exists and is readable
	if _, err := os.Stat(opts.Binary); err != nil {
		return fmt.Errorf("server switch: candidate binary not accessible: %w", err)
	}

	// Check that the binary's build commit is an ancestor of origin/main.
	// Read the build commit from the binary's buildinfo Source.
	versionLine, err := readVersionLine(opts.Binary)
	if err != nil {
		return fmt.Errorf("server switch: cannot read version from %s: %w", opts.Binary, err)
	}
	fields, ok := buildinfo.Parse(versionLine)
	if !ok {
		return fmt.Errorf("server switch: cannot parse version line from %s", opts.Binary)
	}
	src, ok := fields.FindSource()
	if !ok {
		return fmt.Errorf("server switch: cannot read build source from %s; build from origin/main and switch again", opts.Binary)
	}
	// Check if src.Revision is an ancestor of origin/main.
	ancestor, err := isAncestorOfMain(src.Revision)
	if err != nil {
		return fmt.Errorf("server switch: cannot verify build commit %s of %s: %w", src.Revision, opts.Binary, err)
	}
	if !ancestor {
		return fmt.Errorf("server switch: build commit %s of %s is not an ancestor of origin/main; build from origin/main at its tip and switch again", src.Revision, opts.Binary)
	}

	prev := target + ".prev"

	// Keep existing target binary as previous binary
	if _, err := os.Stat(target); err == nil {
		if err := copyBinary(target, prev); err != nil {
			return fmt.Errorf("server switch: failed to backup current binary to %s: %w", prev, err)
		}
	}

	// Switch target to candidate binary
	if err := copyBinary(opts.Binary, target); err != nil {
		return fmt.Errorf("server switch: failed to copy candidate binary to %s: %w", target, err)
	}

	// Record switch state if rollback is enabled
	stateFile := target + ".switch.json"
	if opts.Rollback {
		window := opts.Window
		if window <= 0 {
			window = DefaultRollbackWindow
		}
		state := SwitchState{
			Target:      target,
			Previous:    prev,
			SwitchedAt:  nowFn(),
			WindowNanos: window.Nanoseconds(),
			Rollback:    true,
		}
		data, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("server switch: failed to marshal switch state: %w", err)
		}
		if err := os.WriteFile(stateFile, data, 0o644); err != nil {
			return fmt.Errorf("server switch: failed to write switch state to %s: %w", stateFile, err)
		}
	} else {
		// ignored: clean up any stale switch state when rollback is disabled
		_ = os.Remove(stateFile)
	}

	return nil
}

// ServerRollback restores the target binary from its previous backup (docs/SPEC-SPRINT.md section 14).
func ServerRollback(ctx context.Context, target string) error {
	prev := target + ".prev"
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("server rollback: no previous binary found at %s: %w", prev, err)
	}

	if err := copyBinary(prev, target); err != nil {
		return fmt.Errorf("server rollback: failed to restore binary from %s: %w", prev, err)
	}

	stateFile := target + ".switch.json"
	// ignored: clean up state file after rollback completes
	_ = os.Remove(stateFile)
	return nil
}

// CheckRollbackOnLandFailure checks whether target binary was recently switched with rollback
// enabled, and if a land failed within the rollback window, rolls back to previous binary
// (docs/SPEC-SPRINT.md section 14; item 14).
func CheckRollbackOnLandFailure(target string, landErr error, now time.Time) (rolledBack bool, err error) {
	if landErr == nil {
		return false, nil
	}

	stateFile := target + ".switch.json"
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	var state SwitchState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, fmt.Errorf("check rollback: invalid switch state in %s: %w", stateFile, err)
	}

	if !state.Rollback {
		return false, nil
	}

	window := time.Duration(state.WindowNanos)
	if window <= 0 {
		window = DefaultRollbackWindow
	}

	deadline := state.SwitchedAt.Add(window)
	if now.After(deadline) {
		// Window has expired: remove state file, switch is permanent
		// ignored: clean up state file after rollback window expiry
		_ = os.Remove(stateFile)
		return false, nil
	}

	// Land failed within the rollback window! Roll back to previous binary.
	if _, err := os.Stat(state.Previous); err != nil {
		return false, fmt.Errorf("check rollback: previous binary %s missing: %w", state.Previous, err)
	}

	if err := copyBinary(state.Previous, state.Target); err != nil {
		return false, fmt.Errorf("check rollback: failed to restore %s from %s: %w", state.Target, state.Previous, err)
	}

	// ignored: clean up state file after rollback on failure
	_ = os.Remove(stateFile)
	return true, nil
}

// readVersionLine runs the binary with "version" and returns its output.
func readVersionLine(binary string) (string, error) {
	cmd := exec.Command(binary, "version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isAncestorOfMain checks if the given revision is an ancestor of origin/main.
func isAncestorOfMain(revision string) (bool, error) {
	// First, fetch origin/main to ensure we have the latest
	cmd := exec.Command("git", "fetch", "origin", "main")
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git fetch origin main: %w", err)
	}
	// Check if revision is an ancestor of origin/main
	cmd = exec.Command("git", "merge-base", "--is-ancestor", revision, "origin/main")
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		// revision is not an ancestor
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
}

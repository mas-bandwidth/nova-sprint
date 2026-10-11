package sprint

import (
	"strings"
)

// The seat's push target (docs/SPEC-SPRINT.md, "The push target"): the harness the AI
// holding the seat runs in and where its adapter delivers, which the push loop (inbox
// --wait --push seat) writes each judgment into. It proves nothing: the proof that the
// seat reads what is pushed is the hook (hook.go; the owner, 2026-10-10: the push proof's
// PROOF files and seat pong, answered by hand while nothing read the pushes, are gone).

// AdapterFolder is the adapter of a harness with no deliver command (Claude Code's, and
// every harness whose nova-friend adapter is passive): the push loop writes each pushed
// message as one file into the target directory.
const AdapterFolder = "folder"

// PushRecord is one name's push target: the harness, the adapter the push loop delivers
// through (AdapterFolder, or "" for the harness's own deliver command) and its deliver
// target (a directory, and a session where the harness names one).
type PushRecord struct {
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Adapter string `json:"adapter,omitempty"`
	Target  string `json:"target"`
	Session string `json:"session,omitempty"`
}

// AdapterName is the adapter the push loop delivers through: the record's adapter, else
// the harness's own ("-" when there is neither).
func (r PushRecord) AdapterName() string {
	switch {
	case r.Adapter != "":
		return r.Adapter
	case r.Harness != "":
		return r.Harness
	}
	return "-"
}

// PushSetup is the command that records name's push target and installs the push loop:
// harness is the record's, else a placeholder.
func PushSetup(name string, rec PushRecord, ok bool) string {
	h, target := "<harness>", "<session dir>"
	if ok && rec.Harness != "" {
		h, target = rec.Harness, rec.Target
	}
	return "nova-sprint seat install --actor " + orDash(name) + " --harness " + h + " --target " + target
}

// NotPushTarget is why a push record may not be written for name, "" is may: a name, a
// harness, and a target directory.
func NotPushTarget(rec PushRecord) string {
	switch {
	case !ValidID(rec.Name):
		return "a push record wants --actor <name>, the seat it pushes to: letters, digits, _ and -: " + orDash(rec.Name)
	case strings.TrimSpace(rec.Harness) == "":
		return "the push loop delivers through the harness's adapter: --harness <name> is required"
	case strings.TrimSpace(rec.Target) == "":
		return "--target <dir> is required: the session's directory, where the harness's adapter delivers"
	}
	return ""
}

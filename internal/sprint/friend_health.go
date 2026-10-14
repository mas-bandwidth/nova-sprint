package sprint

import "github.com/mas-bandwidth/nova-tools/hook"

// FirstSeatGeneration is the first coordinator seat generation.
const FirstSeatGeneration uint64 = 1

// FriendHookStatus combines the authoritative hook read with sprint's hold.
// A missing, unproven or unavailable hook is down even while sprint holds her.
func FriendHookStatus(h hook.Status, held bool) string {
	if h.State != hook.StateProven || h.Down != "" {
		return Down
	}
	if held {
		return Held
	}
	return Up
}

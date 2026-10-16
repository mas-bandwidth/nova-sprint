package sprint

import "github.com/mas-bandwidth/nova-tools/hook"

// FirstSeatGeneration is the first coordinator seat generation.
const FirstSeatGeneration uint64 = 1

// FriendStates is every status the sprint gives a friend, and there are three (the owner,
// 2026-10-14, v1.3.6: "there is only up, down, held from the sprint POV"): Up and Down are
// the nova-hook record's, the one truth of whether she is there, and Held is the
// coordinator's explicit hold (nova-sprint friend down or hold, released by friend up or
// unhold). A down friend is not dealt and an up friend is. A judgment may report a fact
// about her work (she holds cards and finished none in a while), and it never changes her
// state nor keeps a card from her. FriendHookStatus is the one function that writes a
// friend's status; TestTheSprintHasExactlyThreeFriendStates holds every value the friends
// rows can carry to this list.
var FriendStates = []string{Up, Down, Held}

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

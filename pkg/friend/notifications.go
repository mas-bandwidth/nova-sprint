package friend

import (
	"slices"
	"strings"
)

// Notifications use the one receiver and the bus's pending entries, never a second
// consumer or a card scheduler (docs/SPEC-FRIEND.md, notifications; tla/FriendNotifications.tla).
const (
	NotificationReadyText   = "NOVA-FRIEND READY QUEUE\nSprint deliveries are available. Read the canonical ready queue and continue the coordinator's existing dispatcher; this notification claims or executes no card.\n"
	NotificationBatchPrefix = "NOVA-FRIEND NOTIFICATION "
)

// NotificationKey is the durable enqueue family: one global ready wake, or a batch's
// immutable message-id fingerprint. Only exact notifier prefixes participate.
func NotificationKey(text string) string {
	if text == NotificationReadyText || strings.HasPrefix(text, NotificationReadyText) {
		return "ready queue"
	}
	if first, _, ok := strings.Cut(text, "\n"); ok && strings.HasPrefix(first, NotificationBatchPrefix) {
		return first
	}
	return ""
}

// NotificationCategory bounds unread useful input to one batch of each category;
// report backpressure cannot consume the urgent category's capacity.
func NotificationCategory(text string) string {
	if text == NotificationReadyText || strings.HasPrefix(text, NotificationReadyText) {
		return "ready"
	}
	first, _, ok := strings.Cut(text, "\n")
	if !ok || !strings.HasPrefix(first, NotificationBatchPrefix) {
		return ""
	}
	parts := strings.Fields(first)
	if len(parts) == 4 && slices.Contains([]string{"urgent", "report", "notice"}, parts[2]) {
		return parts[2]
	}
	return "urgent" // the earlier immutable marker format remains recognizable on recovery
}

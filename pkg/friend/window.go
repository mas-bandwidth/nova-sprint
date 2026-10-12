package friend

import (
	"strings"
)

// AccessibilityCheckScript asks whether this process is trusted. It calls
// AXIsProcessTrusted and never AXIsProcessTrustedWithOptions, so it cannot prompt.
const AccessibilityCheckScript = "use framework \"ApplicationServices\"\nreturn (current application's AXIsProcessTrusted() as boolean) as text\n"

// accessibilityAsks reports a script that would prompt for the permission.
func accessibilityAsks(script string) bool {
	if strings.Contains(script, "AXIsProcessTrustedWithOptions") {
		return true
	}
	return strings.Contains(strings.ToLower(script), "with prompt")
}

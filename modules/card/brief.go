package card

import (
	"strings"
)

// RouteField is the retired v2 metadata line (`NEEDS-ROUTE: <name>`), kept
// so brief and card admission can recognize and refuse it with a remedy
// naming RESOURCES.
const RouteField = "NEEDS-ROUTE"

// BriefField is every value of metadata field key in brief: the lines
// after the tier line up to the first blank line, `KEY: value`, the key
// exactly as given (the brief's keys are upper case). A brief the add lint
// refused never reaches the store, so this reads; it never judges.
func BriefField(brief, key string) []string {
	var out []string
	lines := strings.Split(strings.ReplaceAll(brief, "\r\n", "\n"), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		if strings.TrimSpace(line) == "" {
			break
		}
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.TrimSpace(k) == key {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

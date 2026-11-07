package card

import (
	"slices"
	"strings"
)

// RouteField is the brief's metadata line a card that needs a device or a
// route declares it with (CARD-CONTRACT "Routes"): `NEEDS-ROUTE: <name>`,
// one name per line, the line repeated for more. It is read here only, so
// add's admission, the deal (steps: only rows that serve every route) and a
// fault naming a missing route all read one parse.
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

// NeedsRoutes is the routes brief declares (RouteField), in brief order,
// each once; none for a card that needs none.
func NeedsRoutes(brief string) []string {
	var out []string
	for _, r := range BriefField(brief, RouteField) {
		if r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

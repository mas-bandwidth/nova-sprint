package sprint_test

import "github.com/mas-bandwidth/nova-sprint/internal/sprint"

// nova-sprint's own rows of THE CLASS RULE: NO SECRET REACHES AN ERROR
// (secrets_in_errors_test.go, which the seed takes from nova-tools). They are for
// functions only this repository has, so nova-tools' table cannot carry them; this
// file is a RECIPE keep, and the RECIPE's literal-file line prefixes the allowlist
// with sprintNovaToolsLeak, so every re-seed keeps both.

func init() {
	sprintSecretOpeners["internal/sprint.ParseNovaToolsVersion"] = sprint.ParseNovaToolsVersion
}

// sprintNovaToolsLeak is the allowlist row for the function above, in the allowlist's
// own form: `<key> <reason>`, one per line.
const sprintNovaToolsLeak = `
internal/sprint.ParseNovaToolsVersion echoes the rejected NOVA-TOOLS-VERSION line with %q (internal/sprint/toolsversion.go; nova-sprint's own file, a release tag and a commit hash)
`

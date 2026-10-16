// Package docs compiles this repository's documentation data into the binary,
// so a built binary can answer which fixes it carries with no file beside it
// (docs/SPEC-SPRINT.md section 11, the version verb), and print the
// coordinator's playbook (nova-sprint help playbook).
package docs

import _ "embed"

// FixesSexp is docs/fixes.sexp compiled in, the fixes ledger
// `nova-sprint version --features` and `--has` read. It is decoded with the
// same parser `make roadmap` uses (internal/roadmapdoc), so the binary and the
// page agree; a test holds these bytes equal to docs/fixes.sexp, so a stale
// embed cannot ship.
//
//go:embed fixes.sexp
var FixesSexp []byte

// CoordinatorPlaybook is docs/COORDINATOR-PLAYBOOK.md compiled in, the page
// `nova-sprint help playbook` prints: the coordinator's workflows, each the
// verbs in order. A test (cmd/nova-sprint/playbook_test.go) holds these bytes
// equal to the file, and every verb, flag and usage line the page names to the
// help, so the page cannot go stale.
//
//go:embed COORDINATOR-PLAYBOOK.md
var CoordinatorPlaybook string

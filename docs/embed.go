// Package docs compiles this repository's documentation data into the binary,
// so a built binary can answer which fixes it carries with no file beside it
// (docs/SPEC-SPRINT.md section 11, the version verb).
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

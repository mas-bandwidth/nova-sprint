package decide

// The import (SPEC-NOVA-DECIDE, the backfill-2026-10-04.w1 subsection of section 4) turns
// finished decisions into labelled records: a heavy read's VERDICT.md, a judgment file
// joined with the answer the sprint's log holds for it, and a HOLD report. Each item is one
// decision whose outcome is its label, with an id from its source path and content, so the
// same item imported again is the record it already is. It asks no backend: the answers are
// empty and the backend is "import", so a calibration of a model's answers never reads one.

// The kinds an import records, and the decision name each is recorded under.
const (
	ImportVerdict  = "verdict"
	ImportJudgment = "judgment"
	ImportReport   = "report"
)

// ImportKinds is every kind, in the order a result prints them.
var ImportKinds = []string{ImportVerdict, ImportJudgment, ImportReport}

// ImportSources names what to import; an empty field is a source not read. Verdicts and
// Reports are globs of files; Judgments is a directory of judgment files, which needs Log,
// a nova-sprint log --json export, to hold the answers.
type ImportSources struct {
	Verdicts, Judgments, Log, Reports string
}

// ImportCount is how many items of a kind were recorded now and how many were there already.
type ImportCount struct{ New, Existing int }

// ImportResult is the count per kind, and the judgments the log has no answer for (they are
// not recorded: an unlabelled item is not a label).
type ImportResult struct {
	Kinds      map[string]ImportCount
	Unanswered int
}

type importItem struct {
	kind, path, label, state string
}

package swarm

// THE RESULT HAS ONE SHAPE, so the fold is mechanical and a person reads counts.
//
// This parser is the whole of rule 15, and its hardest property is what it does NOT do: a
// report that does not parse yields NO findings, ever. A parser that salvaged the lines it
// liked would be a parser with an opinion, and the one path from a malformed report to a
// person is `result --id <job>`, which prints the file verbatim to the person who asked for
// it by id.
//
// The classes, and rule 8's law that completion is evidence SEPARATE from the count:
//
//	ok         a head with findings: <n>, n > 0
//	clean      a head with findings: 0 -- a bounded review that finished and found nothing,
//	           or a probe-row that finished `not done` with its reason. NEVER a failure: the
//	           tool must not pay a worker for finding something.
//	plan-only  a RESULT.md with NO head, whatever else it holds
//	malformed  a head without findings: <n>, a fourth state word, a table that does not
//	           parse -- quarantined, never folded, counted in no other column

// Finding is one appended finding line.
type Finding struct {
	Line     int
	Text     string
	Dup      bool
	Rule     string // the rule quoted verbatim, between backticks
	File     string // normalized: relative, no leading ./, backslashes read as /
	FileLine string
	Job      string
}

// Item is one row of the Per item table.
type Item struct {
	Text     string
	State    string
	Evidence string
}

// Gate is one row of the Gates table.
type Gate struct {
	Name    string
	Result  string
	Seconds string
}

// Report is one parsed RESULT.md.
type Report struct {
	Class         string
	MalformedLine int
	Heading       string
	HasHead       bool
	Findings      int // the head's own number, the worker's count
	NotesRead     int
	HasNotesRead  bool
	Repo          string
	Rev           string
	Paragraph     string
	Items         []Item
	Gates         []Gate
	LeftOwed      []string
	OneLine       string
	FindingLines  []Finding
	Hash          string
	Bytes         int
}

// ParseReport reads one RESULT.md into a Report. It never returns a finding from a report
// it classifies malformed.

// RULE 8'S FIRST LINE (SPEC-SWARM.md:117): the evidence of completion is the
// report's `## Head`, "whose first line is `findings: <n>`". FIRST. A head
// that opened with `notes read:`, `repo:` or `rev:` was read here as a
// complete report, so the one shape a coordinator classifies on was not the
// shape the parser required, and completion evidence could sit anywhere in
// the head. A shape the parser bends is not a shape.
//
// A BLANK LINE IS A LINE. Skipping whitespace to find the count gave "first"
// a second reading, under which `## Head` / `` / `findings: 0` was a complete
// report (read 4, F7). The first line of the head is the first line.

// The head's paragraph: what was asked, what the state is now, and the
// single most important fact. The FIRST line of the head has to be
// findings: <n>, which is the completion evidence.

// RULE 2'S "OR THE NEXT" (SPEC-SWARM.md:82-84): "A finding line carries
// the rule it rests on, quoted word for word, with `file:line`, on the
// same line or the next." The parser read ONE line per finding, so a
// finding that wrapped its quote onto the following line had no rule and
// no file: it was counted `unquoted`, it got no de-duplication key
// (rule 15), and it was folded into nobody's page. The parser discarded
// the exact evidence rule 2 exists to demand.
//
// The NEXT line, and no further; and only for a finding that did not
// already carry both, so a complete claim is never re-read from the line
// below it. This is the whole of the widening: the parser gains no
// opinion, it reads the second line rule 2 always allowed.

// D3 (the real run, 2026-09-11): ONE report has ONE finding count. The head's
// `findings: <n>` is the worker's own answer and the thing rule 8 classifies on, and
// the bullets under `## Findings` are what is FOLDED. When the head says 0, the
// section holds no findings whatever it says in words: a complete review that wrote
// `- none` ended `result=clean findings=1` and put "none" in a coordinator's page.

// A head with no findings: line at all. The head is the completion evidence, and
// evidence that does not say what it evidences is not evidence.

// malformed drops every finding the parser had collected. This is the quarantine, and it is
// one line of code because it is one rule: a malformed report yields nothing at all.

// tableRow reads one row of the template's tables into its cells. A `|` INSIDE A BACKTICK
// SPAN IS TEXT, not a cell boundary: rule 2 (SPEC-SWARM.md:82-84) requires every claim to
// quote its rule VERBATIM, and this tool's own rules are grammar lines full of `|`, so a
// reader obeying rule 2 could not write a row about one. The row was split on every pipe,
// the state column landed on prose, and a complete report with five findings was
// `RUN MALFORMED line=66` and lost to failed/ (dogfood D12, 2026-09-11).
//
// This is a cell boundary read correctly, not a report salvaged: the parser gains no
// opinion, and a row whose state word is still not one of rule 15's three is malformed
// exactly as before. An unterminated backtick quotes to the end of the line, which leaves
// too few cells and is malformed -- the quarantine is never opened by a missing mark.

// parseFinding reads one appended finding line. The shape is the prompt's:
//
//   - <what was found> `<the rule, quoted verbatim>` <file>:<line>
//
// and a line beginning `dup:` is a finding already on the owed list, which is not a new
// finding (rule 1: 25 of 67 findings in batch 1 were duplicates of the owed list).

// carry folds rule 2's "or the next" line into the finding above it: the quote and the
// `file:line` that did not fit on the bullet. It fills only what is EMPTY, and it keeps the
// continuation in the finding's text so a coordinator's page shows the evidence beside the
// claim it rests on.

// lastBacktickSpan returns the text of the last `...` span, which is where the rule quoted
// verbatim sits. The LAST rather than the first because a finding's own prose may quote an
// identifier before it quotes the rule it rests on.

// fileAndLine finds the <path>:<line> a finding rests on and NORMALIZES the path -- a
// leading ./ stripped, \ read as / -- so that ./internal/x.go:10 and internal\x.go:10 under
// one rev are one finding rather than two (rule 15).

// NormalizePath is rule 15's normalization, in one place.

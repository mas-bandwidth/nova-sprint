package swarm

// A CARD'S MINUTES ARE MEASURED PER PHASE: the aim is a faster average wall clock
// time per card and each card operating more efficiently in tokens and in time from
// start to finish. The harness log carried no timestamps, so nothing could say
// which phase -- clone, deps, read, edit, test, retry, result -- spent a card's wall.
//
// The harness REPORTS its own events on the child's output, one line each, and this reader
// timestamps them as they arrive: the harness says what happened, the run says when. One row
// is written per model turn and per tool call into `<job>/timeline.tsv`, beside the card's
// `usage.tsv`, so `nova-swarm profile --jobs <glob>` can fold a fleet of cards into seconds
// per phase without re-reading a transcript.
//
// The event grammar is the harness adapter's, and it is deliberately two lines per event so
// both ends of a span are observed rather than guessed:
//
//	NOVA-TIMELINE TURN BEGIN
//	NOVA-TIMELINE TURN END in=<n|-> out=<n|->
//	NOVA-TIMELINE TOOL BEGIN name=<tool> cmd=<command to the end of the line>
//	NOVA-TIMELINE TOOL END name=<tool> rc=<n>
//
// A field the harness does not report is the empty string in the row, never a zero.

import (
	"sync"
	"time"
)

// TimelineRow is one observed event: a model turn or a tool call.
type TimelineRow struct {
	Start        time.Time
	End          time.Time
	Tool         string // `model` for a turn, else the tool name and command
	InputTokens  string // empty when the harness reported none
	OutputTokens string
}

// Timeline is an io.Writer over the child's output: it timestamps the harness's own
// NOVA-TIMELINE report lines as they arrive and keeps one row per completed span. It is
// safe for the child's concurrent stdout and stderr copies; a partial line is held until
// its newline arrives.
type Timeline struct {
	mu   sync.Mutex
	buf  []byte
	rows []TimelineRow
	open *timelineSpan
}

type timelineSpan struct {
	start    time.Time
	kind     string // "turn" | "tool"
	name     string
	cmd      string
	haveMeta bool
}

// NewTimeline returns an empty timeline recorder.

// Write accepts the child's output, one chunk at a time, and observes every complete line.

// Rows returns a copy of the spans observed so far, in report order.

// observe parses one line. A line that is not a NOVA-TIMELINE report is ignored; a BEGIN
// with a span already open closes that one at this line's instant (a harness reporting two
// spans with no end between them is read as one span ending where the next began).

// close finishes the open span at end and appends its row.

// cutWord splits the first whitespace-delimited word off s, and reports whether one was
// there.

// metaValue reads one `key=value` token whose value holds no spaces.

// cmdValue reads `cmd=` to the end of the line, because a command carries spaces.

// tokenOrEmpty keeps a token count the harness gave, and reads `-` (the harness's own word
// for "did not report") as an absence, never a zero.

// WriteTimeline writes one card's timeline.tsv atomically: the header line and one row per
// event, in order. A field is scrubbed of tabs and newlines so the row is always one row.

// ReadTimeline reads one card's timeline.tsv, mapping its columns by the header so a reader
// is not wedded to a column's position. An absent file is an empty timeline and no error,
// which is how a job that reported nothing phases as nothing rather than failing the fold.

// ProfileJobs reads every timeline a job glob names and prints one PROFILE line per job and
// one PROFILE SUMMARY line with the mean per phase. A glob match may be a job directory (its
// timeline.tsv is read) or the timeline file itself; a match with no readable timeline is
// skipped. The command exits 0 even when the glob matches nothing, printing a zero summary,
// because an empty fleet is a measurement and not a refusal.

// profileLine renders one job's PROFILE line in the fixed phase order.

// profileRows folds one job's rows: the wall is the span from the first start to the last
// end, and each tool call's seconds land in the phase its command names. A test run that
// follows a failing test run is retry, and only the first `go build` is a deps cost.

// phaseOfTool names the phase a tool call's command belongs to, or "" when no listed phase
// owns it. The checks run most-specific first: a RESULT.md write is the result even though
// it is also an edit, and a test is a test before it is a read.

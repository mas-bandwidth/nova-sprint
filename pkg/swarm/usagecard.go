package swarm

// SLICE 10: USAGE ROWS PER CARD (lesson 10).
//
// A native run writes one usage.tsv beside its RESULT.md -- one header line and one row --
// so a batch can fold every card into token and dollar totals without re-reading the
// harness. The token numbers come from the harness's own sqlite store, the messages table's
// assistant rows grouped by provider and model, read through the one program this package
// runs (sqlite3, read-only). The row keeps the same dash-versus-zero law as the pool's
// usage file: a field the provider did not report is the literal "-", never 0.

// CardUsageColumns are the fourteen columns of one card's usage.tsv, in this order. The
// order is the contract between the native run that writes it and the batch that sums it.
//
// `end` JOINED THEM WITH SPEC-SWARM RULE 13d (issue #1545). The word was being computed by
// every native launch and thrown away: `writeNativeUsage` has set `row["end"]` since issue
// #644's follow-up, and there was no column to put it in, so `end=wall` for a card the wall
// stopped -- and now `end=budget` for one a budget stopped -- reached no reader at all.
// Rule 13d requires it by name: "The launch a budget ended has `end=budget` in its row",
// and demanded test 13d reads it there.
//
// IT IS INSERTED WHERE RULE 12 PUTS IT, after `ended` and before `rc`, so that a person who
// knows the pool's sixteen-column file reads this one without relearning it. THAT IS SAFE
// FOR OLD READS, and it was checked before it was done: every reader of this file in this
// repo maps its columns BY HEADER NAME and says so in terms -- `cmd/nova-tokens`'s
// `readCardFile` ("mapping its columns by the header so the reader never depends on a fixed
// index"), the pulse package's `parseProgressUsage` and `status`'s own index. A file written
// before this change has thirteen columns and no `end`, and every one of those readers
// answers the empty string for it, which is what an absence is.
var CardUsageColumns = []string{
	"job", "attempt", "started", "ended", "end", "rc", "provider", "model",
	"tokens_in", "tokens_out", "cache_write", "cache_read", "reasoning", "usd",
}

// cardMessagesSQL is the one statement this reader runs against the harness's own store: the
// assistant rows of the `message` table, grouped by provider and model. The numbers live in
// the `data` JSON -- the provider, the model, the role, and the five token types plus cost --
// and the row's own `time_created` column is MILLISECONDS since the epoch, so the window is
// two ms bounds, widened five seconds each side. A column no message reported sums to NULL,
// which prints as the empty string and is read back as an absence -- never as a zero.

// the card's requests (one assistant message is one model call) and its largest
// prompt, what a long-context price is decided on (pkg/cardcost, Predict)

// cardStoreLocations are the store paths OpenCode may keep inside one data home, in the
// order this reader tries them: the run's own data directory first -- <dataHome>/opencode/
// opencode.db, the XDG_DATA_HOME location this tool exports -- then the HOME/.local/share
// location a real OpenCode honours when it reads HOME instead of XDG_DATA_HOME. The native
// run points both HOME and XDG_DATA_HOME at the same data directory, so both live under the
// path native.go passes here.

// ReadCardUsageAfter is the same read with a FLOOR under the window, and the floor is what
// keeps a retried card's rows DISJOINT (SPEC-SWARM rule 13d, issue #1545).
//
// THE DEFECT IT CLOSES, measured: the window is widened five seconds each side, because the
// launch's own clock and the store's need not agree to the millisecond. On a retried card
// the SECOND launch begins within those five seconds of the first one's end -- the launch
// grace's retry is seconds, and a test pins it shorter still -- so the second launch's
// window reached back over the first launch's rows and counted them again. Rule 13d's own
// worked example is exactly this: two launches finally reported at 40 and 70 "print
// `budget=110/100`, and their rows hold 40 and 70, never 40 and 110". Before this floor the
// rows held 40 and 110 and added to 150, which is what a downstream `cost` would have
// charged.
//
// `notBefore` is the EARLIER launch's end. The zero time is no floor at all, which is what
// a first launch has and what every caller outside the retry loop wants.

// A QUERY THAT FAILED IS NOT AN EMPTY TABLE. These two shared the `no-rows`
// token and they are not the same fact: this one is a database that is locked,
// corrupt, or did not answer inside the timeout -- a READER THAT STOPPED, and
// a caller that acts on the number is acting on a number nobody could see. The
// one below is a read that worked and found nothing. A caller that must tell
// them apart could not, and one that reported both as a fault cried wolf on
// every card killed before its first answer.

// The database exists, the query ran, and the window holds no message with
// tokens: the harness started and reported nothing yet. An ABSENCE.

// dashCardTokens is the row of a store that reported nothing: every numeric column a dash.

// queryCardMessages runs the one statement, read-only, under the same timeout every usage
// read carries (rule 13). The statement's window is the caller's two ms bounds.

// foldCardMessages sums the grouped assistant rows into the columns the usage row carries.
// A field some message reported is the sum of the messages that did; one no message reported
// stays a dash, never a zero. Beside the row's columns it keeps what a card's cost record
// reads (pkg/cardcost): the harness's cost as the decimal the store printed for its
// float sum of opencode's per-message float costs, uncut ("cost", where the row's usd is
// cut to four places): the harness's own computation, not an invoice; the requests and
// the largest prompt; none of the three reaches the usage file.

// AppendCardUsage appends one attempt's usage row to a card's usage.tsv, writing the header
// first when the file is new (issue #900). A native run that retried a launch writes one row
// per attempt -- attempt=1,2,3 for one card -- so the file holds the header and one row per
// launch, and a reader folds them. A field the provider did not report stays a dash, never a
// zero, and a tab or a newline in a value is scrubbed so the row is always one row.

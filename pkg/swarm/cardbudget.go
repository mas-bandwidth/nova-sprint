package swarm

// THE CARD BUDGET (CARD-8317): a runaway card is a prompt defect, not a model
// one, and the pool says so.
//
// A worker description optionally carries `max_turns` and `max_cache_read`.
// The supervisor samples usage every --usage-interval beside the token
// budget (rule 13); when a running task's cache_read exceeds max_cache_read,
// or its turn count exceeds max_turns, the supervisor stops the task on the
// existing stop path used for the deadline, the job moves to failed/ with
// end=budget in usage.tsv, and the task's report carries
// `PROMPT-DEFECT task=<id> reason=budget cache_read=<n> max=<m> turns=<t>`.

// HasCardBudget reports whether the description carries any card budget.

// CardCacheRead is the observed cache_read as a number, and whether it was one.

// CountCardTurnsIn counts a job's assistant turns against a NAMED log: the log's
// assistant lines, or the usage row count where the log has fewer, because the two routes keep the
// harness's words in two different files: rule 13d names this log and rule 13b counts the
// turns in it, so the log to count is `<job>/harness-output.log`, since
// `native` never writes `harness.log`.
//
// THE NAMES ARE NOT INTERCHANGEABLE. `harness.log` has two owners on the native route
// already -- a batch pins its runner's stdout to it, which is where the NATIVE OK line
// lands -- so counting "assistant" in it would count the machinery's own lines and not the
// model's turns. `harness-output.log` is the capture with one writer.

// countAssistantLines counts the harness log lines that carry an assistant
// turn, the way the usage source counts assistant rows of the message table.

// OverCardBudget reports whether the observed usage and turn count exceed the
// description's card budget. It returns the observed cache_read (0 for an
// absence, which never fires) and the budget that fired: max_cache_read where
// the cache fired, else max_turns.

// PromptDefectLine is the report line a budget stop writes into the task's
// report. One line, the one-line output grammar's fields, no payload.

// THE MEASURED STARTUP COST. A measured run showed a 2k cap smaller
// than the harness's own first context, so every card carrying it would have
// died at once, at load, before doing any work. A card budget is only a budget
// if it is above what the harness spends before the card's first turn; a
// known-answer probe writes the measurement to `<root>/startup-cost.tsv`
// (columns `harness_sha256`, `first_usage_cache_read`, `first_usage_turns`),
// and a description's budget below it is refused BY NAME at load, before any
// launch, and never applied.

// StartupCostFile is the measurement's name under a pool's or a native run's root.
const StartupCostFile = "startup-cost.tsv"

// StartupCost is the harness's measured first turn: what the harness spends
// before the card says anything. Present is false when no file has been
// written yet, which accepts every budget.
type StartupCost struct {
	HarnessSHA256       string
	FirstUsageCacheRead int
	FirstUsageTurns     int
	Present             bool
}

// ReadStartupCost reads `<root>/startup-cost.tsv`. An absent file is not an
// error and answers Present=false: no measurement accepts every budget. A
// header line naming the columns is skipped.

// RecordStartupCost writes the known-answer measurement at
// `<root>/startup-cost.tsv`, a header and one row.

// RecordStartupCostIfAbsent writes the measurement from the first finished
// task, and only when no measurement is there yet: the first run has nothing
// to compare against, so it accepts its budget and leaves the next run a fact.

// BudgetRefusal is the one line a card budget below the measured startup cost
// is refused with, or "" when the budget stands or no measurement exists. A
// max_cache_read below twice the measured first cache read cannot leave room
// for the card; a max_turns below the measured first turns plus two stops the
// card before its first real turn. The refusal names both numbers.
